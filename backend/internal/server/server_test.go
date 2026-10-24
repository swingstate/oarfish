package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestHealthz(t *testing.T) {
	rec := get(t, NewHandler(fstest.MapFS{}), http.MethodGet, "/healthz")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %q, want ok", body["status"])
	}
}

func TestHealthzRejectsNonGet(t *testing.T) {
	rec := get(t, NewHandler(fstest.MapFS{}), http.MethodPost, "/healthz")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestServesEmbeddedFrontend(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>Oarfish</title>")},
		"assets/app.js": {Data: []byte("console.log('oarfish')")},
	}
	h := NewHandler(assets)

	rec := get(t, h, http.MethodGet, "/")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>Oarfish</title>") {
		t.Fatalf("GET / = %d %q", rec.Code, rec.Body.String())
	}
	rec = get(t, h, http.MethodGet, "/assets/app.js")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "oarfish") {
		t.Fatalf("GET /assets/app.js = %d %q", rec.Code, rec.Body.String())
	}
	rec = get(t, h, http.MethodGet, "/missing")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /missing = %d, want 404", rec.Code)
	}
}

func TestMissingFrontendReports503(t *testing.T) {
	rec := get(t, NewHandler(fstest.MapFS{}), http.MethodGet, "/")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := get(t, NewHandler(fstest.MapFS{}), http.MethodGet, "/healthz")
	for _, name := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if rec.Header().Get(name) == "" {
			t.Errorf("missing header %s", name)
		}
	}
}

func TestNewSetsTimeouts(t *testing.T) {
	srv := New("127.0.0.1:0", http.NotFoundHandler())
	if srv.ReadHeaderTimeout == 0 || srv.ReadTimeout == 0 || srv.WriteTimeout == 0 || srv.IdleTimeout == 0 {
		t.Fatal("server timeouts must be set")
	}
}
