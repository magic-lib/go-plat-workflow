package rulegox

import (
	"fmt"
	"github.com/rulego/rulego/api/types"
)

var _ types.BeforeAspect = (*SubChainInjectAspect)(nil)

// SubChainInjectAspect 实现 BeforeAdvice
type SubChainInjectAspect struct{}

func (a *SubChainInjectAspect) Order() int {
	// 该切面用于注入子链参数，需在其它切面之前执行，故返回较小顺序值
	return 0
}

func (a *SubChainInjectAspect) New() types.Aspect {
	return &SubChainInjectAspect{}
}

func (a *SubChainInjectAspect) PointCut(ctx types.RuleContext, msg types.RuleMsg, relationType string) bool {
	fmt.Println("SubChainInjectAspect PointCut", relationType)

	return ctx.Self().Type() == "flow"
}

func (a *SubChainInjectAspect) Before(ctx types.RuleContext, msg types.RuleMsg, relationType string) types.RuleMsg {
	ctx.RuleChain().Config()

	return msg
}
