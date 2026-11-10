package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/magic-lib/go-plat-workflow/workflow/common"
	"github.com/rulego/rulego/api/types"
)

func loopNode(id, bodyType, bodyID string) *types.RuleNode {
	return &types.RuleNode{
		Id:   id,
		Type: common.LoopNodeTypeName,
		Configuration: types.Configuration{
			common.LoopConfigField: map[string]any{"mode": "count", "count": "2", "body_type": bodyType, "body_id": bodyID},
		},
	}
}

func TestFlowSubChainIDs_IncludesLoopSubChain(t *testing.T) {
	nodes := []*types.RuleNode{
		{Id: "S1", Type: "flow", Configuration: types.Configuration{"ruleChainId": "p:S1"}},
		loopNode("N1__a", common.LoopBodySubChain, "S2"),
		loopNode("N1__b", common.LoopBodySubChain, "S1"),
		loopNode("N1__c", common.LoopBodyRootChain, "R9"),
	}
	if got := strings.Join(flowSubChainIDs(nodes), ","); got != "S1,S2" {
		t.Fatalf("got %s", got)
	}
}

func TestRootChainCallTarget(t *testing.T) {
	if target, label, ok := rootChainCallTarget(loopNode("N1__a", common.LoopBodyRootChain, "R9")); !ok || target != "R9" || label != "循环节点" {
		t.Fatalf("target=%s label=%s ok=%v", target, label, ok)
	}
	if _, _, ok := rootChainCallTarget(loopNode("N1__a", common.LoopBodySubChain, "S1")); ok {
		t.Fatalf("sub chain loop should not be a root chain call")
	}
	if target, _, ok := rootChainCallTarget(callNode("N2__a", "R3")); !ok || target != "R3" {
		t.Fatalf("target=%s ok=%v", target, ok)
	}
}

func TestCheckRootChainCalls_LoopSelfCall(t *testing.T) {
	s := &WorkflowService{}
	err := s.checkRootChainCalls(context.Background(), "p", "R1", []*types.RuleNode{loopNode("N1__a", common.LoopBodyRootChain, "R1")})
	if err == nil || !strings.Contains(err.Error(), "循环调用") {
		t.Fatalf("expected self call error, got %v", err)
	}
	err = s.checkRootChainCalls(context.Background(), "p", "R1", []*types.RuleNode{loopNode("N1__a", common.LoopBodyRootChain, "")})
	if err == nil || !strings.Contains(err.Error(), "循环节点 N1__a 未选择") {
		t.Fatalf("expected empty target error, got %v", err)
	}
}

func TestParseReleasedDSL_LoopSubChain(t *testing.T) {
	chain := &types.RuleChain{}
	chain.Metadata.Nodes = []*types.RuleNode{loopNode("N1__a", common.LoopBodySubChain, "S7"), loopNode("N1__b", common.LoopBodyRootChain, "R2")}
	b, _ := json.Marshal(chain)
	_, _, subs, ok := parseReleasedDSL(string(b))
	if !ok || strings.Join(subs, ",") != "S7" {
		t.Fatalf("subs=%v ok=%v", subs, ok)
	}
}
