package service

import (
	"context"
	"strings"
	"testing"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
	"github.com/rulego/rulego/api/types"
)

func callNode(id, target string, argKeys ...string) *types.RuleNode {
	args := make([]any, 0, len(argKeys))
	for _, k := range argKeys {
		args = append(args, map[string]any{"key": k, "value": "{{arguments." + k + "}}"})
	}
	return &types.RuleNode{
		Id:   id,
		Type: common.RootChainCallNodeTypeName,
		Configuration: types.Configuration{
			common.RootChainCallConfigKey: target,
			"arguments":                   args,
		},
	}
}

func TestMissingRootChainCallArgs(t *testing.T) {
	n := callNode("N1__a", "R2", "uid", "mobile")
	if got := missingRootChainCallArgs(n, []string{"uid", "mobile"}); len(got) != 0 {
		t.Fatalf("unexpected missing: %v", got)
	}
	if got := missingRootChainCallArgs(n, []string{"uid", "N3__x.card"}); strings.Join(got, ",") != "N3__x.card" {
		t.Fatalf("unexpected missing: %v", got)
	}
}

func TestCheckRootChainCalls_SelfAndEmptyTarget(t *testing.T) {
	s := &WorkflowService{}
	if err := s.checkRootChainCalls(context.Background(), "p", "R1", []*types.RuleNode{callNode("N1__a", "R1")}); err == nil || !strings.Contains(err.Error(), "循环") {
		t.Fatalf("expected self call error, got %v", err)
	}
	if err := s.checkRootChainCalls(context.Background(), "p", "R1", []*types.RuleNode{callNode("N1__a", "")}); err == nil || !strings.Contains(err.Error(), "未选择") {
		t.Fatalf("expected empty target error, got %v", err)
	}
	// 非调用根链节点不受影响
	if err := s.checkRootChainCalls(context.Background(), "p", "R1", []*types.RuleNode{{Id: "N2", Type: common.ActivityNodeTypeName}}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestTestRootChainCallNode_RequiresTarget(t *testing.T) {
	s := &WorkflowService{}
	nodeDef := &workflow.NodeDef{Project: "p1", NodeID: "N000009", Type: common.RootChainCallNodeTypeName, Configuration: []byte(`{}`)}
	if _, err := s.testRootChainCallNode(context.Background(), &TestNodeRequest{Project: "p1", NodeID: "N000009"}, nodeDef); err == nil || !strings.Contains(err.Error(), "未固定目标根链") {
		t.Fatalf("expected missing target error, got %v", err)
	}
}
