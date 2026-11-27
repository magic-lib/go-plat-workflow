package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFlowShareTokenRoundTrip(t *testing.T) {
	now := time.Now()
	p := flowSharePayload{Project: "yangloan", ChainID: "R000012", TraceID: "trace-1", ExpiresAt: now.Add(time.Hour).Unix()}
	token, err := signFlowShareToken(p, "secret-b")
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	// 任一项目密钥验过即可
	got, err := verifyFlowShareToken(token, []string{"secret-a", "secret-b"}, now)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got != p {
		t.Fatalf("payload mismatch: got %+v want %+v", got, p)
	}
}

func TestFlowShareTokenRejects(t *testing.T) {
	now := time.Now()
	valid, _ := signFlowShareToken(flowSharePayload{Project: "p", ChainID: "R1", TraceID: "t1", ExpiresAt: now.Add(time.Hour).Unix()}, "s1")
	expired, _ := signFlowShareToken(flowSharePayload{Project: "p", ChainID: "R1", ExpiresAt: now.Add(-time.Second).Unix()}, "s1")

	// 篡改载荷（换成另一个 trace）但沿用原签名
	_, _, sig, _ := parseFlowShareToken(valid)
	forgedBody, _ := signFlowShareToken(flowSharePayload{Project: "p", ChainID: "R1", TraceID: "other", ExpiresAt: now.Add(time.Hour).Unix()}, "x")
	body, _, _ := strings.Cut(forgedBody, ".")
	tampered := body + "." + sig

	cases := map[string]struct {
		token   string
		secrets []string
	}{
		"wrong secret": {valid, []string{"s2"}},
		"no secrets":   {valid, nil},
		"expired":      {expired, []string{"s1"}},
		"tampered":     {tampered, []string{"s1"}},
		"garbage":      {"not-a-token", []string{"s1"}},
		"empty":        {"", []string{"s1"}},
	}
	for name, c := range cases {
		if _, err := verifyFlowShareToken(c.token, c.secrets, now); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestSlimFlowDSLStripsConfig(t *testing.T) {
	dsl := `{"ruleChain":{"id":"R1"},"metadata":{"nodes":[
		{"id":"N1__a","name":"查询","type":"custom/Activity","configuration":{"url":"http://secret","node_config":{"switch_condition":"x > 1","params":{"k":"v"}}}},
		{"id":"N2__b","name":"调用","type":"custom/RootChainCall","configuration":{"target_chain_id":"R2","token":"abc"}}
	]}}`
	out := slimFlowDSL(dsl)
	for _, leak := range []string{"http://secret", "params", "abc"} {
		if strings.Contains(out, leak) {
			t.Fatalf("slim dsl leaked %q: %s", leak, out)
		}
	}
	var got struct {
		Metadata struct {
			Nodes []struct {
				ID            string `json:"id"`
				Name          string `json:"name"`
				Type          string `json:"type"`
				Configuration struct {
					NodeConfig struct {
						SwitchCondition string `json:"switch_condition"`
					} `json:"node_config"`
					TargetChainID string `json:"target_chain_id"`
				} `json:"configuration"`
			} `json:"nodes"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Metadata.Nodes) != 2 {
		t.Fatalf("want 2 nodes, got %d", len(got.Metadata.Nodes))
	}
	n1, n2 := got.Metadata.Nodes[0], got.Metadata.Nodes[1]
	if n1.ID != "N1__a" || n1.Name != "查询" || n1.Configuration.NodeConfig.SwitchCondition != "x > 1" {
		t.Fatalf("node1 wrong: %+v", n1)
	}
	if n2.Configuration.TargetChainID != "R2" {
		t.Fatalf("node2 wrong: %+v", n2)
	}
	if slimFlowDSL("") != "" || slimFlowDSL("{bad") != "" {
		t.Fatal("invalid dsl should give empty string")
	}
}

func TestFlowShareRoutesAuth(t *testing.T) {
	if !isPublicRequest(httptest.NewRequest("GET", "/api/flow-view/shared?share=x", nil)) {
		t.Fatal("GET /api/flow-view/shared should be public")
	}
	if isPublicRequest(httptest.NewRequest("POST", "/api/flow-view/share?project=p", nil)) {
		t.Fatal("POST /api/flow-view/share must require login")
	}
	if isPublicRequest(httptest.NewRequest("POST", "/api/flow-view/shared", nil)) {
		t.Fatal("only GET of /api/flow-view/shared is public")
	}
}

func TestResolveFlowShareTraceID(t *testing.T) {
	chainLevel := flowSharePayload{Project: "p", ChainID: "R1", ExpiresAt: 1}
	bound := flowSharePayload{Project: "p", ChainID: "R1", TraceID: "bound-trace", ExpiresAt: 1}

	// 按根链分享：用地址里的 trace_id，可任意替换，也可为空
	if got, err := resolveFlowShareTraceID(chainLevel, " trace-a "); err != nil || got != "trace-a" {
		t.Fatalf("chain-level: got %q, %v", got, err)
	}
	if got, err := resolveFlowShareTraceID(chainLevel, ""); err != nil || got != "" {
		t.Fatalf("chain-level empty: got %q, %v", got, err)
	}
	if _, err := resolveFlowShareTraceID(chainLevel, strings.Repeat("x", flowShareMaxTraceIDLen+1)); err == nil {
		t.Fatal("too long trace_id should be rejected")
	}
	// 按单次执行分享：只认 token 里绑定的，地址里改了不生效
	if got, err := resolveFlowShareTraceID(bound, "other-trace"); err != nil || got != "bound-trace" {
		t.Fatalf("bound: got %q, %v", got, err)
	}
}
