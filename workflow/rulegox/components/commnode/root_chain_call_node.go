package commnode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/id-generator/id"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/magic-lib/go-plat-utils/utils/httputil/param"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
	"github.com/magic-lib/go-plat-workflow/workflow/rulegox"
	"github.com/redis/go-redis/v9"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
	"github.com/rulego/rulego/components/base"
)

// MaxRootChainCallDepth 根链互相调用的最大嵌套层数（含最外层根链），超过即报错，防止无限递归。
const MaxRootChainCallDepth = 5

// RootChainInvokeRequest 调用根链节点发起一次根链调用的请求。
type RootChainInvokeRequest struct {
	// Project 所属项目（与调用方根链相同）
	Project string
	// ChainID 目标根链 ID
	ChainID string
	// Env 执行环境（沿用调用方）
	Env string
	// TraceId 链路 ID（沿用调用方，便于把两条链的日志串起来）
	TraceId string
	// Payload 传给目标根链的入参
	Payload map[string]any
}

// RootChainInvoker 同步调用另一条根链（当前发布版本）并返回其返回值的能力抽象。
// 定义在 commnode 包内，避免 commnode -> service 的循环依赖：真正的实现由 service 包注入。
type RootChainInvoker interface {
	InvokeRootChainForNode(ctx context.Context, req *RootChainInvokeRequest) (any, error)
}

// defaultRootChainInvoker 包级默认的根链调用实现（由 service 包在构造 WorkflowService 时注入）。
var defaultRootChainInvoker RootChainInvoker

// SetRootChainInvoker 注入（或清空）包级默认的根链调用实现。
func SetRootChainInvoker(invoker RootChainInvoker) {
	defaultRootChainInvoker = invoker
}

type rootChainCallPathKey struct{}

// rootChainCallPath 取出 ctx 中记录的根链调用路径（从最外层根链到当前根链的 ID 列表）。
func rootChainCallPath(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	path, _ := ctx.Value(rootChainCallPathKey{}).([]string)
	return path
}

// withRootChainCallPath 在 ctx 中记录根链调用路径，随 ctx 传入目标根链的执行，供下一层检查循环与深度。
func withRootChainCallPath(ctx context.Context, path []string) context.Context {
	return context.WithValue(ctx, rootChainCallPathKey{}, path)
}

// nextRootChainCallPath 校验本次调用不会形成循环或超过最大深度，返回调用目标根链时应记录的新路径。
// path 为空时以 currentChainID（当前执行的根链）作为最外层。
func nextRootChainCallPath(path []string, currentChainID, targetChainID string) ([]string, error) {
	if len(path) == 0 && currentChainID != "" {
		path = []string{currentChainID}
	}
	for _, p := range path {
		if p == targetChainID {
			return nil, fmt.Errorf("根链循环调用：%s -> %s", strings.Join(path, " -> "), targetChainID)
		}
	}
	if len(path) >= MaxRootChainCallDepth {
		return nil, fmt.Errorf("根链调用嵌套超过 %d 层：%s -> %s", MaxRootChainCallDepth, strings.Join(path, " -> "), targetChainID)
	}
	next := make([]string, 0, len(path)+1)
	next = append(next, path...)
	return append(next, targetChainID), nil
}

// RootChainCallConfiguration 调用根链节点的配置。
type RootChainCallConfiguration struct {
	// TargetChainID 目标根链 ID（编排时选择，组装 DSL 时写入）
	TargetChainID string `json:"target_chain_id"`
	// Arguments 传给目标根链的入参（key 为目标根链的入参名，value 为取值表达式）
	Arguments []*param.BindConfig `json:"arguments"`
}

// RootChainCallNode 调用根链节点（custom/RootChainCall）。
//
// 执行时按本节点 arguments 解析出入参，同步调用目标根链的当前发布版本，
// 把目标根链的返回值写入本节点的 steps.<实例ID>.responses，供下游节点引用。
// 成功走 Success；失败（目标根链未发布、执行出错、循环调用等）走 Failure。
type RootChainCallNode struct {
	Configuration *RootChainCallConfiguration
	ruleObj       *templates.RuleExprEngine
	nodeLogCli    *redis.Client
	nodeName      string
	// responseKeys 节点定义的返回值 key（configuration.responses）；hasResponseDefs 为 true 时
	// 只输出这些 key（未定义任何返回值则输出 {}），为 false（编排时才选目标根链的节点）时输出目标根链的全部返回值
	responseKeys    []string
	hasResponseDefs bool
}

func (x *RootChainCallNode) Type() string {
	return common.RootChainCallNodeTypeName
}

// Destroy 清理资源（无状态，无需处理）。
func (x *RootChainCallNode) Destroy() {
}

func (x *RootChainCallNode) New() types.Node {
	return &RootChainCallNode{
		Configuration: new(RootChainCallConfiguration),
		ruleObj:       templates.NewRuleExprEngine(),
	}
}

func (x *RootChainCallNode) Init(_ types.Config, configuration types.Configuration) error {
	x.Configuration = new(RootChainCallConfiguration)
	x.ruleObj = templates.NewRuleExprEngine()
	ruleNode := base.NodeUtils.GetSelfDefinition(configuration.Copy())
	cfg := ruleNode.Configuration
	if len(cfg) == 0 {
		cfg = configuration
	}
	if len(cfg) > 0 {
		if err := conv.Unmarshal(cfg, x.Configuration); err != nil {
			return err
		}
	}
	x.Configuration.TargetChainID = strings.TrimSpace(x.Configuration.TargetChainID)
	if x.Configuration.TargetChainID == "" {
		return fmt.Errorf("调用根链节点未选择目标根链")
	}
	if raw, ok := cfg["responses"]; ok {
		x.hasResponseDefs = true
		x.responseKeys = RootChainCallResponseKeys(raw)
	}
	if ruleNode.Name != "" {
		x.nodeName = ruleNode.Name
	}
	return nil
}

// OnMsg 解析入参 → 调用目标根链 → 写入本节点返回值。
func (x *RootChainCallNode) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	startTime := time.Now()
	actMetaData := getActionMeta(msg)
	currNodeId := getNodeId(ctx)
	nodeSpanId := id.GetUUID(string(currNodeId))

	allParam := new(paramx.FlowContext)
	if err := conv.Unmarshal(msg.GetData(), allParam); err != nil {
		x.fail(ctx, msg, allParam, currNodeId, actMetaData, nodeSpanId, startTime, &paramx.Step{StartTimeMs: startTime.UnixMilli()},
			fmt.Errorf("解析流程上下文失败: %w", err))
		return
	}
	allDataMap, _ := allParam.ToMaps()
	arguments := replaceBindConfig(x.ruleObj, allDataMap, x.Configuration.Arguments)
	fillPrivateArguments(arguments, allParam.GetStepArguments(currNodeId), string(currNodeId), x.Configuration.Arguments)
	nodeStep := &paramx.Step{
		Arguments:   arguments,
		Status:      paramx.StepStatusPending,
		StartTimeMs: startTime.UnixMilli(),
	}

	responses, err := x.invoke(ctx.GetContext(), actMetaData, arguments)
	if err != nil {
		x.fail(ctx, msg, allParam, currNodeId, actMetaData, nodeSpanId, startTime, nodeStep, err)
		return
	}

	if x.hasResponseDefs {
		responses = PickRootChainCallResponses(responses, x.responseKeys)
	}
	nodeStep.Responses = responses
	nodeStep.Status = paramx.StepStatusSuccess
	nodeStep.EndTimeMs = time.Now().UnixMilli()
	allParam.SetStep(currNodeId, nodeStep)
	msg.SetData(conv.String(allParam))

	nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, time.Since(startTime).Milliseconds(), string(currNodeId), x.nodeName, "success", "info", types.Success, allParam,
		arguments, responses, nil)
	if cliErr == nil && x.nodeLogCli == nil {
		x.nodeLogCli = nodeCli
	}
	ctx.TellSuccess(msg)
}

// invoke 校验循环与深度后，通过注入的 RootChainInvoker 调用目标根链。
func (x *RootChainCallNode) invoke(goCtx context.Context, metaData *rulegox.ActivityMetaData, arguments map[string]any) (any, error) {
	return invokeRootChainTarget(goCtx, metaData, x.Configuration.TargetChainID, arguments)
}

// invokeRootChainTarget 校验循环与深度后，通过注入的 RootChainInvoker 调用目标根链（调用根链节点、循环节点共用）。
func invokeRootChainTarget(goCtx context.Context, metaData *rulegox.ActivityMetaData, target string, arguments map[string]any) (any, error) {
	if defaultRootChainInvoker == nil {
		return nil, fmt.Errorf("调用根链节点未初始化（RootChainInvoker 未注入）")
	}
	if goCtx == nil {
		goCtx = context.Background()
	}
	path, err := nextRootChainCallPath(rootChainCallPath(goCtx), metaData.RootChainID, target)
	if err != nil {
		return nil, err
	}
	resp, err := defaultRootChainInvoker.InvokeRootChainForNode(withRootChainCallPath(goCtx, path), &RootChainInvokeRequest{
		Project: metaData.Project,
		ChainID: target,
		Env:     metaData.Env,
		TraceId: metaData.TraceId,
		Payload: arguments,
	})
	if err != nil {
		return nil, fmt.Errorf("调用根链 %s 失败: %w", target, err)
	}
	return resp, nil
}

// fillPrivateArguments 补齐本节点的私有入参。
// 调用根链节点的入参名就是目标根链的入参名，本身可能带 "."（如目标根链的私有参数 N000036__70lic.mobile），
// 编排时取值为 {{arguments.<本节点实例ID>.<入参名>}}。调用方以 { <本节点实例ID>: {...} } 传入时，
// 入口已把这部分放进 steps.<本节点实例ID>.arguments（按 "." 展开成嵌套结构），模板替换取不到，
// 这里按入参名的 "." 路径从中取值覆盖。
func fillPrivateArguments(arguments map[string]any, stepArgs map[string]any, nodeID string, binds []*param.BindConfig) {
	if len(stepArgs) == 0 {
		return
	}
	prefix := "arguments." + nodeID + "."
	for _, b := range binds {
		if b == nil || b.Key == "" || !strings.Contains(conv.String(b.Value), prefix) {
			continue
		}
		if v, ok := lookupArgPath(stepArgs, b.Key); ok {
			arguments[b.Key] = v
		}
	}
}

// lookupArgPath 按 "." 路径在 m 中取值，同时兼容整体作为一个 key 的平铺写法。
func lookupArgPath(m map[string]any, path string) (any, bool) {
	if v, ok := m[path]; ok {
		return v, true
	}
	var cur any = m
	for _, part := range strings.Split(path, ".") {
		next, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = next[part]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// fail 记录失败步骤、上报失败日志与告警，并走 Failure 分支。
func (x *RootChainCallNode) fail(ctx types.RuleContext, msg types.RuleMsg, allParam *paramx.FlowContext, currNodeId paramx.StepId,
	actMetaData *rulegox.ActivityMetaData, nodeSpanId string, startTime time.Time, nodeStep *paramx.Step, err error) {
	nodeStr := string(currNodeId)
	sendAlert(context.Background(), actMetaData.Project, actMetaData.Env, "[工作流告警] Node 执行失败",
		fmt.Sprintf("流程ID: %s\n流程名称: %s\n节点名称: %s\n节点ID: %s\n项目: %s\n环境: %s\nTraceId: %s\n错误信息: %s\n时间: %s",
			actMetaData.RootChainID, actMetaData.RootChainName, x.nodeName, nodeStr, actMetaData.Project, actMetaData.Env, actMetaData.TraceId, err.Error(), time.Now().Format("2006-01-02 15:04:05")))

	nodeStep.Status = paramx.StepStatusFail
	nodeStep.EndTimeMs = time.Now().UnixMilli()
	nodeStep.Error = &paramx.ErrorInfo{
		Code:    "500",
		Message: err.Error(),
		Stack:   err.Error(),
	}
	allParam.SetStep(currNodeId, nodeStep)
	msg.SetData(conv.String(allParam))

	nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, time.Since(startTime).Milliseconds(), nodeStr, x.nodeName, "fail", "error", types.Failure, allParam,
		nodeStep.Arguments, nodeStep.Responses, err)
	if cliErr == nil && x.nodeLogCli == nil {
		x.nodeLogCli = nodeCli
	}
	ctx.TellFailure(msg, err)
}

// RootChainCallResponseKeys 从节点 configuration.responses（返回值定义数组，元素含 key）中取出返回值 key 列表。
func RootChainCallResponseKeys(raw any) []string {
	var items []struct {
		Key string `json:"key"`
	}
	if b, err := json.Marshal(raw); err == nil {
		_ = json.Unmarshal(b, &items)
	}
	keys := make([]string, 0, len(items))
	for _, it := range items {
		if k := strings.TrimSpace(it.Key); k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

// PickRootChainCallResponses 按节点定义的返回值 key 从目标根链返回值中取值，只输出这些 key（取不到的为 nil）；
// 没有定义任何返回值时返回空对象 {}，不把目标根链的其它内部结果带出来。
func PickRootChainCallResponses(resp any, keys []string) map[string]any {
	out := make(map[string]any, len(keys))
	if len(keys) == 0 {
		return out
	}
	src, ok := resp.(map[string]any)
	if !ok && resp != nil {
		src = make(map[string]any)
		if b, err := json.Marshal(resp); err == nil {
			_ = json.Unmarshal(b, &src)
		}
	}
	for _, k := range keys {
		out[k] = src[k]
	}
	return out
}

func init() {
	Registry.Add(&RootChainCallNode{})
	_ = rulego.Registry.Register(&RootChainCallNode{})
}
