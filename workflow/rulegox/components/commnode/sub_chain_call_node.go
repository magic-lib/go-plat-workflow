package commnode

import (
	"context"
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

// SubChainCallConfiguration 调用子链节点的配置。
type SubChainCallConfiguration struct {
	// TargetId 引擎池里子链的 ID（project:嵌入子链ID，旧数据为 project:子链ID）
	TargetId string `json:"targetId"`
	// Arguments 子链各入参的取值来源（key 为子链里 {{arguments.xxx}} 的参数名）
	Arguments []*param.BindConfig `json:"arguments"`
}

// SubChainCallNode 调用子链节点（custom/SubChainCall）。
//
// 子链在编排里按普通节点处理：执行时先按本节点 arguments 解析出入参，作为子链内的 arguments
// （子链里的 {{arguments.xxx}} 取到的就是这些值，同一子链添加多次时各实例入参互不影响），
// 子链里返回值节点写入的 responses 作为本节点的返回值。子链结束后恢复外层链的 arguments / responses，
// 并把入参与返回值写入本节点的 steps.<实例ID>，供下游节点引用。
// 子链每个结束分支都按其结束关系转发给下游（与 rulego flow 节点 extend 模式一致）；子链执行出错走 Failure。
type SubChainCallNode struct {
	Configuration *SubChainCallConfiguration
	ruleObj       *templates.RuleExprEngine
	nodeLogCli    *redis.Client
	nodeName      string
}

func (x *SubChainCallNode) Type() string {
	return common.SubChainCallNodeTypeName
}

// Destroy 清理资源（无状态，无需处理）。
func (x *SubChainCallNode) Destroy() {
}

func (x *SubChainCallNode) New() types.Node {
	return &SubChainCallNode{
		Configuration: new(SubChainCallConfiguration),
		ruleObj:       templates.NewRuleExprEngine(),
	}
}

func (x *SubChainCallNode) Init(_ types.Config, configuration types.Configuration) error {
	x.Configuration = new(SubChainCallConfiguration)
	x.ruleObj = templates.NewRuleExprEngine()
	ruleNode := base.NodeUtils.GetSelfDefinition(configuration.Copy())
	cfg := ruleNode.Configuration
	if len(cfg) == 0 {
		cfg = configuration
	}
	if len(cfg) > 0 {
		if err := conv.Unmarshal(cfg, x.Configuration); err != nil {
			return fmt.Errorf("解析调用子链节点配置失败: %w", err)
		}
	}
	x.Configuration.TargetId = strings.TrimSpace(x.Configuration.TargetId)
	if x.Configuration.TargetId == "" {
		if ref, _ := cfg["ruleChainId"].(string); strings.TrimSpace(ref) != "" {
			x.Configuration.TargetId = strings.TrimSpace(ref)
		}
	}
	if x.Configuration.TargetId == "" {
		return fmt.Errorf("调用子链节点未指定子链")
	}
	if ruleNode.Name != "" {
		x.nodeName = ruleNode.Name
	}
	return nil
}

// OnMsg 解析入参 → 以入参作为 arguments 执行子链 → 恢复外层上下文并写入本节点的入参与返回值。
func (x *SubChainCallNode) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
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
	inputs := replaceBindConfig(x.ruleObj, allDataMap, x.Configuration.Arguments)
	fillPrivateArguments(inputs, allParam.GetStepArguments(currNodeId), string(currNodeId), x.Configuration.Arguments)

	parentArgs := allParam.Arguments
	parentResp := allParam.Responses
	parentStepIds := make(map[paramx.StepId]struct{}, len(allParam.Steps))
	for k := range allParam.Steps {
		parentStepIds[k] = struct{}{}
	}
	callPath := SubChainCallPath(actMetaData.CallPath, string(currNodeId))
	allParam.Arguments = SubChainArguments(inputs)
	allParam.Responses = nil
	subMsg := msg.Copy()
	subMsg.SetData(conv.String(allParam))
	// 子链里的节点写日志时据此标出所在子链与调用路径，区分主链节点及同一子链的不同调用
	subMsg.GetMetadata().PutValue(metaKeyChainID, SubChainIDOfTarget(x.Configuration.TargetId))
	subMsg.GetMetadata().PutValue(metaKeyCallPath, callPath)

	ctx.TellFlow(x.Configuration.TargetId, subMsg, types.WithContext(ctx.GetContext()),
		types.WithOnEnd(func(_ types.RuleContext, endMsg types.RuleMsg, err error, relationType string) {
			nodeStep := &paramx.Step{
				Arguments:   inputs,
				Status:      paramx.StepStatusPending,
				StartTimeMs: startTime.UnixMilli(),
			}
			// 回到外层链：调用路径与所在链恢复为外层的值，避免后续外层节点被标成子链节点
			endMsg.GetMetadata().PutValue(metaKeyChainID, actMetaData.ChainID)
			endMsg.GetMetadata().PutValue(metaKeyCallPath, actMetaData.CallPath)
			out := new(paramx.FlowContext)
			if perr := conv.Unmarshal(endMsg.GetData(), out); perr != nil {
				// 子链输出无法解析时以进入子链前的上下文为准，避免丢失外层链已有的步骤
				out = allParam
				if err == nil {
					err = fmt.Errorf("解析子链输出失败: %w", perr)
				}
			} else {
				ScopeSubChainSteps(out, parentStepIds, callPath)
			}
			subResp := out.Responses
			out.Arguments = parentArgs
			out.Responses = parentResp
			if err != nil {
				x.fail(ctx, endMsg, out, currNodeId, actMetaData, nodeSpanId, startTime, nodeStep, fmt.Errorf("执行子链 %s 失败: %w", x.Configuration.TargetId, err))
				return
			}
			if subResp == nil {
				subResp = map[string]any{}
			}
			nodeStep.Responses = subResp
			nodeStep.Status = paramx.StepStatusSuccess
			nodeStep.EndTimeMs = time.Now().UnixMilli()
			out.SetStep(currNodeId, nodeStep)
			endMsg.SetData(conv.String(out))

			nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, time.Since(startTime).Milliseconds(), string(currNodeId), x.nodeName, "success", "info", types.Success, out,
				inputs, subResp, nil)
			if cliErr == nil && x.nodeLogCli == nil {
				x.nodeLogCli = nodeCli
			}
			ctx.TellNext(endMsg, relationType)
		}))
}

// SubChainArguments 把调用子链节点解析出的入参转成子链内的 arguments：参数名带 "."（如子链内节点的私有参数
// N000036__70lic.mobile）时同时按层级展开，与根链入口处理调用方入参的方式一致，子链里 {{arguments.a.b}} 都能取到。
func SubChainArguments(inputs map[string]any) map[string]any {
	args := conv.MapFromKeyList(inputs)
	if args == nil {
		args = make(map[string]any, len(inputs))
	}
	for k, v := range inputs {
		args[k] = v
	}
	return args
}

// 执行元数据（msg.Metadata）里子链所在链与调用路径的 key，与 rulegox.ActivityMetaData 的 json 字段一致。
const (
	metaKeyChainID  = "chain_id"
	metaKeyCallPath = "call_path"
)

// SubChainCallPath 在外层调用路径后追加本调用子链节点的实例 ID，多层嵌套用 "/" 拼接。
func SubChainCallPath(parentPath, nodeID string) string {
	if parentPath == "" {
		return nodeID
	}
	return parentPath + "/" + nodeID
}

// SubChainIDOfTarget 从引擎池里子链的 targetId（project:子链ID#hash）取出子链 ID。
func SubChainIDOfTarget(targetID string) string {
	if i := strings.Index(targetID, ":"); i >= 0 {
		targetID = targetID[i+1:]
	}
	if i := strings.Index(targetID, "#"); i >= 0 {
		targetID = targetID[:i]
	}
	return targetID
}

// ScopeSubChainSteps 把子链执行期间新增的步骤改用「调用路径/节点ID」作 key，避免同一子链多次调用时
// 子链内节点的步骤互相覆盖，也不与外层链的节点混在一起；已带本调用路径前缀的（嵌套子链已改过的）保持不变。
func ScopeSubChainSteps(out *paramx.FlowContext, parentStepIds map[paramx.StepId]struct{}, callPath string) {
	if out == nil || len(out.Steps) == 0 {
		return
	}
	prefix := callPath + "/"
	for k, v := range out.Steps {
		if _, ok := parentStepIds[k]; ok || strings.HasPrefix(string(k), prefix) {
			continue
		}
		delete(out.Steps, k)
		out.Steps[paramx.StepId(prefix+string(k))] = v
	}
}

// fail 记录失败步骤、上报失败日志与告警，并走 Failure 分支。
func (x *SubChainCallNode) fail(ctx types.RuleContext, msg types.RuleMsg, allParam *paramx.FlowContext, currNodeId paramx.StepId,
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

func init() {
	Registry.Add(&SubChainCallNode{})
	_ = rulego.Registry.Register(&SubChainCallNode{})
}
