package service

import (
	"testing"

	"github.com/rulego/rulego/api/types"
)

func TestCheckSubChainSingleExit(t *testing.T) {
	chain := func(conns ...types.NodeConnection) *types.RuleChain {
		return &types.RuleChain{Metadata: types.RuleMetadata{Connections: conns}}
	}
	ok := []*types.RuleChain{
		chain(), // 单节点
		chain(types.NodeConnection{FromId: "a", ToId: "b", Type: types.Success}, types.NodeConnection{FromId: "b", ToId: "c", Type: types.Success}),
		// 条件分支：每次只走一条
		chain(types.NodeConnection{FromId: "a", ToId: "b", Type: types.True}, types.NodeConnection{FromId: "a", ToId: "c", Type: types.False}),
		chain(types.NodeConnection{FromId: "a", ToId: "b", Type: types.Success}, types.NodeConnection{FromId: "a", ToId: "c", Type: types.Failure}),
	}
	for i, c := range ok {
		if err := checkSubChainSingleExit("S1", c); err != nil {
			t.Errorf("case %d should pass: %v", i, err)
		}
	}
	fanOut := chain(types.NodeConnection{FromId: "a", ToId: "b", Type: types.Success}, types.NodeConnection{FromId: "a", ToId: "c", Type: types.Success})
	if err := checkSubChainSingleExit("S1", fanOut); err == nil {
		t.Fatal("parallel fan-out should be rejected")
	}
}
