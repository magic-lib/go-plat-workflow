package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/magic-lib/go-plat-utils/goroutines"
	"github.com/magic-lib/go-plat-utils/id-generator/id"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
	"log"
	"net/http"
	"time"

	"github.com/magic-lib/go-plat-utils/utils/httputil"

	mq "github.com/magic-lib/go-plat-mq/mq"
	"github.com/magic-lib/go-plat-utils/conn"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/magic-lib/go-plat-workflow/workflow/rulegox"
	"github.com/magic-lib/go-plat-workflow/workflow/rulegox/components/commnode"
	"github.com/rulego/rulego/api/types"
)

// MQ Topic 定义（分布式 worker 订阅这些 topic 执行对应任务）。
const (
	// TopicTestNode 测试单个节点的 topic，worker 端据此加载并执行单个节点。
	TopicTestNode = "workflow:test_node"
	// TopicExecuteRootChain 执行某个 rootChain 的 topic。
	TopicExecuteRootChain = "workflow:execute_root_chain"
)

// TestNodePayload 测试单个节点时投递到 MQ 的 payload。
// worker 端读取该结构，按 project+node_id 加载节点并以其参数执行。
type TestNodePayload struct {
	// NodeID 被测节点 ID
	NodeID string `json:"node_id"`
	Env    string `json:"env"`
	// NodeDef 节点定义
	NodeDef *NodeDef `json:"node_def"`
	// InputParams 测试时传入的参数（map 形式，key=参数名 value=参数值）
	InputParams map[string]interface{} `json:"input_params"`
	// UseCache 是否复用全局 rulego 引擎池中已存在的同名链（以 node_id 为 key）。
	// true：配置稳定、性能更好（正式/高频复用场景）；false（默认）：每次基于最新配置重建，修改即时生效（测试/开发场景）。
	UseCache bool `json:"use_cache,omitempty"`
}

// ExecuteRootChainPayload 执行某个 rootChain 时投递到 MQ 的 payload。
type ExecuteRootChainPayload struct {
	// Project 项目标识
	Project string `json:"project"`
	// ChainID 根链 ID
	ChainID string `json:"chain_id"`
	// Payload 根链执行输入（JSON 字符串）
	Payload string `json:"payload"`
	// EnvVars 环境变量（可选）
	EnvVars map[string]string `json:"env_vars,omitempty"`
	// UseRelease 是否使用已发布版本
	UseRelease bool `json:"use_release,omitempty"`
}

// MQExecutor 基于 asynq（Redis 后端）的分布式任务执行器。
// 调用方通过 Request 同步投递任务到 MQ，由分布式 worker 执行后实时返回结果。
type MQExecutor struct {
	// Namespace asynq 队列命名空间
	Namespace string
	// Timeout 任务等待超时
	Timeout time.Duration

	// logStore activity 执行日志仓储（直接落库）。为 nil 时执行日志降级跳过（不影响主流程返回）。
	logStore ActivityLogStore

	// envConfigStore 环境配置仓储，用于解析测试/执行所需的 Redis 配置。
	// 为 nil 时 getRedisConfig 返回明确错误（无法定位 redis）。
	envConfigStore EnvConfigStore
}

// buildConnect 根据 EnvConfigDef 的 Redis 配置构建 conn.Connect。
func buildConnect(redisCfg *RedisConfig) (*conn.Connect, error) {
	if redisCfg == nil || redisCfg.Addr == "" {
		return nil, fmt.Errorf("redis config (addr) is required for MQ execution")
	}
	host := redisCfg.Addr
	port := "6379"
	// Addr 形如 host:port 或 host
	if idx := indexByte(host, ':'); idx >= 0 {
		port = host[idx+1:]
		host = host[:idx]
	}
	return &conn.Connect{
		Driver:   "redis",
		Host:     host,
		Port:     port,
		Username: redisCfg.Username,
		Password: redisCfg.Password,
		Database: fmt.Sprintf("%d", redisCfg.DB),
	}, nil
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// newMQClient 根据 Redis 配置创建 asynq MQ 客户端。
func (e *MQExecutor) newMQClient(redisCfg *RedisConfig) (*mq.AsynqMessageQueue, error) {
	c, err := buildConnect(redisCfg)
	if err != nil {
		return nil, err
	}
	cfg := &mq.AsynqMessageQueue{
		Namespace: e.Namespace,
		Timeout:   e.Timeout,
	}
	client, err := mq.NewAsynqMessageQueue(c, cfg)
	if err != nil {
		return nil, fmt.Errorf("create asynq mq client: %w", err)
	}
	return client, nil
}

// TestNodeResultData 测试单个节点时返回的数据，携带执行结果 Data 与本次测试的链路 ID（TraceID）。
// TraceID 用于串联本次测试执行产生的所有 activity 日志（见 wf_activity_logs.trace_id），
// 便于在测试记录中回查完整执行链路。
type TestNodeResultData struct {
	// Data 执行结果（FlowContext / map 等，由具体节点类型决定）
	Data         any    `json:"data"`
	RelationType string `json:"relation_type"`
	// TraceID 本次测试的分布式追踪 ID
	TraceID string `json:"trace_id"`
	// DurationMs 节点真实执行耗时（毫秒），即 rulegox.StartActivityFlow 调用本身的耗时
	DurationMs int64 `json:"duration_ms"`
}

// TestNode 通过 MQ 同步调用分布式 worker 测试单个节点，返回 worker 执行结果。
func (e *MQExecutor) TestNode(ctx context.Context, payload *TestNodePayload) (any, error) {
	if payload == nil || payload.NodeDef == nil {
		return nil, fmt.Errorf("payload or nodeDef is required")
	}
	if payload.NodeDef.Type == common.CondSwitchNodeTypeName {
		return e.testNodeForCondSwitch(ctx, payload)
	}
	if payload.NodeDef.Type == common.ActivityNodeTypeName {
		return e.testNodeForActivity(ctx, payload)
	}
	return nil, fmt.Errorf("unsupported node type: %s", payload.NodeDef.Type)
}

// RequestActivity 通过 MQ 同步调用分布式 worker 执行指定 activity，返回 worker 执行结果。
// 该实现复用 TestMQWorkerRequest 的调用方式：
//   - actNamespace / actName 由调用方从 activity 配置（ActivityDef）中获取
//   - params（测试参数）由前端传入
//   - topic 为 activity/{actNamespace}/{actName}，与分布式 worker 端 SubscribeActivity 订阅的 topic 一致
//
// 流程：根据环境 Redis 配置构建连接 -> 创建 MQWorker -> 调用 RequestActivity 同步等待远程监听程序执行。
func (e *MQExecutor) RequestActivity(ctx context.Context, worker *rulegox.MQWorker, actDef *ActivityDef, params any, logInfo *ActivityLogValue) (*httputil.CommResponse, error) {
	if actDef.ActNamespace == "" || actDef.ActName == "" {
		return nil, fmt.Errorf("act_namespace and act_name are required")
	}
	wfWorker, err := NewWfWorkerWithMQWorker(worker)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	var headers http.Header
	if logInfo != nil {
		metaData := &rulegox.MetaDataHeader{
			RootChainID: logInfo.RootChainID,
			TraceID:     logInfo.TraceID,
			SpanID:      logInfo.SpanID,
		}
		headers = metaData.ToHeader(headers)
	}

	resp, err := wfWorker.RequestActivity(ctx, actDef, params, headers)
	durationMs := time.Since(start).Milliseconds()

	// 执行后异步记录 activity 日志（入参 + 结果/错误），由服务端 ActivityCollector 从 redis list 消费落库。
	// 不阻塞主流程返回；网络/redis 异常仅降级记日志，不影响调用方拿结果。
	level := "info"
	errMsg := ""
	if err != nil {
		level = "error"
		errMsg = err.Error()
	}
	rootChainID, traceID, spanID := "", "", actDef.ActivityID
	var attributes any
	if logInfo != nil {
		rootChainID = logInfo.RootChainID
		traceID = logInfo.TraceID
		spanID = logInfo.SpanID
		attributes = logInfo.Attributes
	}
	e.asyncPushLog(worker.Project, worker.Env, actDef.ActNamespace, actDef.ActName,
		level, start.Unix(), durationMs, params, resp, errMsg, rootChainID, traceID, spanID, attributes)

	if err != nil {
		return nil, err
	}
	return resp, nil
}

// asyncPushLog 异步将一条 activity 执行日志直接落库到 wf_activity_logs（不阻塞调用方）。
// 复用 workflow.ActivityLogStore 接口（与服务端 activity_collector 共用同一仓储），
// logStore 为 nil 时（未注入仓储）直接跳过，日志降级，不影响主流程返回。
func (e *MQExecutor) asyncPushLog(project, env, actNamespace, actName, level string, ts, durationMs int64, payload, result any, errMsg, rootChainID, traceID, spanID string, attributes any) {
	if e.logStore == nil {
		return
	}
	store := e.logStore
	goroutines.GoAsync(func(params ...any) {
		def := &ActivityLogDef{
			Project:      project,
			Env:          env,
			ActNamespace: actNamespace,
			ActName:      actName,
			Level:        level,
			Timestamp:    ts,
			DurationMs:   durationMs,
			Payload:      toLogRawMessage(payload),
			Result:       toLogRawMessage(result),
			ErrorMsg:     errMsg,
			RootChainID:  rootChainID,
			TraceID:      traceID,
			SpanID:       spanID,
			Attributes:   toLogString(attributes),
		}
		if err := store.Create(context.Background(), def); err != nil {
			log.Printf("mq_executor: save activity log failed, err: %v", err)
		}
	})
}

// toLogRawMessage 将任意值转为 json.RawMessage，便于直接存入 ActivityLogDef 的 Payload/Result。
// nil 时返回 nil；已是 json.RawMessage 则原样返回；其余按 JSON 序列化。
func toLogRawMessage(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	if raw, ok := v.(json.RawMessage); ok {
		return raw
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

// toLogString 将任意值转为 JSON 字符串，便于直接存入 ActivityLogDef.Attributes（string 类型）。
// nil 时返回空串；其余按 JSON 序列化。
func toLogString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func (e *MQExecutor) BuildWorker(env string, projectName string, redisCfg *RedisConfig) (*rulegox.MQWorker, error) {
	c, err := buildConnect(redisCfg)
	if err != nil {
		return nil, err
	}
	return commnode.GetMQWorker(projectName, env, c, func(p, e string, rc *conn.Connect) (*rulegox.MQWorker, error) {
		return rulegox.NewMQWorker(p, e, c)
	})
}

// testNodeForCondSwitch 本地执行 condSwitch 类型节点。
// condSwitch 节点在 rulego 中实际由 commnode.CondSwitchNode（注册 Type 为 "condition"）执行：
// 从消息体（msg.Data 的 JSON）中解析出参数 map，再使用 RuleExprEngine 执行 configuration 中的 condition 表达式，
// 根据结果路由到 True/False 或指定分支。
// 因此这里直接复用 condition_node 组件：将节点以单节点 RuleChain 形式加载到 rulego 引擎执行（ruleNode.Type 用 "condition"），
// 并将前端传入的 InputParams 作为消息体传入（condSwitch 节点从 msg.Data 读取参数，而非 configuration.arguments）。
func (e *MQExecutor) testNodeForCondSwitch(ctx context.Context, payload *TestNodePayload) (any, error) {
	if payload.NodeDef.Type != common.CondSwitchNodeTypeName {
		return nil, fmt.Errorf("node type is not %s: %s", common.CondSwitchNodeTypeName, payload.NodeDef.Type)
	}

	// 解析节点配置（condition 表达式存放在 configuration.condition）
	config := make(types.Configuration)
	if len(payload.NodeDef.Configuration) > 0 {
		if err := json.Unmarshal(payload.NodeDef.Configuration, &config); err != nil {
			return nil, fmt.Errorf("parse condSwitch node configuration: %w", err)
		}
	}

	// condSwitch 节点在 rulego 中实际由 commnode.CondSwitchNode 处理（其注册 Type 为 "condition"），
	// 因此单节点 RuleChain 的 type 固定为 "condition"（package commnode 已在文件顶部 import 触发注册）。
	ruleNode := &types.RuleNode{
		Id:            payload.NodeDef.NodeID,
		Type:          payload.NodeDef.Type,
		Name:          payload.NodeDef.Name,
		DebugMode:     payload.NodeDef.DebugMode,
		Configuration: config,
	}

	rootDsl := &types.RuleChain{
		RuleChain: types.RuleChainBaseInfo{
			ID:   ruleNode.Id,
			Name: "condition " + ruleNode.Id,
			Root: true,
		},
		Metadata: types.RuleMetadata{
			Nodes:       []*types.RuleNode{ruleNode},
			Connections: []types.NodeConnection{},
		},
	}

	var (
		relationTypeString string
		execErr            error
	)
	flowCtx := paramx.NewFlowContext(ruleNode.Id, id.NewUUID(), payload.InputParams)

	flowCfg := &rulegox.ActivityFlowConfig{
		RootChainDSL: rootDsl,
		FlowContext:  flowCtx,
		IsAsync:      false,
		UseCache:     false,
		EndFunc: func(_ context.Context, relationType string, param *paramx.FlowContext, err error) {
			relationTypeString = relationType
			execErr = err
		},
	}

	// RedisConfig 通过 env 解析：按 project+env 从环境配置中查出 redis 配置，
	// 再转换为 rulegox 所需的 *conn.Connect（与 MQ 执行路径共用同一套解析逻辑）。
	redisDef, err := e.getRedisConfig(ctx, payload.NodeDef.Project, payload.Env)
	if err != nil {
		return nil, err
	}
	redisConn, err := buildConnect(redisDef)
	if err != nil {
		return nil, err
	}

	metaData := &rulegox.ActivityMetaData{
		RootChainID: ruleNode.Id,
		Env:         payload.Env,
		Project:     payload.NodeDef.Project,
		TraceId:     id.NewUUID(),
		RedisConfig: redisConn,
	}

	execStart := time.Now()
	if err := rulegox.StartActivityFlow(ctx, flowCfg, metaData); err != nil {
		return nil, err
	}
	execMs := time.Since(execStart).Milliseconds()
	if execErr != nil {
		return nil, execErr
	}
	return &TestNodeResultData{
		Data:         relationTypeString,
		RelationType: relationTypeString,
		TraceID:      metaData.TraceId,
		DurationMs:   execMs,
	}, nil
}

// testNodeForActivity 本地利用被测节点的全部配置，构建一个「仅包含该 ActivityNode」的单节点规则链，
// 交由 rulegox.StartActivityFlow 在本进程内同步执行：
// 前端传入的 InputParams 作为流程入参（Variables），
// 执行结束后通过 EndFunc 回调拿到 ActivityNode 写回的 ParamCtx，作为测试结果返回。
// 不再走 MQ 远程 worker。
func (e *MQExecutor) testNodeForActivity(ctx context.Context, payload *TestNodePayload) (any, error) {
	if payload.NodeDef.Type != common.ActivityNodeTypeName {
		return nil, fmt.Errorf("node type is not %s: %s", common.ActivityNodeTypeName, payload.NodeDef.Type)
	}

	// 解析节点配置（ActivityNode 从 configuration 读取 node_config.activities / arguments 等）
	config := make(types.Configuration)
	if len(payload.NodeDef.Configuration) > 0 {
		if err := conv.Unmarshal(payload.NodeDef.Configuration, &config); err != nil {
			return nil, fmt.Errorf("parse activity node configuration: %w", err)
		}
	}

	// ActivityNode 已在 commnode 包的 init() 中通过 rulego.Registry.Register 注册
	// （Type 为 common.ActivityNodeTypeName），故单节点 RuleChain 的 type 固定使用该注册名。
	ruleNode := &types.RuleNode{
		Id:            payload.NodeDef.NodeID,
		Type:          payload.NodeDef.Type,
		Name:          payload.NodeDef.Name,
		DebugMode:     payload.NodeDef.DebugMode,
		Configuration: config,
	}

	rootDsl := &types.RuleChain{
		RuleChain: types.RuleChainBaseInfo{
			ID:   ruleNode.Id,
			Name: "activity " + ruleNode.Id,
			Root: true,
		},
		Metadata: types.RuleMetadata{
			Nodes:       []*types.RuleNode{ruleNode},
			Connections: []types.NodeConnection{},
		},
	}

	var (
		resultParam        *paramx.FlowContext
		relationTypeString string
		execErr            error
	)
	flowCtx := paramx.NewFlowContext(ruleNode.Id, id.NewUUID(), payload.InputParams)

	flowCfg := &rulegox.ActivityFlowConfig{
		RootChainDSL: rootDsl,
		FlowContext:  flowCtx,
		IsAsync:      false,
		UseCache:     false,
		EndFunc: func(_ context.Context, relationType string, param *paramx.FlowContext, err error) {
			resultParam = param
			relationTypeString = relationType
			execErr = err
		},
	}
	// RedisConfig 通过 env 解析：按 project+env 从环境配置中查出 redis 配置，
	// 再转换为 rulegox 所需的 *conn.Connect（与 MQ 执行路径共用同一套解析逻辑）。
	redisDef, err := e.getRedisConfig(ctx, payload.NodeDef.Project, payload.Env)
	if err != nil {
		return nil, err
	}
	redisConn, err := buildConnect(redisDef)
	if err != nil {
		return nil, err
	}

	metaData := &rulegox.ActivityMetaData{
		RootChainID: ruleNode.Id,
		Env:         payload.Env,
		Project:     payload.NodeDef.Project,
		TraceId:     id.NewUUID(),
		RedisConfig: redisConn,
	}

	execStart := time.Now()
	if err = rulegox.StartActivityFlow(ctx, flowCfg, metaData); err != nil {
		return nil, err
	}
	execMs := time.Since(execStart).Milliseconds()
	if execErr != nil {
		return nil, execErr
	}
	return &TestNodeResultData{
		Data:         resultParam,
		RelationType: relationTypeString,
		TraceID:      metaData.TraceId,
		DurationMs:   execMs,
	}, nil
}

// getRedisConfig 从环境配置中解析出执行所需的 Redis 配置。
// 与 WorkflowService.getRedisConfig 语义一致：env 必填，按 project+env 查询环境配置，
// 并校验其 RedisConfig.Addr 非空；未注入 envConfigStore 时返回明确错误。
func (e *MQExecutor) getRedisConfig(ctx context.Context, project, env string) (*RedisConfig, error) {
	if env == "" {
		return nil, fmt.Errorf("env is required to resolve redis config")
	}
	if e.envConfigStore == nil {
		return nil, fmt.Errorf("env config store is not injected into MQExecutor, cannot resolve redis config for env %q", env)
	}
	envDef, err := e.envConfigStore.GetByName(ctx, project, env)
	if err != nil {
		return nil, fmt.Errorf("env config %q not found: %w", env, err)
	}
	if envDef.RedisConfig == nil || envDef.RedisConfig.Addr == "" {
		return nil, fmt.Errorf("env config %q has no redis config (addr is empty)", env)
	}
	return envDef.RedisConfig, nil
}

// NewMQExecutor 创建 MQ 执行器，使用默认命名空间和超时。
// 不注入日志仓储，执行日志降级跳过（不影响主流程返回）。
func NewMQExecutor() *MQExecutor {
	return &MQExecutor{
		Namespace: "workflow",
		Timeout:   30 * time.Second,
	}
}

// NewMQExecutorWithLog 创建 MQ 执行器并注入 activity 日志仓储，
// 执行 Activity 后会直接异步落库到 wf_activity_logs。
func NewMQExecutorWithLog(logStore ActivityLogStore) *MQExecutor {
	return &MQExecutor{
		Namespace: "workflow",
		Timeout:   30 * time.Second,
		logStore:  logStore,
	}
}

// NewMQExecutorWithEnv 创建 MQ 执行器并注入环境配置仓储，
// 使 getRedisConfig 能按 project+env 解析出 Redis 配置（供本地执行/测试节点使用）。
func NewMQExecutorWithEnv(envConfigStore EnvConfigStore) *MQExecutor {
	return &MQExecutor{
		Namespace:      "workflow",
		Timeout:        30 * time.Second,
		envConfigStore: envConfigStore,
	}
}

// NewMQExecutorWithLogAndEnv 同时注入日志仓储与环境配置仓储。
func NewMQExecutorWithLogAndEnv(logStore ActivityLogStore, envConfigStore EnvConfigStore) *MQExecutor {
	return &MQExecutor{
		Namespace:      "workflow",
		Timeout:        30 * time.Second,
		logStore:       logStore,
		envConfigStore: envConfigStore,
	}
}
