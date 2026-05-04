package flow_step

import (
	"context"
	"fmt"
	"github.com/hashicorp/go-multierror"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/goroutines"
	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/magic-lib/go-plat-workflow/common"
	"github.com/magic-lib/go-plat-workflow/task"
	"github.com/magic-lib/go-plat-workflow/tools"
	"github.com/samber/lo"
	"log"
	"time"
)

type stepConfig struct {
	checkCondition bool //condition的结果
}

// Step 表示工作流中的一个步骤,包含一组条件触发的actions
type Step struct {
	Id               string           `yaml:"id" json:"id,omitempty"`                 // 该步骤Id
	Name             string           `yaml:"name" json:"name,omitempty"`             // 步骤名称
	DependsOn        []*task.Activity `yaml:"depends_on" json:"depends_on,omitempty"` // 手动填入依赖
	Condition        string           `yaml:"condition" json:"condition,omitempty"`   // 执行条件表达式
	task.FlowControl                  // 流程控制
	Strategy         []*task.Activity `yaml:"strategy" json:"strategy,omitempty"` // 策略列表
	stepConfig       stepConfig
}

// extractDependenciesFromCondition 从条件表达式中提取依赖的 activity IDs
func (s *Step) extractDependenciesFromCondition() []string {
	if s.Condition == "" {
		return nil
	}
	keyPrefix := task.ReturnKeyPrefix()
	actIdList := tools.ExtractDependsActivityIds(s.Condition, keyPrefix)
	return lo.Uniq(actIdList)
}

// GetAllDependencies 获取该步骤的所有依赖（包括显式声明和从条件中提取的）
func (s *Step) getAllDependencies() []*task.Activity {
	deps := make([]*task.Activity, 0)
	mapDeps := make(map[string]*task.Activity)
	// 从条件表达式中提取依赖
	conditionDeps := s.extractDependenciesFromCondition()
	for _, depId := range conditionDeps {
		mapDeps[depId] = &task.Activity{
			Id: depId,
		}
	}

	// 添加显式声明的依赖
	for _, dep := range s.DependsOn {
		if dep.Id != "" {
			mapDeps[dep.Id] = dep
			continue
		}
		deps = append(deps, dep)
	}
	for _, dep := range mapDeps {
		deps = append(deps, dep)
	}
	return deps
}

// checkCondition 检查步骤的执行条件是否满足
func (s *Step) checkCondition(inputParams map[string]any) (bool, error) {
	if s.Condition == "" {
		return true, nil
	}

	ruleExpr := templates.NewRuleExprEngine()
	checkResult, err := ruleExpr.RunString(s.Condition, inputParams)
	if err != nil {
		return false, fmt.Errorf("条件解析失败 condition: %s error: %w", s.Condition, err)
	}

	resultBool, err := conv.Convert[bool](checkResult)
	if err != nil {
		return false, fmt.Errorf("条件解析返回不是bool: %s, %w", s.Condition, err)
	}

	log.Printf("Step [%s] condition check result: %v", s.Name, resultBool)
	return resultBool, nil
}

// getTimeoutDuration 获取超时时长
func (s *Step) getTimeoutDuration() time.Duration {
	if s.Timeout <= 0 {
		return 0
	}
	return time.Duration(s.Timeout) * time.Second
}

// Execute 执行步骤主逻辑
func (s *Step) Execute(ctx context.Context, inputParams map[string]any) (map[string]any, error) {
	log.Printf("Step [%s] starting", s.Name)

	// 应用步骤级别的超时控制
	execCtx := ctx
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, s.getTimeoutDuration())
		defer cancel()
	}

	// 处理延迟执行
	if s.DelayDuration > 0 {
		select {
		case <-time.After(time.Duration(s.DelayDuration) * time.Second):
		case <-execCtx.Done():
			return inputParams, execCtx.Err()
		}
	} else if s.DelayDuration < 0 {
		// 异步执行
		goroutines.GoAsync(func(params ...any) {
			_, _ = s.executeStepLogic(execCtx, inputParams)
		})
		return inputParams, nil
	}

	// 同步执行
	return s.executeStepLogic(execCtx, inputParams)
}

func (s *Step) executeAllDependencies(ctx context.Context, args map[string]any) (map[string]any, error) {
	var retErr error
	actList := s.getAllDependencies()
	lo.ForEachWhile(actList, func(act *task.Activity, index int) bool {
		newArgs, err := act.Execute(ctx, args)
		if err != nil {
			log.Print("execute activity error:", err)
			retErr = err
			return false
		}
		args = lo.Assign(args, newArgs)
		return true
	})
	return args, retErr
}
func (s *Step) executeAllStrategy(ctx context.Context, args map[string]any) (map[string]any, error) {
	if len(s.Strategy) == 0 {
		return args, nil
	}
	var retErr error
	lo.ForEachWhile(s.Strategy, func(act *task.Activity, index int) bool {
		newArgs, err := act.Execute(ctx, args)
		if err != nil {
			log.Print("execute activity error:", err)
			retErr = multierror.Append(retErr, err)
			return true
		}
		args = lo.Assign(args, newArgs)
		return true
	})
	return args, retErr
}

func (s *Step) mergeResponseWithId(keyPrefix string, resultMap map[string]any, shouldExecute bool) map[string]any {
	if s.Id == "" {
		return resultMap
	}

	idKey := s.Id
	if keyPrefix != "" {
		idKey = keyPrefix + idKey
	}

	return lo.Assign(resultMap, map[string]any{
		idKey: map[string]any{
			common.Responses: shouldExecute,
		},
	})
}

// executeStepLogic 执行步骤的核心逻辑
func (s *Step) executeStepLogic(ctx context.Context, inputParams map[string]any) (map[string]any, error) {
	{ //执行depends
		var err error
		inputParams, err = s.executeAllDependencies(ctx, inputParams)
		if err != nil {
			return inputParams, err
		}
	}

	log.Printf("Step [%s] execute depend: %s", s.Name, conv.String(inputParams))

	// 1. 检查执行条件
	shouldExecute, err := s.checkCondition(inputParams)
	if err != nil {
		return inputParams, err
	}
	keyPrefix := task.ReturnKeyPrefix()
	inputParams = s.mergeResponseWithId(keyPrefix, inputParams, shouldExecute)

	s.setStepConfig(shouldExecute)

	if !shouldExecute {
		log.Printf("Step [%s] condition not met, skipping execution", s.Name)
		return inputParams, nil
	}

	// 2. 如果没有策略，直接返回
	if len(s.Strategy) == 0 {
		log.Printf("Step [%s] has no strategy to execute", s.Name)
		return inputParams, nil
	}

	inputParams, err = s.executeAllStrategy(ctx, inputParams)
	log.Printf("Step [%s] successfully", s.Name)
	return inputParams, err
}

func (s *Step) setStepConfig(isCheckCondition bool) {
	s.stepConfig.checkCondition = isCheckCondition
}
