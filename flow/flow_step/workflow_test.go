package flow_step_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/utils/httputil/param"
	"github.com/magic-lib/go-plat-workflow/flow/flow_step"
	"github.com/magic-lib/go-plat-workflow/task"
)

func TestWorkflowExecute(t *testing.T) {
	registerAction()

	workflow := &flow_step.Workflow{
		Name: "测试工作流",
		Variables: []*param.BindConfig{
			{Key: "userId", Value: "U123456"},
			{Key: "orderId", Value: "ORD789012"},
		},
		Activities: []*task.Activity{
			{
				Id:        "commonLog",
				Namespace: ns,
				Activity:  "Log",
				Arguments: []*param.BindConfig{
					{Key: "message", Value: "工作流开始执行"},
				},
			},
		},
		Steps: []*flow_step.Step{
			{
				Id:   "step1",
				Name: "第一步：获取订单信息",
				Strategy: []*task.Activity{
					createTestActivity("getOrder", ns, "GetOrderInfo", "", []*param.BindConfig{
						{Key: "name", Value: "{{variables.userId}}"},
						{Key: "group", Value: "M07"},
					}),
				},
			},
			{
				Id:        "step2",
				Name:      "第二步：条件设置订单",
				DependsOn: []*task.Activity{{Id: "step1"}},
				Condition: "[step1.responses]",
				Strategy: []*task.Activity{
					createTestActivity("setOrder", ns, "SetOrderInfo", "{{variables.orderId}}", []*param.BindConfig{
						{Key: "orderId", Value: "{{variables.orderId}}"},
					}),
				},
			},
		},
		Responses: map[string]any{
			"orderName": "{{step1.responses}}",
			"success":   "{{step2.responses}}",
		},
	}

	ctx := context.Background()
	inputParams := map[string]any{
		"orderId": "tianlin0",
	}

	result, err := workflow.Execute(ctx, inputParams)

	fmt.Printf("工作流执行结果:\n%s\n", conv.String(result))
	if err != nil {
		fmt.Printf("工作流执行错误: %v\n", err)
	}
}
