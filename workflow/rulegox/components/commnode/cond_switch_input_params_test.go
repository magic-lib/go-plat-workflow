package commnode

import (
	"testing"
	"time"

	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/logs"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/magic-lib/go-plat-workflow/workflow/engine"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
)

// 条件判断节点：编排时在本链定义的参数（score）按来源取值，取不到时用默认值，再按 switch_condition 路由
const condSwitchInputTestDSL = `{
  "ruleChain": {"id": "R000002", "name": "cond"},
  "metadata": {
    "nodes": [
      {"id": "N000020__c", "type": "custom/CondSwitch", "name": "条件判断",
       "configuration": {
         "node_config": {"switch_condition": "[arguments.score] > 50"},
         "input_params": [{"key": "score", "type": "int64", "value": "30"}],
         "arguments": [{"key": "score", "value": "{{arguments.score}}", "type": "int64"}]
       }}
    ],
    "connections": []
  }
}`

func runCondSwitchInput(t *testing.T, input map[string]any) (string, map[string]any) {
	t.Helper()
	// 解析节点参数时会写执行日志，测试环境没有初始化日志，用打印日志代替
	if engine.MysqlLogger == nil {
		engine.MysqlLogger = logs.NewPrintLogger(logs.INFO)
	}
	ruleEngine, err := rulego.New("cond_switch_input", []byte(condSwitchInputTestDSL))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer rulego.Del("cond_switch_input")

	flowCtx := paramx.NewFlowContext("R000002", "inst", input, func(string, any) (any, bool) { return nil, false })
	md := types.NewMetadata()
	md.PutValue("project", "p1")
	md.PutValue("env", "test")
	msg := types.NewMsg(0, "TEST", types.JSON, md, conv.String(flowCtx))

	var (
		relation string
		data     string
	)
	done := make(chan struct{})
	ruleEngine.OnMsg(msg, types.WithOnEnd(func(ctx types.RuleContext, m types.RuleMsg, err error, relationType string) {
		relation, data = relationType, m.GetData()
		if err != nil {
			t.Logf("err=%v", err)
		}
		close(done)
	}))
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout")
	}
	out := new(paramx.FlowContext)
	_ = conv.Unmarshal(data, out)
	step := out.GetStep("N000020__c")
	if step == nil {
		t.Fatalf("step missing: relation=%s data=%s", relation, data)
	}
	return relation, step.Arguments
}

func TestCondSwitchNode_InputParams(t *testing.T) {
	relation, args := runCondSwitchInput(t, map[string]any{"score": "80"})
	if relation != "True" || conv.String(args["score"]) != "80" {
		t.Fatalf("passed score: relation=%s args=%v", relation, args)
	}
	relation, args = runCondSwitchInput(t, map[string]any{})
	if relation != "False" || conv.String(args["score"]) != "30" {
		t.Fatalf("default score: relation=%s args=%v", relation, args)
	}
}
