package action

import (
	"fmt"
	"github.com/magic-lib/go-plat-utils/plugins"
	"github.com/magic-lib/go-plat-utils/utils"
	"reflect"
)

// ChangeToActor 转换为Actor
func ChangeToActor[I, O any](method any, ac *ActionMeta) (Actor, error) {
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

// RegisterAction 注册全局Action方法
func RegisterAction(ai Actor) error {
	if ai == nil {
		return fmt.Errorf("actor is nil")
	}
	am := ai.ActionMeta()
	if am == nil || am.Activity == "" {
		return fmt.Errorf("activity name is empty")
	}
	return plugins.Register(am.Namespace, ai)
}

// GetAction 获取Action方法
func GetAction(ns string, activity string) (Actor, error) {
	ai, err := plugins.Load(ns, activity)
	if err != nil {
		return nil, fmt.Errorf("action err: %v is not registered, ns:%s, action:%s", err, ns, activity)
	}
	am, ok := ai.(Actor)
	if ok {
		return am, nil
	}
	return nil, fmt.Errorf("action %s is not registered, ns:%s, action:%s", activity, ns, activity)
}
