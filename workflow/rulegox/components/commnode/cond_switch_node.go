package commnode

import (
	"fmt"
	"github.com/magic-lib/go-plat-utils/cond"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/id-generator/id"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
	"github.com/magic-lib/go-plat-workflow/workflow/rulegox"
	"github.com/redis/go-redis/v9"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
	"log"
	"time"
)

type condSwitchCfg struct {
	Condition string `json:"condition"`
}

type CondSwitchNode struct {
	Configuration *CommConfiguration `json:"configuration"`
	condition     string
	nodeLogCli    *redis.Client
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
	if err := conv.Unmarshal(x.Configuration.NodeConfig, newCond); err != nil {
		panic(fmt.Errorf("condRouter error parsing configuration: %s, %v", conv.String(x.Configuration), err))
	}
	x.condition = newCond.Condition
	log.Println("condRouter init, condition: ", x.condition)
	return x.condition
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

	metaDataAny := msg.GetMetadata()
	metaDataMap := make(map[string]any)
	metaDataAny.ForEach(func(key string, value string) bool {
		metaDataMap[key] = value
		return true
	})
	actMetaData := new(rulegox.ActivityMetaData)
	_ = conv.Unmarshal(metaDataMap, actMetaData)

	nodeSpanId := id.NewUUID()

	currNodeId := getNodeId(ctx)
	nodeStr := string(currNodeId)

	condStr := x.getCondition()
	if condStr == "" {
		ctx.TellNext(msg) //结束流程了
		return
	}
	nodeParams, err := NodeParams(allParams, "", x.Configuration.ArgTemplate, x.Configuration.Arguments)
	if err != nil {
		ctx.TellFailure(msg, err)
		return
	}

	conResult, err := x.ruleObj.RunString(condStr, nodeParams)
	if err != nil {
		ctx.TellFailure(msg, err)
		return
	}

	durationMs := time.Now().UnixMilli() - startTime

	isBool := cond.IsBool(conResult)
	if isBool {
		boolResult, err := conv.Convert[bool](conResult)
		if err != nil {
			nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, durationMs, nodeStr, "", "fail", "error", allParams, nodeParams, err)
			if cliErr == nil {
				x.nodeLogCli = nodeCli
			}
			ctx.TellFailure(msg, err)
			return
		}
		nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, durationMs, nodeStr, "", "success", "info", allParams, nodeParams, nil)
		if cliErr == nil {
			x.nodeLogCli = nodeCli
		}
		if boolResult {
			ctx.TellNext(msg, types.True)
		} else {
			ctx.TellNext(msg, types.False)
		}
		return
	}
	if retStr, ok := conResult.(string); ok {
		nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, durationMs, nodeStr, "", "success", "info", allParams, nodeParams, nil)
		if cliErr == nil {
			x.nodeLogCli = nodeCli
		}
		ctx.TellNext(msg, retStr)
		return
	}
	// 默认使用Success和Failure
	changeBool, err := conv.Convert[bool](conResult)
	if err != nil {
		nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, durationMs, nodeStr, "", "fail", "error", allParams, nodeParams, err)
		if cliErr == nil {
			x.nodeLogCli = nodeCli
		}
		// 如果出错，则采用转为字符串来进行处理
		ctx.TellNext(msg, conv.String(conResult))
		return
	}

	nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, durationMs, nodeStr, "", "success", "info", allParams, nodeParams, nil)
	if cliErr == nil {
		x.nodeLogCli = nodeCli
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
