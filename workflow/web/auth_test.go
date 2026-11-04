package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsPublicRequest(t *testing.T) {
	cases := []struct {
		method, path string
		want         bool
	}{
		{"GET", "/", true},
		{"GET", "/orch", true},
		{"GET", "/assets/common.js", true},
		{"POST", "/api/login", true},
		{"GET", "/api/health", true},
		{"POST", "/api/projects/zamloan2/config", true},
		{"POST", "/api/project/zamloan2/env/test/workflow/invoke", true},
		{"POST", "/api/project/zamloan2/env/test/redis-config", true},
		// 旧白名单按后缀放行会误放的路径
		{"POST", "/api/nodes/x/config", false},
		{"POST", "/api/projects/a/b/config", false},
		{"GET", "/api/projects/zamloan2/config", false},
		{"POST", "/api/workflow/invoke", false},
		{"POST", "/api/workflow/execute", false},
		{"GET", "/api/env-configs", false},
		{"GET", "/api/me", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.path, nil)
		if got := isPublicRequest(r); got != c.want {
			t.Errorf("%s %s: isPublicRequest=%v, want %v", c.method, c.path, got, c.want)
		}
	}
}

func TestAuthMiddlewareRejectsWithoutSession(t *testing.T) {
	ws := &WebServer{mux: http.NewServeMux()}
	called := false
	ws.mux.HandleFunc("GET /api/env-configs", func(w http.ResponseWriter, r *http.Request) { called = true })
	rec := httptest.NewRecorder()
	ws.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/env-configs?project=p", nil))
	if rec.Code != http.StatusUnauthorized || called {
		t.Fatalf("want 401 and handler not called, got code=%d called=%v", rec.Code, called)
	}
}

func TestResolveProjectWithQuery(t *testing.T) {
	ws := &WebServer{}

	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/env-configs?project=A", nil)
	if p, ok := ws.resolveProject(rec, r, "B"); ok || p != "" || rec.Code != http.StatusBadRequest {
		t.Fatalf("body project differs from query: want reject 400, got p=%q ok=%v code=%d", p, ok, rec.Code)
	}

	rec = httptest.NewRecorder()
	if p, ok := ws.resolveProject(rec, r, "A"); !ok || p != "A" {
		t.Fatalf("same project: want A, got p=%q ok=%v", p, ok)
	}
	if p, ok := ws.resolveProject(rec, r, ""); !ok || p != "A" {
		t.Fatalf("empty body project: want A, got p=%q ok=%v", p, ok)
	}
}

func TestLoginLimiter(t *testing.T) {
	l := &loginLimiter{entries: map[string]*loginFailEntry{}}
	for i := 0; i < loginFailLimit-1; i++ {
		l.fail("user:a", "ip:1")
	}
	if l.blocked("user:a") {
		t.Fatal("should not block before limit")
	}
	l.fail("user:a", "ip:1")
	if !l.blocked("user:a") || !l.blocked("user:x", "ip:1") {
		t.Fatal("should block user and ip after limit")
	}
	l.reset("user:a", "ip:1")
	if l.blocked("user:a", "ip:1") {
		t.Fatal("should unblock after reset")
	}
}
