package builder

import (
	"strings"
	"testing"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/engine"
)

// 同一子链添加两次：每个实例生成一个 flow 节点，节点 ID 为实例 ID，引用的子链仍为 project:子链ID；
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
		if n.Type != "flow" || n.Configuration["targetId"] != want[n.Id] || n.Configuration["ruleChainId"] != want[n.Id] {
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
