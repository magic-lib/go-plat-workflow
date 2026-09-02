package service

import (
	"testing"
)

// 构造一个发布 DSL：含两个活动节点（带实例后缀 id）、一个 flow 子链引用节点。
const testReleaseDSL = `{
  "ruleChain": {"id":"rc1","name":"根链"},
  "metadata": {
    "nodes": [
      {"id":"node_a__0","type":"custom/Activity","configuration":{"node_config":{"stages":[[{"act_namespace":"ns1","act_name":"actA"}],[{"act_namespace":"ns1","act_name":"actB"}]],"act_namespace":"ns1","act_name":"entry"}}},
      {"id":"node_b","type":"custom/Activity","configuration":{"node_config":{"act_namespace":"ns2","act_name":"actC"}}},
      {"id":"sub_1__0","type":"flow","configuration":{"ruleChainId":"sre:sub_x"}},
      {"id":"node_c__2","type":"custom/Sleep","configuration":{}}
    ],
    "connections": []
  }
}`

func TestParseReleasedDSL_Nodes(t *testing.T) {
	nodes, _, subIDs, ok := parseReleasedDSL(testReleaseDSL)
	if !ok {
		t.Fatal("parseReleasedDSL failed")
	}
	for _, want := range []string{"node_a", "node_b", "sub_1", "node_c"} {
		if _, found := nodes[want]; !found {
			t.Fatalf("expect node %q collected, got %v", want, nodes)
		}
	}
	if len(nodes) != 4 {
		t.Fatalf("expect 4 nodes, got %d: %v", len(nodes), nodes)
	}
	if len(subIDs) != 1 || subIDs[0] != "sub_x" {
		t.Fatalf("expect sub chain [sub_x], got %v", subIDs)
	}
}

func TestParseReleasedDSL_Activities(t *testing.T) {
	_, activities, _, ok := parseReleasedDSL(testReleaseDSL)
	if !ok {
		t.Fatal("parseReleasedDSL failed")
	}
	want := map[string]bool{
		"ns1\x00actA":  true,
		"ns1\x00actB":  true,
		"ns1\x00entry": true,
		"ns2\x00actC":  true,
	}
	if len(activities) != len(want) {
		t.Fatalf("expect %d activities, got %d: %v", len(want), len(activities), activities)
	}
	for k := range want {
		if _, found := activities[k]; !found {
			t.Fatalf("expect activity %q collected, got %v", k, activities)
		}
	}
}

func TestParseReleasedDSL_Invalid(t *testing.T) {
	if _, _, _, ok := parseReleasedDSL("not-json"); ok {
		t.Fatal("expect invalid DSL to return ok=false")
	}
}

func TestBaseNodeID(t *testing.T) {
	cases := map[string]string{
		"node_a__0": "node_a",
		"node_a":    "node_a",
		"a__1__2":   "a",
		"":          "",
	}
	for in, want := range cases {
		if got := baseNodeID(in); got != want {
			t.Fatalf("baseNodeID(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestFlowSubChainID(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`{"ruleChainId":"sre:sub_x"}`, "sub_x"},
		{`{"ruleChainId":"sub_y"}`, "sub_y"},
		{`{}`, ""},
		{``, ""},
		{`bad-json`, ""},
	}
	for _, c := range cases {
		if got := flowSubChainID([]byte(c.raw)); got != c.want {
			t.Fatalf("flowSubChainID(%q)=%q, want %q", c.raw, got, c.want)
		}
	}
}

func TestSplitCSV(t *testing.T) {
	if got := splitCSV(""); len(got) != 0 {
		t.Fatalf("expect empty, got %v", got)
	}
	got := splitCSV(" a , b ,,c ")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("unexpected split result: %v", got)
	}
}
