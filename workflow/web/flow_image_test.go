package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func flowImageTestData() *flowShareData {
	at := func(sec int) time.Time { return time.Date(2026, 11, 28, 10, 0, sec, 0, time.UTC) }
	return &flowShareData{
		Project: "demo",
		TraceID: "trace-1",
		Chain: flowShareChain{
			ChainID:         "R000001",
			Name:            "测试<链>",
			NodeIDs:         "N1__a,N2__b,N3__c,N4__d",
			ConnectionsData: `[{"from_id":"N1__a","to_id":"N2__b","type":"Success"},{"from_id":"N2__b","to_id":"N3__c","type":"True"},{"from_id":"N2__b","to_id":"N4__d","type":"False"}]`,
			DSLJSON:         `{"metadata":{"nodes":[{"id":"N2__b","name":"是否P1","type":"custom/Activity","configuration":{"node_config":{"switch_condition":"[responses.is_p1]"}}}]}}`,
		},
		Nodes: []flowShareNode{
			{NodeID: "N1", Name: "取用户", Type: "custom/Activity", Kind: "condition"},
			{NodeID: "N2", Name: "P1", Type: "custom/Activity", Kind: "condition"},
			{NodeID: "N3", Name: "打P1标签", Type: "custom/Activity", Kind: "action"},
			{NodeID: "N4", Name: "其它", Type: "custom/Activity", Kind: "action"},
		},
		Logs: []flowShareLog{
			{ID: 3, NodeID: "N4__d", RelationType: "Success", DurationMs: 12, CreatedAt: at(3), Level: "error", ErrorMsg: "boom"},
			{ID: 1, NodeID: "N1__a", RelationType: "Success", DurationMs: 1500, CreatedAt: at(1)},
			{ID: 2, NodeID: "N2__b", RelationType: "False", DurationMs: 980, CreatedAt: at(2)},
			{ID: 9, NodeID: "", Level: "flow_total", CreatedAt: at(9)},
		},
	}
}

func dotLine(dot, prefix string) string {
	for _, l := range strings.Split(dot, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), prefix) {
			return l
		}
	}
	return ""
}

func TestBuildFlowDot(t *testing.T) {
	dot := buildFlowDot(flowImageTestData(), time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC))
	if !strings.HasPrefix(dot, "digraph flow {") || !strings.HasSuffix(dot, "}\n") {
		t.Fatalf("bad dot:\n%s", dot)
	}
	// 标题文本需转义
	if !strings.Contains(dot, "测试&lt;链&gt;（R000001）") || strings.Contains(dot, "测试<链>") {
		t.Fatalf("title not escaped:\n%s", dot)
	}
	// 走过的连线标红，未走的不标
	if l := dotLine(dot, `"N2__b" -> "N4__d"`); !strings.Contains(l, flowImageTraceColor) || !strings.Contains(l, "False") {
		t.Fatalf("False edge should be on trace: %s", l)
	}
	if l := dotLine(dot, `"N2__b" -> "N3__c"`); strings.Contains(l, flowImageTraceColor) {
		t.Fatalf("True edge should not be on trace: %s", l)
	}
	if l := dotLine(dot, `"N1__a" -> "N2__b"`); !strings.Contains(l, flowImageTraceColor) {
		t.Fatalf("first edge should be on trace: %s", l)
	}
	// 路由节点显示条件与耗时；未执行节点不标红；出错节点虚线
	n2 := dotLine(dot, `"N2__b" [`)
	for _, want := range []string{"[路由] 是否P1", "条件: [responses.is_p1]", "耗时 980ms", flowImageTraceColor} {
		if !strings.Contains(n2, want) {
			t.Fatalf("N2 missing %q: %s", want, n2)
		}
	}
	if l := dotLine(dot, `"N1__a" [`); !strings.Contains(l, "耗时 1.50s") {
		t.Fatalf("N1 duration: %s", l)
	}
	if l := dotLine(dot, `"N3__c" [`); strings.Contains(l, "耗时") || strings.Contains(l, flowImageTraceColor) {
		t.Fatalf("N3 should not be on trace: %s", l)
	}
	if l := dotLine(dot, `"N4__d" [`); !strings.Contains(l, "dashed") || !strings.Contains(l, flowImageErrorColor) {
		t.Fatalf("N4 should be error style: %s", l)
	}
	if !strings.Contains(dot, "执行步数: 3") {
		t.Fatalf("steps count missing:\n%s", dot)
	}
}

func TestBuildFlowImageTraceMultiRuns(t *testing.T) {
	d := flowImageTestData()
	at := func(sec int) time.Time { return time.Date(2026, 11, 28, 11, 0, sec, 0, time.UTC) }
	// 同一 trace 第二轮走了 True 分支：与页面一致，两条分支都算走过
	d.Logs = append(d.Logs,
		flowShareLog{ID: 11, NodeID: "N1__a", RelationType: "Success", DurationMs: 100, CreatedAt: at(1)},
		flowShareLog{ID: 12, NodeID: "N2__b", RelationType: "True", DurationMs: 200, CreatedAt: at(2)},
		flowShareLog{ID: 13, NodeID: "N3__c", RelationType: "Success", DurationMs: 300, CreatedAt: at(3)},
	)
	graph := map[string]bool{"N1__a": true, "N2__b": true, "N3__c": true, "N4__d": true}
	conns := []flowImageConn{{"N1__a", "N2__b", "Success"}, {"N2__b", "N3__c", "True"}, {"N2__b", "N4__d", "False"}}
	tr := buildFlowImageTrace(d.Logs, graph, conns)
	if tr.steps != 6 {
		t.Fatalf("steps = %d", tr.steps)
	}
	for _, k := range []string{"N1__a|N2__b", "N2__b|N3__c", "N2__b|N4__d"} {
		if !tr.edges[k] {
			t.Fatalf("edge %s should be on trace: %v", k, tr.edges)
		}
	}
	if got := flowImageDurText(tr.nodes["N2__b"]); got != "980ms / 200ms（2 次，共 1.18s）" {
		t.Fatalf("dur text = %q", got)
	}
}

func TestBuildFlowDotWithoutTrace(t *testing.T) {
	d := flowImageTestData()
	d.TraceID = ""
	d.Logs = nil
	dot := buildFlowDot(d, time.Now())
	if strings.Contains(dot, flowImageTraceColor) || strings.Contains(dot, "耗时") || strings.Contains(dot, "trace_id") {
		t.Fatalf("no trace should not highlight:\n%s", dot)
	}
}

func TestFmtFlowMs(t *testing.T) {
	cases := map[int64]string{0: "0ms", 999: "999ms", 1500: "1.50s", 12345: "12.3s", 61000: "1m1s", 125500: "2m6s"}
	for in, want := range cases {
		if got := fmtFlowMs(in); got != want {
			t.Fatalf("fmtFlowMs(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFlowImageRouteAuth(t *testing.T) {
	if !isPublicRequest(httptest.NewRequest("GET", "/api/flow-view/image?share=x", nil)) {
		t.Fatal("GET /api/flow-view/image should be public")
	}
	if isPublicRequest(httptest.NewRequest("POST", "/api/flow-view/image", nil)) {
		t.Fatal("only GET of /api/flow-view/image is public")
	}
}

func TestFlowShareImageRejects(t *testing.T) {
	ws := &WebServer{}
	rec := httptest.NewRecorder()
	ws.handleFlowShareImage(rec, httptest.NewRequest("GET", "/api/flow-view/image?share=x&format=gif", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad format code = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	ws.handleFlowShareImage(rec, httptest.NewRequest("GET", "/api/flow-view/image", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing share code = %d", rec.Code)
	}
}

func TestRenderFlowDot(t *testing.T) {
	if _, err := exec.LookPath(flowImageDotBin); err != nil {
		t.Skip("本机未安装 graphviz")
	}
	img, err := renderFlowDot(context.Background(), buildFlowDot(flowImageTestData(), time.Now()), "png")
	if err != nil {
		t.Fatal(err)
	}
	if len(img) < 8 || string(img[1:4]) != "PNG" {
		t.Fatalf("not a png, %d bytes", len(img))
	}
}
