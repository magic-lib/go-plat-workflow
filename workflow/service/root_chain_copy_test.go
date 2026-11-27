package service

import (
	"encoding/json"
	"testing"
)

func TestRewriteRootChainDSLIdentity(t *testing.T) {
	src := `{"ruleChain":{"id":"R000001","name":"A","root":true,"additionalInfo":{"k":"v"}},"metadata":{"nodes":[{"id":"N1__a"}],"connections":[]}}`
	got, err := rewriteRootChainDSLIdentity(src, "R000009", "A-副本")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatal(err)
	}
	rc := m["ruleChain"].(map[string]any)
	if rc["id"] != "R000009" || rc["name"] != "A-副本" || rc["root"] != true || rc["additionalInfo"].(map[string]any)["k"] != "v" {
		t.Fatalf("ruleChain=%v", rc)
	}
	nodes := m["metadata"].(map[string]any)["nodes"].([]any)
	if len(nodes) != 1 || nodes[0].(map[string]any)["id"] != "N1__a" {
		t.Fatalf("metadata changed: %v", m["metadata"])
	}
	if got, _ := rewriteRootChainDSLIdentity(" {} ", "R2", "x"); got != "{}" {
		t.Fatalf("empty dsl got %s", got)
	}
	if _, err := rewriteRootChainDSLIdentity("{bad", "R2", "x"); err == nil {
		t.Fatalf("expected parse error")
	}
}
