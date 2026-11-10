package service

import (
	"reflect"
	"testing"
)

func TestRootChainsReferencingSubChain(t *testing.T) {
	roots := []*subChainRootRef{
		{ChainID: "R000002", Name: "授信", SubChainIDs: []string{"F000001"}},
		{ChainID: "R000001", Name: "放款", SubChainIDs: []string{"F000003"}},
		{ChainID: "R000003", SubChainIDs: nil},
	}
	// F000003 嵌套引用 F000002；F000004 与 F000005 互相引用（循环）
	nested := map[string][]string{
		"F000003": {"F000002"},
		"F000004": {"F000005"},
		"F000005": {"F000004"},
	}
	cases := []struct {
		target string
		want   []string
	}{
		{"F000001", []string{"R000002 授信"}},
		{"F000002", []string{"R000001 放款"}},
		{"F000003", []string{"R000001 放款"}},
		{"F000009", nil},
	}
	for _, c := range cases {
		if got := rootChainsReferencingSubChain(c.target, roots, nested); !reflect.DeepEqual(got, c.want) {
			t.Errorf("target %s: got %v, want %v", c.target, got, c.want)
		}
	}
	// 循环引用不能死循环
	loop := []*subChainRootRef{{ChainID: "R000009", SubChainIDs: []string{"F000004"}}}
	if got := rootChainsReferencingSubChain("F000005", loop, nested); !reflect.DeepEqual(got, []string{"R000009"}) {
		t.Errorf("cycle: got %v", got)
	}
	if got := rootChainsReferencingSubChain("F000001", loop, nested); got != nil {
		t.Errorf("cycle without target: got %v", got)
	}
}
