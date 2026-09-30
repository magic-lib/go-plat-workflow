package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/rulego/rulego/api/types"
)

// newTestRuleChain 构造带根节点返回值定义的 DSL 结构。
func newTestRuleChain(items []workflow.RootResponseItem) *types.RuleChain {
	rc := &types.RuleChain{
		RuleChain: types.RuleChainBaseInfo{ID: "R000001", Name: "test", Root: true},
	}
	if len(items) > 0 {
		rc.RuleChain.AdditionalInfo = map[string]interface{}{rootResponsesDSLKey: items}
	}
	return rc
}

// newTestFlowContext 构造执行结果上下文：两个 step，各有 arguments 与 responses。
func newTestFlowContext() *paramx.FlowContext {
	ctx := paramx.NewFlowContext("R000001", "inst-1", map[string]any{
		"keyword": "手机",
	})
	ctx.SetStepArguments("N000001", map[string]any{"name": "alice", "age": 18})
	ctx.SetStepResponse("N000001", map[string]any{"total": 100, "list": []any{"a", "b"}})
	ctx.SetStepResponse("N000002", "plain string")
	return ctx
}

func TestFillRootResponses(t *testing.T) {
	svc := &WorkflowService{}
	items := []workflow.RootResponseItem{
		{Key: "total", Type: workflow.RootResponseTypeInt64, Value: "{{steps.N000001.responses.total}}"},
		{Key: "name", Type: workflow.RootResponseTypeString, Value: "{{steps.N000001.arguments.name}}"},
		{Key: "keyword", Value: "{{arguments.keyword}}"},
		{Key: "list", Type: workflow.RootResponseTypeSlice, Value: "{{steps.N000001.responses.list}}"},
		{Key: "score", Type: workflow.RootResponseTypeFloat64, Value: "9.5"},
		{Key: "ok", Type: workflow.RootResponseTypeBool, Value: "true"},
		{Key: "desc", Value: "共 {{steps.N000001.responses.total}} 条"},
		{Key: "missing", Value: "{{steps.N000009.responses.x}}"},
	}
	fc := newTestFlowContext()
	svc.fillRootResponses(context.Background(), fc, newTestRuleChain(items))

	got, ok := fc.GetResponses().(map[string]any)
	if !ok {
		t.Fatalf("responses 应为 map[string]any，实际: %T", fc.GetResponses())
	}
	// 引用节点返回值 + int64 转换
	if got["total"] != int64(100) {
		t.Fatalf("total 应为 int64(100)，实际: %#v", got["total"])
	}
	// 引用节点入参 + string 转换
	if got["name"] != "alice" {
		t.Fatalf("name 应为 alice，实际: %#v", got["name"])
	}
	// 引用调用入参（{{arguments.xxx}}）
	if got["keyword"] != "手机" {
		t.Fatalf("keyword 应为 手机，实际: %#v", got["keyword"])
	}
	// 引用整个数组 + slice 转换（应保持数组类型）
	if _, ok := got["list"].([]any); !ok {
		t.Fatalf("list 应为 []any，实际: %#v", got["list"])
	}
	// 固定值 + float64 转换
	if got["score"] != 9.5 {
		t.Fatalf("score 应为 9.5，实际: %#v", got["score"])
	}
	// 固定值 + bool 转换
	if got["ok"] != true {
		t.Fatalf("ok 应为 true，实际: %#v", got["ok"])
	}
	// 占位符与文本混排：按字符串替换
	if got["desc"] != "共 100 条" {
		t.Fatalf("desc 应为「共 100 条」，实际: %#v", got["desc"])
	}
	// 取不到值：写入空串，保证返回结构完整
	if got["missing"] != "" {
		t.Fatalf("missing 应为空串，实际: %#v", got["missing"])
	}
}

// TestFillRootResponsesNoDef 无定义时不应改动 Responses。
func TestFillRootResponsesNoDef(t *testing.T) {
	svc := &WorkflowService{}
	fc := newTestFlowContext()
	svc.fillRootResponses(context.Background(), fc, newTestRuleChain(nil))
	if fc.GetResponses() != nil {
		t.Fatalf("无定义时 responses 应保持为 nil，实际: %#v", fc.GetResponses())
	}
	// 空 key 的定义也应被忽略
	fc2 := newTestFlowContext()
	svc.fillRootResponses(context.Background(), fc2,
		newTestRuleChain([]workflow.RootResponseItem{{Key: "  ", Value: "x"}}))
	if fc2.GetResponses() != nil {
		t.Fatalf("空 key 时 responses 应保持为 nil，实际: %#v", fc2.GetResponses())
	}
}

// TestParseRootResponsesFromDSL 验证从 DSL 反序列化的定义能被正确解析
//（additionalInfo 反序列化后是 []interface{} of map，需重新 marshal）。
func TestParseRootResponsesFromDSL(t *testing.T) {
	// 模拟「DSL 字符串 → types.RuleChain」的真实链路
	rc := newTestRuleChain([]workflow.RootResponseItem{
		{Key: "a", Type: "int64", Value: "{{steps.N1.responses.t}}"},
	})
	b, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("marshal dsl failed: %v", err)
	}
	var parsed types.RuleChain
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal dsl failed: %v", err)
	}
	items := parseRootResponsesFromDSL(&parsed)
	if len(items) != 1 || items[0].Key != "a" || items[0].Type != "int64" {
		t.Fatalf("解析 DSL 中的定义失败: %#v", items)
	}
}
