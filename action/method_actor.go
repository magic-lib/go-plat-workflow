package action

import (
	"fmt"
	"github.com/magic-lib/go-plat-utils/utils"
	"reflect"
)

// MethodToActor 转换为Actor
func MethodToActor[I, O any](method any, ac *ActMeta) (Actor, error) {
	if ac == nil || method == nil {
		return nil, fmt.Errorf("data or method is nil")
	}
	if ac.Activity == "" {
		return nil, fmt.Errorf("action name is empty")
	}

	newMethod, err := utils.ContextMethodToAnyHandler[I, O](method)
	if err != nil {
		return nil, err
	}
	if ac.ArgumentType == nil {
		var zero I
		ac.ArgumentType = reflect.TypeOf(zero)
	}
	ac.actionMethod = newMethod
	return ac, nil
}
