package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/swingstate/oarfish/backend/internal/auth"
	"github.com/swingstate/oarfish/backend/internal/clock"
	"github.com/swingstate/oarfish/backend/internal/clock/clocktest"
	"github.com/swingstate/oarfish/backend/internal/updater"
)

// fakeUpdates is the updater behind the routes. It records what the routes ask
// of it, in order.
type fakeUpdates struct {
	mu         sync.Mutex
	calls      []string
	state      updater.State
	checkErr   *updater.Error
	reserveErr *updater.Error
	applicable *updater.Error
	awaiting   bool
	started    []updater.ApplyRequest
	startErr   *updater.Error
	discardErr *updater.Error
}

func (f *fakeUpdates) rec(s string) { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }

func (f *fakeUpdates) list() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, ",")
}

func (f *fakeUpdates) State(context.Context) updater.State { f.rec("state"); return f.state }

func (f *fakeUpdates) Check(_ context.Context, force bool) (updater.State, *updater.Error) {
	if force {
		f.rec("check-force")
	} else {
		f.rec("check")
	}
	return f.state, f.checkErr
}

func (f *fakeUpdates) Notice(context.Context) updater.Notice {
	f.rec("notice")
	return updater.Notice{SchemaVersion: 1, InstalledVersion: "0.1.0-alpha.6"}
}

func (f *fakeUpdates) Reserve() (updater.Slot, *updater.Error) {
	f.rec("reserve")
	if f.reserveErr != nil {
		return nil, f.reserveErr
	}
	return &fakeSlot{f: f}, nil
}

func (f *fakeUpdates) Applicable(v string) *updater.Error {
	f.rec("applicable " + v)
	return f.applicable
}
func (f *fakeUpdates) Resume(v string) *updater.Error { f.rec("resume " + v); return nil }
func (f *fakeUpdates) Awaiting(string) bool           { return f.awaiting }
func (f *fakeUpdates) Discard(_ context.Context, v, cid string) *updater.Error {
	f.rec("discard " + v + " " + cid)
	return f.discardErr
}

type fakeSlot struct{ f *fakeUpdates }

func (s *fakeSlot) Release() { s.f.rec("release") }
func (s *fakeSlot) Start(r updater.ApplyRequest) *updater.Error {
	s.f.rec("start")
	s.f.mu.Lock()
	s.f.started = append(s.f.started, r)
	s.f.mu.Unlock()
	return s.f.startErr
}

// updateFixture is the access fixture with the update routes behind a fake.
func updateFixture(t *testing.T, c clock.Clock) (*accessFixture, *fakeUpdates, session) {
	t.Helper()
	f := newAccessFixture(t, c)
	fake := &fakeUpdates{}
	assets := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}
	f.handler = NewHandler(assets, WithAuth(f.auth.m), WithAudit(f.audit), WithUpdater(fake))
	return f, fake, f.setupViaAPI()
}

func applyBody(version, password string) string {
	b, _ := json.Marshal(map[string]any{"version": version, "current_password": password})
	return string(b)
}

const upd = "0.1.0-alpha.9"

// D-C46, U9: every update route needs a session; the state-changing ones also
// need the CSRF layers, JSON and a body of at most 1 KiB.
func TestUpdateRoutesNeedASessionAndTheCSRFLayers(t *testing.T) {
	f, fake, s := updateFixture(t, clock.System())
	gets := []string{"/api/v1/update", "/api/v1/update/notice"}
	posts := []string{"/api/v1/update/check", "/api/v1/update/apply", "/api/v1/update/discard"}
	for _, p := range gets {
		expect(t, f.do(req{method: http.MethodGet, path: p}), http.StatusUnauthorized, "unauthenticated")
		expect(t, f.do(req{method: http.MethodGet, path: p, session: &session{token: "forged-" + s.token}}), http.StatusUnauthorized, "unauthenticated")
		expect(t, f.do(req{method: http.MethodGet, path: p, session: &s}), http.StatusOK, "")
		for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			expect(t, f.do(req{method: m, path: p, session: &s, body: "{}"}), http.StatusMethodNotAllowed, "method_not_allowed")
		}
		// A GET takes no body.
		expect(t, f.do(req{method: http.MethodGet, path: p, session: &s, body: "{}"}), http.StatusBadRequest, "request_body_not_allowed")
	}
	body := map[string]string{"/api/v1/update/check": `{}`, "/api/v1/update/apply": applyBody(upd, testPassword), "/api/v1/update/discard": `{"version":"` + upd + `"}`}
	for _, p := range posts {
		b := body[p]
		expect(t, f.do(req{method: http.MethodPost, path: p, body: b}), http.StatusUnauthorized, "unauthenticated")
		expect(t, f.do(req{method: http.MethodPost, path: p, session: &s, noCSRF: true, body: b}), http.StatusForbidden, "csrf_failed")
		expect(t, f.do(req{method: http.MethodPost, path: p, session: &s, csrf: "wrong", body: b}), http.StatusForbidden, "csrf_failed")
		expect(t, f.do(req{method: http.MethodPost, path: p, session: &s, origin: "http://evil.example", body: b}), http.StatusForbidden, "origin_rejected")
		expect(t, f.do(req{method: http.MethodPost, path: p, session: &s, origin: "-", secFetchSite: "-", body: b}), http.StatusForbidden, "origin_rejected")
		expect(t, f.do(req{method: http.MethodPost, path: p, session: &s, secFetchSite: "cross-site", body: b}), http.StatusForbidden, "origin_rejected")
		expect(t, f.do(req{method: http.MethodPost, path: p, session: &s, contentType: "text/plain", body: b}), http.StatusUnsupportedMediaType, "unsupported_media_type")
		expect(t, f.do(req{method: http.MethodPost, path: p, session: &s, contentType: "application/x-www-form-urlencoded", body: "version=" + upd}), http.StatusUnsupportedMediaType, "")
		expect(t, f.do(req{method: http.MethodGet, path: p, session: &s}), http.StatusMethodNotAllowed, "method_not_allowed")
		if p != "/api/v1/update/check" { // check accepts an empty body
			expect(t, f.do(req{method: http.MethodPost, path: p, session: &s, body: `{"version":"` + strings.Repeat("1", 2000) + `"}`}), http.StatusRequestEntityTooLarge, "request_too_large")
			expect(t, f.do(req{method: http.MethodPost, path: p, session: &s, body: `{"version":"` + upd + `","path":"/etc"}`}), http.StatusBadRequest, "invalid_request")
			expect(t, f.do(req{method: http.MethodPost, path: p, session: &s, body: `{"version":"` + upd + `"} {}`}), http.StatusBadRequest, "invalid_request")
		}
	}
	// None of the refused requests reached the updater beyond the GETs.
	if got := fake.list(); strings.Contains(got, "reserve") || strings.Contains(got, "start") || strings.Contains(got, "discard") || strings.Contains(got, "check") {
		t.Fatalf("a refused request reached the updater: %s", got)
	}
}

// A body over 1 KiB on apply and discard is request_too_large with the
// recovery of the update routes, not the one of login and setup (G6.8, C2).
func TestApplyAndDiscardOversizeBodyRecovery(t *testing.T) {
	f, fake, s := updateFixture(t, clock.System())
	for _, p := range []string{"/api/v1/update/apply", "/api/v1/update/discard", "/api/v1/update/check"} {
		rec := f.do(req{method: http.MethodPost, path: p, session: &s, body: `{"version":"` + strings.Repeat("1", 2000) + `"}`})
		expect(t, rec, http.StatusRequestEntityTooLarge, "request_too_large")
		var body struct {
			Error apiError `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Error.Recovery != "Send a smaller request." {
			t.Errorf("%s: recovery %q", p, body.Error.Recovery)
		}
	}
	// The access routes keep their own text.
	rec := f.do(req{method: http.MethodPost, path: "/api/v1/account/password", session: &s, body: passwordBody(strings.Repeat("x", 2000), "y")})
	expect(t, rec, http.StatusRequestEntityTooLarge, "request_too_large")
	if !strings.Contains(rec.Body.String(), "Use a shorter user name and password.") {
		t.Errorf("password change: %s", rec.Body.String())
	}
	if fake.list() != "" {
		t.Fatalf("calls: %s", fake.list())
	}
}

// The routes take no path, URL or command: a version that is not a version is
// refused before anything else happens.
func TestApplyAndDiscardTakeOnlyAVersion(t *testing.T) {
	f, fake, s := updateFixture(t, clock.System())
	for _, v := range []string{"", "latest", "../../x", "1.2", "1.2.3/../x", "v1.2.3", "1.2.3 ", "https://example.org/x", "1.2.3\n", strings.Repeat("1", 70) + ".0.0"} {
		expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/apply", session: &s, body: applyBody(v, testPassword)}), http.StatusBadRequest, "invalid_request")
		d, _ := json.Marshal(map[string]string{"version": v})
		expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/discard", session: &s, body: string(d)}), http.StatusBadRequest, "invalid_request")
	}
	if fake.list() != "" {
		t.Fatalf("calls: %s", fake.list())
	}
}

func TestApplyOrderPasswordThenStart(t *testing.T) {
	f, fake, s := updateFixture(t, clock.System())
	rec := f.do(req{method: http.MethodPost, path: "/api/v1/update/apply", session: &s, body: applyBody(upd, testPassword)})
	expect(t, rec, http.StatusAccepted, "")
	if fake.list() != "reserve,applicable "+upd+",start,release" { // Release after Start is a no-op of the real slot
		t.Fatalf("calls: %s", fake.list())
	}
	r := fake.started[0]
	if r.Version != upd || r.User != testUser || r.CorrelationID != rec.Header().Get("X-Correlation-ID") || len(r.CorrelationID) != 16 {
		t.Fatalf("%+v", r)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["accepted"] != true {
		t.Fatalf("%s", rec.Body)
	}
	// "start" ran after the password: the audit has update_started only through the updater (the fake writes none).
}

// U9, U12, ADR 0009 A3: a wrong password never starts anything, is audited as
// update_started/wrong_password with the version target, and goes through the
// shared limiter.
func TestApplyPasswordGate(t *testing.T) {
	f, fake, s := updateFixture(t, clock.System())
	for i := 0; i < 3; i++ {
		rec := f.do(req{method: http.MethodPost, path: "/api/v1/update/apply", session: &s, body: applyBody(upd, "wrong-password-123")})
		expect(t, rec, http.StatusForbidden, "wrong_password")
	}
	if strings.Contains(fake.list(), "start") {
		t.Fatalf("an apply with a wrong password started: %s", fake.list())
	}
	if strings.Count(fake.list(), "release") != 3 {
		t.Errorf("the slot was not given back: %s", fake.list())
	}
	// An empty password is wrong as well.
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/apply", session: &s, body: applyBody(upd, "")}), http.StatusForbidden, "wrong_password")
	lines := f.auditLines(t)
	n := 0
	for _, l := range lines {
		if l["action"] == "update_started" {
			n++
			if l["result"] != "wrong_password" || l["target"] != "update/"+upd || l["user"] != testUser {
				t.Errorf("%v", l)
			}
		}
	}
	if n != 4 {
		t.Fatalf("%d update_started entries, want 4 (not aggregated)", n)
	}
}

// The same global limiter as login: five failures anywhere start the backoff,
// and then even the correct password is refused, with Retry-After.
func TestApplySharesTheLimiterWithLogin(t *testing.T) {
	fake := clocktest.New()
	f, up, s := updateFixture(t, fake)
	for i := 0; i < auth.FailuresBeforeBackoff; i++ { // five failed logins
		f.do(req{method: http.MethodPost, path: "/api/v1/session", body: loginBody(testUser, "wrong-password-123")})
		if i < auth.FailuresBeforeBackoff-1 {
			fake.Advance(7 * time.Second) // stay inside the ten attempts per minute
		}
	}
	rec := f.do(req{method: http.MethodPost, path: "/api/v1/update/apply", session: &s, body: applyBody(upd, testPassword)})
	expect(t, rec, http.StatusTooManyRequests, "rate_limited")
	if rec.Header().Get("Retry-After") == "" {
		t.Error("no Retry-After")
	}
	if strings.Contains(up.list(), "start") {
		t.Fatal("apply started while the limiter refused")
	}
	// And the other way round: failed applies count against the login.
	f2, _, s2 := updateFixture(t, clocktest.New())
	for i := 0; i < auth.FailuresBeforeBackoff; i++ {
		f2.do(req{method: http.MethodPost, path: "/api/v1/update/apply", session: &s2, body: applyBody(upd, "wrong-password-123")})
	}
	expect(t, f2.do(req{method: http.MethodPost, path: "/api/v1/session", body: loginBody(testUser, testPassword)}), http.StatusTooManyRequests, "rate_limited")
	_ = s2
}

// The cheap refusals come before the hash: a request that cannot start does not
// cost an Argon2id hash and does not count against the limiter.
func TestApplyRefusalsBeforeTheHashDoNotUseTheLimiter(t *testing.T) {
	fake := clocktest.New()
	f, up, s := updateFixture(t, fake)
	up.reserveErr = &updater.Error{Code: updater.CodeBusy}
	for i := 0; i < 20; i++ {
		expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/apply", session: &s, body: applyBody(upd, "wrong-password-123")}), http.StatusConflict, "busy")
	}
	up.reserveErr = nil
	up.applicable = &updater.Error{Code: updater.CodeVersionMismatch}
	for i := 0; i < 20; i++ {
		expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/apply", session: &s, body: applyBody(upd, "wrong-password-123")}), http.StatusConflict, "version_mismatch")
	}
	up.applicable = &updater.Error{Code: updater.CodeNotChecked}
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/apply", session: &s, body: applyBody(upd, testPassword)}), http.StatusConflict, "not_checked")
	up.applicable = &updater.Error{Code: updater.CodeSignatureInvalid}
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/apply", session: &s, body: applyBody(upd, testPassword)}), http.StatusUnprocessableEntity, "signature_invalid")
	up.applicable = nil
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/apply", session: &s, body: applyBody(upd, testPassword)}), http.StatusAccepted, "")
	if strings.Contains(up.list(), "start,start") {
		t.Fatal("two starts")
	}
}

func TestApplyStartErrorsAreTyped(t *testing.T) {
	f, up, s := updateFixture(t, clock.System())
	up.startErr = &updater.Error{Code: updater.CodeVersionMismatch}
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/apply", session: &s, body: applyBody(upd, testPassword)}), http.StatusConflict, "version_mismatch")
}

func TestApplySecondStageNeedsThePasswordAgain(t *testing.T) {
	f, up, s := updateFixture(t, clock.System())
	up.awaiting = true
	body := func(pw string) string {
		b, _ := json.Marshal(map[string]any{"version": upd, "current_password": pw, "components_confirmed": true})
		return string(b)
	}
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/apply", session: &s, body: body("wrong-password-123")}), http.StatusForbidden, "wrong_password")
	if strings.Contains(up.list(), "resume") {
		t.Fatal("resumed with a wrong password")
	}
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/apply", session: &s, body: body(testPassword)}), http.StatusAccepted, "")
	if up.list() != "resume "+upd {
		t.Fatalf("%s", up.list())
	}
}

// A first apply never carries the second confirmation (ADR 0010 A17, S7): with
// components_confirmed and no waiting job the request is refused before the slot
// and before the password, and nothing starts.
func TestApplyFirstStageRefusesTheSecondConfirmation(t *testing.T) {
	f, up, s := updateFixture(t, clock.System())
	up.awaiting = false
	b, _ := json.Marshal(map[string]any{"version": upd, "current_password": testPassword, "components_confirmed": true})
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/apply", session: &s, body: string(b)}), http.StatusBadRequest, "invalid_request")
	if len(up.started) != 0 || strings.Contains(up.list(), "reserve") || strings.Contains(up.list(), "resume") {
		t.Fatalf("%s %+v", up.list(), up.started)
	}
}

func TestCheckRouteBodyAndErrors(t *testing.T) {
	f, up, s := updateFixture(t, clock.System())
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/check", session: &s}), http.StatusOK, "")
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/check", session: &s, body: `{}`}), http.StatusOK, "")
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/check", session: &s, body: `{"force":true}`}), http.StatusOK, "")
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/check", session: &s, body: `{"force":"yes"}`}), http.StatusBadRequest, "invalid_request")
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/check", session: &s, body: `{"url":"https://x"}`}), http.StatusBadRequest, "invalid_request")
	if up.list() != "check,check,check-force" {
		t.Fatalf("%s", up.list())
	}
	up.checkErr = &updater.Error{Code: updater.CodeBusy}
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/check", session: &s, body: `{}`}), http.StatusConflict, "busy")
}

func TestDiscardRoute(t *testing.T) {
	f, up, s := updateFixture(t, clock.System())
	rec := f.do(req{method: http.MethodPost, path: "/api/v1/update/discard", session: &s, body: `{"version":"` + upd + `"}`})
	expect(t, rec, http.StatusOK, "")
	if up.list() != "discard "+upd+" "+rec.Header().Get("X-Correlation-ID") {
		t.Fatalf("%s", up.list())
	}
	up.discardErr = &updater.Error{Code: updater.CodeNotFound}
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/discard", session: &s, body: `{"version":"` + upd + `"}`}), http.StatusNotFound, "not_found")
	up.discardErr = &updater.Error{Code: updater.CodeBusy}
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/update/discard", session: &s, body: `{"version":"` + upd + `"}`}), http.StatusConflict, "busy")
}

// A11.1: the notice is a read, it does not extend the session; the page's
// state does.
func TestNoticeDoesNotExtendTheSession(t *testing.T) {
	fake := clocktest.New()
	f, _, s := updateFixture(t, fake)
	for i := 0; i < 3; i++ {
		fake.Advance(auth.IdleTimeout / 2)
		if i < 1 {
			expect(t, f.do(req{method: http.MethodGet, path: "/api/v1/update/notice", session: &s}), http.StatusOK, "")
		}
	}
	// Idle for 1.5 h with only a notice at 30 min: expired.
	expect(t, f.do(req{method: http.MethodGet, path: "/api/v1/update", session: &s}), http.StatusUnauthorized, "unauthenticated")

	f2, _, s2 := updateFixture(t, fake)
	fake.Advance(auth.IdleTimeout / 2)
	expect(t, f2.do(req{method: http.MethodGet, path: "/api/v1/update", session: &s2}), http.StatusOK, "")
	fake.Advance(auth.IdleTimeout / 2)
	expect(t, f2.do(req{method: http.MethodGet, path: "/api/v1/update", session: &s2}), http.StatusOK, "")
}

func TestUpdateRoutesWithoutAnUpdater(t *testing.T) {
	f := newAccessFixture(t, clock.System())
	s := f.setupViaAPI()
	for _, c := range []struct{ m, p, b string }{
		{http.MethodGet, "/api/v1/update", ""}, {http.MethodGet, "/api/v1/update/notice", ""}, {http.MethodPost, "/api/v1/update/check", "{}"},
		{http.MethodPost, "/api/v1/update/apply", applyBody(upd, testPassword)}, {http.MethodPost, "/api/v1/update/discard", `{"version":"` + upd + `"}`},
	} {
		expect(t, f.do(req{method: c.m, path: c.p, session: &s, body: c.b}), http.StatusServiceUnavailable, "update_unavailable")
	}
}

// ORB-SEC-007: the password reaches neither an answer nor the audit file.
func TestUpdateRedaction(t *testing.T) {
	f, up, s := updateFixture(t, clock.System())
	f.do(req{method: http.MethodPost, path: "/api/v1/update/apply", session: &s, body: applyBody(upd, "wrong-"+testPassword)})
	f.do(req{method: http.MethodPost, path: "/api/v1/update/apply", session: &s, body: applyBody(upd, testPassword)})
	up.applicable = &updater.Error{Code: updater.CodeNotChecked}
	f.do(req{method: http.MethodPost, path: "/api/v1/update/apply", session: &s, body: applyBody(upd, testPassword)})
	lines := f.auditLines(t)
	dump, _ := json.Marshal(lines)
	all := string(dump) + strings.Join(f.bodies, "\n")
	if strings.Contains(all, testPassword) || strings.Contains(all, "current_password") {
		t.Fatal("the password or its field name leaked")
	}
}
