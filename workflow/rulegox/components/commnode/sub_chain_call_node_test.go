package commnode

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/logs"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/magic-lib/go-plat-workflow/workflow/engine"
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

// captureNodeLogSaver 收集 pushNodeLog 落库的节点日志，供测试校验所在链与调用路径。
type captureNodeLogSaver struct {
	mu   sync.Mutex
	logs []*NodeLogDef
}

func (c *captureNodeLogSaver) CreateNodeLog(_ context.Context, def *NodeLogDef) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logs = append(c.logs, def)
	return nil
}

// byNodeAndPath 返回 node_id+call_path -> chain_id 的映射。
func (c *captureNodeLogSaver) byNodeAndPath() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := make(map[string]string, len(c.logs))
	for _, l := range c.logs {
		m[l.NodeID+"|"+l.CallPath] = l.ChainID
	}
	return m
}

func TestSubChainCallNode_ScopedArgumentsAndResponses(t *testing.T) {
	saver := &captureNodeLogSaver{}
	prevSaver := defaultNodeLogSaver
	SetNodeLogSaver(saver)
	defer SetNodeLogSaver(prevSaver)

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
	md.PutValue("root_chain_id", "R000001")
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

	// 子链内节点的步骤按「调用路径/节点ID」各自保存，两次调用互不覆盖，也不与根链节点混在一起
	if s := out.GetStep("F000001__a/N000020__in"); s == nil || conv.String(s.Responses) != `{"uid":7}` {
		t.Fatalf("instance a inner step wrong: %s", data)
	}
	if s := out.GetStep("F000001__b/N000020__in"); s == nil || conv.String(s.Responses) != `{"uid":99}` {
		t.Fatalf("instance b inner step wrong: %s", data)
	}
	if out.GetStep("N000020__in") != nil {
		t.Fatalf("inner step leaked to root steps: %s", data)
	}

	// 节点日志：子链内节点带所在子链与调用路径，根链节点（含子链调用节点本身）在根链、调用路径为空
	want := map[string]string{
		"N000020__in|F000001__a":            "F000001",
		"custom/ReturnValue__rv|F000001__a": "F000001",
		"N000020__in|F000001__b":            "F000001",
		"F000001__a|":                       "R000001",
		"F000001__b|":                       "R000001",
		"custom/ReturnValue__root|":         "R000001",
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := saver.byNodeAndPath()
		missing := ""
		for k, chain := range want {
			if got[k] != chain {
				missing = k + "=" + got[k]
				break
			}
		}
		if missing == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("node log chain/call_path wrong, first mismatch %s, all=%v", missing, got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSubChainCallPathAndTarget(t *testing.T) {
	if got := SubChainCallPath("", "F000001__a"); got != "F000001__a" {
		t.Fatalf("root path=%s", got)
	}
	if got := SubChainCallPath("F000001__a", "F000003__b"); got != "F000001__a/F000003__b" {
		t.Fatalf("nested path=%s", got)
	}
	for in, want := range map[string]string{"p1:F000001#abc": "F000001", "p1:F000001": "F000001", "F000001": "F000001"} {
		if got := SubChainIDOfTarget(in); got != want {
			t.Fatalf("SubChainIDOfTarget(%s)=%s", in, got)
		}
	}
}

func TestScopeSubChainSteps(t *testing.T) {
	out := &paramx.FlowContext{Steps: map[paramx.StepId]*paramx.Step{
		"N000001__root":            {Status: paramx.StepStatusSuccess},
		"N000020__in":              {Status: paramx.StepStatusSuccess},
		"F000001__a/F000003__b":    {Status: paramx.StepStatusSuccess},
		"F000001__a/F000003__b/N1": {Status: paramx.StepStatusSuccess},
	}}
	ScopeSubChainSteps(out, map[paramx.StepId]struct{}{"N000001__root": {}}, "F000001__a")
	for _, k := range []paramx.StepId{"N000001__root", "F000001__a/N000020__in", "F000001__a/F000003__b", "F000001__a/F000003__b/N1"} {
		if out.Steps[k] == nil {
			t.Fatalf("missing step %s: %v", k, out.Steps)
		}
	}
	if len(out.Steps) != 4 {
		t.Fatalf("unexpected steps: %v", out.Steps)
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

// 子链以查询节点结束（无下游连线），子链按其判定结果结束（条件恒为真，以 True 结束）
const subChainCallCondSubDSL = `{
  "ruleChain": {"id": "F000002#abc", "name": "cond sub", "root": false},
  "metadata": {
    "nodes": [
      {"id": "N000030__sw", "type": "custom/CondSwitch", "name": "子链判断",
       "configuration": {"node_config": {"switch_condition": "1 > 0"},
         "arguments": [{"key": "uid", "value": "{{arguments.uid}}", "type": "int64"}]}}
    ],
    "connections": []
  }
}`

// 根链：子链节点按子链结束关系分流，True 走返回值节点
const subChainCallCondRootDSL = `{
  "ruleChain": {"id": "R000002", "name": "cond root", "root": true},
  "metadata": {
    "nodes": [
      {"id": "F000002__c", "type": "custom/SubChainCall", "name": "子链C",
       "configuration": {"targetId": "p1:F000002#abc",
         "arguments": [{"key": "uid", "value": "{{arguments.uid}}"}]}},
      {"id": "custom/ReturnValue__t", "type": "custom/ReturnValue", "name": "True 分支",
       "configuration": {"arguments": [{"key": "branch", "value": "true"}]}}
    ],
    "connections": [{"fromId": "F000002__c", "toId": "custom/ReturnValue__t", "type": "True"}]
  }
}`

func TestSubChainCallNode_ForwardsSubChainRelation(t *testing.T) {
	// 查询节点执行时会写调试日志，单测里服务未初始化日志器，这里补一个打印日志器
	if engine.MysqlLogger == nil {
		engine.MysqlLogger = logs.NewPrintLogger(logs.INFO)
		defer func() { engine.MysqlLogger = nil }()
	}
	saver := &captureNodeLogSaver{}
	prevSaver := defaultNodeLogSaver
	SetNodeLogSaver(saver)
	defer SetNodeLogSaver(prevSaver)

	if _, err := rulego.New("p1:F000002#abc", []byte(subChainCallCondSubDSL)); err != nil {
		t.Fatalf("new sub engine: %v", err)
	}
	defer rulego.Del("p1:F000002#abc")
	ruleEngine, err := rulego.New("sub_chain_call_cond_root", []byte(subChainCallCondRootDSL))
	if err != nil {
		t.Fatalf("new root engine: %v", err)
	}
	defer rulego.Del("sub_chain_call_cond_root")

	flowCtx := paramx.NewFlowContext("R000002", "inst", map[string]any{"uid": "7"}, func(key string, val any) (any, bool) { return val, false })
	md := types.NewMetadata()
	md.PutValue("project", "p1")
	md.PutValue("env", "test")
	md.PutValue("root_chain_id", "R000002")
	msg := types.NewMsg(0, "TEST", types.JSON, md, conv.String(flowCtx))

	var data string
	done := make(chan struct{})
	ruleEngine.OnMsg(msg, types.WithOnEnd(func(ctx types.RuleContext, m types.RuleMsg, err error, relationType string) {
		data = m.GetData()
		close(done)
	}))
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout")
	}
	out := new(paramx.FlowContext)
	_ = conv.Unmarshal(data, out)
	// 子链以 True 结束，根链沿 True 连线走到返回值节点
	if got := conv.String(out.Responses); got != `{"branch":"true"}` {
		t.Fatalf("root did not follow True branch, responses=%s data=%s", got, data)
	}
	// 子链调用节点的日志记录实际转发的关系 True，而不是固定的 Success
	deadline := time.Now().Add(3 * time.Second)
	for {
		relation := ""
		saver.mu.Lock()
		for _, l := range saver.logs {
			if l.NodeID == "F000002__c" {
				relation = l.RelationType
			}
		}
		saver.mu.Unlock()
		if relation == types.True {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sub chain call node log relation=%q, want True", relation)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
