package tools

import (
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/samber/lo"
)

func createMap(data any) map[string]any {
	m := make(map[string]any)
	_ = conv.Unmarshal(conv.String(data), &m)
	return m
}

func CloneMap(data map[string]any) map[string]any {
	m := make(map[string]any)
	err := conv.Unmarshal(conv.String(data), &m)
	if err == nil {
		return m
	}
	return lo.Assign(data)
}

func MergeNewArguments(oldArgs map[string]any, args map[string]any) map[string]any {
	if len(args) == 0 {
		return oldArgs
	}
	jsonTemplate := templates.NewJsonMapTemplate()
	newArgs, err := jsonTemplate.Replace(oldArgs, args)
	if err == nil {
		_ = conv.Unmarshal(newArgs, &oldArgs)
	}
	for key, val := range args {
		if _, ok := oldArgs[key]; !ok {
			oldArgs[key] = val
		}
	}
	return oldArgs
}
