package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
)

// jsNode 生成一个 jsTransform 节点：给 msg 加上 key=val 后转发。
func jsNode(id, key, val string) string {
	return fmt.Sprintf(`{"id":%q,"type":"jsTransform","name":%q,"configuration":{"jsScript":"msg['%s']='%s'; return {'msg':msg,'metadata':metadata,'msgType':msgType};"}}`,
		id, id, key, val)
}

func chainDSL(id string, root bool, nodes []string, conns string) string {
	return fmt.Sprintf(`{"ruleChain":{"id":%q,"name":%q,"root":%v},"metadata":{"firstNodeIndex":0,"nodes":[%s],"connections":[%s]}}`,
		id, id, root, strings.Join(nodes, ","), conns)
}

func TestNormalizeFlowDSL(t *testing.T) {
	dsl := chainDSL("r", true, []string{`{"id":"f1","type":"flow","configuration":{"ruleChainId":"p:S1"}}`}, "")
	out := NormalizeFlowDSL([]byte(dsl))
	chain := new(types.RuleChain)
	if err := json.Unmarshal(out, chain); err != nil {
		t.Fatal(err)
	}
	cfg := chain.Metadata.Nodes[0].Configuration
	if cfg["targetId"] != "p:S1" || cfg["extend"] != true || cfg["ruleChainId"] != "p:S1" {
		t.Fatalf("flow node not normalized: %v", cfg)
	}
	// 已显式配置的不覆盖；没有 flow 节点时原样返回
	dsl2 := chainDSL("r", true, []string{`{"id":"f1","type":"flow","configuration":{"ruleChainId":"p:S1","targetId":"x","extend":false}}`}, "")
	if string(NormalizeFlowDSL([]byte(dsl2))) != dsl2 {
		t.Fatal("explicit targetId/extend should be kept as-is")
	}
	plain := chainDSL("r", true, []string{jsNode("n1", "a", "1")}, "")
	if string(NormalizeFlowDSL([]byte(plain))) != plain {
		t.Fatal("dsl without flow node should be returned unchanged")
	}
}

func TestLoadOrReloadUpdatesExistingChain(t *testing.T) {
	key := "test:reload_" + fmt.Sprint(time.Now().UnixNano())
	defer rulego.Del(key)
	if _, err := LoadOrReload(key, []byte(chainDSL(key, true, []string{jsNode("n1", "v", "old")}, ""))); err != nil {
		t.Fatal(err)
	}
	ins, err := LoadOrReload(key, []byte(chainDSL(key, true, []string{jsNode("n1", "v", "new")}, "")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ins.DSL()), "new") {
		t.Fatalf("existing instance not reloaded, dsl=%s", ins.DSL())
	}
}

func newTestEngine() *WorkflowEngine {
	return &WorkflowEngine{loadedChains: make(map[string]bool)}
}

// 根链一个节点扇出到两个结束节点：OnEnd 会回调两次，原实现第二次写 channel 时永久阻塞。
func TestExecuteWithEnvFanOutReturns(t *testing.T) {
	e := newTestEngine()
	project, chainID := "test", fmt.Sprint("fan_", time.Now().UnixNano())
	dsl := chainDSL(chainID, true,
		[]string{jsNode("n1", "a", "1"), jsNode("n2", "b", "2"), jsNode("n3", "c", "3")},
		`{"fromId":"n1","toId":"n2","type":"Success"},{"fromId":"n1","toId":"n3","type":"Success"}`)
	if err := e.LoadChainDSL(context.Background(), project, chainID, dsl, ""); err != nil {
		t.Fatal(err)
	}
	defer e.UnloadChain(context.Background(), project, chainID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := e.Execute(ctx, project, chainID, `{}`)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(data, `"a"`) {
		t.Fatalf("unexpected result: %s", data)
	}
}

// 根链 flow 节点只配了 ruleChainId（builder 历史产物/已发布快照），应能调到子链。
func TestFlowNodeWithRuleChainIdCallsSubChain(t *testing.T) {
	e := newTestEngine()
	project := "test"
	suffix := fmt.Sprint(time.Now().UnixNano())
	subID, rootID := "S_"+suffix, "R_"+suffix
	subKey := project + ":" + subID
	if _, err := LoadOrReload(subKey, []byte(chainDSL(subID, false, []string{jsNode("s1", "from_sub", "yes")}, ""))); err != nil {
		t.Fatal(err)
	}
	defer rulego.Del(subKey)
	root := chainDSL(rootID, true,
		[]string{jsNode("n1", "a", "1"), fmt.Sprintf(`{"id":"f1","type":"flow","configuration":{"ruleChainId":%q}}`, subKey)},
		`{"fromId":"n1","toId":"f1","type":"Success"}`)
	if err := e.LoadChainDSL(context.Background(), project, rootID, root, ""); err != nil {
		t.Fatal(err)
	}
	defer e.UnloadChain(context.Background(), project, rootID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := e.Execute(ctx, project, rootID, `{}`)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(data, "from_sub") {
		t.Fatalf("sub chain not executed, result: %s", data)
	}
}

// 未设置 firstNodeIndex 时按连线推算入口节点；已设置或推算结果就是 0 时不改动。
func TestNormalizeFirstNode(t *testing.T) {
	conns := []types.NodeConnection{{FromId: "s1", ToId: "s2", Type: "Success"}}
	nodes := []*types.RuleNode{{Id: "s2"}, {Id: "s1"}}
	chain := &types.RuleChain{Metadata: types.RuleMetadata{Nodes: nodes, Connections: conns}}
	if !NormalizeFirstNode(chain) || chain.Metadata.FirstNodeIndex != 1 {
		t.Fatalf("first node should be inferred as 1, got %d", chain.Metadata.FirstNodeIndex)
	}
	chain = &types.RuleChain{Metadata: types.RuleMetadata{FirstNodeIndex: 1, Nodes: []*types.RuleNode{{Id: "s1"}, {Id: "s2"}}, Connections: conns}}
	if NormalizeFirstNode(chain) || chain.Metadata.FirstNodeIndex != 1 {
		t.Fatal("explicit firstNodeIndex should be kept")
	}
	chain = &types.RuleChain{Metadata: types.RuleMetadata{Nodes: []*types.RuleNode{{Id: "s1"}, {Id: "s2"}}, Connections: conns}}
	if NormalizeFirstNode(chain) {
		t.Fatal("entry node already at index 0, should not change")
	}
}

// 子链节点按添加顺序排列、入口节点不在第一个时，子链也要从推算出的入口节点开始执行。
func TestSubChainStartsFromInferredFirstNode(t *testing.T) {
	e := newTestEngine()
	project := "test"
	suffix := fmt.Sprint(time.Now().UnixNano())
	subID, rootID := "F_"+suffix, "R_"+suffix
	subKey := project + ":" + subID
	// s2 先添加、s1 后添加，连线 s1 -> s2：入口应为 s1
	sub := chainDSL(subID, false,
		[]string{jsNode("s2", "from_s2", "yes"), jsNode("s1", "from_s1", "yes")},
		`{"fromId":"s1","toId":"s2","type":"Success"}`)
	if _, err := LoadOrReload(subKey, []byte(sub)); err != nil {
		t.Fatal(err)
	}
	defer rulego.Del(subKey)
	root := chainDSL(rootID, true,
		[]string{jsNode("n1", "a", "1"), fmt.Sprintf(`{"id":"f1","type":"flow","configuration":{"ruleChainId":%q}}`, subKey)},
		`{"fromId":"n1","toId":"f1","type":"Success"}`)
	if err := e.LoadChainDSL(context.Background(), project, rootID, root, ""); err != nil {
		t.Fatal(err)
	}
	defer e.UnloadChain(context.Background(), project, rootID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := e.Execute(ctx, project, rootID, `{}`)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(data, "from_s1") || !strings.Contains(data, "from_s2") {
		t.Fatalf("sub chain should run s1 then s2, result: %s", data)
	}
}

// 同一子链在根链里添加两次（两个 flow 实例节点指向同一子链），两次都要执行。
func TestSameSubChainTwoInstances(t *testing.T) {
	e := newTestEngine()
	project := "test"
	suffix := fmt.Sprint(time.Now().UnixNano())
	subID, rootID := "F_"+suffix, "R_"+suffix
	subKey := project + ":" + subID
	counter := `{"id":"c1","type":"jsTransform","name":"c1","configuration":{"jsScript":"msg['cnt']=String(Number(msg['cnt']||0)+1); return {'msg':msg,'metadata':metadata,'msgType':msgType};"}}`
	if _, err := LoadOrReload(subKey, []byte(chainDSL(subID, false, []string{counter}, ""))); err != nil {
		t.Fatal(err)
	}
	defer rulego.Del(subKey)
	flow := func(id string) string {
		return fmt.Sprintf(`{"id":%q,"type":"flow","configuration":{"ruleChainId":%q}}`, id, subKey)
	}
	root := chainDSL(rootID, true,
		[]string{jsNode("n1", "a", "1"), flow(subID + "__aaaaa"), flow(subID + "__bbbbb")},
		fmt.Sprintf(`{"fromId":"n1","toId":%q,"type":"Success"},{"fromId":%q,"toId":%q,"type":"Success"}`,
			subID+"__aaaaa", subID+"__aaaaa", subID+"__bbbbb"))
	if err := e.LoadChainDSL(context.Background(), project, rootID, root, ""); err != nil {
		t.Fatal(err)
	}
	defer e.UnloadChain(context.Background(), project, rootID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := e.Execute(ctx, project, rootID, `{}`)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(data, `"cnt":"2"`) {
		t.Fatalf("sub chain should run twice, result: %s", data)
	}
}

func TestSubChainBaseID(t *testing.T) {
	for in, want := range map[string]string{"F000001__ab12c": "F000001", "F000001": "F000001", "": ""} {
		if got := SubChainBaseID(in); got != want {
			t.Errorf("SubChainBaseID(%q)=%q, want %q", in, got, want)
		}
	}
}

// 嵌入子链：两条根链嵌入同一子链 ID 的不同内容（保存时间不同），各自执行自己嵌入的内容，互不覆盖。
func TestEmbeddedSubChainVersionsIsolated(t *testing.T) {
	e := newTestEngine()
	project := "test"
	suffix := fmt.Sprint(time.Now().UnixNano())
	subID := "F_" + suffix
	build := func(rootID, val string) string {
		root := &types.RuleChain{}
		eid, err := EmbedSubChain(root, subID, chainDSL(subID, false, []string{jsNode("s1", "ver", val)}, ""))
		if err != nil {
			t.Fatal(err)
		}
		info, _ := json.Marshal(root.RuleChain.AdditionalInfo)
		return fmt.Sprintf(`{"ruleChain":{"id":%q,"name":%q,"root":true,"additionalInfo":%s},"metadata":{"nodes":[%s,%s],"connections":[{"fromId":"n1","toId":"f1","type":"Success"}]}}`,
			rootID, rootID, info, jsNode("n1", "a", "1"),
			fmt.Sprintf(`{"id":"f1","type":"flow","configuration":{"ruleChainId":%q,"targetId":%q}}`, project+":"+subID, project+":"+eid))
	}
	rootA, rootB := "RA_"+suffix, "RB_"+suffix
	for id, val := range map[string]string{rootA: "v1", rootB: "v2"} {
		if err := e.LoadChainDSL(context.Background(), project, id, build(id, val), subID); err != nil {
			t.Fatal(err)
		}
		defer e.UnloadChain(context.Background(), project, id)
	}
	for id, want := range map[string]string{rootA: `"ver":"v1"`, rootB: `"ver":"v2"`} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		data, err := e.Execute(ctx, project, id, `{}`)
		cancel()
		if err != nil {
			t.Fatalf("execute %s failed: %v", id, err)
		}
		if !strings.Contains(data, want) {
			t.Fatalf("%s should run its embedded sub chain (%s), result: %s", id, want, data)
		}
	}
}

func TestExtractEmbeddedSubChainsNested(t *testing.T) {
	inner := &types.RuleChain{}
	innerID, _ := EmbedSubChain(inner, "F2", `{"ruleChain":{"id":"F2"},"metadata":{"nodes":[]}}`)
	innerDSL, _ := json.Marshal(inner)
	root := &types.RuleChain{}
	outerID, _ := EmbedSubChain(root, "F1", string(innerDSL))
	dsl, _ := json.Marshal(root)
	got, ok := ExtractEmbeddedSubChains(dsl)
	if !ok || len(got) != 2 || got[innerID] == nil || got[outerID] == nil {
		t.Fatalf("got %v ok=%v", got, ok)
	}
	if EmbeddedSubChainBaseID(outerID) != "F1" {
		t.Fatalf("base id of %s", outerID)
	}
}
