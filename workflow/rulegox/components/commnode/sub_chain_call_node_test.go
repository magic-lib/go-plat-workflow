package commnode

import (
	"testing"
	"time"

	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
)

// 子链：参数输入节点取 {{arguments.uid}}，返回值节点把它作为返回值 out
const subChainCallSubDSL = `{
  "ruleChain": {"id": "F000001#abc", "name": "sub", "root": false},
  "metadata": {
    "nodes": [
      {"id": "N000020__in", "type": "custom/ParamInput", "name": "子链输入",
       "configuration": {"input_params": [{"key": "uid", "type": "int64"}],
         "arguments": [{"key": "uid", "value": "{{arguments.uid}}", "type": "int64"}]}},
      {"id": "custom/ReturnValue__rv", "type": "custom/ReturnValue", "name": "子链返回",
       "configuration": {"arguments": [{"key": "out", "value": "{{steps.N000020__in.responses.uid}}", "type": "int64"}]}}
    ],
    "connections": [{"fromId": "N000020__in", "toId": "custom/ReturnValue__rv", "type": "Success"}]
  }
}`

// 根链：同一子链添加两次，第一个实例入参取调用传入，第二个取固定值；根链返回值引用两个实例的返回值
const subChainCallRootDSL = `{
  "ruleChain": {"id": "R000001", "name": "root", "root": true},
  "metadata": {
    "nodes": [
      {"id": "F000001__a", "type": "custom/SubChainCall", "name": "子链A",
       "configuration": {"ruleChainId": "p1:F000001", "targetId": "p1:F000001#abc",
         "arguments": [{"key": "uid", "value": "{{arguments.uid}}"}]}},
      {"id": "F000001__b", "type": "custom/SubChainCall", "name": "子链B",
       "configuration": {"ruleChainId": "p1:F000001", "targetId": "p1:F000001#abc",
         "arguments": [{"key": "uid", "value": "99"}]}},
      {"id": "custom/ReturnValue__root", "type": "custom/ReturnValue", "name": "根链返回",
       "configuration": {"arguments": [
         {"key": "a", "value": "{{steps.F000001__a.responses.out}}"},
         {"key": "b", "value": "{{steps.F000001__b.responses.out}}"},
         {"key": "uid", "value": "{{arguments.uid}}"}]}}
    ],
    "connections": [
      {"fromId": "F000001__a", "toId": "F000001__b", "type": "Success"},
      {"fromId": "F000001__b", "toId": "custom/ReturnValue__root", "type": "Success"}
    ]
  }
}`

func TestSubChainCallNode_ScopedArgumentsAndResponses(t *testing.T) {
	if _, err := rulego.New("p1:F000001#abc", []byte(subChainCallSubDSL)); err != nil {
		t.Fatalf("new sub engine: %v", err)
	}
	defer rulego.Del("p1:F000001#abc")
	ruleEngine, err := rulego.New("sub_chain_call_root", []byte(subChainCallRootDSL))
	if err != nil {
		t.Fatalf("new root engine: %v", err)
	}
	defer rulego.Del("sub_chain_call_root")

	flowCtx := paramx.NewFlowContext("R000001", "inst", map[string]any{"uid": "7"}, func(key string, val any) (any, bool) { return val, false })
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
		t.Fatalf("relation=%s data=%s", relation, data)
	}
	out := new(paramx.FlowContext)
	_ = conv.Unmarshal(data, out)

	// 两个实例入参互不影响，各自的返回值写入本实例 steps
	stepA, stepB := out.GetStep("F000001__a"), out.GetStep("F000001__b")
	if stepA == nil || stepB == nil {
		t.Fatalf("missing sub chain steps: %s", data)
	}
	if got := conv.String(stepA.Responses); got != `{"out":7}` {
		t.Fatalf("instance a responses=%s", got)
	}
	if got := conv.String(stepB.Responses); got != `{"out":99}` {
		t.Fatalf("instance b responses=%s", got)
	}
	if got := conv.String(stepB.Arguments); got != `{"uid":"99"}` {
		t.Fatalf("instance b arguments=%s", got)
	}
	// 子链结束后恢复根链的入参，根链返回值只含根链返回值节点写入的值
	if got := conv.String(out.Arguments["uid"]); got != "7" {
		t.Fatalf("root arguments not restored: %s", conv.String(out.Arguments))
	}
	if got := conv.String(out.Responses); got != `{"a":"7","b":"99","uid":"7"}` {
		t.Fatalf("root responses=%s", got)
	}
}

func TestSubChainCallNode_InitRequiresTarget(t *testing.T) {
	n := &SubChainCallNode{}
	if err := n.Init(types.NewConfig(), types.Configuration{}); err == nil {
		t.Fatalf("expected error without targetId")
	}
	if err := n.Init(types.NewConfig(), types.Configuration{"ruleChainId": "p1:F000001"}); err != nil || n.Configuration.TargetId != "p1:F000001" {
		t.Fatalf("ruleChainId fallback failed: %v %+v", err, n.Configuration)
	}
}

func TestSubChainArguments(t *testing.T) {
	args := SubChainArguments(map[string]any{"uid": 1, "N000036__70lic.mobile": "138"})
	if args["uid"] != 1 || args["N000036__70lic.mobile"] != "138" {
		t.Fatalf("flat keys missing: %v", args)
	}
	nested, _ := args["N000036__70lic"].(map[string]any)
	if nested == nil || nested["mobile"] != "138" {
		t.Fatalf("nested keys missing: %v", args)
	}
}
