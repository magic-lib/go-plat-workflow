package builder

import (
	"encoding/json"
	"testing"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/rulego/rulego/api/types"
)

// TestInjectRootResponses 验证根节点返回值定义被写入 DSL，且空定义不会污染 DSL。
func TestInjectRootResponses(t *testing.T) {
	rc := &types.RuleChain{RuleChain: types.RuleChainBaseInfo{ID: "R1"}}
	injectRootResponses(rc, []workflow.RootResponseItem{
		{Key: "total", Type: workflow.RootResponseTypeInt64, Value: "{{steps.N1.responses.total}}"},
		{Key: "  ", Value: "空 key 应被丢弃"},
	})
	raw, ok := rc.RuleChain.GetAdditionalInfo(rootResponsesDSLKey)
	if !ok {
		t.Fatalf("定义未写入 DSL additionalInfo")
	}
	b, _ := json.Marshal(raw)
	if string(b) == "" {
		t.Fatalf("定义序列化为空")
	}
	var items []workflow.RootResponseItem
	if err := json.Unmarshal(b, &items); err != nil {
		t.Fatalf("回读定义失败: %v", err)
	}
	if len(items) != 1 || items[0].Key != "total" {
		t.Fatalf("空 key 应被过滤，实际: %#v", items)
	}

	// 空定义：不写入，且清掉残留
	injectRootResponses(rc, nil)
	if _, ok := rc.RuleChain.GetAdditionalInfo(rootResponsesDSLKey); ok {
		t.Fatalf("无有效定义时不应残留 root_responses")
	}
}

// TestNormalizeRootResponses 验证清洗逻辑：空 key 过滤 + 返回 nil（便于不写库）。
func TestNormalizeRootResponses(t *testing.T) {
	if got := normalizeRootResponses(nil); got != nil {
		t.Fatalf("nil 输入应返回 nil")
	}
	if got := normalizeRootResponses([]workflow.RootResponseItem{{Key: "  "}}); got != nil {
		t.Fatalf("全空 key 应返回 nil")
	}
	got := normalizeRootResponses([]workflow.RootResponseItem{{Key: " a ", Label: " b ", Type: " int64 ", Value: " {{x}} "}})
	if len(got) != 1 || got[0].Key != "a" || got[0].Label != "b" || got[0].Type != "int64" || got[0].Value != "{{x}}" {
		t.Fatalf("字段未正确 trim: %#v", got)
	}
}
