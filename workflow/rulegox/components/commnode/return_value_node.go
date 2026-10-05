package commnode

import (
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/id-generator/id"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
	"github.com/magic-lib/go-plat-workflow/workflow/rulegox"
	"github.com/redis/go-redis/v9"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
	"github.com/rulego/rulego/components/base"
)

// ReturnValueNode 根链返回值节点（custom/ReturnValue）。
//
// 作用：在根链编排时，把"返回值定义"里配置的 Key 列表作为该节点的参数面板展示；
// 用户在各分支上为每个 Key 填写取值表达式（沿用现有节点参数的占位符写法，如
// {{steps.<实例ID>.responses.x}} / {{arguments.x}} / 字面量）。流程执行到该节点时，
// 把解析后的值写入整条根链的 FlowContext.Responses，再随 msg.Data 向下/向结束节点传递。
//
// 多分支场景天然由"只触达执行分支"实现差异化返回：仅被执行的那个分支会走到该节点并写入；
// 若同时只存在一个 Return 节点（无分支），则直接写返回值；若有多个分支各放一个 Return 节点，
// 真正执行到的那个会写入（后写的覆盖前写的，符合"实际执行的分支胜出"语义）。
//
// 该节点不再依赖 DSL 中 additionalInfo.root_responses 的静态 Value 解析（已由本节点运行期写入取代）。
type ReturnValueNode struct {
	Configuration *CommConfiguration
	ruleObj       *templates.RuleExprEngine
	nodeLogCli    *redis.Client
	nodeName      string
}

func (x *ReturnValueNode) Type() string {
	return common.ReturnValueNodeTypeName
}

// Destroy 清理资源（无状态，无需处理）。
func (x *ReturnValueNode) Destroy() {
}

func (x *ReturnValueNode) New() types.Node {
	cfg := new(CommConfiguration)
	if x.Configuration != nil {
		_ = conv.Unmarshal(x.Configuration, cfg)
	}
	node := &ReturnValueNode{
		Configuration: cfg,
		ruleObj:       x.ruleObj,
	}
	if node.ruleObj == nil {
		node.ruleObj = templates.NewRuleExprEngine()
	}
	return node
}

func (x *ReturnValueNode) Init(_ types.Config, configuration types.Configuration) error {
	x.Configuration = new(CommConfiguration)
	if len(configuration) == 0 {
		return nil
	}
	ruleNode := base.NodeUtils.GetSelfDefinition(configuration.Copy())
	if err := conv.Unmarshal(ruleNode.Configuration, x.Configuration); err != nil {
		return err
	}
	x.ruleObj = templates.NewRuleExprEngine()
	if ruleNode.Name != "" {
		x.nodeName = ruleNode.Name
	}
	return nil
}

// OnMsg 反序列化运行上下文，按本节点 arguments（返回值 Key->取值表达式）解析每个 Key，
// 写入 FlowContext.Responses 后重新序列化并 TellSuccess。
func (x *ReturnValueNode) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	metaDataAny := msg.GetMetadata()
	metaDataMap := make(map[string]any)
	metaDataAny.ForEach(func(key string, value string) bool {
		metaDataMap[key] = value
		return true
	})
	actMetaData := new(rulegox.ActivityMetaData)
	_ = conv.Unmarshal(metaDataMap, actMetaData)

	currNodeId := getNodeId(ctx)
	nodeStr := string(currNodeId)

	nodeSpanId := id.GetUUID(nodeStr)

	allParam := new(paramx.FlowContext)
	if err := conv.Unmarshal(msg.GetData(), allParam); err != nil {
		// 无法解析则原样向下传递，不中断链路

		nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, 0, nodeStr, x.nodeName, "fail", "error", types.Failure, allParam,
			allParam.Arguments, allParam.Responses, err)
		if cliErr == nil && x.nodeLogCli == nil {
			x.nodeLogCli = nodeCli
		}

		ctx.TellSuccess(msg)
		return
	}
	if len(x.Configuration.Arguments) == 0 {
		ctx.TellSuccess(msg)
		return
	}
	allDataMap, _ := allParam.ToMaps()
	resolved := replaceBindConfig(x.ruleObj, allDataMap, x.Configuration.Arguments)

	responses := map[string]any{}
	if existing, ok := allParam.GetResponses().(map[string]any); ok && existing != nil {
		for k, v := range existing {
			responses[k] = v
		}
	}
	if len(resolved) > 0 {
		for k, v := range resolved {
			responses[k] = v
		}
	}

	nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, 0, nodeStr, x.nodeName, "success", "info", types.Success, allParam,
		allParam.Arguments, responses, nil)
	if cliErr == nil && x.nodeLogCli == nil {
		x.nodeLogCli = nodeCli
	}

	allParam.SetResponses(responses)
	msg.SetData(conv.String(allParam))
	ctx.TellSuccess(msg)
}

func init() {
	Registry.Add(&ReturnValueNode{})
	_ = rulego.Registry.Register(&ReturnValueNode{})
}
