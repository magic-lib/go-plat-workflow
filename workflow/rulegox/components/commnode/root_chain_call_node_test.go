package commnode

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
)

type fakeRootChainInvoker struct {
	mu      sync.Mutex
	req     *RootChainInvokeRequest
	path    []string
	resp    any
	err     error
	invoked int
}

func (f *fakeRootChainInvoker) InvokeRootChainForNode(ctx context.Context, req *RootChainInvokeRequest) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invoked++
	f.req = req
	f.path = rootChainCallPath(ctx)
	return f.resp, f.err
}

func TestNextRootChainCallPath(t *testing.T) {
	path, err := nextRootChainCallPath(nil, "R1", "R2")
	if err != nil || strings.Join(path, ",") != "R1,R2" {
		t.Fatalf("path=%v err=%v", path, err)
	}
	if _, err := nextRootChainCallPath([]string{"R1", "R2"}, "R2", "R1"); err == nil || !strings.Contains(err.Error(), "循环") {
		t.Fatalf("expected cycle error, got %v", err)
	}
	if _, err := nextRootChainCallPath(nil, "R1", "R1"); err == nil {
		t.Fatalf("expected self call error")
	}
	deep := []string{"R1", "R2", "R3", "R4", "R5"}
	if _, err := nextRootChainCallPath(deep, "R5", "R6"); err == nil || !strings.Contains(err.Error(), "嵌套") {
		t.Fatalf("expected depth error, got %v", err)
	}
	// 不能修改调用方传入的 path
	in := []string{"R1"}
	out, _ := nextRootChainCallPath(in, "R1", "R2")
	out[0] = "X"
	if in[0] != "R1" {
		t.Fatalf("input path mutated")
	}
}

const rootChainCallTestDSL = `{
  "ruleChain": {"id": "R000001", "name": "caller"},
  "metadata": {
    "nodes": [
      {"id": "N000009__call", "type": "custom/RootChainCall", "name": "调用B",
       "configuration": {"target_chain_id": "R000002",
         "arguments": [{"key": "user_id", "value": "{{arguments.uid}}"}, {"key": "flag", "value": "fixed"},
           {"key": "N1__x.phone", "value": "{{arguments.N000009__call.N1__x.phone}}"}]}}
    ],
    "connections": []
  }
}`

func runRootChainCallChain(t *testing.T, chainID string) (string, *paramx.FlowContext, error) {
	t.Helper()
	ruleEngine, err := rulego.New(chainID, []byte(rootChainCallTestDSL))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer rulego.Del(chainID)

	// 与入口 getParamContext 一致：key 为节点实例 ID 的入参（私有参数）放进该节点的 steps.arguments
	payload := map[string]any{"uid": 7, "N000009__call": map[string]any{"N1__x": map[string]any{"phone": "138"}}}
	flowCtx := paramx.NewFlowContext("R000001", "inst", payload, func(key string, val any) (any, bool) {
		return val, key == "N000009__call"
	})
	md := types.NewMetadata()
	md.PutValue("project", "p1")
	md.PutValue("env", "test")
	md.PutValue("trace_id", "trace-1")
	md.PutValue("root_chain_id", "R000001")
	msg := types.NewMsg(0, "TEST", types.JSON, md, conv.String(flowCtx))

	var (
		relation string
		data     string
		endErr   error
	)
	done := make(chan struct{})
	ruleEngine.OnMsg(msg, types.WithOnEnd(func(ctx types.RuleContext, m types.RuleMsg, err error, relationType string) {
		relation, data, endErr = relationType, m.GetData(), err
		close(done)
	}))
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout")
	}
	out := new(paramx.FlowContext)
	_ = conv.Unmarshal(data, out)
	return relation, out, endErr
}

func TestRootChainCallNode_Success(t *testing.T) {
	fake := &fakeRootChainInvoker{resp: map[string]any{"score": 99}}
	SetRootChainInvoker(fake)
	defer SetRootChainInvoker(nil)

	relation, out, err := runRootChainCallChain(t, "root_chain_call_ok")
	if err != nil || relation != types.Success {
		t.Fatalf("relation=%s err=%v", relation, err)
	}
	if fake.req == nil || fake.req.ChainID != "R000002" || fake.req.Project != "p1" || fake.req.Env != "test" || fake.req.TraceId != "trace-1" {
		t.Fatalf("unexpected req: %+v", fake.req)
	}
	if conv.String(fake.req.Payload["user_id"]) != "7" || fake.req.Payload["flag"] != "fixed" || fake.req.Payload["N1__x.phone"] != "138" {
		t.Fatalf("unexpected payload: %v", fake.req.Payload)
	}
	if _, ok := fake.req.Payload["target_chain_id"]; ok {
		t.Fatalf("target_chain_id should not be passed as argument")
	}
	if strings.Join(fake.path, ",") != "R000001,R000002" {
		t.Fatalf("unexpected call path: %v", fake.path)
	}
	step := out.GetStep("N000009__call")
	if step == nil || step.Status != paramx.StepStatusSuccess {
		t.Fatalf("unexpected step: %+v", step)
	}
	if conv.String(step.Responses) != `{"score":99}` {
		t.Fatalf("unexpected responses: %s", conv.String(step.Responses))
	}
}

func TestRootChainCallNode_Failure(t *testing.T) {
	fake := &fakeRootChainInvoker{err: errors.New("boom")}
	SetRootChainInvoker(fake)
	defer SetRootChainInvoker(nil)

	relation, out, err := runRootChainCallChain(t, "root_chain_call_fail")
	if relation != types.Failure || err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("relation=%s err=%v", relation, err)
	}
	step := out.GetStep("N000009__call")
	if step == nil || step.Status != paramx.StepStatusFail || step.Error == nil {
		t.Fatalf("unexpected step: %+v", step)
	}
}

func TestRootChainCallNode_InitRequiresTarget(t *testing.T) {
	dsl := strings.Replace(rootChainCallTestDSL, `"target_chain_id": "R000002",`, ``, 1)
	if _, err := rulego.New("root_chain_call_no_target", []byte(dsl)); err == nil {
		rulego.Del("root_chain_call_no_target")
		t.Fatalf("expected init error without target_chain_id")
	}
}
