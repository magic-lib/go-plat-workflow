package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/magic-lib/go-plat-utils/utils/httputil"

	mq "github.com/magic-lib/go-plat-mq/mq"
	"github.com/magic-lib/go-plat-utils/conn"
	"github.com/magic-lib/go-plat-utils/conv"
	_ "github.com/magic-lib/go-plat-utils/plugins/rulegox/components/commnode"
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
	// Project 项目标识
	Project string `json:"project"`
	// NodeID 被测节点 ID
	NodeID string `json:"node_id"`
	// NodeDef 节点定义
	NodeDef *NodeDef `json:"node_def"`
	// NodeName 被测节点名称（快照，便于 worker 日志）
	NodeName string `json:"node_name,omitempty"`
	// InputParams 测试时传入的参数（map 形式，key=参数名 value=参数值）
	InputParams map[string]interface{} `json:"input_params"`
	// EnvVars 环境变量（可选，key-value），注入到执行上下文中
	EnvVars map[string]string `json:"env_vars,omitempty"`
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
func (e *MQExecutor) TestNode(ctx context.Context, payload *TestNodePayload, redisCfg *RedisConfig) (any, error) {
	if payload == nil || payload.NodeDef == nil {
		return nil, fmt.Errorf("payload or nodeDef is required")
	}
	if payload.NodeDef.Type == "condSwitch" {
		return e.RunNodeForCondSwitch(ctx, payload, redisCfg)
	}
	if payload.NodeDef.Type == "activity" {
		return e.RunNodeForActivity(ctx, payload, redisCfg)
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
func (e *MQExecutor) RequestActivity(ctx context.Context, project, env, actNamespace, actName string, params any, redisCfg *RedisConfig) (*httputil.CommResponse, error) {
	if actNamespace == "" || actName == "" {
		return nil, fmt.Errorf("act_namespace and act_name are required")
	}
	c, err := buildConnect(redisCfg)
	if err != nil {
		return nil, err
	}
	worker, err := NewMQWorker(project, env, c)
	if err != nil {
		return nil, err
	}
	defer worker.Stop()

	return worker.RequestActivity(ctx, actNamespace, actName, params)
}

// RunNodeForCondSwitch 本地执行 condSwitch 类型节点。
// condSwitch 节点在 rulego 中实际由 commnode.CondRouterNode（注册 Type 为 "condition"）执行：
// 从消息体（msg.Data 的 JSON）中解析出参数 map，再使用 RuleExprEngine 执行 configuration 中的 condition 表达式，
// 根据结果路由到 True/False 或指定分支。
// 因此这里直接复用 condition_node 组件：将节点以单节点 RuleChain 形式加载到 rulego 引擎执行（ruleNode.Type 用 "condition"），
// 并将前端传入的 InputParams 作为消息体传入（condSwitch 节点从 msg.Data 读取参数，而非 configuration.arguments）。
func (e *MQExecutor) RunNodeForCondSwitch(ctx context.Context, payload *TestNodePayload, redisCfg *RedisConfig) (any, error) {
	if payload.NodeDef.Type != "condSwitch" {
		return nil, fmt.Errorf("node type is not condSwitch: %s", payload.NodeDef.Type)
	}

	// 解析节点配置（condition 表达式存放在 configuration.condition）
	config := make(types.Configuration)
	if len(payload.NodeDef.Configuration) > 0 {
		if err := json.Unmarshal(payload.NodeDef.Configuration, &config); err != nil {
			return nil, fmt.Errorf("parse condSwitch node configuration: %w", err)
		}
	}

	// condSwitch 节点在 rulego 中实际由 commnode.CondRouterNode 处理（其注册 Type 为 "condition"），
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

	chainKey := payload.NodeDef.Type + ":" + payload.Project + ":" + payload.NodeDef.NodeID
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

func (e *MQExecutor) RunNodeForActivity(ctx context.Context, payload *TestNodePayload, redisCfg *RedisConfig) (any, error) {
	if payload.NodeDef.Type != "activity" {
		return nil, fmt.Errorf("node type is not activity: %s", payload.NodeDef.Type)
	}
	client, err := e.newMQClient(redisCfg)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	event := &mq.Event{
		Topic:   payload.NodeID,
		Payload: payload,
	}
	resp, err := client.Request(ctx, event)
	if err != nil {
		return nil, err
	}
	return resp, nil
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
func NewMQExecutor() *MQExecutor {
	return &MQExecutor{
		Namespace: "workflow",
		Timeout:   30 * time.Second,
	}
}
