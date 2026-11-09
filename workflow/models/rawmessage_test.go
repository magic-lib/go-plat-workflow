package models

import (
	"encoding/json"
	"testing"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

func TestJSONColumnValue(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "{}"},
		{"spaces", "  \n ", "{}"},
		{"object", `{"a":1}`, `{"a":1}`},
		{"null", "null", "{}"},
		{"array", `[1,2]`, `[1,2]`},
		{"plain text", "abc", `"abc"`},
		{"broken json", `{"a":`, `"{\"a\":"`},
	}
	for _, c := range cases {
		got := jsonColumnValue([]byte(c.in))
		if got != c.want {
			t.Errorf("%s: jsonColumnValue(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
		if !json.Valid([]byte(got)) {
			t.Errorf("%s: result %q is not valid JSON", c.name, got)
		}
	}
}

func TestNodeLogModelFromDefEmptyJSON(t *testing.T) {
	var m NodeLogModel
	m.FromDef(&workflow.NodeLogDef{Level: "flow_total"})
	if m.Payload != "{}" || m.Arguments != "{}" {
		t.Fatalf("empty payload/arguments should be stored as {}, got payload=%q arguments=%q", m.Payload, m.Arguments)
	}
}
