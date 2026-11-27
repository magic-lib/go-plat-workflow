package builder

import (
	"strings"
	"testing"

	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
	"github.com/magic-lib/go-plat-workflow/workflow/engine"
)

// 同一子链添加两次：每个实例生成一个调用子链节点，节点 ID 为实例 ID，引用的子链仍为 project:子链ID；
// 不带后缀的旧数据实例 ID 即子链 ID。
func TestBuildRuleChain_SubChainInstances(t *testing.T) {
	b := &DSLBuilder{}
	req := &workflow.BuildRequest{
		Project:     "p",
		ChainID:     "R000001",
		SubChainIDs: []string{"F000001__aaaaa", "F000001__bbbbb", "F000002"},
		Connections: []workflow.ConnectionDef{{FromID: "F000001__aaaaa", ToID: "F000001__bbbbb", Type: "Success"}},
	}
	subs := []*workflow.SubChainDef{{ChainID: "F000001", Name: "子链1"}, {ChainID: "F000002", Name: "子链2"}}
	chain := b.buildRuleChain(req, nil, subs)
	want := map[string]string{"F000001__aaaaa": "p:F000001", "F000001__bbbbb": "p:F000001", "F000002": "p:F000002"}
	if len(chain.Metadata.Nodes) != len(want) {
		t.Fatalf("nodes=%d, want %d", len(chain.Metadata.Nodes), len(want))
	}
	for _, n := range chain.Metadata.Nodes {
		if n.Type != common.SubChainCallNodeTypeName || n.Configuration["targetId"] != want[n.Id] || n.Configuration["ruleChainId"] != want[n.Id] {
			t.Fatalf("node %s: type=%s config=%v", n.Id, n.Type, n.Configuration)
		}
	}
	if c := chain.Metadata.Connections[0]; c.FromId != "F000001__aaaaa" || c.ToId != "F000001__bbbbb" {
		t.Fatalf("connection=%+v", c)
	}
	if got := dedupBaseIDs(req.SubChainIDs); len(got) != 2 || got[0] != "F000001" || got[1] != "F000002" {
		t.Fatalf("dedupBaseIDs=%v", got)
	}
}

// 保存链时子链内容嵌入 DSL：flow 节点 targetId 指向嵌入子链（子链ID#内容哈希），ruleChainId 仍为 project:子链ID；
// 子链内容变了哈希跟着变。
func TestBuildRuleChain_EmbedSubChain(t *testing.T) {
	b := &DSLBuilder{}
	req := &workflow.BuildRequest{Project: "p", ChainID: "R000001", SubChainIDs: []string{"F000001__aaaaa", "F000001__bbbbb"}}
	subDSL := `{"ruleChain":{"id":"F000001","name":"子链1"},"metadata":{"nodes":[{"id":"n1","type":"jsTransform"}]}}`
	chain := b.buildRuleChain(req, nil, []*workflow.SubChainDef{{ChainID: "F000001", Name: "子链1", DSLJSON: subDSL}})

	store, _ := chain.RuleChain.AdditionalInfo[engine.EmbeddedSubChainsKey].(map[string]interface{})
	if len(store) != 1 {
		t.Fatalf("embedded=%v", chain.RuleChain.AdditionalInfo)
	}
	var eid string
	for k := range store {
		eid = k
	}
	if !strings.HasPrefix(eid, "F000001#") {
		t.Fatalf("embedded id=%s", eid)
	}
	for _, n := range chain.Metadata.Nodes {
		if n.Configuration["targetId"] != "p:"+eid || n.Configuration["ruleChainId"] != "p:F000001" {
			t.Fatalf("node %s config=%v", n.Id, n.Configuration)
		}
	}

	changed := b.buildRuleChain(req, nil, []*workflow.SubChainDef{{ChainID: "F000001", DSLJSON: strings.Replace(subDSL, "n1", "n2", 1)}})
	if changed.Metadata.Nodes[0].Configuration["targetId"] == "p:"+eid {
		t.Fatal("sub chain content changed but embedded id not changed")
	}
}

// 子链实例的参数配置写入调用子链节点 arguments（按 key 排序），实例名称覆盖生效；没配置入参时为空数组。
func TestBuildRuleChain_SubChainCallArguments(t *testing.T) {
	b := &DSLBuilder{}
	req := &workflow.BuildRequest{
		Project:     "p",
		ChainID:     "R000001",
		SubChainIDs: []string{"F000001__aaaaa", "F000001__bbbbb"},
		NodeParamOverrides: map[string]map[string]interface{}{
			"F000001__aaaaa": {
				"uid":  map[string]any{"src": "upstream", "value": "{{steps.N000001__x.responses.uid}}", "type": "int64"},
				"name": map[string]any{"src": "entry", "value": "{{arguments.name}}"},
			},
		},
		NodeNameOverrides:   map[string]string{"F000001__aaaaa": "查询用户"},
		NodeSwitchOverrides: map[string]string{"F000001__aaaaa": " [responses.out] > 5 ", "F000001__bbbbb": "  "},
	}
	chain := b.buildRuleChain(req, nil, []*workflow.SubChainDef{{ChainID: "F000001", Name: "子链1"}})
	byID := map[string]int{}
	for i, n := range chain.Metadata.Nodes {
		byID[n.Id] = i
	}
	a := chain.Metadata.Nodes[byID["F000001__aaaaa"]]
	if got := conv.String(a.Configuration["arguments"]); got != `[{"key":"name","value":"{{arguments.name}}","policy":"backend"},{"key":"uid","value":"{{steps.N000001__x.responses.uid}}","type":"int64","policy":"backend"}]` {
		t.Fatalf("arguments=%s", got)
	}
	if a.Name != "查询用户" {
		t.Fatalf("name=%s", a.Name)
	}
	if got := conv.String(chain.Metadata.Nodes[byID["F000001__bbbbb"]].Configuration["arguments"]); got != `[]` {
		t.Fatalf("instance without overrides arguments=%s", got)
	}
	// 路由条件写入 node_config.switch_condition；空白条件不写
	if got := conv.String(a.Configuration["node_config"]); got != `{"switch_condition":"[responses.out] > 5"}` {
		t.Fatalf("node_config=%s", got)
	}
	if _, ok := chain.Metadata.Nodes[byID["F000001__bbbbb"]].Configuration["node_config"]; ok {
		t.Fatal("blank switch condition should not be written")
	}
}

// 保存根链时子链实例的参数配置不被节点定义对照裁掉。
func TestKeepSubChainOverrides(t *testing.T) {
	pruned := map[string]map[string]interface{}{
		"N000001__x":     {"a": "1"},
		"F000001__aaaaa": {"uid": map[string]any{"src": "entry", "value": "{{arguments.uid}}"}},
	}
	out := reconcileNodeParamOverrides(pruned, nil)
	if _, ok := out["F000001__aaaaa"]; ok {
		t.Fatal("reconcile should drop entries without node definition")
	}
	keepSubChainOverrides(out, pruned, []string{"F000001__aaaaa"})
	if _, ok := out["F000001__aaaaa"]; !ok {
		t.Fatalf("sub chain overrides lost: %v", out)
	}
	if _, ok := out["N000001__x"]; ok {
		t.Fatalf("node overrides without definition should stay dropped: %v", out)
	}
}

// 子链返回值定义写入子链 DSL 的 additionalInfo.root_responses。
func TestInjectSubChainResponses(t *testing.T) {
	b := &DSLBuilder{}
	chain := b.buildRuleChain(&workflow.BuildRequest{Project: "p", ChainID: "F000009"}, nil, nil)
	injectRootResponses(chain, []workflow.RootResponseItem{{Key: " out ", Label: "结果"}, {Key: ""}})
	if got := conv.String(chain.RuleChain.AdditionalInfo[rootResponsesDSLKey]); got != `[{"key":"out","label":"结果"}]` {
		t.Fatalf("root_responses=%s", got)
	}
}
