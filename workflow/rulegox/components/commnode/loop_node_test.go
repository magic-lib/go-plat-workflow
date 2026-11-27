package commnode

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
)

// loopTestBodyNode 测试用循环体节点：读 arguments._loop.item，item 为 "bad" 时失败，
// 否则把 item 与 index 写入本节点 responses。
type loopTestBodyNode struct{}

var loopTestBodyCalls int64

func (x *loopTestBodyNode) Type() string                                 { return "test/loopBody" }
func (x *loopTestBodyNode) New() types.Node                              { return &loopTestBodyNode{} }
func (x *loopTestBodyNode) Init(types.Config, types.Configuration) error { return nil }
func (x *loopTestBodyNode) Destroy()                                     {}
func (x *loopTestBodyNode) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	atomic.AddInt64(&loopTestBodyCalls, 1)
	fc := new(paramx.FlowContext)
	_ = conv.Unmarshal(msg.GetData(), fc)
	vars, _ := fc.Arguments["_loop"].(map[string]any)
	if conv.String(vars["item"]) == "bad" {
		ctx.TellFailure(msg, errors.New("bad item"))
		return
	}
	fc.SetStep(paramx.StepId(ctx.GetSelfId()), &paramx.Step{
		Status:    paramx.StepStatusSuccess,
		Responses: map[string]any{"item": vars["item"], "index": vars["index"], "uid": fc.Arguments["uid"]},
	})
	msg.SetData(conv.String(fc))
	ctx.TellSuccess(msg)
}

func init() {
	_ = rulego.Registry.Register(&loopTestBodyNode{})
}

func loopRootDSL(chainID, loopCfg, args string) string {
	if args == "" {
		args = "[]"
	}
	return fmt.Sprintf(`{
  "ruleChain": {"id": %q, "name": "loop"},
  "metadata": {
    "nodes": [
      {"id": "N000020__loop", "type": "custom/Loop", "name": "循环", "configuration": {"loop": %s, "arguments": %s}}
    ],
    "connections": []
  }
}`, chainID, loopCfg, args)
}

const loopSubDSL = `{
  "ruleChain": {"id": "S000001", "name": "body"},
  "metadata": {"nodes": [{"id": "N000021__body", "type": "test/loopBody", "name": "body"}], "connections": []}
}`

// runLoopChain 加载子链 p1:S000001（以及嵌入形式的 p1:S000001#e1）与根链后执行一次，返回结束关系、上下文与错误。
func runLoopChain(t *testing.T, chainID, loopCfg, args string, payload map[string]any) (string, *paramx.FlowContext, error) {
	t.Helper()
	for _, key := range []string{"p1:S000001", "p1:S000001#e1"} {
		if _, err := rulego.New(key, []byte(loopSubDSL)); err != nil {
			t.Fatalf("new sub chain: %v", err)
		}
		defer rulego.Del(key)
	}
	ruleEngine, err := rulego.New(chainID, []byte(loopRootDSL(chainID, loopCfg, args)))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer rulego.Del(chainID)

	flowCtx := paramx.NewFlowContext(chainID, "inst", payload)
	flowCtx.SetStep("N000001__src", &paramx.Step{Status: paramx.StepStatusSuccess, Responses: map[string]any{
		"list": []any{"a", "b", "c"},
		"obj":  map[string]any{"y": 2, "x": 1},
		"n":    3,
	}})
	md := types.NewMetadata()
	md.PutValue("project", "p1")
	md.PutValue("env", "test")
	md.PutValue("trace_id", "trace-1")
	md.PutValue("root_chain_id", chainID)
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
	case <-time.After(10 * time.Second):
		t.Fatalf("timeout")
	}
	out := new(paramx.FlowContext)
	_ = conv.Unmarshal(data, out)
	return relation, out, endErr
}

func loopResultOf(t *testing.T, out *paramx.FlowContext) *LoopResult {
	t.Helper()
	step := out.GetStep("N000020__loop")
	if step == nil {
		t.Fatalf("loop step missing")
	}
	res := new(LoopResult)
	if err := conv.Unmarshal(conv.String(step.Responses), res); err != nil {
		t.Fatalf("parse loop result: %v", err)
	}
	return res
}

func TestLoopNode_ForeachSubChain(t *testing.T) {
	cfg := `{"mode":"foreach","range":"{{steps.N000001__src.responses.list}}","body_type":"subchain","body_id":"S000001"}`
	relation, out, err := runLoopChain(t, "loop_foreach_sub", cfg, "", map[string]any{"uid": 7})
	if err != nil || relation != types.Success {
		t.Fatalf("relation=%s err=%v", relation, err)
	}
	res := loopResultOf(t, out)
	if res.Count != 3 || res.Success != 3 || res.Failed != 0 || res.BreakIndex != -1 || len(res.Items) != 3 {
		t.Fatalf("unexpected result: %s", conv.String(res))
	}
	for i, want := range []string{"a", "b", "c"} {
		m, _ := res.Items[i].(map[string]any)
		if conv.String(m["item"]) != want || conv.String(m["index"]) != fmt.Sprint(i) || conv.String(m["uid"]) != "7" {
			t.Fatalf("item %d: %s", i, conv.String(res.Items[i]))
		}
	}
	// 循环变量不应写回外层上下文
	if _, ok := out.Arguments["_loop"]; ok {
		t.Fatalf("_loop leaked into outer context")
	}
}

func TestLoopNode_ForeachObjectSortedByKey(t *testing.T) {
	cfg := `{"mode":"foreach","range":"{{steps.N000001__src.responses.obj}}","body_type":"subchain","body_id":"S000001"}`
	_, out, err := runLoopChain(t, "loop_foreach_obj", cfg, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	res := loopResultOf(t, out)
	if len(res.Items) != 2 || conv.String(res.Items[0].(map[string]any)["item"]) != "1" || conv.String(res.Items[1].(map[string]any)["item"]) != "2" {
		t.Fatalf("unexpected items: %s", conv.String(res.Items))
	}
}

func TestLoopNode_ItemsParamRootChain(t *testing.T) {
	fake := &fakeRootChainInvoker{resp: map[string]any{"ok": true}}
	SetRootChainInvoker(fake)
	defer SetRootChainInvoker(nil)

	// 循环数据来自参数 loop_items（引用上游数组），优先于循环配置里的旧字段 range
	cfg := `{"mode":"foreach","range":"[1]","body_type":"rootchain","body_id":"R000009"}`
	args := `[{"key":"loop_items","value":"{{steps.N000001__src.responses.list}}","type":"slice"},{"key":"idx","value":"{{arguments._loop.index}}"},{"key":"name","value":"{{arguments._loop.item}}"},{"key":"uid","value":"{{arguments.uid}}"}]`
	relation, out, err := runLoopChain(t, "loop_items_root", cfg, args, map[string]any{"uid": 5})
	if err != nil || relation != types.Success {
		t.Fatalf("relation=%s err=%v", relation, err)
	}
	if fake.invoked != 3 {
		t.Fatalf("invoked=%d", fake.invoked)
	}
	// 最后一次调用的入参
	if conv.String(fake.req.Payload["idx"]) != "2" || fake.req.Payload["name"] != "c" || conv.String(fake.req.Payload["uid"]) != "5" || fake.req.ChainID != "R000009" {
		t.Fatalf("unexpected req: %+v", fake.req)
	}
	if _, ok := fake.req.Payload["loop_items"]; ok {
		t.Fatalf("loop_items should not be passed to target root chain: %v", fake.req.Payload)
	}
	if strings.Join(fake.path, ",") != "loop_items_root,R000009" {
		t.Fatalf("unexpected call path: %v", fake.path)
	}
	res := loopResultOf(t, out)
	if res.Count != 3 || res.Success != 3 {
		t.Fatalf("unexpected result: %s", conv.String(res))
	}
}

func TestLoopNode_WhileAndBreak(t *testing.T) {
	// 条件循环：index < 5 时继续；跳出条件：本轮结果 index == 2
	cfg := `{"mode":"while","condition":"[arguments._loop.index] < 5","break_condition":"[arguments._loop.last.index] == 2","body_type":"subchain","body_id":"S000001"}`
	relation, out, err := runLoopChain(t, "loop_while", cfg, "", nil)
	if err != nil || relation != types.Success {
		t.Fatalf("relation=%s err=%v", relation, err)
	}
	res := loopResultOf(t, out)
	if res.Count != 3 || res.BreakIndex != 2 {
		t.Fatalf("unexpected result: %s", conv.String(res))
	}

	cfg = `{"mode":"while","condition":"[arguments._loop.index] < 4","body_type":"subchain","body_id":"S000001"}`
	_, out, err = runLoopChain(t, "loop_while_cond", cfg, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res := loopResultOf(t, out); res.Count != 4 {
		t.Fatalf("unexpected result: %s", conv.String(res))
	}
}

func TestLoopNode_WhileExceedsMax(t *testing.T) {
	cfg := `{"mode":"while","condition":"true","max_iterations":3,"body_type":"subchain","body_id":"S000001"}`
	relation, out, err := runLoopChain(t, "loop_while_max", cfg, "", nil)
	if relation != types.Failure || err == nil || !strings.Contains(err.Error(), "最大循环次数") {
		t.Fatalf("relation=%s err=%v", relation, err)
	}
	if res := loopResultOf(t, out); res.Count != 3 {
		t.Fatalf("unexpected result: %s", conv.String(res))
	}
}

func TestLoopNode_FailPolicy(t *testing.T) {
	cfg := `{"mode":"foreach","range":"[\"a\",\"bad\",\"c\"]","body_type":"subchain","body_id":"S000001"}`
	relation, out, err := runLoopChain(t, "loop_fail_stop", cfg, "", nil)
	if relation != types.Failure || err == nil || !strings.Contains(err.Error(), "第 2 轮") {
		t.Fatalf("relation=%s err=%v", relation, err)
	}
	if res := loopResultOf(t, out); res.Count != 2 || res.Failed != 1 {
		t.Fatalf("unexpected result: %s", conv.String(res))
	}

	cfg = `{"mode":"foreach","range":"[\"a\",\"bad\",\"c\"]","fail_policy":"continue","body_type":"subchain","body_id":"S000001"}`
	relation, out, err = runLoopChain(t, "loop_fail_continue", cfg, "", nil)
	if relation != types.Success || err != nil {
		t.Fatalf("relation=%s err=%v", relation, err)
	}
	res := loopResultOf(t, out)
	if res.Count != 3 || res.Success != 2 || res.Failed != 1 || len(res.Errors) != 1 || res.Errors[0].Index != 1 || res.Items[1] != nil {
		t.Fatalf("unexpected result: %s", conv.String(res))
	}
}

func TestLoopNode_ParallelKeepsOrder(t *testing.T) {
	items := make([]string, 30)
	for i := range items {
		items[i] = fmt.Sprintf("%q", fmt.Sprint("v", i))
	}
	rangeJSON := strings.ReplaceAll("["+strings.Join(items, ",")+"]", `"`, `\"`)
	cfg := fmt.Sprintf(`{"mode":"foreach","range":"%s","concurrency":8,"body_type":"subchain","body_id":"S000001"}`, rangeJSON)
	relation, out, err := runLoopChain(t, "loop_parallel", cfg, "", nil)
	if err != nil || relation != types.Success {
		t.Fatalf("relation=%s err=%v", relation, err)
	}
	res := loopResultOf(t, out)
	if res.Count != 30 || res.Success != 30 {
		t.Fatalf("unexpected result: %s", conv.String(res))
	}
	for i, it := range res.Items {
		if conv.String(it.(map[string]any)["item"]) != fmt.Sprint("v", i) {
			t.Fatalf("item %d out of order: %s", i, conv.String(it))
		}
	}
}

func TestLoopNode_TooManyItems(t *testing.T) {
	cfg := `{"mode":"foreach","range":"{{steps.N000001__src.responses.list}}","max_iterations":2,"body_type":"subchain","body_id":"S000001"}`
	relation, _, err := runLoopChain(t, "loop_too_many", cfg, "", nil)
	if relation != types.Failure || err == nil || !strings.Contains(err.Error(), "超过最大循环次数") {
		t.Fatalf("relation=%s err=%v", relation, err)
	}
}

func TestLoopNode_ItemsFromEntryArgument(t *testing.T) {
	// 调用传入：loop_items 取调用方传入的数组
	cfg := `{"mode":"foreach","body_type":"subchain","body_id":"S000001"}`
	args := `[{"key":"loop_items","value":"{{arguments.phones}}"}]`
	relation, out, err := runLoopChain(t, "loop_entry_items", cfg, args, map[string]any{"phones": []any{"138", "139"}})
	if err != nil || relation != types.Success {
		t.Fatalf("relation=%s err=%v", relation, err)
	}
	res := loopResultOf(t, out)
	if res.Count != 2 || conv.String(res.Items[1].(map[string]any)["item"]) != "139" {
		t.Fatalf("unexpected result: %s", conv.String(res))
	}
}

func TestLoopNode_SubChainNotLoaded(t *testing.T) {
	cfg := `{"mode":"foreach","range":"[1]","body_type":"subchain","body_id":"S999999"}`
	relation, _, err := runLoopChain(t, "loop_no_sub", cfg, "", nil)
	if relation != types.Failure || err == nil || !strings.Contains(err.Error(), "未加载") {
		t.Fatalf("relation=%s err=%v", relation, err)
	}
}

func TestLoopConfigNormalize(t *testing.T) {
	cases := []struct {
		cfg  LoopConfig
		want string
	}{
		{LoopConfig{Mode: "foreach", BodyType: "subchain", BodyID: "S1"}, "loop_items"},
		{LoopConfig{Mode: "count", Range: "[1]", BodyType: "subchain", BodyID: "S1"}, "不支持的循环模式"},
		{LoopConfig{Mode: "while", BodyType: "subchain", BodyID: "S1"}, "继续条件"},
		{LoopConfig{Mode: "x", BodyType: "subchain", BodyID: "S1"}, "不支持的循环模式"},
		{LoopConfig{Mode: "foreach", Range: "[1]", BodyType: "y", BodyID: "S1"}, "循环体类型"},
		{LoopConfig{Mode: "foreach", Range: "[1]", BodyType: "subchain"}, "未选择循环体"},
		{LoopConfig{Mode: "foreach", Range: "[1]", BodyType: "subchain", BodyID: "S1", MaxIterations: 1001}, "最大循环次数"},
		{LoopConfig{Mode: "foreach", Range: "[1]", BodyType: "subchain", BodyID: "S1", Concurrency: 21}, "并发数"},
		{LoopConfig{Mode: "while", Condition: "true", BodyType: "subchain", BodyID: "S1", Concurrency: 2}, "串行"},
		{LoopConfig{Mode: "foreach", Range: "[1]", BreakCondition: "true", BodyType: "subchain", BodyID: "S1", Concurrency: 2}, "串行"},
		{LoopConfig{Mode: "foreach", Range: "[1]", BodyType: "subchain", BodyID: "S1", FailPolicy: "z"}, "失败策略"},
	}
	for _, c := range cases {
		cfg := c.cfg
		if err := cfg.normalize(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("cfg=%+v err=%v want %s", c.cfg, err, c.want)
		}
	}
	ok := LoopConfig{Range: "[1]", BodyType: "rootchain", BodyID: "R1"}
	if err := ok.normalize(); err != nil || ok.MaxIterations != DefaultLoopMaxIterations || ok.Concurrency != 1 || ok.FailPolicy != LoopFailStop {
		t.Fatalf("normalize defaults: %+v err=%v", ok, err)
	}
}

func TestLoopBodyRef(t *testing.T) {
	bt, bid := LoopBodyRef(types.Configuration{"loop": map[string]any{"body_type": "subchain", "body_id": " S1 "}})
	if bt != "subchain" || bid != "S1" {
		t.Fatalf("bt=%s bid=%s", bt, bid)
	}
	bt, bid = LoopBodyRef(types.Configuration{"loop": `{"body_type":"rootchain","body_id":"R1"}`})
	if bt != "rootchain" || bid != "R1" {
		t.Fatalf("bt=%s bid=%s", bt, bid)
	}
}

func TestLoopNode_ParallelFailStop(t *testing.T) {
	cfg := `{"mode":"foreach","range":"[\"a\",\"bad\",\"c\",\"d\"]","concurrency":4,"body_type":"subchain","body_id":"S000001"}`
	relation, out, err := runLoopChain(t, "loop_parallel_stop", cfg, "", nil)
	if relation != types.Failure || err == nil || !strings.Contains(err.Error(), "第 2 轮") {
		t.Fatalf("relation=%s err=%v", relation, err)
	}
	if res := loopResultOf(t, out); res.Failed != 1 {
		t.Fatalf("unexpected result: %s", conv.String(res))
	}
}

func TestLoopNode_SubChainByEmbeddedTarget(t *testing.T) {
	// body_target 指向嵌入的子链；body_id 对应的实例不存在也能执行
	cfg := `{"mode":"foreach","range":"[1,2]","body_type":"subchain","body_id":"S000009","body_target":"p1:S000001#e1"}`
	relation, out, err := runLoopChain(t, "loop_embedded", cfg, "", nil)
	if err != nil || relation != types.Success {
		t.Fatalf("relation=%s err=%v", relation, err)
	}
	if res := loopResultOf(t, out); res.Count != 2 || res.Success != 2 {
		t.Fatalf("unexpected result: %s", conv.String(res))
	}
}
