package workflow

import (
	"encoding/json"
	"testing"
)

func TestNodeFieldBriefs(t *testing.T) {
	if NodeFieldBriefs(nil) != nil || NodeFieldBriefs(json.RawMessage(`[]`)) != nil || NodeFieldBriefs(json.RawMessage(`null`)) != nil {
		t.Fatal("empty input should return nil")
	}
	if NodeFieldBriefs(json.RawMessage(`{bad`)) != nil {
		t.Fatal("invalid json should return nil")
	}

	got := NodeFieldBriefs(json.RawMessage(`[{"key":"a","label":"甲","type":"string"},{"key":"b"}]`))
	if len(got) != 2 || got[0].Key != "a" || got[0].Label != "甲" || got[1].Key != "b" || got[1].Label != "" {
		t.Fatalf("unexpected briefs: %+v %+v", got[0], got[1])
	}

	// 历史数据以字符串形式存放的数组
	got = NodeFieldBriefs(json.RawMessage(`"[{\"key\":\"x\",\"label\":\"X\"}]"`))
	if len(got) != 1 || got[0].Key != "x" || got[0].Label != "X" {
		t.Fatalf("string-encoded array not parsed: %+v", got)
	}

	// 没有 key 时回退 name
	got = NodeFieldBriefs(json.RawMessage(`[{"name":"n1","label":"名"}]`))
	if len(got) != 1 || got[0].Key != "n1" {
		t.Fatalf("name fallback failed: %+v", got)
	}
}
