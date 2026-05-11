package action

import (
	"fmt"
	"github.com/magic-lib/go-plat-utils/plugins"
)

// RegisterAction 注册全局Action方法
func RegisterAction(ai Actor) error {
	if ai == nil {
		return fmt.Errorf("actor is nil")
	}
	am := ai.ActMeta()
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
