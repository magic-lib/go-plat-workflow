package rulegox

import (
	"reflect"
	"testing"

	"github.com/magic-lib/go-plat-workflow/workflow/config"
)

func TestExecActivityResponse_NilNotAllowed(t *testing.T) {
	w := &MQWorker{}
	rvs := []*config.ReturnValue{{Key: "user_id", Type: "int64"}}
	data, err := w.execActivityResponse("ns/act", nil, nil, rvs, "trace", false)
	if err == nil {
		t.Fatalf("expected error when nil not allowed, got data=%v", data)
	}
}

func TestExecActivityResponse_NilAllowedObject(t *testing.T) {
	w := &MQWorker{}
	rvs := []*config.ReturnValue{
		{Key: "user_id", Type: "int64"},
		{Key: "mobile", Type: "string"},
		{Key: "tags", Type: "slice"},
	}
	data, err := w.execActivityResponse("ns/act", nil, nil, rvs, "trace", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("expected new object map, got %T(%v)", data, data)
	}
	if m["mobile"] != "" {
		t.Errorf("mobile want empty string, got %#v", m["mobile"])
	}
	if v, ok := m["user_id"]; !ok || reflect.ValueOf(v).Convert(reflect.TypeOf(int64(0))).Int() != 0 {
		t.Errorf("user_id want 0, got %#v", m["user_id"])
	}
	if s, ok := m["tags"].([]any); !ok || len(s) != 0 {
		t.Errorf("tags want empty slice, got %#v", m["tags"])
	}
}

func TestExecActivityResponse_NilAllowedScalar(t *testing.T) {
	w := &MQWorker{}
	data, err := w.execActivityResponse("ns/act", nil, nil, []*config.ReturnValue{{Key: "", Type: "bool"}}, "trace", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != false {
		t.Errorf("want false, got %#v", data)
	}
}

func TestNilReturnPlaceholder(t *testing.T) {
	cases := []struct {
		name string
		rvs  []*config.ReturnValue
		want any
	}{
		{"named key -> new object", []*config.ReturnValue{{Key: "a", Type: "string"}}, map[string]any{}},
		{"scalar string", []*config.ReturnValue{{Key: "", Type: "string"}}, ""},
		{"scalar map -> new object", []*config.ReturnValue{{Key: "", Type: "map"}}, map[string]any{}},
		{"scalar no type -> new object", []*config.ReturnValue{{Key: ""}}, map[string]any{}},
		{"nil item skipped", []*config.ReturnValue{nil, {Key: "b"}}, map[string]any{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := nilReturnPlaceholder(c.rvs)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %#v, want %#v", got, c.want)
			}
		})
	}
}
