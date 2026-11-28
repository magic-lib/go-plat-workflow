package repo

import (
	"strings"
	"testing"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

func TestFormatBizID(t *testing.T) {
	cases := []struct {
		prefix string
		id     uint
		want   string
	}{
		{"N", 1, "N000001"},
		{"F", 12, "F000012"},
		{"M", 999999, "M999999"},
		// 超过 6 位自然变为 7 位，不截断
		{"T", 1000000, "T1000000"},
	}
	for _, c := range cases {
		if got := formatBizID(c.prefix, c.id); got != c.want {
			t.Fatalf("formatBizID(%q, %d) = %q, want %q", c.prefix, c.id, got, c.want)
		}
	}
}

func TestNeedAutoBizID(t *testing.T) {
	if !needAutoBizID("") {
		t.Fatal("empty id should be auto")
	}
	if !needAutoBizID(workflow.NewTempBizID()) {
		t.Fatal("temp id should be auto")
	}
	if needAutoBizID("N000012") {
		t.Fatal("explicit id should not be auto")
	}
}

func TestNewTempBizID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := workflow.NewTempBizID()
		if !strings.HasPrefix(id, workflow.TempBizIDPrefix) || !workflow.IsTempBizID(id) {
			t.Fatalf("bad temp id %q", id)
		}
		if len(id) > 64 {
			t.Fatalf("temp id too long: %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate temp id %q", id)
		}
		seen[id] = true
	}
}
