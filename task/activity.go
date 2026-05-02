package task

import (
	"context"
	"fmt"
	"github.com/magic-lib/go-plat-cache/cache"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/goroutines"
	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/magic-lib/go-plat-utils/utils"
	"github.com/magic-lib/go-plat-utils/utils/httputil/param"
	"github.com/magic-lib/go-plat-workflow/action"
	"github.com/magic-lib/go-plat-workflow/common"
	"github.com/magic-lib/go-plat-workflow/tools"
	"github.com/samber/lo"
	"log"
	"time"
)

var (
	activityCacheTime = 5 * time.Minute
	activityCache     = cache.NewMemGoCache[map[string]any](activityCacheTime, 10*time.Minute)
)

type (
	// Activity 单个 task 配置
	Activity struct {
		Id          string              `yaml:"id" json:"id,omitempty"` // 唯一标识，用于区分多个相同的action
		Namespace   string              `yaml:"namespace" json:"namespace,omitempty"`
		Activity    string              `yaml:"activity" json:"activity,omitempty"`
		Arguments   []*param.BindConfig `yaml:"arguments" json:"arguments,omitempty"`
		ArgTemplate string              `yaml:"arg_template" json:"arg_template,omitempty"` // 如果参数直接是数字的话，则这里需要改为获取数字类型
		Responses   map[string]any      `yaml:"responses" json:"responses"`                 // 返回的参数map，可以自定义添加内容，比如命名转换
		DependsOn   []*Activity         `yaml:"depends_on" json:"depends_on,omitempty"`     // 依赖别的activity的Id
		Hooks       LifecycleHooks      `yaml:"hooks" json:"hooks,omitempty"`               // activity执行时的钩子程序
		Control     ActivityControl     `yaml:"control" json:"control,omitempty"`           // 该activity的控制面板
		Desc        string              `yaml:"desc" json:"desc"`
	}
)

// extractDependenciesFromArguments 从 Arguments 中自动提取依赖的 activity IDs
func (ac *Activity) extractDependenciesFromArguments(keyPrefix string) []string {
	deps := make(map[string]bool)

	for _, arg := range ac.Arguments {
		if arg.Value != nil {
			valueStr := conv.String(arg.Value)
			matches := tools.ExtractDependsActivityIds(valueStr, keyPrefix)
			for _, match := range matches {
				deps[match] = true
			}
		}
	}

	// 也从 ArgTemplate 中提取
	if ac.ArgTemplate != "" {
		matches := tools.ExtractDependsActivityIds(ac.ArgTemplate, keyPrefix)
		for _, match := range matches {
			deps[match] = true
		}
	}

	// 条件中提取
	if ac.Control.When != "" {
		matches := tools.ExtractDependsActivityIds(ac.Control.When, keyPrefix)
		for _, match := range matches {
			deps[match] = true
		}
	}

	var result []string
	for dep := range deps {
		result = utils.AppendUniq(result, dep)
	}
	return result
}

// GetDependsOnIds 获取有效的依赖列表
func (ac *Activity) GetDependsOnIds() []string {
	keyPrefix := returnKeyPrefix
	oneDependsOn := ac.extractDependenciesFromArguments(keyPrefix)
	if len(ac.DependsOn) == 0 {
		return oneDependsOn
	}
	lo.ForEach(ac.DependsOn, func(dep *Activity, index int) {
		oneDependsOn = utils.AppendUniq(oneDependsOn, dep.Id)
	})
	return oneDependsOn
}

func (ac *Activity) getResponseStoreKey(keyPrefix string, linkChar string) string {
	activityName := ""
	if ac.Namespace == "" {
		activityName = ac.Activity
	} else {
		activityName = ac.Namespace + linkChar + ac.Activity
	}
	return keyPrefix + activityName
}

func (ac *Activity) mergeResponseWithId(keyPrefix string, resultMap map[string]any, actionFun action.Actor, requestParams any, retData any) map[string]any {
	if ac.Id == "" {
		return resultMap
	}

	if actionFun != nil {
		newParam, err := conv.ConvertForType(actionFun.ActMeta().ArgumentType, requestParams)
		if err == nil {
			requestParams = newParam
		}
	}

	idKey := ac.Id
	if keyPrefix != "" {
		idKey = keyPrefix + idKey
	}

	return lo.Assign(resultMap, map[string]any{
		idKey: map[string]any{
			common.Arguments: requestParams,
			common.Responses: retData,
		},
	})
}

// createResponse 生成返回的结果
func (ac *Activity) createResponse(keyPrefix string, linkChar string, actionFun action.Actor, requestParams any, retData any) map[string]any {
	resultMap := make(map[string]any)

	//param
	paramMap := make(map[string]any)
	_ = conv.Unmarshal(requestParams, &paramMap)
	if len(paramMap) > 0 {
		resultMap = lo.Assign(resultMap, paramMap)
	}

	//return
	retMap := make(map[string]any)
	_ = conv.Unmarshal(retData, &retMap)
	if len(retMap) == 0 {
		// 不是map类型，所以这里需要保存返回值
		actionKey := ac.getResponseStoreKey(keyPrefix, linkChar)
		retMap[actionKey] = retData
	}
	resultMap = lo.Assign(resultMap, retMap)

	return ac.mergeResponseWithId(keyPrefix, resultMap, actionFun, requestParams, retData)
}

// Execute 执行动作主逻辑：合并参数→执行依赖→执行主动作→合并结果
func (ac *Activity) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	// 0、获取当前活动的所有参数
	inputParams := ac.makeInputMap(args)
	log.Print("action start param:", conv.String(inputParams))

	execCtx := ctx
	if ac.Control.Timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, time.Duration(ac.Control.Timeout)*time.Second)
		defer cancel()
	}

	keyPrefix := returnKeyPrefix
	linkChar := namespaceLinkNameChar

	if ac.Control.DelayDuration > 0 {
		select {
		case <-time.After(time.Duration(ac.Control.DelayDuration) * time.Second):
		case <-execCtx.Done():
			return inputParams, execCtx.Err()
		}
	} else if ac.Control.DelayDuration < 0 {
		_, err := goroutines.GoAsyncTimeout(time.Duration(ac.Control.Timeout)*time.Second, func(paramsIn ...any) (map[string]any, error) {
			_, err := ac.execThisAction(keyPrefix, linkChar, execCtx, inputParams)
			if err != nil {
				return nil, err
			}
			return nil, nil
		})
		if err != nil {
			return inputParams, err
		}
		return inputParams, nil
	}
	return ac.execThisAction(keyPrefix, linkChar, execCtx, inputParams)
}

func (ac *Activity) execThisAction(keyPrefix, linkChar string, execCtx context.Context, inputParams map[string]any) (map[string]any, error) {
	actionKey := ""
	paramKey := ""
	if ac.Control.CacheTime > 0 {
		actionKey = ac.getResponseStoreKey(keyPrefix, linkChar)
		paramKey, _ = utils.UniqueJsonId(inputParams)
		//是否有缓存
		actionResult, err := cache.NsGet[map[string]any](execCtx, activityCache, actionKey, paramKey)
		if err == nil {
			return actionResult, nil
		}
	}

	if ac.Control.When != "" {
		var checkBool bool
		var err error
		checkBool, inputParams, err = ac.execThisWhen(execCtx, inputParams)
		log.Print("execThisAction execThisWhen:", checkBool, conv.String(inputParams), err)
		if err != nil {
			return inputParams, fmt.Errorf("条件解析失败 when: %s error: %w, ", ac.Control.When, err)
		}
		if !checkBool {
			return inputParams, nil
		}
	}

	actionFun, err := action.GetAction(ac.Namespace, ac.Activity)
	if err != nil {
		return inputParams, err
	}

	retData, err := ac.executeByHookEvent(execCtx, linkChar, LifecycleEventOnStart, inputParams)
	if err != nil {
		return inputParams, err
	}

	if len(retData) > 0 {
		inputParams = tools.MergeNewArguments(inputParams, retData)
	}

	var actionFunParam any = inputParams
	if ac.ArgTemplate != "" {
		ruleExpr := templates.NewRuleExprEngine()
		newArgs, err := ruleExpr.RunString(ac.ArgTemplate, inputParams)
		if err == nil {
			actionFunParam = newArgs
		}
	}

	execRetData, err := actionFun.Execute(execCtx, actionFunParam)
	resultMap := ac.createResponse(keyPrefix, linkChar, actionFun, actionFunParam, execRetData)

	log.Print("baseFlow task return:", conv.String(resultMap), err)

	var onErr error

	retData, onErr = ac.executeByHookEvent(execCtx, linkChar, LifecycleEventOnComplete, inputParams)
	if onErr == nil {
		if len(retData) > 0 {
			resultMap = tools.MergeNewArguments(resultMap, retData)
		}
	}
	if err != nil {
		retData, onErr = ac.executeByHookEvent(execCtx, linkChar, LifecycleEventOnError, inputParams)
		if onErr == nil {
			if len(retData) > 0 {
				resultMap = tools.MergeNewArguments(resultMap, retData)
			}
		}
		return resultMap, err
	}

	// 需要缓存该执行对象
	if ac.Control.CacheTime > 0 {
		_, _ = cache.NsSet[map[string]any](execCtx, activityCache, actionKey, paramKey, resultMap, time.Duration(ac.Control.CacheTime)*time.Second)
	}

	retData, onErr = ac.executeByHookEvent(execCtx, linkChar, LifecycleEventOnSuccess, inputParams)
	if onErr == nil {
		if len(retData) > 0 {
			resultMap = tools.MergeNewArguments(resultMap, retData)
		}
	}
	resultMap = tools.MergeNewArguments(resultMap, inputParams)
	return resultMap, nil
}
func (ac *Activity) execThisWhen(execCtx context.Context, inputParams map[string]any) (bool, map[string]any, error) {
	//if len(ac.Control.WhenDepends) > 0 {
	//	var retErr error
	//	lo.ForEachWhile(ac.Control.WhenDepends, func(item *Activity, index int) bool {
	//		oneReturn, err := item.Execute(execCtx, inputParams)
	//		if err != nil {
	//			retErr = err
	//			return false
	//		}
	//		retMap := ac.makeInputMap(oneReturn)
	//		inputParams = tools.MergeNewArguments(inputParams, retMap)
	//		return true
	//	})
	//	if retErr != nil {
	//		return false, inputParams, retErr
	//	}
	//}

	ruleExpr := templates.NewRuleExprEngine()
	checkResult, err := ruleExpr.RunString(ac.Control.When, inputParams)
	if err != nil {
		return false, inputParams, fmt.Errorf("条件: %s", ac.Control.When)
	}
	resultBool, err := conv.Convert[bool](checkResult)
	if err != nil {
		return false, inputParams, fmt.Errorf("条件解析返回不是bool: %s, %w", ac.Control.When, err)
	}
	return resultBool, inputParams, nil
}

func (ac *Activity) makeInputMap(arguments map[string]any) map[string]any {
	args := tools.CloneMap(arguments)

	//2、将是自己id的参数覆盖进来
	if ac.Id != "" {
		if oneParam, ok := args[ac.Id]; ok {
			if oneParamMap, ok1 := oneParam.(map[string]any); ok1 {
				args = lo.Assign(args, oneParamMap)
			}
		}
	}

	// 本身的参数列表是否包含
	//3、activity中自定义进行覆盖，主要是将前面流程的参数和返回值加到里面
	args = param.MergeArgumentsByBinding(args, ac.Arguments)

	return args
}

// ExecuteByHookEvent 检查控制条件是否满足（简化实现，实际可集成表达式引擎）
func (ac *Activity) executeByHookEvent(ctx context.Context, linkChar string, event LifecycleEvent, args map[string]any) (map[string]any, error) {
	if len(ac.Hooks) == 0 {
		return nil, nil
	}
	if oneAct, ok := ac.Hooks[event]; ok {
		if oneAct.Id == "" && ac.Id != "" {
			oneAct.Id = ac.Id + linkChar + string(event)
		}
		return oneAct.Execute(ctx, args)
	}
	return nil, nil
}
