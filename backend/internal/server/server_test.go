package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/swingstate/oarfish/backend/internal/clock"
	"github.com/swingstate/oarfish/backend/internal/platform/detector"
	"github.com/swingstate/oarfish/backend/internal/version"
)

func get(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, newRequest(method, path, nil))
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
	var body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		PID     int    `json:"pid"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" || body.Version != version.Version || body.PID != os.Getpid() {
		t.Errorf("body = %+v, want status ok, version %q, pid %d", body, version.Version, os.Getpid())
	}
	// A11.2: the installer only globs for this text, so it must stay in the
	// body in this form and the status member must come first.
	if !strings.HasPrefix(rec.Body.String(), `{"status":"ok",`) || !installerGlob(rec.Body.String()) {
		t.Errorf("the installer check does not match %q", rec.Body.String())
	}
}

// installerGlob is the check of install/install.sh: the body contains
// "status":"ok". TestInstallerChecksHealthzTheSameWay pins the text to the installer.
func installerGlob(body string) bool { return strings.Contains(body, `"status":"ok"`) }

// The installer's check, run by a shell with the very case pattern of
// install/install.sh, on the new body (A11.2).
func TestInstallerChecksHealthzTheSameWay(t *testing.T) {
	raw, err := os.ReadFile("../../../install/install.sh")
	if err != nil {
		t.Skip("install.sh is not next to the sources")
	}
	pattern := `*'"status":"ok"'*)`
	if !strings.Contains(string(raw), pattern) {
		t.Fatal("install.sh no longer globs for \"status\":\"ok\"; update this test and A11.2")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	body := get(t, NewHandler(fstest.MapFS{}), http.MethodGet, "/healthz").Body.String()
	script := "case $1 in " + pattern + " exit 0 ;; esac; exit 1"
	if err := exec.Command(sh, "-c", script, "sh", body).Run(); err != nil {
		t.Fatalf("the installer glob does not match %q", body)
	}
	// And it does not match the not-ready body of /readyz.
	if err := exec.Command(sh, "-c", script, "sh", `{"status":"not_ready"}`).Run(); err == nil {
		t.Fatal("the installer glob matches the not-ready body")
	}
}

type fakeDevice struct{ id string }

func (d fakeDevice) Result() detector.Result {
	return detector.Result{Profile: detector.ProfileRef{ID: d.id}}
}
func (fakeDevice) CPUTemperature() (float64, bool) { return 0, false }

func TestReadyz(t *testing.T) {
	fx := newAccessFixture(t, clock.System())
	assets := fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}
	full := func(a fs.FS, opts ...Option) *httptest.ResponseRecorder {
		return get(t, NewHandler(a, opts...), http.MethodGet, "/readyz")
	}
	// Nothing is wired: not ready, and the body has no "status":"ok".
	rec := full(assets)
	if rec.Code != http.StatusServiceUnavailable || strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("empty handler: %d %s", rec.Code, rec.Body)
	}
	am, log, dev := fx.auth.m, fx.audit, fakeDevice{id: "generic"}
	ok := full(assets, WithAuth(am), WithAudit(log), WithDevice(dev))
	if ok.Code != http.StatusOK || strings.TrimSpace(ok.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("ready: %d %s", ok.Code, ok.Body)
	}
	for name, r := range map[string]*httptest.ResponseRecorder{
		"no auth":     full(assets, WithAudit(log), WithDevice(dev)),
		"no audit":    full(assets, WithAuth(am), WithDevice(dev)),
		"no device":   full(assets, WithAuth(am), WithAudit(log)),
		"no frontend": full(fstest.MapFS{}, WithAuth(am), WithAudit(log), WithDevice(dev)),
		"no profile":  full(assets, WithAuth(am), WithAudit(log), WithDevice(fakeDevice{})),
	} {
		if r.Code != http.StatusServiceUnavailable || strings.Contains(r.Body.String(), `"status":"ok"`) {
			t.Errorf("%s: %d %s", name, r.Code, r.Body)
		}
	}
	// Public: no session, no state change.
	if r := get(t, NewHandler(assets, WithAuth(am), WithAudit(log), WithDevice(dev)), http.MethodPost, "/readyz"); r.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /readyz = %d", r.Code)
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
