package builder

import (
	"encoding/json"
	"testing"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
	"github.com/rulego/rulego/api/types"
)

func TestBuildRuleNodes_LoopConfig(t *testing.T) {
	b := &DSLBuilder{}
	defs := map[string]*workflow.NodeDef{
		"N000030": {NodeID: "N000030", Name: "循环节点", Type: common.LoopNodeTypeName},
	}
	loopCfg := `{"mode":"foreach","range":"{{steps.N1__a.responses.list}}","body_type":"rootchain","body_id":"R000002","max_iterations":50}`
	overrides := map[string]map[string]interface{}{
		"N000030__abc": {
			common.LoopConfigKey: map[string]any{"src": "fixed", "value": loopCfg},
			"uid":                map[string]any{"src": "fixed", "value": "{{arguments._loop.item.uid}}"},
		},
	}
	nodes := b.buildRuleNodes([]instanceRef{{baseId: "N000030", instanceId: "N000030__abc"}}, defs, overrides, nil, nil)
	if len(nodes) != 1 {
		t.Fatalf("nodes=%d", len(nodes))
	}
	cfg := nodes[0].Configuration
	loop, _ := cfg[common.LoopConfigField].(map[string]any)
	if loop["body_type"] != "rootchain" || loop["body_id"] != "R000002" || loop["range"] != "{{steps.N1__a.responses.list}}" {
		t.Fatalf("loop=%v", cfg[common.LoopConfigField])
	}
	raw, _ := json.Marshal(cfg["arguments"])
	var args []map[string]any
	_ = json.Unmarshal(raw, &args)
	if len(args) != 1 || args[0]["key"] != "uid" || args[0]["value"] != "{{arguments._loop.item.uid}}" {
		t.Fatalf("arguments=%s", raw)
	}
}

func TestParseLoopConfigOverride(t *testing.T) {
	if got := parseLoopConfigOverride(`{"mode":"count"}`); got["mode"] != "count" {
		t.Fatalf("got %v", got)
	}
	if got := parseLoopConfigOverride(map[string]any{"mode": "while"}); got["mode"] != "while" {
		t.Fatalf("got %v", got)
	}
	if got := parseLoopConfigOverride("not json"); got != nil {
		t.Fatalf("got %v", got)
	}
	if got := parseLoopConfigOverride(""); got != nil {
		t.Fatalf("got %v", got)
	}
}

func TestCollectInputArguments_SkipLoopVars(t *testing.T) {
	rc := &types.RuleChain{}
	dsl := `{"ruleChain":{"id":"R1"},"metadata":{"nodes":[
	  {"id":"N1__a","type":"custom/Activity","configuration":{"arguments":[
	    {"key":"x","value":"{{arguments._loop.item.uid}}"},
	    {"key":"y","value":"{{arguments._loop}}"},
	    {"key":"z","value":"{{arguments._loopx}}"},
	    {"key":"m","value":"{{arguments.mobile}}"}]}}
	]}}`
	if err := json.Unmarshal([]byte(dsl), rc); err != nil {
		t.Fatal(err)
	}
	got := CollectRequiredInputArguments(rc.Metadata.Nodes)
	if len(got) != 2 || got[0] != "_loopx" || got[1] != "mobile" {
		t.Fatalf("got %v", got)
	}
}
