package flow_step_test

import (
	"context"
	"fmt"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/utils/httputil/param"
	"github.com/magic-lib/go-plat-workflow/action"
	"github.com/magic-lib/go-plat-workflow/flow/flow_step"
	"github.com/magic-lib/go-plat-workflow/task"
	"github.com/samber/lo"
	"log"
	"testing"
)

type Order struct {
	Name  string `json:"name"`
	Group string `json:"group"`
}

func (o *Order) GetOrderName(ctx context.Context, id int) (string, error) {
	if id == 0 {
		return "", fmt.Errorf("err:no id")
	}
	return fmt.Sprintf("%d", id+1), nil
}

func (o *Order) GetMemberGroup(ctx context.Context, id int) (string, error) {
	if id == 0 {
		return "", fmt.Errorf("err:no id")
	}
	return "M8", nil
}
func (o *Order) GetOrderInfo(ctx context.Context, or *Order) (*Order, error) {
	return or, nil
}
func (o *Order) SetOrderInfo(ctx context.Context, orderId string) (bool, error) {
	fmt.Println(orderId)
	return true, nil
}
func (o *Order) Logger(ctx context.Context, or *Order) (bool, error) {
	log.Print("Logger : ", conv.String(or))
	return true, nil
}

var ns = "order"

func registerAction() {
	orderModel := &Order{
		Name: "tianlin999777",
	}
	getOrderNameInterface, err := action.ChangeToActor[int, string](orderModel.GetOrderName, &action.ActMeta{
		Namespace:    ns,
		Activity:     "GetOrderName",
		Desc:         "获取订单名称",
		RequiredArgs: []string{"name", "age"},
		RequiredResp: nil,
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	getMemberGroupInterface, err := action.ChangeToActor[int, string](orderModel.GetMemberGroup, &action.ActMeta{
		Namespace:    ns,
		Activity:     "GetMemberGroup",
		Desc:         "获取用户客群",
		RequiredArgs: []string{"name", "age"},
		RequiredResp: nil,
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	getOrderInfoInterface, err := action.ChangeToActor[*Order, *Order](orderModel.GetOrderInfo, &action.ActMeta{
		Namespace:    ns,
		Activity:     "GetOrderInfo",
		Desc:         "获取订单信息",
		RequiredArgs: []string{"name", "group"},
		RequiredResp: nil,
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	setOrderInfoInterface, err := action.ChangeToActor[string, bool](orderModel.SetOrderInfo, &action.ActMeta{
		Namespace:    ns,
		Activity:     "SetOrderInfo",
		Desc:         "设置订单信息",
		RequiredArgs: nil,
		RequiredResp: nil,
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	getLoggerInterface, err := action.ChangeToActor[*Order, bool](orderModel.Logger, &action.ActMeta{
		Activity: "Log",
		Desc:     "日志",
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	actionList := []action.Actor{
		getOrderNameInterface,
		getMemberGroupInterface,
		getOrderInfoInterface,
		setOrderInfoInterface,
		getLoggerInterface,
	}

	lo.ForEach(actionList, func(item action.Actor, _ int) {
		err = action.RegisterAction(item)
		if err != nil {
			fmt.Println(err)
			return
		}
	})
}

// createTestActivity 创建测试用的 Activity
func createTestActivity(id, namespace, activity string, argTmpl string, arguments []*param.BindConfig) *task.Activity {
	return &task.Activity{
		Id:          id,
		Namespace:   namespace,
		Activity:    activity,
		Arguments:   arguments,
		ArgTemplate: argTmpl,
		Control: task.ActivityControl{
			Timeout: 5,
		},
	}
}

func TestStepDependencyExtraction(t *testing.T) {
	registerAction()

	step := &flow_step.Step{
		Id:   "test-deps",
		Name: "Test Dependencies",
		DependsOn: []*task.Activity{
			createTestActivity("validateUser", ns, "GetOrderInfo", "", nil),
		},
		Condition: "[validateUser.responses.name] == 'tianlin111'",
		Strategy: []*task.Activity{
			createTestActivity("act1", ns, "SetOrderInfo", "{{name}}", []*param.BindConfig{
				{Key: "orderId", Value: "545435353"},
			}),
		},
	}

	all, err := step.Execute(context.Background(), map[string]any{
		"newName": "tianlin",
		"name":    "tianlin111",
		"group":   "M07",
	})

	fmt.Printf("Extracted dependencies: %s\n %v", conv.String(all), err)
}

//
//func TestStepConditionCheck(t *testing.T) {
//	registerMockActions()
//
//	testCases := []struct {
//		name           string
//		condition      string
//		inputParams    map[string]any
//		expectedResult bool
//		expectError    bool
//	}{
//		{
//			name:      "no condition - should pass",
//			condition: "",
//			inputParams: map[string]any{
//				"value": 100,
//			},
//			expectedResult: true,
//			expectError:    false,
//		},
//		{
//			name:      "simple condition - true",
//			condition: "${value} > 50",
//			inputParams: map[string]any{
//				"value": 100,
//			},
//			expectedResult: true,
//			expectError:    false,
//		},
//		{
//			name:      "simple condition - false",
//			condition: "${value} > 200",
//			inputParams: map[string]any{
//				"value": 100,
//			},
//			expectedResult: false,
//			expectError:    false,
//		},
//		{
//			name:      "complex condition - true",
//			condition: "${userTier} == 'VIP' && ${amount} > 10000",
//			inputParams: map[string]any{
//				"userTier": "VIP",
//				"amount":   15000,
//			},
//			expectedResult: true,
//			expectError:    false,
//		},
//		{
//			name:      "complex condition - false",
//			condition: "${userTier} == 'VIP' || ${amount} > 20000",
//			inputParams: map[string]any{
//				"userTier": "NORMAL",
//				"amount":   15000,
//			},
//			expectedResult: false,
//			expectError:    false,
//		},
//	}
//
//	for _, tc := range testCases {
//		t.Run(tc.name, func(t *testing.T) {
//			step := &flow_step.Step{
//				Id:        "test-condition",
//				Name:      "Test Condition",
//				Condition: tc.condition,
//				Strategy: []*task.Activity{
//					createTestActivity("act1", "mock", "validateUser", nil),
//				},
//			}
//
//			ctx := context.Background()
//			result, err := checkStepCondition(step, ctx, tc.inputParams)
//
//			if tc.expectError && err == nil {
//				t.Errorf("Expected error but got none")
//			}
//			if !tc.expectError && err != nil {
//				t.Errorf("Expected no error but got: %v", err)
//			}
//			if result != tc.expectedResult {
//				t.Errorf("Expected condition result %v, got %v", tc.expectedResult, result)
//			}
//		})
//	}
//}
//
//func TestStepExecuteWithCondition(t *testing.T) {
//	registerMockActions()
//
//	testCases := []struct {
//		name        string
//		condition   string
//		inputParams map[string]any
//		expectExec  bool
//	}{
//		{
//			name:      "condition met - should execute",
//			condition: "${value} > 50",
//			inputParams: map[string]any{
//				"value": 100,
//			},
//			expectExec: true,
//		},
//		{
//			name:      "condition not met - should skip",
//			condition: "${value} > 200",
//			inputParams: map[string]any{
//				"value": 100,
//			},
//			expectExec: false,
//		},
//	}
//
//	for _, tc := range testCases {
//		t.Run(tc.name, func(t *testing.T) {
//			step := &flow_step.Step{
//				Id:        "test-exec",
//				Name:      "Test Execution",
//				Condition: tc.condition,
//				Strategy: []*task.Activity{
//					createTestActivity("act1", "mock", "validateUser", nil),
//				},
//			}
//
//			ctx := context.Background()
//			result, err := step.Execute(ctx, tc.inputParams)
//
//			if err != nil {
//				t.Errorf("Execution failed: %v", err)
//			}
//
//			fmt.Printf("Step execution result: %v\n", conv.String(result))
//		})
//	}
//}
//
//func TestStepSequentialExecution(t *testing.T) {
//	registerMockActions()
//
//	step := &flow_step.Step{
//		Id:   "test-sequential",
//		Name: "Sequential Execution Test",
//		FlowControl: task.FlowControl{
//			ExecutionOrder: []task.SortType{task.SortTypeSequence},
//			Timeout:        10,
//		},
//		Strategy: []*task.Activity{
//			createTestActivity("act1", "mock", "validateUser", nil),
//			createTestActivity("act2", "mock", "fetchOrder", nil),
//			createTestActivity("act3", "mock", "assessRiskLevel", nil),
//		},
//	}
//
//	ctx := context.Background()
//	inputParams := map[string]any{
//		"userId":  "U123456",
//		"orderId": "ORD789012",
//	}
//
//	result, err := step.Execute(ctx, inputParams)
//	if err != nil {
//		t.Errorf("Sequential execution failed: %v", err)
//	}
//
//	fmt.Printf("Sequential execution result: %v\n", conv.String(result))
//
//	if len(result) == 0 {
//		t.Error("Expected non-empty result")
//	}
//}
//
//func TestStepParallelExecution(t *testing.T) {
//	registerMockActions()
//
//	step := &flow_step.Step{
//		Id:   "test-parallel",
//		Name: "Parallel Execution Test",
//		FlowControl: task.FlowControl{
//			ExecutionOrder: []task.SortType{task.SortTypeParallel},
//			Timeout:        10,
//		},
//		Strategy: []*task.Activity{
//			createTestActivity("act1", "mock", "validateUser", nil),
//			createTestActivity("act2", "mock", "fetchOrder", nil),
//			createTestActivity("act3", "mock", "assessRiskLevel", nil),
//		},
//	}
//
//	ctx := context.Background()
//	inputParams := map[string]any{
//		"userId":  "U123456",
//		"orderId": "ORD789012",
//	}
//
//	startTime := time.Now()
//	result, err := step.Execute(ctx, inputParams)
//	elapsed := time.Since(startTime)
//
//	if err != nil {
//		t.Errorf("Parallel execution failed: %v", err)
//	}
//
//	fmt.Printf("Parallel execution completed in %v\n", elapsed)
//	fmt.Printf("Parallel execution result: %v\n", conv.String(result))
//
//	if len(result) == 0 {
//		t.Error("Expected non-empty result")
//	}
//}
//
//func TestStepWithErrorHandling(t *testing.T) {
//	registerMockActions()
//
//	testCases := []struct {
//		name         string
//		onError      task.OnErrorType
//		strategy     []*task.Activity
//		expectError  bool
//		continueExec bool
//	}{
//		{
//			name:    "on_error exit - should stop on error",
//			onError: task.OnExitExit,
//			strategy: []*task.Activity{
//				createTestActivity("act1", "mock", "validateUser", nil),
//				{
//					Id:        "act2",
//					Namespace: "mock",
//					Activity:  "nonExistentAction",
//				},
//				createTestActivity("act3", "mock", "fetchOrder", nil),
//			},
//			expectError:  true,
//			continueExec: false,
//		},
//		{
//			name:    "on_error ignore - should continue on error",
//			onError: task.OnErrorIgnore,
//			strategy: []*task.Activity{
//				createTestActivity("act1", "mock", "validateUser", nil),
//				{
//					Id:        "act2",
//					Namespace: "mock",
//					Activity:  "nonExistentAction",
//				},
//				createTestActivity("act3", "mock", "fetchOrder", nil),
//			},
//			expectError:  false,
//			continueExec: true,
//		},
//	}
//
//	for _, tc := range testCases {
//		t.Run(tc.name, func(t *testing.T) {
//			step := &flow_step.Step{
//				Id:   "test-error",
//				Name: "Error Handling Test",
//				FlowControl: task.FlowControl{
//					OnError: tc.onError,
//				},
//				Strategy: tc.strategy,
//			}
//
//			ctx := context.Background()
//			inputParams := map[string]any{}
//
//			result, err := step.Execute(ctx, inputParams)
//
//			if tc.expectError && err == nil {
//				t.Errorf("Expected error but got none")
//			}
//			if !tc.expectError && err != nil {
//				t.Logf("Got error (may be expected depending on implementation): %v", err)
//			}
//
//			fmt.Printf("Error handling result: %v\n", conv.String(result))
//		})
//	}
//}
//
//func TestStepWithDelay(t *testing.T) {
//	registerMockActions()
//
//	step := &flow_step.Step{
//		Id:   "test-delay",
//		Name: "Delay Execution Test",
//		FlowControl: task.FlowControl{
//			DelayDuration: 1, // 1 second delay
//		},
//		Strategy: []*task.Activity{
//			createTestActivity("act1", "mock", "validateUser", nil),
//		},
//	}
//
//	ctx := context.Background()
//	inputParams := map[string]any{}
//
//	startTime := time.Now()
//	result, err := step.Execute(ctx, inputParams)
//	elapsed := time.Since(startTime)
//
//	if err != nil {
//		t.Errorf("Delayed execution failed: %v", err)
//	}
//
//	if elapsed < 1*time.Second {
//		t.Errorf("Expected delay of at least 1 second, got %v", elapsed)
//	}
//
//	fmt.Printf("Delayed execution completed in %v\n", elapsed)
//	fmt.Printf("Result: %v\n", conv.String(result))
//}
//
//func TestStepWithTimeout(t *testing.T) {
//	registerMockActions()
//
//	step := &flow_step.Step{
//		Id:   "test-timeout",
//		Name: "Timeout Test",
//		FlowControl: task.FlowControl{
//			Timeout: 1, // 1 second timeout
//		},
//		Strategy: []*task.Activity{
//			{
//				Id:        "slowAct",
//				Namespace: "mock",
//				Activity:  "validateUser",
//				Control: task.ActivityControl{
//					Timeout: 5, // This should be overridden by step timeout
//				},
//			},
//		},
//	}
//
//	ctx := context.Background()
//	inputParams := map[string]any{}
//
//	startTime := time.Now()
//	result, err := step.Execute(ctx, inputParams)
//	elapsed := time.Since(startTime)
//
//	if err != nil {
//		t.Logf("Timeout test result: %v", err)
//	}
//
//	fmt.Printf("Timeout test completed in %v\n", elapsed)
//	fmt.Printf("Result: %v\n", conv.String(result))
//}
//
//func TestStepDescription(t *testing.T) {
//	step := &flow_step.Step{
//		Id:        "test-desc",
//		Name:      "Test Description",
//		Condition: "${value} > 100",
//		Strategy: []*task.Activity{
//			createTestActivity("act1", "mock", "validateUser", nil),
//			createTestActivity("act2", "mock", "fetchOrder", nil),
//		},
//	}
//
//	desc := step.GetDescription()
//	expectedSubstr := "test-desc"
//
//	if desc == "" {
//		t.Error("Expected non-empty description")
//	}
//
//	if !contains(desc, expectedSubstr) {
//		t.Errorf("Description should contain '%s', got: %s", expectedSubstr, desc)
//	}
//
//	fmt.Printf("Step description: %s\n", desc)
//}
//
//func TestStepIntegrationScenario(t *testing.T) {
//	registerMockActions()
//
//	// Simulate the VIP priority processing scenario from demo.yaml
//	vipStep := &flow_step.Step{
//		Id:   "vipPriorityProcessing",
//		Name: "VIP优先处理",
//		DependsOn: []*task.Activity{
//			createTestActivity("validateUser", "mock", "validateUser", nil),
//			createTestActivity("fetchOrder", "mock", "fetchOrder", nil),
//			createTestActivity("assessRiskLevel", "mock", "assessRiskLevel", nil),
//		},
//		Condition: "${validateUser_responses_userTier} == 'VIP' && ${fetchOrder_responses_totalAmount} > 10000 && ${assessRiskLevel_responses_riskScore} < 20",
//		FlowControl: task.FlowControl{
//			OnError: task.OnErrorIgnore,
//			OnExit:  task.OnExitContinue,
//			Timeout: 15,
//		},
//		Strategy: []*task.Activity{
//			createTestActivity("expeditedProcessing", "mock", "expeditedProcessing", []*param.BindConfig{
//				{Key: "orderId", Value: "${fetchOrder_responses_orderId}"},
//				{Key: "priorityLevel", Value: "HIGH"},
//			}),
//			createTestActivity("vipBonusPoints", "mock", "vipBonusPoints", []*param.BindConfig{
//				{Key: "userId", Value: "${validateUser_responses_userId}"},
//				{Key: "points", Value: "300"},
//			}),
//		},
//	}
//
//	ctx := context.Background()
//	inputParams := map[string]any{
//		"validateUser_responses_userTier":     "VIP",
//		"validateUser_responses_userId":       "U123456",
//		"fetchOrder_responses_totalAmount":    15000,
//		"fetchOrder_responses_orderId":        "ORD789012",
//		"assessRiskLevel_responses_riskScore": 15,
//	}
//
//	result, err := vipStep.Execute(ctx, inputParams)
//	if err != nil {
//		t.Errorf("VIP priority processing failed: %v", err)
//	}
//
//	fmt.Printf("VIP priority processing result:\n%v\n", conv.String(result))
//
//	if len(result) == 0 {
//		t.Error("Expected non-empty result from VIP processing")
//	}
//}
