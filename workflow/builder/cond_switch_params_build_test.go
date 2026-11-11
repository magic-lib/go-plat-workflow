package builder

import (
	"encoding/json"
	"testing"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
	"github.com/rulego/rulego/api/types"
)

// 条件判断节点：编排里定义过参数时以编排为准，节点定义里有、编排里删掉的参数（old）不再写入 arguments
func TestBuildRuleNodes_CondSwitchInputDefs(t *testing.T) {
	b := &DSLBuilder{}
	defs := map[string]*workflow.NodeDef{
		"N000020": {NodeID: "N000020", Name: "条件判断", Type: common.CondSwitchNodeTypeName,
			Params: json.RawMessage(`[{"key":"age","label":"年龄","type":"int64","value":"","policy":"frontend+"},{"key":"old","label":"旧参数","value":"1"}]`)},
	}
	inputDefs := `[{"key":"age","label":"年龄（编排）","type":"int64"},{"key":"score","label":"分数","type":"int64","value":"60"},{"key":"tag","label":"标签"}]`
	overrides := map[string]map[string]interface{}{
		"N000020__x": {
			common.ParamInputDefsKey: map[string]any{"src": "fixed", "value": inputDefs},
			"age":                    map[string]any{"src": "entry", "value": "{{arguments.age}}"},
			"old":                    map[string]any{"src": "fixed", "value": "2"},
			"score":                  map[string]any{"src": "upstream", "value": "{{steps.N1__a.responses.score}}", "type": "int64"},
		},
	}
	nodes := b.buildRuleNodes([]instanceRef{{baseId: "N000020", instanceId: "N000020__x"}}, defs, overrides, map[string]string{"N000020__x": "[arguments.score] > 50"}, nil)
	if len(nodes) != 1 {
		t.Fatalf("nodes=%d", len(nodes))
	}
	cfg := nodes[0].Configuration
	got, _ := json.Marshal(cfg[common.ParamInputConfigKey])
	if string(got) != `[{"key":"age","label":"年龄（编排）","type":"int64"},{"key":"score","label":"分数","type":"int64","value":"60"},{"key":"tag","label":"标签"}]` {
		t.Fatalf("input_params=%s", got)
	}
	raw, _ := json.Marshal(cfg["arguments"])
	var args []map[string]any
	_ = json.Unmarshal(raw, &args)
	if len(args) != 2 || args[0]["key"] != "age" || args[1]["key"] != "score" || args[1]["type"] != "int64" ||
		args[1]["value"] != "{{steps.N1__a.responses.score}}" {
		t.Fatalf("arguments=%s", raw)
	}
	for _, a := range args {
		if a["key"] == common.ParamInputDefsKey {
			t.Fatalf("reserved key leaked into arguments: %s", raw)
		}
	}
	labels := map[string]string{}
	_ = json.Unmarshal([]byte(nodes[0].AdditionalInfo["node_param_labels"].(string)), &labels)
	if labels["age"] != "年龄（编排）" || labels["old"] != "" || labels["score"] != "分数" || labels["tag"] != "标签" {
		t.Fatalf("labels=%v", labels)
	}
	nc, _ := cfg["node_config"].(map[string]any)
	if nc["switch_condition"] != "[arguments.score] > 50" {
		t.Fatalf("node_config=%v", nc)
	}
}

// 参数覆盖对齐：条件判断节点的本链参数定义及其来源不能被当成「节点定义已删除的参数」裁掉，
// 且以编排为准，不再按节点定义补回编排里删掉的参数
func TestReconcileNodeParamOverrides_CondSwitchKeepsInputDefs(t *testing.T) {
	nodes := []*workflow.NodeDef{
		{NodeID: "N000020", Type: common.CondSwitchNodeTypeName, Params: json.RawMessage(`[{"key":"age","value":"1"}]`)},
		{NodeID: "N000030", Type: common.ActivityNodeTypeName, Params: json.RawMessage(`[{"key":"age","value":"1"}]`)},
	}
	inputDefs := `[{"key":"score"}]`
	overrides := map[string]map[string]interface{}{
		"N000020__x": {
			common.ParamInputDefsKey: map[string]any{"src": "fixed", "value": inputDefs},
			"score":                  map[string]any{"src": "fixed", "value": "1"},
			"gone":                   map[string]any{"src": "fixed", "value": "1"},
		},
		"N000030__y": {
			common.ParamInputDefsKey: map[string]any{"src": "fixed", "value": inputDefs},
			"score":                  map[string]any{"src": "fixed", "value": "1"},
		},
	}
	out := reconcileNodeParamOverrides(overrides, nodes)
	cond := out["N000020__x"]
	if _, ok := cond[common.ParamInputDefsKey]; !ok {
		t.Fatalf("cond lost defs: %v", cond)
	}
	if _, ok := cond["score"]; !ok {
		t.Fatalf("cond lost score: %v", cond)
	}
	if _, ok := cond["gone"]; ok {
		t.Fatalf("cond kept removed key: %v", cond)
	}
	if _, ok := cond["age"]; ok {
		t.Fatalf("cond should not re-add node def param removed in orchestration: %v", cond)
	}
	act := out["N000030__y"]
	if _, ok := act["score"]; ok {
		t.Fatalf("activity should not keep undefined params: %v", act)
	}
}

// 条件判断节点本链参数配置了默认值时，调用方可不传
func TestCollectRequiredInputArguments_CondSwitchDefault(t *testing.T) {
	rc := &types.RuleChain{}
	dsl := `{"ruleChain":{"id":"R1"},"metadata":{"nodes":[
	  {"id":"N1__a","type":"custom/CondSwitch","configuration":{
	    "input_params":[{"key":"score","value":"60"},{"key":"tag"}],
	    "arguments":[{"key":"score","value":"{{arguments.score}}"},{"key":"tag","value":"{{arguments.tag}}"}]}}
	]}}`
	if err := json.Unmarshal([]byte(dsl), rc); err != nil {
		t.Fatal(err)
	}
	got := CollectRequiredInputArguments(rc.Metadata.Nodes)
	if len(got) != 1 || got[0] != "tag" {
		t.Fatalf("required got %v", got)
	}
}
