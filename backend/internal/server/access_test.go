package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/swingstate/oarfish/backend/internal/audit"
	"github.com/swingstate/oarfish/backend/internal/auth"
	"github.com/swingstate/oarfish/backend/internal/clock"
	"github.com/swingstate/oarfish/backend/internal/clock/clocktest"
	"github.com/swingstate/oarfish/backend/internal/metrics"
	"github.com/swingstate/oarfish/backend/internal/metrics/metricstest"
)

// accessFixture is a full handler with access control, audit, a sampler and
// the frontend placeholder, talked to as a browser would.
type accessFixture struct {
	t       *testing.T
	auth    *testAuth
	audit   *audit.Log
	clock   clock.Clock
	sampler *metrics.Sampler
	handler http.Handler
	bodies  []string // every response body, for the redaction test
}

func newAccessFixture(t *testing.T, c clock.Clock) *accessFixture {
	t.Helper()
	a := newTestAuth(t, c)
	l, err := audit.Open(a.stateDir, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := metrics.NewSampler(metrics.Config{
		Collector: metrics.NewCollector(metricstest.NewLive(t, "pi-zero-2w"), metricstest.Statfs(piSizes)),
		Interval:  15 * time.Millisecond, Warmup: 5 * time.Millisecond, OnceWindow: 5 * time.Millisecond,
	})
	assets := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>Oarfish</title>")}}
	h := NewHandler(assets, WithAuth(a.m), WithAudit(l), WithMetrics(s, WithHeartbeat(time.Hour)))
	return &accessFixture{t: t, auth: a, audit: l, clock: c, sampler: s, handler: h}
}

// req describes one request as a browser on the Oarfish page sends it, with
// fields to break one rule at a time.
type req struct {
	method, path, body string
	session            *session
	noCSRF             bool
	csrf               string // overrides the session's token
	origin             string // "" = same origin; "-" = no Origin header
	secFetchSite       string // "" = same-origin; "-" = not sent
	contentType        string // "" = application/json; "-" = not sent
	host               string // "" = 127.0.0.1:8484
}

func (f *accessFixture) do(r req) *httptest.ResponseRecorder {
	f.t.Helper()
	var body io.Reader
	if r.body != "" {
		body = strings.NewReader(r.body)
	}
	hr := newRequest(r.method, r.path, body)
	if r.host != "" {
		hr.Host = r.host
	}
	if stateChanging(r.method) {
		switch r.origin {
		case "":
			hr.Header.Set("Origin", "http://"+hr.Host)
		case "-":
		default:
			hr.Header.Set("Origin", r.origin)
		}
		switch r.secFetchSite {
		case "":
			hr.Header.Set("Sec-Fetch-Site", "same-origin")
		case "-":
		default:
			hr.Header.Set("Sec-Fetch-Site", r.secFetchSite)
		}
		switch r.contentType {
		case "":
			hr.Header.Set("Content-Type", "application/json")
		case "-":
		default:
			hr.Header.Set("Content-Type", r.contentType)
		}
	}
	if r.session != nil {
		hr.AddCookie(&http.Cookie{Name: cookieName, Value: r.session.token})
		if !r.noCSRF {
			token := r.session.csrf
			if r.csrf != "" {
				token = r.csrf
			}
			hr.Header.Set(csrfHeader, token)
		}
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, hr)
	f.bodies = append(f.bodies, rec.Body.String())
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error apiError `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Error.Code
}

func expect(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status || (code != "" && errorCode(t, rec) != code) {
		t.Errorf("got %d %s, want %d %s", rec.Code, strings.TrimSpace(rec.Body.String()), status, code)
	}
}

func loginBody(user, password string) string {
	b, _ := json.Marshal(map[string]string{"username": user, "password": password})
	return string(b)
}

func setupBody(token, user, password string) string {
	b, _ := json.Marshal(map[string]string{"setup_token": token, "username": user, "password": password})
	return string(b)
}

func passwordBody(current, next string) string {
	b, _ := json.Marshal(map[string]string{"current_password": current, "new_password": next})
	return string(b)
}

// sessionFrom reads the cookie and CSRF token from a login or setup answer.
func sessionFrom(t *testing.T, rec *httptest.ResponseRecorder) session {
	t.Helper()
	var s session
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName {
			s.token = c.Value
		}
	}
	var body sessionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	s.csrf = body.CSRFToken
	if s.token == "" || s.csrf == "" || body.State != "authenticated" {
		t.Fatalf("no session in the answer: %d %s", rec.Code, rec.Body)
	}
	return s
}

func (f *accessFixture) setupViaAPI() session {
	f.t.Helper()
	rec := f.do(req{method: http.MethodPost, path: "/api/v1/setup", body: setupBody(f.auth.setupToken(f.t), testUser, testPassword)})
	expect(f.t, rec, http.StatusOK, "")
	return sessionFrom(f.t, rec)
}

func (f *accessFixture) loginViaAPI() session {
	f.t.Helper()
	rec := f.do(req{method: http.MethodPost, path: "/api/v1/session", body: loginBody(testUser, testPassword)})
	expect(f.t, rec, http.StatusOK, "")
	return sessionFrom(f.t, rec)
}

// readRoutes are the GET routes that need a session (D-C46).
var readRoutes = []string{"/api/v1/device", "/api/v1/metrics", "/api/v1/events"}

// ORB-ONB-002, E5: while no account exists, everything but the session status,
// the setup, /healthz and the frontend answers setup_required.
func TestSetupModeBlocksEverythingElse(t *testing.T) {
	f := newAccessFixture(t, clock.System())
	for _, p := range readRoutes {
		expect(t, f.do(req{method: http.MethodGet, path: p}), http.StatusUnauthorized, "setup_required")
	}
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/session", body: loginBody(testUser, testPassword)}), http.StatusUnauthorized, "setup_required")
	expect(t, f.do(req{method: http.MethodDelete, path: "/api/v1/session"}), http.StatusUnauthorized, "setup_required")
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/account/password", body: passwordBody(testPassword, testPassword+"x")}), http.StatusUnauthorized, "setup_required")
	rec := f.do(req{method: http.MethodGet, path: "/api/v1/session"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"state":"setup_required"`) || strings.Contains(rec.Body.String(), "csrf") {
		t.Errorf("session status = %d %s", rec.Code, rec.Body)
	}
	expect(t, f.do(req{method: http.MethodGet, path: "/healthz"}), http.StatusOK, "")
	expect(t, f.do(req{method: http.MethodGet, path: "/"}), http.StatusOK, "")
	if f.sampler.Running() {
		t.Error("a refused request started sampling")
	}
	// The setup works and leaves setup mode.
	token := f.auth.setupToken(t)
	s := f.setupViaAPI()
	expect(t, f.do(req{method: http.MethodGet, path: "/api/v1/device", session: &s}), http.StatusServiceUnavailable, "device_info_unavailable")
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/setup", body: setupBody(token, testUser, testPassword)}), http.StatusConflict, "setup_not_available")
}

// D-C46, ORB-AUTH-002: every /api/v1 read route needs a session; the session
// status is public; /healthz and the frontend stay public and expose nothing.
func TestEveryAPIRouteNeedsASession(t *testing.T) {
	f := newAccessFixture(t, clock.System())
	s := f.setupViaAPI()
	for _, p := range readRoutes {
		expect(t, f.do(req{method: http.MethodGet, path: p}), http.StatusUnauthorized, "unauthenticated")
		expect(t, f.do(req{method: http.MethodGet, path: p, session: &session{token: "forged-" + s.token}}), http.StatusUnauthorized, "unauthenticated")
	}
	if rec := f.do(req{method: http.MethodGet, path: "/api/v1/metrics", session: &s}); rec.Code != http.StatusOK {
		t.Errorf("metrics with a session = %d", rec.Code)
	}
	rec := f.do(req{method: http.MethodGet, path: "/api/v1/session"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"state":"login_required"`) {
		t.Errorf("session status without a session = %d %s", rec.Code, rec.Body)
	}
	hz := f.do(req{method: http.MethodGet, path: "/healthz"})
	if hz.Code != http.StatusOK || strings.TrimSpace(hz.Body.String()) != `{"status":"ok"}` {
		t.Errorf("/healthz = %d %s", hz.Code, hz.Body)
	}
	// Only the four account endpoints take a state-changing method.
	for _, p := range readRoutes {
		for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			expect(t, f.do(req{method: m, path: p, session: &s, body: "{}"}), http.StatusMethodNotAllowed, "method_not_allowed")
		}
	}
	expect(t, f.do(req{method: http.MethodPut, path: "/api/v1/session", session: &s, body: "{}"}), http.StatusMethodNotAllowed, "")
	expect(t, f.do(req{method: http.MethodGet, path: "/api/v1/setup"}), http.StatusMethodNotAllowed, "")
	expect(t, f.do(req{method: http.MethodGet, path: "/api/v1/account/password", session: &s}), http.StatusMethodNotAllowed, "")
}

// ORB-SEC-004, E3: each state-changing route against each broken rule.
func TestStateChangingRequestsMatrix(t *testing.T) {
	f := newAccessFixture(t, clock.System())
	s := f.setupViaAPI()
	routes := []struct {
		name, method, path, body string
		needsSession             bool
	}{
		{"login", http.MethodPost, "/api/v1/session", loginBody(testUser, testPassword), false},
		{"setup", http.MethodPost, "/api/v1/setup", setupBody("00", testUser, testPassword), false},
		{"logout", http.MethodDelete, "/api/v1/session", "", true},
		{"password", http.MethodPost, "/api/v1/account/password", passwordBody(testPassword, "Another-Sentinel-55"), true},
	}
	for _, rt := range routes {
		t.Run(rt.name, func(t *testing.T) {
			base := req{method: rt.method, path: rt.path, body: rt.body, session: &s}
			with := func(change func(*req)) req { r := base; change(&r); return r }

			expect(t, f.do(with(func(r *req) { r.origin = "http://evil.example" })), http.StatusForbidden, "origin_rejected")
			expect(t, f.do(with(func(r *req) { r.origin = "null" })), http.StatusForbidden, "origin_rejected")
			expect(t, f.do(with(func(r *req) { r.secFetchSite = "cross-site" })), http.StatusForbidden, "origin_rejected")
			expect(t, f.do(with(func(r *req) { r.secFetchSite = "same-site" })), http.StatusForbidden, "origin_rejected")
			expect(t, f.do(with(func(r *req) { r.secFetchSite = "-"; r.origin = "http://evil.example" })), http.StatusForbidden, "origin_rejected")
			expect(t, f.do(with(func(r *req) { r.secFetchSite = "-"; r.origin = "-" })), http.StatusForbidden, "origin_rejected")
			expect(t, f.do(with(func(r *req) { r.contentType = "text/plain" })), http.StatusUnsupportedMediaType, "unsupported_media_type")
			expect(t, f.do(with(func(r *req) { r.contentType = "application/x-www-form-urlencoded" })), http.StatusUnsupportedMediaType, "unsupported_media_type")
			expect(t, f.do(with(func(r *req) { r.contentType = "-" })), http.StatusUnsupportedMediaType, "unsupported_media_type")
			expect(t, f.do(with(func(r *req) { r.host = "evil.example:8484"; r.origin = "http://evil.example:8484" })), http.StatusForbidden, "host_rejected")
			if rt.needsSession {
				expect(t, f.do(with(func(r *req) { r.session = nil })), http.StatusUnauthorized, "unauthenticated")
				expect(t, f.do(with(func(r *req) { r.noCSRF = true })), http.StatusForbidden, "csrf_failed")
				expect(t, f.do(with(func(r *req) { r.csrf = s.csrf + "x" })), http.StatusForbidden, "csrf_failed")
				expect(t, f.do(with(func(r *req) { r.csrf = s.token })), http.StatusForbidden, "csrf_failed")
			}
		})
	}
	// None of the refused requests ended the session.
	if rec := f.do(req{method: http.MethodGet, path: "/api/v1/session", session: &s}); !strings.Contains(rec.Body.String(), "authenticated") {
		t.Fatalf("the session was ended by a refused request: %s", rec.Body)
	}
	// The port is ignored, so another local tunnel port works; Origin without
	// Sec-Fetch-Site (older browsers) works too.
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/session", body: loginBody(testUser, testPassword), host: "127.0.0.1:9000", origin: "http://127.0.0.1:9000", secFetchSite: "-"}), http.StatusOK, "")
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/session", body: loginBody(testUser, testPassword), host: "localhost:8484", origin: "http://localhost:5173"}), http.StatusOK, "")
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/session", body: loginBody(testUser, testPassword), contentType: "application/json; charset=utf-8"}), http.StatusOK, "")
}

// E3: the Host guard against DNS rebinding covers every path, also /healthz
// and the frontend. Loopback names pass whatever the port.
func TestHostHeaderGuard(t *testing.T) {
	f := newAccessFixture(t, clock.System())
	for _, h := range []string{"evil.example", "evil.example:8484", "127.0.0.1.evil.example", "localhost.", "192.0.2.1:8484", "[::ffff:192.0.2.1]:8484", "[fe80::1%25lo]:8484", " "} {
		for _, p := range []string{"/", "/healthz", "/api/v1/session", "/api/v1/device"} {
			expect(t, f.do(req{method: http.MethodGet, path: p, host: h}), http.StatusForbidden, "host_rejected")
		}
	}
	for _, h := range []string{"127.0.0.1", "127.0.0.1:8484", "127.0.0.1:9000", "[::1]:8484", "localhost:8484", "LOCALHOST", "127.0.0.2:8484"} {
		expect(t, f.do(req{method: http.MethodGet, path: "/healthz", host: h}), http.StatusOK, "")
	}
	noHost := newRequest(http.MethodGet, "/healthz", nil)
	noHost.Host = ""
	rec0 := httptest.NewRecorder()
	f.handler.ServeHTTP(rec0, noHost)
	expect(t, rec0, http.StatusForbidden, "host_rejected")
	// The allow-list parameter (empty in G5.1) admits a configured name.
	h := NewHandler(fstest.MapFS{}, WithAllowedHosts("oarfish.example"))
	r := newRequest(http.MethodGet, "/healthz", nil)
	r.Host = "OARFISH.example:8484"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Errorf("configured host = %d", rec.Code)
	}
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// ORB-AUTH-002, E3: the cookie is HttpOnly, SameSite=Strict, Path=/, has no
// Domain and no lifetime of its own, and is not Secure on plain HTTP, also
// when a client claims X-Forwarded-Proto: https.
func TestSessionCookieAttributesPlainHTTP(t *testing.T) {
	f := newAccessFixture(t, clock.System())
	f.setupViaAPI()
	r := newRequest(http.MethodPost, "/api/v1/session", strings.NewReader(loginBody(testUser, testPassword)))
	r.Header.Set("Origin", "http://127.0.0.1:8484")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, r)
	raw := rec.Header().Get("Set-Cookie")
	c := cookieNamed(rec, cookieName)
	if c == nil {
		t.Fatalf("no %s cookie: %q", cookieName, raw)
	}
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.Domain != "" || c.Secure || c.MaxAge != 0 || !c.Expires.IsZero() {
		t.Errorf("cookie attributes: %q", raw)
	}
	for _, bad := range []string{"Secure", "Domain", "Max-Age", "Expires"} {
		if strings.Contains(raw, bad) {
			t.Errorf("Set-Cookie %q contains %s", raw, bad)
		}
	}
	if cookieNamed(rec, cookieNameTLS) != nil {
		t.Error("the __Host- cookie was set on plain HTTP")
	}
}

// E3: on a TLS connection the cookie is __Host-oarfish_session and Secure,
// and only that name is read.
func TestSessionCookieAttributesTLS(t *testing.T) {
	f := newAccessFixture(t, clock.System())
	srv := httptest.NewTLSServer(f.handler)
	defer srv.Close()
	client := srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	post := func(path, body string, cookies ...*http.Cookie) *http.Response {
		r, _ := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
		r.Header.Set("Origin", srv.URL)
		r.Header.Set("Content-Type", "application/json")
		for _, c := range cookies {
			r.AddCookie(c)
		}
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	resp := post("/api/v1/setup", setupBody(f.auth.setupToken(t), testUser, testPassword))
	if resp.TLS == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("setup over TLS = %d", resp.StatusCode)
	}
	var tlsCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == cookieNameTLS {
			tlsCookie = c
		}
		if c.Name == cookieName {
			t.Error("the plain cookie name was used over TLS")
		}
	}
	if tlsCookie == nil || !tlsCookie.Secure || !tlsCookie.HttpOnly || tlsCookie.SameSite != http.SameSiteStrictMode || tlsCookie.Path != "/" || tlsCookie.Domain != "" {
		t.Fatalf("TLS cookie: %+v (%q)", tlsCookie, resp.Header.Get("Set-Cookie"))
	}
	get := func(c *http.Cookie) int {
		r, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/metrics", nil)
		r.AddCookie(c)
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := get(tlsCookie); code != http.StatusOK {
		t.Errorf("TLS cookie: %d", code)
	}
	if code := get(&http.Cookie{Name: cookieName, Value: tlsCookie.Value}); code != http.StatusUnauthorized {
		t.Errorf("the plain name is read over TLS: %d", code)
	}
}

// E2, E3: the session status gives the CSRF token and the expiry hints, and
// does not count as activity; ordinary requests do.
func TestSessionStatusAndIdleExtension(t *testing.T) {
	c := clocktest.New()
	f := newAccessFixture(t, c)
	s := f.setupViaAPI()
	status := func() sessionResponse {
		var body sessionResponse
		rec := f.do(req{method: http.MethodGet, path: "/api/v1/session", session: &s})
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Error("no Cache-Control: no-store on the session status")
		}
		return body
	}
	st := status()
	if st.State != "authenticated" || st.Username != testUser || st.CSRFToken != s.csrf || st.IdleTimeoutSeconds != 3600 ||
		st.ExpiresAt != "2026-10-29T00:00:00Z" || st.IdleExpiresAt != "2026-10-28T13:00:00Z" || st.SchemaVersion != 1 {
		t.Fatalf("status = %+v", st)
	}
	c.Advance(50 * time.Minute)
	if st := status(); st.IdleExpiresAt != "2026-10-28T13:00:00Z" {
		t.Fatalf("the status request extended the session: %+v", st)
	}
	f.do(req{method: http.MethodGet, path: "/api/v1/metrics", session: &s})
	if st := status(); st.IdleExpiresAt != "2026-10-28T13:50:00Z" {
		t.Fatalf("an ordinary request did not extend the session: %+v", st)
	}
	c.Advance(61 * time.Minute)
	rec := f.do(req{method: http.MethodGet, path: "/api/v1/session", session: &s})
	if !strings.Contains(rec.Body.String(), `"state":"login_required"`) {
		t.Fatalf("after the idle timeout: %s", rec.Body)
	}
	if ck := cookieNamed(rec, cookieName); ck == nil || ck.MaxAge >= 0 {
		t.Error("a dead session cookie is not cleared")
	}
}

// ORB-AUTH-001: login, logout, password change over HTTP.
func TestLoginLogoutAndPasswordChange(t *testing.T) {
	f := newAccessFixture(t, clock.System())
	setup := f.setupViaAPI()
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/session", body: loginBody(testUser, "wrong-password-123")}), http.StatusUnauthorized, "invalid_credentials")
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/session", body: loginBody("nobody", "wrong-password-123")}), http.StatusUnauthorized, "invalid_credentials")
	s := f.loginViaAPI()

	const next = "Another-Sentinel-55"
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/account/password", session: &s, body: passwordBody("wrong-password-123", next)}), http.StatusForbidden, "wrong_password")
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/account/password", session: &s, body: passwordBody(testPassword, "short")}), http.StatusBadRequest, "password_too_short")
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/account/password", session: &s, body: passwordBody(testPassword, "password1234")}), http.StatusBadRequest, "password_too_common")
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/account/password", session: &s, body: passwordBody(testPassword, next)}), http.StatusOK, "")
	// The other session (from the setup) has ended, this one lives on.
	expect(t, f.do(req{method: http.MethodGet, path: "/api/v1/metrics", session: &setup}), http.StatusUnauthorized, "unauthenticated")
	expect(t, f.do(req{method: http.MethodGet, path: "/api/v1/metrics", session: &s}), http.StatusOK, "")

	rec := f.do(req{method: http.MethodDelete, path: "/api/v1/session", session: &s})
	expect(t, rec, http.StatusOK, "")
	if ck := cookieNamed(rec, cookieName); ck == nil || ck.MaxAge >= 0 || ck.Value != "" {
		t.Errorf("logout did not clear the cookie: %q", rec.Header().Get("Set-Cookie"))
	}
	expect(t, f.do(req{method: http.MethodGet, path: "/api/v1/metrics", session: &s}), http.StatusUnauthorized, "unauthenticated")
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/session", body: loginBody(testUser, testPassword)}), http.StatusUnauthorized, "invalid_credentials")
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/session", body: loginBody(testUser, next)}), http.StatusOK, "")
}

// E4: bodies are at most 1 KiB, JSON objects with exactly the known fields.
func TestAuthRequestBodies(t *testing.T) {
	f := newAccessFixture(t, clock.System())
	f.setupViaAPI()
	big := loginBody(testUser, strings.Repeat("x", 1100))
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/session", body: big}), http.StatusRequestEntityTooLarge, "request_too_large")
	for _, b := range []string{"", "not json", `{"username":"a","password":"b","admin":true}`, `{"username":"a","password":"b"}{}`, `["a","b"]`, `{"username":1,"password":"b"}`} {
		expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/session", body: b}), http.StatusBadRequest, "invalid_request")
	}
}

// ORB-AUTH-003 over HTTP: after five failures 429 with Retry-After, also for
// the right password.
func TestLoginRateLimitOverHTTP(t *testing.T) {
	c := clocktest.New()
	f := newAccessFixture(t, c)
	f.setupViaAPI()
	for i := 0; i < 5; i++ {
		expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/session", body: loginBody(testUser, "wrong-password-123")}), http.StatusUnauthorized, "invalid_credentials")
	}
	rec := f.do(req{method: http.MethodPost, path: "/api/v1/session", body: loginBody(testUser, testPassword)})
	expect(t, rec, http.StatusTooManyRequests, "rate_limited")
	var body struct {
		Error apiError `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Header().Get("Retry-After") != "1" || body.Error.RetryAfterSeconds != 1 {
		t.Errorf("Retry-After %q, body %+v", rec.Header().Get("Retry-After"), body.Error)
	}
	c.Advance(time.Second)
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/session", body: loginBody(testUser, testPassword)}), http.StatusOK, "")
}

// 8.7: every response has a server-made 64-bit correlation ID; typed errors
// carry it; a client value is ignored. Auth and API answers are not cached.
func TestCorrelationIDAndNoStore(t *testing.T) {
	f := newAccessFixture(t, clock.System())
	hexID := regexp.MustCompile(`^[0-9a-f]{16}$`)
	r := newRequest(http.MethodGet, "/api/v1/device", nil)
	r.Header.Set("X-Correlation-ID", "client-chosen")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, r)
	id := rec.Header().Get("X-Correlation-ID")
	var body struct {
		Error apiError `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if !hexID.MatchString(id) || body.Error.CorrelationID != id {
		t.Fatalf("header %q, body %q", id, body.Error.CorrelationID)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("API error is cacheable")
	}
	other := f.do(req{method: http.MethodGet, path: "/"}).Header().Get("X-Correlation-ID")
	if !hexID.MatchString(other) || other == id {
		t.Errorf("second ID %q", other)
	}
	s := f.setupViaAPI()
	for _, rr := range []req{
		{method: http.MethodPost, path: "/api/v1/session", body: loginBody(testUser, testPassword)},
		{method: http.MethodGet, path: "/api/v1/session", session: &s},
		{method: http.MethodGet, path: "/api/v1/device", session: &s},
		{method: http.MethodDelete, path: "/api/v1/session", session: &s},
	} {
		if got := f.do(rr).Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s %s: Cache-Control %q", rr.method, rr.path, got)
		}
	}
}

// E2: an event stream ends at once when its session ends, with a last
// "session" event, and the sampler stops after it. It ends on logout, on
// expiry and on eviction.
func TestEventStreamEndsWithItsSession(t *testing.T) {
	cases := map[string]func(f *accessFixture, s session, fake *clocktest.Fake){
		"logout": func(f *accessFixture, s session, _ *clocktest.Fake) {
			f.do(req{method: http.MethodDelete, path: "/api/v1/session", session: &s})
		},
		"expiry": func(_ *accessFixture, _ session, fake *clocktest.Fake) { fake.Advance(auth.IdleTimeout) },
		"eviction": func(f *accessFixture, _ session, _ *clocktest.Fake) {
			for i := 0; i < auth.MaxSessions; i++ {
				f.auth.login(f.t)
			}
		},
		"password change elsewhere": func(f *accessFixture, _ session, _ *clocktest.Fake) {
			other := f.auth.login(f.t)
			f.do(req{method: http.MethodPost, path: "/api/v1/account/password", session: &other, body: passwordBody(testPassword, "Another-Sentinel-55")})
		},
	}
	for name, end := range cases {
		t.Run(name, func(t *testing.T) {
			fake := clocktest.New()
			f := newAccessFixture(t, fake)
			s := f.setupViaAPI()
			srv := httptest.NewServer(f.handler)
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/events", nil)
			r.AddCookie(&http.Cookie{Name: cookieName, Value: s.token})
			resp, err := http.DefaultClient.Do(r)
			if err != nil || resp.StatusCode != http.StatusOK {
				t.Fatalf("open stream: %v %v", resp, err)
			}
			defer resp.Body.Close()
			waitUntil(t, "the sampler to run", f.sampler.Running)
			end(f, s, fake)
			rest, _ := io.ReadAll(resp.Body) // returns when the server ends the stream
			if !strings.HasSuffix(string(rest), sessionEnded) {
				t.Errorf("the stream did not end with the session event; tail %q", tail(string(rest), 120))
			}
			waitUntil(t, "the sampler to go idle", func() bool { return !f.sampler.Running() && f.sampler.Clients() == 0 })
		})
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// The event stream does not count as activity (E2): with only a stream open,
// the session still expires after the idle timeout.
func TestEventStreamDoesNotExtendTheSession(t *testing.T) {
	fake := clocktest.New()
	f := newAccessFixture(t, fake)
	s := f.setupViaAPI()
	srv := httptest.NewServer(f.handler)
	defer srv.Close()
	for i := 0; i < 3; i++ { // reconnects, as a browser does
		r, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/events", nil)
		r.AddCookie(&http.Cookie{Name: cookieName, Value: s.token})
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		resp, err := http.DefaultClient.Do(r.WithContext(ctx))
		if err == nil {
			_, _ = io.ReadAll(resp.Body)
			resp.Body.Close()
		}
		cancel()
		fake.Advance(20 * time.Minute)
	}
	expect(t, f.do(req{method: http.MethodGet, path: "/api/v1/metrics", session: &s}), http.StatusUnauthorized, "unauthenticated")
}

func (f *accessFixture) auditLines(t *testing.T) []map[string]any {
	t.Helper()
	f.audit.Close()
	data, err := os.ReadFile(filepath.Join(f.auth.stateDir, audit.DirName, audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("audit line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

// ORB-SEC-006, E7: the account events reach the audit file with the right
// user; a failed login records "-", never the name that was typed.
func TestAccountEventsAreAudited(t *testing.T) {
	f := newAccessFixture(t, clock.System())
	f.setupViaAPI()
	f.do(req{method: http.MethodPost, path: "/api/v1/session", body: loginBody(testPassword, "wrong-password-123")}) // password typed as user name
	f.do(req{method: http.MethodPost, path: "/api/v1/session", body: loginBody(testUser, "wrong-password-123")})
	s2 := f.loginViaAPI()
	f.do(req{method: http.MethodPost, path: "/api/v1/account/password", session: &s2, body: passwordBody("wrong-password-123", "Another-Sentinel-55")})
	f.do(req{method: http.MethodPost, path: "/api/v1/account/password", session: &s2, body: passwordBody(testPassword, "Another-Sentinel-55")})
	f.do(req{method: http.MethodPost, path: "/api/v1/account/password", session: &s2, noCSRF: true, body: "{}"})
	f.do(req{method: http.MethodDelete, path: "/api/v1/session", session: &s2})
	got := map[string]map[string]any{}
	for _, e := range f.auditLines(t) {
		got[e["action"].(string)+"/"+e["result"].(string)] = e
	}
	want := map[string]string{
		"setup_completed/success":               testUser,
		"login_failed/invalid_credentials":      "-",
		"login_succeeded/success":               testUser,
		"password_change_failed/wrong_password": testUser,
		"password_changed/success":              testUser,
		"request_rejected/csrf_failed":          "-",
		"logout/success":                        testUser,
	}
	for k, user := range want {
		e, ok := got[k]
		if !ok {
			t.Errorf("no %s entry in %v", k, got)
			continue
		}
		if e["user"] != user || e["plugin"] != "" || !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(e["correlation_id"].(string)) {
			t.Errorf("%s: %v", k, e)
		}
	}
	if c := got["login_failed/invalid_credentials"]["count"]; c != float64(2) {
		t.Errorf("failed logins aggregated to count %v, want 2", c)
	}
}

// AT-023, ORB-SEC-007: the password, the setup token, the session token and
// the CSRF token appear nowhere: not in the audit file, not in any response
// body except where the token is the intended field (csrf_token of the
// session answers), and not in the journal messages of the audit log.
func TestRedactionOfSecrets(t *testing.T) {
	var warns []string
	f := newAccessFixture(t, clock.System())
	l, _ := audit.Open(f.auth.stateDir, clock.System(), func(m string) { warns = append(warns, m) })
	f.audit = l
	f.handler = NewHandler(fstest.MapFS{}, WithAuth(f.auth.m), WithAudit(l), WithMetrics(f.sampler))
	setupToken := f.auth.setupToken(t)

	f.do(req{method: http.MethodPost, path: "/api/v1/setup", body: setupBody(setupToken+"0", testUser, testPassword)})
	f.do(req{method: http.MethodPost, path: "/api/v1/setup", body: setupBody(setupToken, testPassword, testPassword)}) // invalid user name
	setupAnswer := f.do(req{method: http.MethodPost, path: "/api/v1/setup", body: setupBody(setupToken, testUser, testPassword)})
	s := sessionFrom(t, setupAnswer)
	f.do(req{method: http.MethodPost, path: "/api/v1/session", body: loginBody(testPassword, testPassword)})
	f.do(req{method: http.MethodPost, path: "/api/v1/session", body: loginBody(testUser, testPassword+"!")})
	f.do(req{method: http.MethodPost, path: "/api/v1/account/password", session: &s, csrf: testPassword, body: passwordBody(testPassword, testPassword)})
	f.do(req{method: http.MethodPost, path: "/api/v1/account/password", session: &s, body: passwordBody(testPassword+"!", "Sentinel-Next-9b2d")})
	f.do(req{method: http.MethodPost, path: "/api/v1/account/password", session: &s, body: passwordBody(testPassword, "Sentinel-Next-9b2d")})
	f.do(req{method: http.MethodGet, path: "/api/v1/device?token=" + s.token, session: &s})
	f.do(req{method: http.MethodDelete, path: "/api/v1/session", session: &s, origin: "http://evil.example"})
	f.do(req{method: http.MethodDelete, path: "/api/v1/session", session: &s})

	secrets := map[string]string{"password": testPassword, "new password": "Sentinel-Next-9b2d", "setup token": setupToken, "session token": s.token, "CSRF token": s.csrf}
	f.audit.Close()
	auditData, _ := os.ReadFile(filepath.Join(f.auth.stateDir, audit.DirName, audit.FileName))
	if len(auditData) == 0 {
		t.Fatal("no audit written; the test would be blind")
	}
	for name, secret := range secrets {
		if strings.Contains(string(auditData), secret) {
			t.Errorf("the %s is in the audit file", name)
		}
		if strings.Contains(strings.Join(warns, "\n"), secret) {
			t.Errorf("the %s is in a journal message", name)
		}
		for i, b := range f.bodies {
			if name == "CSRF token" && strings.Contains(b, `"csrf_token":"`+secret+`"`) {
				continue // the intended field of the session answers
			}
			if strings.Contains(b, secret) {
				t.Errorf("the %s is in response %d: %s", name, i, b)
			}
		}
	}
	// The state directory holds the token only until setup, and never the password.
	_ = filepath.Walk(f.auth.stateDir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		for name, secret := range secrets {
			if strings.Contains(string(b), secret) {
				t.Errorf("the %s is in %s", name, p)
			}
		}
		return nil
	})
}
