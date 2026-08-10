package commnode

import (
	"fmt"
	"github.com/magic-lib/go-plat-utils/cond"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
	"log"
)

type condSwitchCfg struct {
	Condition string `json:"condition"`
}

type CondSwitchNode struct {
	Configuration *CommConfiguration `json:"configuration"`
	condition     string
	ruleObj       *templates.RuleExprEngine
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
	x.ruleObj = templates.NewRuleExprEngine()
	return nil
}

// getCondition 解析配置中的 condition 表达式
func (x *CondSwitchNode) getCondition() string {
	if x.condition != "" {
		return x.condition
	}
	newCond := new(condSwitchCfg)
	err := conv.Unmarshal(x.Configuration.NodeConfig, newCond)
	if err != nil {
		panic(fmt.Errorf("condRouter error parsing configuration: %s, %v", conv.String(x.Configuration), err))
		return ""
	}
	x.condition = newCond.Condition
	log.Println("condRouter init, condition: ", x.condition)
	return x.condition
}

// OnMsg 处理消息，通过评估编译的表达式来过滤消息
// OnMsg processes incoming messages by evaluating the compiled expression.
func (x *CondSwitchNode) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	dataStr := msg.GetData()
	allParams := paramx.NewParamCtx()
	err := conv.Unmarshal(dataStr, allParams)
	if err != nil {
		ctx.TellFailure(msg, err)
		return
	}
	condStr := x.getCondition()
	if condStr == "" {
		ctx.TellNext(msg) //结束流程了
		return
	}
	nodeParams, err := NodeParams(allParams, x.Configuration)
	if err != nil {
		ctx.TellFailure(msg, err)
		return
	}

	conResult, err := x.ruleObj.RunString(condStr, nodeParams)
	if err != nil {
		ctx.TellFailure(msg, err)
		return
	}

	isBool := cond.IsBool(conResult)
	if isBool {
		boolResult, err := conv.Convert[bool](conResult)
		if err != nil {
			ctx.TellFailure(msg, err)
			return
		}
		if boolResult {
			ctx.TellNext(msg, types.True)
		} else {
			ctx.TellNext(msg, types.False)
		}
		return
	}
	if retStr, ok := conResult.(string); ok {
		ctx.TellNext(msg, retStr)
		return
	}
	// 默认使用Success和Failure
	changeBool, err := conv.Convert[bool](conResult)
	if err != nil {
		// 如果出错，则采用转为字符串来进行处理
		ctx.TellNext(msg, conv.String(conResult))
		return
	}
	// 默认采用Success和Failure
	if changeBool {
		ctx.TellSuccess(msg)
	} else {
		ctx.TellNext(msg, types.Failure)
	}
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
