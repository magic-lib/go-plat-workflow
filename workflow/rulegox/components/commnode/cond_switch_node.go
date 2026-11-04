package commnode

import (
	"fmt"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/id-generator/id"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
	"github.com/redis/go-redis/v9"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
	"github.com/rulego/rulego/components/base"
	"time"
)

type condSwitchCfg struct {
	SwitchCondition string `json:"switch_condition"`
}

type CondSwitchNode struct {
	Configuration   *CommConfiguration `json:"configuration"`
	switchCondition string
	nodeLogCli      *redis.Client
	ruleObj         *templates.RuleExprEngine
	// nodeName node 中文名（来自 DSL 中 ruleNode.Name，由管理端 builder 写入 NodeDef.Name），
	// 在 Init 时从 SelfDefinition 解析并缓存，用于上报 node 运行日志的 node_name 字段。
	nodeName string
}

// Type 返回组件类型
// Type returns the component type identifier.
func (x *CondSwitchNode) Type() string {
	return common.CondSwitchNodeTypeName
}

// New 创建新实例
// New creates a new instance.
func (x *CondSwitchNode) New() types.Node {
	return &CondSwitchNode{}
}

// Init 初始化组件，验证并编译表达式
// Init initializes the component.
func (x *CondSwitchNode) Init(_ types.Config, configuration types.Configuration) error {
	commonConfig := new(CommConfiguration)
	err := conv.Unmarshal(configuration, commonConfig)
	if err != nil {
		return fmt.Errorf("condRouter error parsing configuration: %s, %v", conv.String(configuration), err)
	}
	x.Configuration = commonConfig
	// 缓存 node 中文名（DSL 中 ruleNode.Name 由管理端 builder 写入 NodeDef.Name），
	// 供上报 node 运行日志时填充 node_name 字段。
	ruleNode := base.NodeUtils.GetSelfDefinition(configuration.Copy())
	if ruleNode.Name != "" {
		x.nodeName = ruleNode.Name
	}
	// condition 在 Init 里一次解析好：节点实例会被并发消息共享，放到 OnMsg 里惰性写字段有数据竞争；
	// 解析失败在这里返回 error（加载链时即报错），而不是在 OnMsg 里 panic 让这条分支静默消失。
	newCond := new(condSwitchCfg)
	if err := conv.Unmarshal(x.Configuration.NodeConfig, newCond); err != nil {
		return fmt.Errorf("condRouter error parsing node_config: %s, %v", conv.String(x.Configuration.NodeConfig), err)
	}
	x.switchCondition = newCond.SwitchCondition
	x.ruleObj = templates.NewRuleExprEngine()
	return nil
}

// OnMsg 处理消息，通过评估编译的表达式来过滤消息
// OnMsg processes incoming messages by evaluating the compiled expression.
func (x *CondSwitchNode) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	allParamStr := msg.GetData()
	allParams := new(paramx.FlowContext)
	if err := conv.Unmarshal(allParamStr, allParams); err != nil {
		ctx.TellFailure(msg, err)
		return
	}

	startTime := time.Now().UnixMilli()

	actMetaData := getActionMeta(msg)

	currNodeId := getNodeId(ctx)
	nodeStr := string(currNodeId)

	nodeSpanId := id.GetUUID(nodeStr)

	nodeParams, argErr := NodeArguments(allParams, currNodeId, x.Configuration.ArgTemplate, x.Configuration.Arguments)

	condStr := x.switchCondition
	if condStr == "" {
		nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, 0, nodeStr, x.nodeName, "success", "info", types.Success, allParams, nil, nil, nil)
		if cliErr == nil && x.nodeLogCli == nil {
			x.nodeLogCli = nodeCli
		}
		if argErr == nil {
			allParams.SetStepArguments(currNodeId, nodeParams)
			msg.SetData(conv.String(allParams))
		}
		ctx.TellNext(msg) //结束流程了
		return
	}
	if argErr != nil {
		nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, 0, nodeStr, x.nodeName, "fail", "error", types.Failure, allParams, nil, nil, argErr)
		if cliErr == nil && x.nodeLogCli == nil {
			x.nodeLogCli = nodeCli
		}
		ctx.TellFailure(msg, argErr)
		return
	}
	allParams.SetStepArguments(currNodeId, nodeParams)
	stepDataMap, _ := allParams.StepMaps(currNodeId)

	relationType, conResult, err := routeByCondition(x.ruleObj, condStr, stepDataMap)
	if err != nil {
		nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, 0, nodeStr, x.nodeName, "fail", "error", types.Failure, allParams, nil, nil, err)
		if cliErr == nil && x.nodeLogCli == nil {
			x.nodeLogCli = nodeCli
		}
		ctx.TellFailure(msg, err)
		return
	}

	durationMs := time.Now().UnixMilli() - startTime

	nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, durationMs, nodeStr, x.nodeName, "success", "info", relationType, allParams, nodeParams, conResult, nil)
	if cliErr == nil && x.nodeLogCli == nil {
		x.nodeLogCli = nodeCli
	}

	msg.SetData(conv.String(allParams))

	ctx.TellNext(msg, relationType)
}

// Desc returns the component description
func (x *CondSwitchNode) Desc() string {
	return "Routes to True/False. Variables: id, ts, data, msg, metadata, type, dataType"
}

// Destroy 清理资源
// Destroy cleans up resources.
func (x *CondSwitchNode) Destroy() {
}

// init 注册ExprFilterNode组件
// init registers the ExprFilterNode component with the default registry.
func init() {
	Registry.Add(&CondSwitchNode{})
	_ = rulego.Registry.Register(&CondSwitchNode{})
}
