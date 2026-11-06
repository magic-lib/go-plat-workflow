package commnode

import (
	"fmt"
	"time"

	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/id-generator/id"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/magic-lib/go-plat-utils/utils/httputil/param"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
	"github.com/redis/go-redis/v9"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
	"github.com/rulego/rulego/components/base"
)

// ParamInputConfiguration 参数输入节点的配置。
type ParamInputConfiguration struct {
	// InputParams 编排时为本实例定义的参数（参数名/类型/说明），组装 DSL 时写入
	InputParams []*ParamInputParam `json:"input_params"`
	// Arguments 各参数的取值来源（key 为参数名，value 为取值表达式）
	Arguments []*param.BindConfig `json:"arguments"`
}

// ParamInputParam 参数输入节点的一个参数定义。
type ParamInputParam struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// ParamInputNode 参数输入节点（custom/ParamInput）。
//
// 编排时为每个实例动态定义参数列表，并为每个参数配置来源（固定配置 / 引用节点 / 调用传入）；
// 执行时按来源解析出各参数的值，同时写入本节点的 steps.<实例ID>.arguments 与 steps.<实例ID>.responses，
// 下游节点可直接引用。只有定义过的参数会被输出；定义了但没配置来源的参数输出为 nil。
type ParamInputNode struct {
	Configuration *ParamInputConfiguration
	ruleObj       *templates.RuleExprEngine
	nodeLogCli    *redis.Client
	nodeName      string
}

func (x *ParamInputNode) Type() string {
	return common.ParamInputNodeTypeName
}

// Destroy 清理资源（无状态，无需处理）。
func (x *ParamInputNode) Destroy() {
}

func (x *ParamInputNode) New() types.Node {
	return &ParamInputNode{
		Configuration: new(ParamInputConfiguration),
		ruleObj:       templates.NewRuleExprEngine(),
	}
}

func (x *ParamInputNode) Init(_ types.Config, configuration types.Configuration) error {
	x.Configuration = new(ParamInputConfiguration)
	x.ruleObj = templates.NewRuleExprEngine()
	ruleNode := base.NodeUtils.GetSelfDefinition(configuration.Copy())
	cfg := ruleNode.Configuration
	if len(cfg) == 0 {
		cfg = configuration
	}
	if len(cfg) > 0 {
		if err := conv.Unmarshal(cfg, x.Configuration); err != nil {
			return fmt.Errorf("解析参数输入节点配置失败: %w", err)
		}
	}
	if ruleNode.Name != "" {
		x.nodeName = ruleNode.Name
	}
	return nil
}

// OnMsg 按各参数配置的来源解析出参数值，写入本节点的 arguments 与 responses。
func (x *ParamInputNode) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	startTime := time.Now()
	actMetaData := getActionMeta(msg)
	currNodeId := getNodeId(ctx)
	nodeStr := string(currNodeId)
	nodeSpanId := id.GetUUID(nodeStr)

	allParam := new(paramx.FlowContext)
	if err := conv.Unmarshal(msg.GetData(), allParam); err != nil {
		err = fmt.Errorf("解析流程上下文失败: %w", err)
		nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, time.Since(startTime).Milliseconds(), nodeStr, x.nodeName, "fail", "error", types.Failure, allParam,
			nil, nil, err)
		if cliErr == nil && x.nodeLogCli == nil {
			x.nodeLogCli = nodeCli
		}
		ctx.TellFailure(msg, err)
		return
	}

	allDataMap, _ := allParam.ToMaps()
	resolved := replaceBindConfig(x.ruleObj, allDataMap, x.Configuration.Arguments)
	fillPrivateArguments(resolved, allParam.GetStepArguments(currNodeId), nodeStr, x.Configuration.Arguments)
	values := x.collectValues(resolved)

	allParam.SetStep(currNodeId, &paramx.Step{
		Arguments:   values,
		Responses:   values,
		Status:      paramx.StepStatusSuccess,
		StartTimeMs: startTime.UnixMilli(),
		EndTimeMs:   time.Now().UnixMilli(),
	})
	msg.SetData(conv.String(allParam))

	nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, time.Since(startTime).Milliseconds(), nodeStr, x.nodeName, "success", "info", types.Success, allParam,
		values, values, nil)
	if cliErr == nil && x.nodeLogCli == nil {
		x.nodeLogCli = nodeCli
	}
	ctx.TellSuccess(msg)
}

// collectValues 按参数定义输出参数值：定义了的参数都输出（未配置来源的为 nil）；
// 没有参数定义时（如旧 DSL）原样输出解析结果。
func (x *ParamInputNode) collectValues(resolved map[string]any) map[string]any {
	if len(x.Configuration.InputParams) == 0 {
		return resolved
	}
	values := make(map[string]any, len(x.Configuration.InputParams))
	for _, p := range x.Configuration.InputParams {
		if p == nil || p.Key == "" {
			continue
		}
		v, ok := resolved[p.Key]
		if !ok {
			values[p.Key] = nil
			continue
		}
		if p.Type != "" {
			if cv, ok := conv.ConvertForTypeString(p.Type, v); ok {
				v = cv
			}
		}
		values[p.Key] = v
	}
	return values
}

func init() {
	Registry.Add(&ParamInputNode{})
	_ = rulego.Registry.Register(&ParamInputNode{})
}
