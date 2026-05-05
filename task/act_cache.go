package task

import (
	"context"
)

type flowCacheContext struct{}

func getFlowCache(ctx context.Context) map[string]map[string]any {
	if cache, ok := ctx.Value(flowCacheContext{}).(map[string]map[string]any); ok {
		return cache
	}
	return nil
}

func WithFlowCache(ctx context.Context) context.Context {
	if getFlowCache(ctx) != nil {
		return ctx
	}
	cache := make(map[string]map[string]any)
	return context.WithValue(ctx, flowCacheContext{}, cache)
}

func setFlowCacheResult(ctx context.Context, actionKey, paramKey string, result map[string]any) {
	cache := getFlowCache(ctx)
	if cache == nil {
		return
	}
	cacheKey := actionKey + ":" + paramKey
	cache[cacheKey] = result
}

func getFlowCacheResult(ctx context.Context, actionKey, paramKey string) (map[string]any, bool) {
	cache := getFlowCache(ctx)
	if cache == nil {
		return nil, false
	}
	cacheKey := actionKey + ":" + paramKey
	result, ok := cache[cacheKey]
	return result, ok
}
