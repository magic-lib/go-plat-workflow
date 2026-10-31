package commnode

import (
	"fmt"
	"github.com/magic-lib/go-plat-utils/cond"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/rulego/rulego/api/types"
	"log"
	"strconv"
	"strings"
)

// relationFloatPrecision 小数作为 relationType 时保留的小数位数，
// 用于消除浮点误差（如 0.1+0.2=0.30000000000000004 → "0.3"），使连线上配置的值能稳定匹配
const relationFloatPrecision = 6

// routeByCondition 对给定的条件表达式求值，并返回应下发的 relationType 与原始表达式结果。
// 路由语义与 condSwitchNode 完全一致：
//   - 结果为 bool → relationType = True/False
//   - 结果为 string → relationType = 该字符串（即自定义 relationType，可对应任意下游连线名）
//   - 结果为数字 → relationType = 数字的字符串形式（如 200 → "200"），不再转 bool，避免非 0 一律走 Success；
//     小数按 relationFloatPrecision 位四舍五入并去掉末尾的 0（如 3.0000000001 → "3"、4.0 → "4"、3.3333333333 → "3.333333"）
//   - 其他类型 → 尝试转为 bool → Success/Failure；转失败则使用字符串化结果作为 relationType
//
// expr 为空时调用方不应进入此函数（调用方应直接走默认分支）。
// 注意：本函数只负责求值与判定 relationType，不负责写 node 运行日志与 TellNext，
// 这些由调用方根据自身上下文（参数、耗时、返回值等）完成。
func routeByCondition(ruleObj *templates.RuleExprEngine, expr string, params map[string]any) (relationType string, result any, err error) {
	conResult, runErr := ruleObj.RunString(expr, params)
	log.Printf("[routeByCondition] expr=%s params=%s", expr, conv.String(params))

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

	// 数字结果按字符串作为自定义 relationType（表达式引擎算出的数字一般为 float64，如 2 → "2"）
	if cond.IsNumeric(conResult) {
		return numberToRelationType(conResult), conResult, nil
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

// numberToRelationType 将数字结果转为 relationType 字符串：
// 小数按 relationFloatPrecision 位四舍五入后去掉末尾的 0 和多余的小数点，其他数字直接字符串化。
func numberToRelationType(v any) string {
	var f float64
	switch n := v.(type) {
	case float64:
		f = n
	case float32:
		f = float64(n)
	default:
		return conv.String(v)
	}
	str := strconv.FormatFloat(f, 'f', relationFloatPrecision, 64)
	str = strings.TrimRight(str, "0")
	str = strings.TrimSuffix(str, ".")
	if str == "-0" {
		// 如 -0.0000001 四舍五入后为 -0
		return "0"
	}
	return str
}
