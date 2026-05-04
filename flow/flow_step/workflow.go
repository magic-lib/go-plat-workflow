package flow_step

import (
	"context"
	"fmt"
	"github.com/hashicorp/go-multierror"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/magic-lib/go-plat-utils/utils/httputil/param"
	"github.com/magic-lib/go-plat-workflow/common"
	"github.com/magic-lib/go-plat-workflow/task"
	"github.com/magic-lib/go-plat-workflow/tools"
	"github.com/samber/lo"
	"log"
)

type (
	Workflow struct {
		Name       string              `yaml:"name" json:"name,omitempty"`             // 工作流名称
		Variables  []*param.BindConfig `yaml:"variables" json:"variables,omitempty"`   // 全局变量
		Responses  map[string]any      `yaml:"responses" json:"responses,omitempty"`   // 请求最终返回的结构
		Activities []*task.Activity    `yaml:"activities" json:"activities,omitempty"` // 公共的activity资源，用于公共执行的部分,比如公共打日志，可以提高使用率
		Steps      []*Step             `yaml:"steps" json:"steps,omitempty"`
	}
)

// mergeVariables 合并前端输入的所有变量
func (w *Workflow) mergeVariables(inputParams map[string]any) map[string]any {
	newInputParams := tools.CloneMap(inputParams)
	if len(w.Variables) == 0 {
		newInputParams[common.Variables] = inputParams
		return newInputParams
	}
	result := param.MergeArgumentsByBinding(inputParams, w.Variables)
	if len(result) > 0 {
		newInputParams = lo.Assign(newInputParams, result)
		newInputParams[common.Variables] = result
	}
	return newInputParams
}

// execActivityById 根据 activityId 执行对应的 activity, 从配置的Activities中获取对应的 activity
func (w *Workflow) execActivityById(ctx context.Context, activityId string, inputParams map[string]any) (map[string]any, error) {
	if activityId == "" {
		return inputParams, fmt.Errorf("not find activity activityId is empty")
	}
	if len(w.Activities) == 0 {
		return inputParams, fmt.Errorf("activities empty: %s", activityId)
	}
	oneAct := lo.FindOrElse(w.Activities, nil, func(activity *task.Activity) bool {
		return activity.Id == activityId
	})
	if oneAct == nil {
		return inputParams, fmt.Errorf("not find activity from activities: %s", activityId)
	}
	newResult, err := oneAct.Execute(ctx, inputParams)
	if err != nil {
		log.Printf("执行公共 activity [%s] 失败: %v", oneAct.Id, err)
		return inputParams, err
	}
	inputParams = lo.Assign(inputParams, newResult)
	return inputParams, nil
}

// execOneStepDependency 运行一个步骤的依赖activity
func (w *Workflow) execOneStepDependencyFromActivities(ctx context.Context, frontSteps []*Step, oneStep *Step, inputParams map[string]any) (map[string]any, error) {
	allDepends := oneStep.getAllDependencies()
	var retErr error
	lo.ForEachWhile(allDepends, func(activity *task.Activity, _ int) bool {
		if activity.Activity != "" {
			//内部有指定需要执行的方法，就不用外部来调用了
			return true
		}
		result, err := w.execActivityById(ctx, activity.Id, inputParams)
		if err != nil {
			//看是否是前面step的Id，如果是，则不用执行该activity了
			findStepId := lo.FindOrElse(frontSteps, nil, func(step *Step) bool {
				return step.Id == activity.Id
			})
			if findStepId == nil {
				log.Printf("执行公共 activity [%s] 失败: %v", activity.Id, err)
				retErr = multierror.Append(retErr, err)
			}
			return true
		}
		inputParams = lo.Assign(inputParams, result)
		return true
	})
	if retErr != nil {
		return inputParams, retErr
	}
	return inputParams, nil
}

// Execute 执行工作流
func (w *Workflow) Execute(ctx context.Context, inputParams map[string]any) (map[string]any, error) {
	log.Printf("工作流 [%s] 开始执行", w.Name)
	if len(w.Steps) == 0 {
		log.Printf("工作流 [%s] 没有步骤需要执行", w.Name)
		return inputParams, nil
	}
	execCtx := ctx
	inputParams = w.mergeVariables(inputParams)

	var retErr error

	frontSteps := make([]*Step, 0)

	lo.ForEachWhile(w.Steps, func(step *Step, _ int) bool {
		frontSteps = append(frontSteps, step)
		select {
		case <-execCtx.Done():
			log.Printf("工作流 [%s] 执行被取消", w.Name)
			retErr = multierror.Append(retErr, execCtx.Err())
			return false
		default:
		}
		var err error
		inputParams, err = w.execOneStepDependencyFromActivities(execCtx, frontSteps, step, inputParams)
		if err != nil {
			if step.FlowControl.ShouldIgnoreOnError() {
				return true
			}
			retErr = multierror.Append(retErr, err)
			return false
		}

		log.Printf("执行当前步骤 [%s]", step.Id)
		result, err := step.Execute(execCtx, inputParams)
		if err != nil {
			if step.FlowControl.ShouldIgnoreOnError() {
				return true
			}
			retErr = multierror.Append(retErr, err)
			return false
		}
		inputParams = lo.Assign(inputParams, result)

		if step.stepConfig.checkCondition {
			// 如果成功，则直接退出
			if step.FlowControl.ShouldExitOnExecute() {
				return false
			}
			if step.FlowControl.ShouldContinueOnExecute() {
				return true
			}
		}

		if step.FlowControl.ShouldReturnOnExecute() {
			log.Printf("步骤 [%s] 配置为执行后返回，后续步骤将异步去执行", step.Id)
			panic("on_return")
			//go func(remainingSteps []*Step, asyncResult map[string]any) {
			//	for _, remainingStep := range remainingSteps {
			//		_, _ = remainingStep.Execute(execCtx, asyncResult)
			//	}
			//}(w.getRemainingSteps(sortedSteps, step), inputParams)
			return false
		}

		return true
	})

	result := w.buildFinalResponse(inputParams)

	if retErr != nil {
		log.Printf("工作流 [%s] 执行完成，但出现错误: %v", w.Name, retErr)
	} else {
		log.Printf("工作流 [%s] 执行成功", w.Name)
	}

	return result, retErr
}

// buildFinalResponse 根据 responses 配置构建最终响应
func (w *Workflow) buildFinalResponse(result map[string]any) map[string]any {
	if len(w.Responses) == 0 {
		return result
	}
	newResult := make(map[string]any)
	ruleExpr := templates.NewRuleExprEngine()
	for key, value := range w.Responses {
		if value == "" {
			newResult[key] = ""
			continue
		}
		checkResult, err := ruleExpr.RunString(conv.String(value), result)
		if err != nil {
			log.Printf("解析工作流 [%s] 响应失败: %v", w.Name, err)
		}
		newResult[key] = checkResult
	}
	if len(newResult) > 0 {
		result = lo.Assign(result, newResult)
		result[common.Responses] = newResult
	}
	return result
}
