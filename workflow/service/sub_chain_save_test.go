package service

import (
	"context"
	"reflect"
	"testing"

	"github.com/rulego/rulego/api/types"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
	"github.com/magic-lib/go-plat-workflow/workflow/engine"
)

func TestSubChainDSLChanged(t *testing.T) {
	base := `{"ruleChain":{"id":"F1","name":"旧名"},"metadata":{"nodes":[{"id":"a"}],"connections":[]}}`
	cases := []struct {
		name string
		dsl  string
		want bool
	}{
		{"键顺序不同（数据库 json 列重排）", `{"metadata":{"connections":[],"nodes":[{"id":"a"}]},"ruleChain":{"name":"旧名","id":"F1"}}`, false},
		{"只改名称", `{"ruleChain":{"id":"F1","name":"新名"},"metadata":{"nodes":[{"id":"a"}],"connections":[]}}`, false},
		{"改了节点", `{"ruleChain":{"id":"F1","name":"旧名"},"metadata":{"nodes":[{"id":"b"}],"connections":[]}}`, true},
		{"非法 JSON", `{`, true},
	}
	for _, c := range cases {
		if got := subChainDSLChanged(base, c.dsl); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSplitSubChainBaseIDs(t *testing.T) {
	got := splitSubChainBaseIDs("F000001__aaaaa, F000001__bbbbb,F000002,")
	if want := []string{"F000001", "F000001", "F000002"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// 带实例后缀的引用也能被引用检查识别
	roots := []*subChainRootRef{{ChainID: "R000001", Name: "根", SubChainIDs: splitSubChainBaseIDs("F000003__x1y2z")}}
	if hits := rootChainsReferencingSubChain("F000003", roots, nil); !reflect.DeepEqual(hits, []string{"R000001 根"}) {
		t.Fatalf("hits=%v", hits)
	}
}

// 子链管理弹框只提交 name/description/status/dsl_json：编排字段沿用原值；DSL 改了时 sub_chain_ids 按 flow 节点重算。
func TestMergeSubChainOrchFields(t *testing.T) {
	dsl := `{"ruleChain":{"id":"F1","name":"a"},"metadata":{"nodes":[{"id":"N1__x","type":"custom/Activity"},{"id":"F2__abcde","type":"flow","configuration":{"ruleChainId":"p:F2"}}]}}`
	existing := &workflow.SubChainDef{DSLJSON: dsl, NodeIDs: "N1__x", SubChainIDs: "F2__abcde", ConnectionsData: `[{"from_id":"N1__x"}]`, NodeParamOverrides: `{"N1__x":{}}`}

	// 只改名称：编排字段全部沿用
	def := &workflow.SubChainDef{Name: "新名", DSLJSON: dsl}
	mergeSubChainOrchFields(def, existing)
	if def.NodeIDs != "N1__x" || def.SubChainIDs != "F2__abcde" || def.ConnectionsData != existing.ConnectionsData || def.NodeParamOverrides != existing.NodeParamOverrides {
		t.Fatalf("orch fields not kept: %+v", def)
	}

	// 没带 dsl_json：沿用原 DSL
	def = &workflow.SubChainDef{Name: "x"}
	mergeSubChainOrchFields(def, existing)
	if def.DSLJSON != dsl {
		t.Fatalf("dsl not kept: %s", def.DSLJSON)
	}

	// 手动改 DSL 加了一个 flow 节点（任意 ID f9 引用 F3）：sub_chain_ids 按 DSL 重算
	changed := `{"ruleChain":{"id":"F1"},"metadata":{"nodes":[{"id":"F2__abcde","type":"flow","configuration":{"ruleChainId":"p:F2"}},{"id":"f9","type":"flow","configuration":{"ruleChainId":"p:F3"}}]}}`
	def = &workflow.SubChainDef{DSLJSON: changed}
	mergeSubChainOrchFields(def, existing)
	if def.SubChainIDs != "F2__abcde,F3" {
		t.Fatalf("sub_chain_ids=%q", def.SubChainIDs)
	}
}

// collectSubChains 优先用链里嵌入的子链内容，不查库（subChainRepo 为空也能取到）。
func TestCollectSubChainsUsesEmbedded(t *testing.T) {
	root := &types.RuleChain{}
	eid, err := engine.EmbedSubChain(root, "F1", `{"ruleChain":{"id":"F1"},"metadata":{"nodes":[{"id":"s1","type":"jsTransform"}]}}`)
	if err != nil {
		t.Fatal(err)
	}
	root.Metadata.Nodes = []*types.RuleNode{{Id: "F1__aaaaa", Type: "flow", Configuration: types.Configuration{"ruleChainId": "p:F1", "targetId": "p:" + eid}}}
	subs, nodes, err := (&WorkflowService{}).collectSubChains(context.Background(), "p", root)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs[0].RuleChain.ID != eid || len(nodes) != 1 || nodes[0].Id != "s1" {
		t.Fatalf("subs=%v nodes=%v", subs, nodes)
	}
}

// 入参作用域：经旧 flow 节点引用的子链与本链共用入参（计入 argNodes），经调用子链节点引用的不计入，
// 其下级子链即使经 flow 节点引用也不计入；同一子链既被 flow 又被调用子链节点引用时按共用入参计入一次。
func TestCollectSubChainsArgNodesScope(t *testing.T) {
	root := &types.RuleChain{}
	inner, err := engine.EmbedSubChain(root, "F3", `{"ruleChain":{"id":"F3"},"metadata":{"nodes":[{"id":"s3","type":"jsTransform"}]}}`)
	if err != nil {
		t.Fatal(err)
	}
	scopedDSL := `{"ruleChain":{"id":"F2"},"metadata":{"nodes":[{"id":"s2","type":"jsTransform"},` +
		`{"id":"F3__x","type":"flow","configuration":{"ruleChainId":"p:F3","targetId":"p:` + inner + `"}}]}}`
	scoped, err := engine.EmbedSubChain(root, "F2", scopedDSL)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := engine.EmbedSubChain(root, "F1", `{"ruleChain":{"id":"F1"},"metadata":{"nodes":[{"id":"s1","type":"jsTransform"}]}}`)
	if err != nil {
		t.Fatal(err)
	}
	root.Metadata.Nodes = []*types.RuleNode{
		{Id: "F2__a", Type: common.SubChainCallNodeTypeName, Configuration: types.Configuration{"ruleChainId": "p:F2", "targetId": "p:" + scoped}},
		{Id: "F1__a", Type: "flow", Configuration: types.Configuration{"ruleChainId": "p:F1", "targetId": "p:" + shared}},
		{Id: "F1__b", Type: common.SubChainCallNodeTypeName, Configuration: types.Configuration{"ruleChainId": "p:F1", "targetId": "p:" + shared}},
	}
	subs, nodes, argNodes, err := (&WorkflowService{}).collectSubChainsWithArgNodes(context.Background(), "p", root)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 3 || len(nodes) != 4 {
		t.Fatalf("subs=%d nodes=%d", len(subs), len(nodes))
	}
	if len(argNodes) != 1 || argNodes[0].Id != "s1" {
		t.Fatalf("argNodes=%v", argNodes)
	}
}

// 子链返回值定义取自子链 DSL 的 additionalInfo.root_responses，空 key 去掉。
func TestSubChainResponses(t *testing.T) {
	chain := &types.RuleChain{}
	if got := subChainResponses(chain); len(got) != 0 {
		t.Fatalf("got=%v", got)
	}
	chain.RuleChain.AdditionalInfo = map[string]interface{}{"root_responses": []any{
		map[string]any{"key": "out", "label": "结果", "type": "int64"},
		map[string]any{"key": " "},
	}}
	got := subChainResponses(chain)
	if len(got) != 1 || got[0].Key != "out" || got[0].Type != "int64" {
		t.Fatalf("got=%v", got)
	}
}
