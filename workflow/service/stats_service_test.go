package service

import (
	"math"
	"testing"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// bucketOf 与 SQL 的 FLOOR(LN(ms+1)*10) 一致。
func bucketOf(ms int64) int {
	return int(math.Floor(math.Log(float64(ms)+1) * 10))
}

func TestHistPercentile(t *testing.T) {
	if got := histPercentile(nil, 0, 0.95); got != 0 {
		t.Errorf("empty = %d, want 0", got)
	}
	// 95 次 100ms + 5 次 10s：P95 落在 100ms 所在桶，上界与 100ms 相差不超过 ~10%
	hist := map[int]int64{bucketOf(100): 95, bucketOf(10000): 5}
	got := histPercentile(hist, 100, 0.95)
	if got < 100 || got > 111 {
		t.Errorf("p95 = %d, want ~100ms bucket upper bound", got)
	}
	// 再多 1 次慢请求，P95 就落到 10s 桶
	hist = map[int]int64{bucketOf(100): 94, bucketOf(10000): 6}
	got = histPercentile(hist, 100, 0.95)
	if got < 10000 || got > 11100 {
		t.Errorf("p95 = %d, want ~10s bucket upper bound", got)
	}
}

func TestBucketUpperCoversValue(t *testing.T) {
	for _, ms := range []int64{0, 1, 7, 100, 999, 1000, 60000, 86400000} {
		if up := bucketUpperMs(bucketOf(ms)); up < ms {
			t.Errorf("bucket upper %d < value %d", up, ms)
		}
	}
}

func TestReleaseVersion(t *testing.T) {
	cases := map[string]int{"R000005@3": 3, "R000002@39": 39, "": 0, "R000002": 0, "R@x": 0}
	for in, want := range cases {
		if got := releaseVersion(in); got != want {
			t.Errorf("releaseVersion(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestBuildRootChainStats(t *testing.T) {
	names := map[string]string{"R1": "链一", "R2": "链二"}
	row := func(rc, rel string, ms, count, failed, last int64) workflow.StatsExecBucketRow {
		return workflow.StatsExecBucketRow{RootChainID: rc, ReleaseID: rel, Bucket: bucketOf(ms), Count: count, Failed: failed, SumMs: ms * count, LastMs: last}
	}
	rows := []workflow.StatsExecBucketRow{
		row("R1", "R1@2", 100, 1, 0, 1000),
		row("R1", "R1@2", 300, 1, 1, 3000),
		row("R1", "R1@1", 200, 1, 0, 2000),
		row("R1", "", 50, 1, 0, 500),
		row("R2", "R2@1", 1000, 1, 0, 4000),
		// 节点单测（根链列表里没有）不计入
		row("N000019", "", 99999, 1, 1, 9000),
	}
	summary, chains := buildRootChainStats(rows, names)
	if summary.Executions != 5 || summary.FailedExecutions != 1 {
		t.Fatalf("summary executions = %d failed = %d, want 5/1", summary.Executions, summary.FailedExecutions)
	}
	if summary.SuccessRate != 0.8 {
		t.Errorf("success rate = %v, want 0.8", summary.SuccessRate)
	}
	if summary.AvgMs != 330 {
		t.Errorf("avg = %d, want 330", summary.AvgMs)
	}
	if summary.P95Ms < 1000 || summary.P95Ms > 1110 {
		t.Errorf("p95 = %d, want ~1000", summary.P95Ms)
	}
	if len(chains) != 2 || chains[0].ChainID != "R1" || chains[1].ChainID != "R2" {
		t.Fatalf("chains order wrong: %+v", chains)
	}
	r1 := chains[0]
	if r1.Name != "链一" || r1.Executions != 4 || r1.Failed != 1 || r1.LastAt != 3000 {
		t.Errorf("R1 stat wrong: %+v", r1.ExecStat)
	}
	if r1.FailRate != 0.25 {
		t.Errorf("R1 fail rate = %v, want 0.25", r1.FailRate)
	}
	// 版本倒序，草稿排最后
	gotVers := []int{}
	for _, v := range r1.Versions {
		gotVers = append(gotVers, v.Version)
	}
	if len(gotVers) != 3 || gotVers[0] != 2 || gotVers[1] != 1 || gotVers[2] != 0 {
		t.Errorf("version order = %v, want [2 1 0]", gotVers)
	}
	if v2 := r1.Versions[0]; v2.Executions != 2 || v2.Failed != 1 || v2.AvgMs != 200 || v2.P95Ms < 300 || v2.P95Ms > 333 {
		t.Errorf("R1@2 stat wrong: %+v", v2.ExecStat)
	}
}

func TestBuildRootChainStatsEmpty(t *testing.T) {
	summary, chains := buildRootChainStats(nil, map[string]string{"R1": "x"})
	if summary.Executions != 0 || summary.SuccessRate != 0 || len(chains) != 0 {
		t.Errorf("empty stats wrong: %+v %v", summary, chains)
	}
}

func TestNormalizeErrorMsg(t *testing.T) {
	a := normalizeErrorMsg("call: wait result timeout for task 8d17bfe6-cd30-11f1-b95b-0242ac15000f, timeout: 1m0s")
	b := normalizeErrorMsg("call: wait result timeout for task dcd31d57-cd30-11f1-b95b-0242ac15000f, timeout: 1m0s")
	if a != b || a != "call: wait result timeout for task <id>, timeout: 1m0s" {
		t.Errorf("uuid not normalized: %q / %q", a, b)
	}
	if got := normalizeErrorMsg("order 1234567890 not found"); got != "order <n> not found" {
		t.Errorf("long number not normalized: %q", got)
	}
	// 普通错误信息（含短数字、HTTP 码）保持不变
	msg := "rpc error: code = Unknown desc = 400:Bad Request"
	if got := normalizeErrorMsg(msg); got != msg {
		t.Errorf("plain msg changed: %q", got)
	}
}

func TestMergeErrorStats(t *testing.T) {
	rows := []workflow.StatsErrorRow{
		{NodeID: "N1", NodeName: "节点1", ErrorMsg: "timeout for task 8d17bfe6-cd30-11f1-b95b-0242ac15000f", Count: 1, LastTs: 10, TraceID: "t1"},
		{NodeID: "N1", NodeName: "节点1", ErrorMsg: "timeout for task dcd31d57-cd30-11f1-b95b-0242ac15000f", Count: 2, LastTs: 20, TraceID: "t2"},
		{NodeID: "N2", NodeName: "节点2", ErrorMsg: "bad request", Count: 2, LastTs: 5, TraceID: "t3"},
		{NodeID: "N3", NodeName: "节点3", ErrorMsg: "other", Count: 1, LastTs: 1, TraceID: "t4"},
	}
	out := mergeErrorStats(rows, 2)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	if out[0].NodeID != "N1" || out[0].Count != 3 || out[0].LastAt != 20000 || out[0].TraceID != "t2" {
		t.Errorf("merged top wrong: %+v", out[0])
	}
	if out[1].NodeID != "N2" || out[1].Count != 2 {
		t.Errorf("second wrong: %+v", out[1])
	}
}
