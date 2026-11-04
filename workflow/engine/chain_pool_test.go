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
