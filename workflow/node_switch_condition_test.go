package workflow_test

import (
	"encoding/json"
	"testing"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// TestNodeDef_HasSwitchConditionExpr 覆盖「节点是否带路由功能」的判定：
// 仅 custom/Activity 与 custom/CondSwitch 支持 switch_condition，且值非空白才算路由节点。
func TestNodeDef_HasSwitchConditionExpr(t *testing.T) {
	cases := []struct {
		name string
		def  *workflow.NodeDef
		want bool
	}{
		{
			name: "空节点",
			def:  &workflow.NodeDef{},
			want: false,
		},
		{
			name: "无配置",
			def:  &workflow.NodeDef{Type: "custom/Activity"},
			want: false,
		},
		{
			name: "Activity 配置了路由条件",
			def: &workflow.NodeDef{Type: "custom/Activity",
				Configuration: json.RawMessage(`{"node_config":{"switch_condition":"responses.code == 0"}}`)},
			want: true,
		},
		{
			name: "CondSwitch 配置了路由条件",
			def: &workflow.NodeDef{Type: "custom/CondSwitch",
				Configuration: json.RawMessage(`{"node_config":{"switch_condition":"responses.ok"}}`)},
			want: true,
		},
		{
			name: "路由条件为空串",
			def: &workflow.NodeDef{Type: "custom/Activity",
				Configuration: json.RawMessage(`{"node_config":{"switch_condition":""}}`)},
			want: false,
		},
		{
			name: "路由条件仅空白字符",
			def: &workflow.NodeDef{Type: "custom/Activity",
				Configuration: json.RawMessage(`{"node_config":{"switch_condition":"   "}}`)},
			want: false,
		},
		{
			name: "未配置 switch_condition（仅有 enable_condition）",
			def: &workflow.NodeDef{Type: "custom/Activity",
				Configuration: json.RawMessage(`{"node_config":{"enable_condition":"a > 1"}}`)},
			want: false,
		},
		{
			name: "已废弃的旧字段 condition 不算路由",
			def: &workflow.NodeDef{Type: "custom/CondSwitch",
				Configuration: json.RawMessage(`{"node_config":{"condition":"a > 1"}}`)},
			want: false,
		},
		{
			name: "不支持路由的节点类型",
			def: &workflow.NodeDef{Type: "custom/Sleep",
				Configuration: json.RawMessage(`{"node_config":{"switch_condition":"a > 1"}}`)},
			want: false,
		},
		{
			name: "非法 JSON 配置",
			def: &workflow.NodeDef{Type: "custom/Activity",
				Configuration: json.RawMessage(`not-json`)},
			want: false,
		},
	}

	for _, c := range cases {
		if got := c.def.HasSwitchConditionExpr(); got != c.want {
			t.Fatalf("%s: HasSwitchConditionExpr()=%v, want %v", c.name, got, c.want)
		}
	}

	// nil 接收者不应 panic
	var nilDef *workflow.NodeDef
	if nilDef.HasSwitchConditionExpr() {
		t.Fatal("nil NodeDef should not have switch condition")
	}
}
