package commnode

import (
	"fmt"

	"github.com/magic-lib/go-plat-utils/cond"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/rulego/rulego/api/types"
)

// routeByCondition 对给定的条件表达式求值，并返回应下发的 relationType 与原始表达式结果。
// 路由语义与 condSwitchNode 完全一致：
//   - 结果为 bool → relationType = True/False
//   - 结果为 string → relationType = 该字符串（即自定义 relationType，可对应任意下游连线名）
//   - 其他类型 → 尝试转为 bool → Success/Failure；转失败则使用字符串化结果作为 relationType
//
// expr 为空时调用方不应进入此函数（调用方应直接走默认分支）。
// 注意：本函数只负责求值与判定 relationType，不负责写 node 运行日志与 TellNext，
// 这些由调用方根据自身上下文（参数、耗时、返回值等）完成。
func routeByCondition(ruleObj *templates.RuleExprEngine, expr string, params map[string]any) (relationType string, result any, err error) {
	conResult, runErr := ruleObj.RunString(expr, params)
	if runErr != nil {
		return "", nil, runErr
	}

	isBool := cond.IsBool(conResult)
	if isBool {
		boolResult, convErr := conv.Convert[bool](conResult)
		if convErr != nil {
			return "", conResult, fmt.Errorf("routeByCondition convert bool failed: %w", convErr)
		}
		rt := types.True
		if !boolResult {
			rt = types.False
		}
		return rt, boolResult, nil
	}

	if relationTypeTemp, ok := conResult.(string); ok {
		return relationTypeTemp, relationTypeTemp, nil
	}

	// 默认分支：尝试转为 bool，走 Success/Failure
	changeBool, convErr := conv.Convert[bool](conResult)
	if convErr != nil {
		// 无法转为 bool：退化为字符串化结果作为 relationType
		return conv.String(conResult), conResult, nil
	}
	rt := types.Success
	if !changeBool {
		rt = types.Failure
	}
	return rt, changeBool, nil
}
