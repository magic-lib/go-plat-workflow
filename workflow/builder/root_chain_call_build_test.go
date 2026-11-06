package builder

import (
	"encoding/json"
	"testing"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
)

func TestBuildRuleNodes_RootChainCallTarget(t *testing.T) {
	b := &DSLBuilder{}
	defs := map[string]*workflow.NodeDef{
		"N000009": {NodeID: "N000009", Name: "调用根链节点", Type: common.RootChainCallNodeTypeName},
	}
	overrides := map[string]map[string]interface{}{
		"N000009__abc": {
			common.RootChainCallTargetKey: map[string]any{"src": "value", "value": " R000002 "},
			"user_id":                     map[string]any{"src": "ref_node", "value": "{{arguments.uid}}", "type": "string"},
		},
	}
	nodes := b.buildRuleNodes([]instanceRef{{baseId: "N000009", instanceId: "N000009__abc"}}, defs, overrides, nil, nil)
	if len(nodes) != 1 {
		t.Fatalf("nodes=%d", len(nodes))
	}
	cfg := nodes[0].Configuration
	if cfg[common.RootChainCallConfigKey] != "R000002" {
		t.Fatalf("target_chain_id=%v", cfg[common.RootChainCallConfigKey])
	}
	raw, _ := json.Marshal(cfg["arguments"])
	var args []map[string]any
	_ = json.Unmarshal(raw, &args)
	if len(args) != 1 || args[0]["key"] != "user_id" {
		t.Fatalf("arguments=%s", raw)
	}
}

func TestBuildRuleNodes_RootChainCallDefinitionResponses(t *testing.T) {
	b := &DSLBuilder{}
	defs := map[string]*workflow.NodeDef{
		"N000010": {NodeID: "N000010", Name: "调用B", Type: common.RootChainCallNodeTypeName,
			Configuration: []byte(`{"target_chain_id":"R000002"}`),
			Params:        []byte(`[{"key":"uid","value":"","policy":"frontend+"}]`)},
		"N000009": {NodeID: "N000009", Name: "调用根链节点", Type: common.RootChainCallNodeTypeName},
	}
	overrides := map[string]map[string]interface{}{
		"N000009__abc": {common.RootChainCallTargetKey: map[string]any{"src": "value", "value": "R000002"}},
	}
	nodes := b.buildRuleNodes([]instanceRef{{baseId: "N000010", instanceId: "N000010__a"}, {baseId: "N000009", instanceId: "N000009__abc"}}, defs, overrides, nil, nil)
	if len(nodes) != 2 {
		t.Fatalf("nodes=%d", len(nodes))
	}
	if raw, ok := nodes[0].Configuration["responses"]; !ok || len(raw.([]any)) != 0 {
		t.Fatalf("definition node should have empty responses, got %v", nodes[0].Configuration["responses"])
	}
	if _, ok := nodes[1].Configuration["responses"]; ok {
		t.Fatalf("orchestration-picked node should not get responses definition")
	}
}
