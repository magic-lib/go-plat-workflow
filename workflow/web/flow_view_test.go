package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeFlowView(t *testing.T) {
	ws := &WebServer{}
	rec := httptest.NewRecorder()
	ws.serveFlowView(rec, httptest.NewRequest(http.MethodGet, "/flow-view?project=p&chain_id=R1&trace_id=t", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
	body, _ := io.ReadAll(rec.Body)
	page := string(body)
	if !strings.Contains(page, `/assets/flow_view.js`) {
		t.Fatal("flow-view page should load flow_view.js")
	}
	if strings.Contains(page, "common.js") {
		t.Fatal("flow-view page should not load common.js")
	}
}

func TestFlowViewAssetEmbedded(t *testing.T) {
	if _, err := webAssets.ReadFile("assets/flow_view.js"); err != nil {
		t.Fatalf("assets/flow_view.js not embedded: %v", err)
	}
}

func TestFlowViewIsPublicPage(t *testing.T) {
	if !isPublicRequest(httptest.NewRequest(http.MethodGet, "/flow-view", nil)) {
		t.Fatal("/flow-view page should be served without session (APIs still require login)")
	}
}
