package workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newInvokeTestServer 启动一个模拟 worker 的 HTTP 服务，按 workflow 接口返回格式回包。
// 返回的 body 可通过 data 参数定制；code 为接口返回码（非 0 表示业务失败）。
func newInvokeTestServer(t *testing.T, code int64, message string, data any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		// 期望命中 /api/project/{project}/env/{env}/workflow/invoke
		if r.URL.Path == "" {
			t.Errorf("empty request path")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    code,
			"message": message,
			"data":    data,
		})
	}))
}

// TestInvokeWorkerFlowAPI_SuccessWithResult 验证：传入携带 Result 出参指针的
// *InvokeRequest 时，返回值返回完整 data，且 Result 指针被填充为 data.responses 子对象。
func TestInvokeWorkerFlowAPI_SuccessWithResult(t *testing.T) {
	ts := newInvokeTestServer(t, 0, "ok", map[string]any{
		"responses": map[string]any{"hello": "world", "count": float64(2)},
		"trace":     "abc",
	})
	defer ts.Close()

	l := &WfLogic{DomainName: ts.URL, Project: "proj", Env: "env", ApiToken: "tok"}

	resultMap := map[string]any{}
	req := &InvokeRequest{
		ChainKey: "chain1",
		Payload:  map[string]any{"a": 1},
		Metadata: InvokeMetadata{TraceID: "trace-1", IsAsync: false},
		Result:   &resultMap,
	}
	data, err := l.InvokeWorkerFlowAPI(
		context.Background(), req,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 返回值应为完整的 data 对象
	dataMap, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", data)
	}
	if dataMap["trace"] != "abc" {
		t.Errorf("data.trace = %v, want abc", dataMap["trace"])
	}

	// Result 指针应被填充为 data.responses 子对象
	if resultMap["hello"] != "world" {
		t.Errorf("result[hello] = %v, want world", resultMap["hello"])
	}
	if resultMap["count"] != float64(2) {
		t.Errorf("result[count] = %v, want 2", resultMap["count"])
	}
	// req.Result 仍指向同一指针，内容已被填充
	if req.Result == nil {
		t.Fatalf("req.Result is nil after invoke")
	}
}

// TestInvokeWorkerFlowAPI_SuccessWithoutResult 验证：不传 result 时仍正常返回，
// 不会因 result 为 nil 而 panic。
func TestInvokeWorkerFlowAPI_SuccessWithoutResult(t *testing.T) {
	ts := newInvokeTestServer(t, 0, "ok", map[string]any{
		"responses": map[string]any{"k": "v"},
	})
	defer ts.Close()

	l := &WfLogic{DomainName: ts.URL, Project: "proj", Env: "env", ApiToken: "tok"}

	req := &InvokeRequest{
		ChainKey: "chain1",
		Payload:  map[string]any{"a": 1},
		Metadata: InvokeMetadata{TraceID: "trace-1", IsAsync: true},
	}

	data, err := l.InvokeWorkerFlowAPI(
		context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dataMap, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", data)
	}
	resp, ok := dataMap["responses"].(map[string]any)
	if !ok || resp["k"] != "v" {
		t.Errorf("data.responses = %v, want map[k:v]", dataMap["responses"])
	}
}

// TestInvokeWorkerFlowAPI_ErrorResponse 验证：worker 返回非 0 code 时，方法返回错误。
func TestInvokeWorkerFlowAPI_ErrorResponse(t *testing.T) {
	ts := newInvokeTestServer(t, 40001, "chain not found", nil)
	defer ts.Close()

	l := &WfLogic{DomainName: ts.URL, Project: "proj", Env: "env", ApiToken: "tok"}

	req := &InvokeRequest{
		ChainKey: "missing-chain",
		Payload:  map[string]any{},
		Metadata: InvokeMetadata{TraceID: "trace-1", IsAsync: false},
	}

	_, err := l.InvokeWorkerFlowAPI(
		context.Background(), req,
	)
	if err == nil {
		t.Fatalf("expected error for code != 0, got nil111")
	}
	if err.Error() != "chain not found" {
		t.Errorf("err = %q, want %q", err.Error(), "chain not found")
	}
}
