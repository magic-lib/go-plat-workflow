package commnode

import (
	"testing"
	"time"

	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
)

const paramInputTestDSL = `{
  "ruleChain": {"id": "R000001", "name": "input"},
  "metadata": {
    "nodes": [
      {"id": "N000010__in", "type": "custom/ParamInput", "name": "参数输入",
       "configuration": {
         "input_params": [{"key": "uid", "type": "int64"}, {"key": "flag", "type": "bool"}, {"key": "phone", "type": "string"}, {"key": "unset", "type": "string"}],
         "arguments": [
           {"key": "uid", "value": "{{arguments.uid}}", "type": "int64"},
           {"key": "flag", "value": "true", "type": "bool"},
           {"key": "phone", "value": "{{arguments.N000010__in.phone}}", "type": "string"}
         ]}}
    ],
    "connections": []
  }
}`

func TestParamInputNode_OnMsg(t *testing.T) {
	ruleEngine, err := rulego.New("param_input_ok", []byte(paramInputTestDSL))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer rulego.Del("param_input_ok")

	flowCtx := paramx.NewFlowContext("R000001", "inst", map[string]any{"uid": "7", "N000010__in": map[string]any{"phone": "138"}},
		func(key string, val any) (any, bool) { return val, key == "N000010__in" })
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
		close(done)
	}))
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout")
	}
	if relation != types.Success {
		t.Fatalf("relation=%s", relation)
	}
	out := new(paramx.FlowContext)
	_ = conv.Unmarshal(data, out)
	step := out.GetStep("N000010__in")
	if step == nil || step.Status != paramx.StepStatusSuccess {
		t.Fatalf("unexpected step: %+v", step)
	}
	resp := conv.String(step.Responses)
	if resp != `{"flag":true,"phone":"138","uid":7,"unset":null}` {
		t.Fatalf("unexpected responses: %s", resp)
	}
	if conv.String(step.Arguments) != resp {
		t.Fatalf("arguments should equal responses: %s", conv.String(step.Arguments))
	}
}
