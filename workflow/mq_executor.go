package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/magic-lib/go-plat-utils/goroutines"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
	"log"
	"time"

	"github.com/magic-lib/go-plat-utils/utils/httputil"

	mq "github.com/magic-lib/go-plat-mq/mq"
	"github.com/magic-lib/go-plat-utils/conn"
	"github.com/magic-lib/go-plat-utils/conv"
	_ "github.com/magic-lib/go-plat-workflow/workflow/rulegox/components/commnode"
	"github.com/rulego/rulego"
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
func (e *MQExecutor) RequestActivity(ctx context.Context, worker *MQWorker, actDef *ActivityDef, params any, logInfo *ActivityLogValue) (*httputil.CommResponse, error) {
	if actDef.ActNamespace == "" || actDef.ActName == "" {
		return nil, fmt.Errorf("act_namespace and act_name are required")
	}
	start := time.Now()
	resp, err := worker.RequestActivity(ctx, actDef, params)
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

	e.asyncPushLog(worker.project, worker.env, actDef.ActNamespace, actDef.ActName,
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
			Attributes:   toLogRawMessage(attributes),
		}
		if err := store.Create(context.Background(), def); err != nil {
			log.Printf("mq_executor: save activity log failed, err: %v", err)
		}
	})
}

// toLogRawMessage 将任意值转为 json.RawMessage，便于直接存入 ActivityLogDef 的 Payload/Result/Attributes。
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

func (e *MQExecutor) BuildWorker(env string, projectName string, redisCfg *RedisConfig) (*MQWorker, error) {
	c, err := buildConnect(redisCfg)
	if err != nil {
		return nil, err
	}
	worker, err := NewMQWorker(projectName, env, c)
	if err != nil {
		return nil, err
	}
	return worker, nil
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
	dsl := map[string]interface{}{
		"ruleChain": map[string]interface{}{
			"id":   "condition_" + payload.NodeDef.NodeID,
			"name": "condition " + payload.NodeDef.NodeID,
			"root": true,
		},
		"metadata": map[string]interface{}{
			"nodes":       []interface{}{ruleNode},
			"connections": []interface{}{},
		},
	}
	dslBytes, err := json.Marshal(dsl)
	if err != nil {
		return nil, fmt.Errorf("marshal condition node dsl: %w", err)
	}

	chainKey := payload.NodeDef.Type + ":" + payload.NodeDef.NodeID
	if _, err := rulego.New(chainKey, dslBytes); err != nil {
		return nil, fmt.Errorf("load condition node chain: %w", err)
	}
	defer rulego.Del(chainKey)

	// condition 节点从 msg.Data(JSON) 解析参数，因此将 InputParams 作为消息体传入。
	inputData := "{}"
	if len(payload.InputParams) > 0 {
		inputData = conv.String(payload.InputParams)
	}
	msg := types.NewMsgWithJsonData(inputData)

	engineInst, ok := rulego.Get(chainKey)
	if !ok {
		return nil, fmt.Errorf("condition chain not found in pool after load")
	}

	var execErr error
	var resultData types.RuleMsg
	var relationType string
	done := make(chan struct{})
	engineInst.OnMsgAndWait(msg,
		types.WithContext(ctx),
		types.WithOnEnd(func(ctx types.RuleContext, msg types.RuleMsg, err error, relType string) {
			if err != nil {
				execErr = err
			} else {
				resultData = msg
				relationType = relType
			}
			close(done)
		}),
	)
	<-done
	if execErr != nil {
		return nil, execErr
	}

	// 返回路由结果（True/False/分支名）与消息体，便于调用方判断走哪个分支。
	return map[string]any{
		"relation_type": relationType,
		"msg":           resultData,
	}, nil
}

// testNodeForActivity 本地利用被测节点的全部配置，构建一个「仅包含该 ActivityNode」的单节点规则链，
// 通过 rulego 引擎在本进程内执行，将前端传入的 InputParams 作为流程入参（消息体），
// 并将执行后的消息体（ActivityNode 写回的 ParamCtx 序列化结果）返回，便于在测试场景验证单节点配置。
// 不再走 MQ 远程 worker。
func (e *MQExecutor) testNodeForActivity(ctx context.Context, payload *TestNodePayload) (any, error) {
	if payload.NodeDef.Type != common.ActivityNodeTypeName {
		return nil, fmt.Errorf("node type is not %s: %s", common.ActivityNodeTypeName, payload.NodeDef.Type)
	}

	// 解析节点配置（ActivityNode 从 configuration 读取 node_config.activities / arguments 等）
	config := make(types.Configuration)
	if len(payload.NodeDef.Configuration) > 0 {
		if err := json.Unmarshal(payload.NodeDef.Configuration, &config); err != nil {
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

	chainKey := payload.NodeDef.NodeID

	dsl := map[string]interface{}{
		"ruleChain": map[string]interface{}{
			"id":   chainKey,
			"name": "activity " + payload.NodeDef.NodeID,
			"root": true,
		},
		"metadata": map[string]interface{}{
			"nodes":       []interface{}{ruleNode},
			"connections": []interface{}{},
		},
	}
	dslBytes := conv.String(dsl)

	fmt.Println(dslBytes)

	return nil, nil
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

// ExecuteRootChain 通过 MQ 同步调用分布式 worker 执行某个 rootChain，返回 worker 执行结果。
func (e *MQExecutor) ExecuteRootChain(ctx context.Context, payload *ExecuteRootChainPayload, redisCfg *RedisConfig) (any, error) {
	client, err := e.newMQClient(redisCfg)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	event := &mq.Event{
		Topic:   TopicExecuteRootChain,
		Payload: payload,
	}
	resp, err := client.Request(ctx, event)
	if err != nil {
		return nil, err
	}
	return resp, nil
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
