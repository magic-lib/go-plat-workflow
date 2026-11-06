package builder

import (
	"encoding/json"
	"testing"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
)

func TestBuildRuleNodes_ParamInputDefs(t *testing.T) {
	b := &DSLBuilder{}
	defs := map[string]*workflow.NodeDef{
		"N000010": {NodeID: "N000010", Name: "参数输入节点", Type: common.ParamInputNodeTypeName},
	}
	inputDefs := `[{"key":"uid","type":"int64","description":"用户ID"},{"key":" ","type":"string"},{"key":"uid","type":"string"},{"key":"name","type":"string","description":"姓名"}]`
	overrides := map[string]map[string]interface{}{
		"N000010__abc": {
			common.ParamInputDefsKey: map[string]any{"src": "fixed", "value": inputDefs},
			"uid":                    map[string]any{"src": "entry", "value": "{{arguments.uid}}", "type": "int64"},
		},
	}
	nodes := b.buildRuleNodes([]instanceRef{{baseId: "N000010", instanceId: "N000010__abc"}}, defs, overrides, nil, nil)
	if len(nodes) != 1 {
		t.Fatalf("nodes=%d", len(nodes))
	}
	cfg := nodes[0].Configuration
	got, _ := json.Marshal(cfg[common.ParamInputConfigKey])
	if string(got) != `[{"key":"uid","type":"int64","description":"用户ID"},{"key":"name","type":"string","description":"姓名"}]` {
		t.Fatalf("input_params=%s", got)
	}
	raw, _ := json.Marshal(cfg["arguments"])
	var args []map[string]any
	_ = json.Unmarshal(raw, &args)
	if len(args) != 1 || args[0]["key"] != "uid" || args[0]["type"] != "int64" {
		t.Fatalf("arguments=%s", raw)
	}
	labels := map[string]string{}
	_ = json.Unmarshal([]byte(nodes[0].AdditionalInfo["node_param_labels"].(string)), &labels)
	if labels["uid"] != "用户ID" || labels["name"] != "姓名" {
		t.Fatalf("labels=%v", labels)
	}
}
