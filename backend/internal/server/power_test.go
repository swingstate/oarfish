package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/swingstate/oarfish/backend/internal/audit"
	"github.com/swingstate/oarfish/backend/internal/auth"
	"github.com/swingstate/oarfish/backend/internal/brokerclient"
	"github.com/swingstate/oarfish/backend/internal/clock"
	"github.com/swingstate/oarfish/backend/internal/clock/clocktest"
	"github.com/swingstate/oarfish/backend/internal/updater"
)

// powerLog is one ordered list of what the power route asked of its fakes.
type powerLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *powerLog) rec(s string) { l.mu.Lock(); l.calls = append(l.calls, s); l.mu.Unlock() }

func (l *powerLog) list() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.calls, ",")
}

func (l *powerLog) count(prefix string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, c := range l.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

type fakePowerBroker struct {
	log       *powerLog
	available bool
	code      brokerclient.Code
	during    func(ctx context.Context) // runs inside Power, before it answers
}

func (b *fakePowerBroker) Available() bool { b.log.rec("stat"); return b.available }

func (b *fakePowerBroker) Power(ctx context.Context, op brokerclient.PowerOp, cid string) brokerclient.Code {
	b.log.rec("power " + string(op) + " " + cid)
	if b.during != nil {
		b.during(ctx)
	}
	return b.code
}

type fakeUpdateActivity struct {
	log *powerLog
	a   updater.Activity
}

func (u *fakeUpdateActivity) Activity(context.Context) updater.Activity {
	u.log.rec("updates")
	return u.a
}

type fakeServiceActivity struct {
	log     *powerLog
	running bool
}

func (s *fakeServiceActivity) ActionRunning() bool { s.log.rec("services"); return s.running }

type powerFixture struct {
	*accessFixture
	log     *powerLog
	broker  *fakePowerBroker
	updates *fakeUpdateActivity
	svc     *fakeServiceActivity
	views   int
}

func newPowerFixture(t *testing.T, c clock.Clock) (*powerFixture, session) {
	t.Helper()
	log := &powerLog{}
	p := &powerFixture{accessFixture: newAccessFixture(t, c), log: log,
		broker:  &fakePowerBroker{log: log, available: true, code: brokerclient.CodeAccepted},
		updates: &fakeUpdateActivity{log: log}, svc: &fakeServiceActivity{log: log}}
	assets := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}
	p.handler = NewHandler(assets, WithAuth(p.auth.m), WithAudit(p.audit),
		WithPower(PowerConfig{Broker: p.broker, Updates: p.updates, Services: p.svc, Views: func() int { p.log.rec("views"); return p.views }}))
	return p, p.setupViaAPI()
}

const powerPath = "/api/v1/power"

func powerBody(action, password string) string {
	b, _ := json.Marshal(map[string]string{"action": action, "current_password": password})
	return string(b)
}

// powerEntries are the audit lines of the power actions.
func powerEntries(t *testing.T, f *powerFixture) []map[string]any {
	t.Helper()
	f.audit.Close()
	var out []map[string]any
	for _, l := range f.auditLines(t) {
		if a := l["action"].(string); a == "system_reboot" || a == "system_poweroff" {
			out = append(out, l)
		}
	}
	return out
}

// B4, ORB-SEC-004, BF22: both routes need a session; the POST also needs the
// three CSRF layers, JSON and a body of at most 1 KiB. A refused request
// reaches neither the password check nor the broker and is audited as today.
func TestPowerRoutesNeedASessionAndTheCSRFLayers(t *testing.T) {
	f, s := newPowerFixture(t, clock.System())
	expect(t, f.do(req{method: http.MethodGet, path: powerPath}), http.StatusUnauthorized, "unauthenticated")
	expect(t, f.do(req{method: http.MethodGet, path: powerPath, session: &session{token: "forged-" + s.token}}), http.StatusUnauthorized, "unauthenticated")
	expect(t, f.do(req{method: http.MethodGet, path: powerPath, session: &s, body: "{}"}), http.StatusBadRequest, "request_body_not_allowed")
	for _, m := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch} {
		expect(t, f.do(req{method: m, path: powerPath, session: &s, body: "{}"}), http.StatusMethodNotAllowed, "method_not_allowed")
	}
	b := powerBody("reboot", testPassword)
	expect(t, f.do(req{method: http.MethodPost, path: powerPath, body: b}), http.StatusUnauthorized, "unauthenticated")
	expect(t, f.do(req{method: http.MethodPost, path: powerPath, session: &s, noCSRF: true, body: b}), http.StatusForbidden, "csrf_failed")
	expect(t, f.do(req{method: http.MethodPost, path: powerPath, session: &s, csrf: "wrong", body: b}), http.StatusForbidden, "csrf_failed")
	expect(t, f.do(req{method: http.MethodPost, path: powerPath, session: &s, origin: "http://evil.example", body: b}), http.StatusForbidden, "origin_rejected")
	expect(t, f.do(req{method: http.MethodPost, path: powerPath, session: &s, origin: "-", secFetchSite: "-", body: b}), http.StatusForbidden, "origin_rejected")
	expect(t, f.do(req{method: http.MethodPost, path: powerPath, session: &s, secFetchSite: "cross-site", body: b}), http.StatusForbidden, "origin_rejected")
	expect(t, f.do(req{method: http.MethodPost, path: powerPath, session: &s, contentType: "text/plain", body: b}), http.StatusUnsupportedMediaType, "unsupported_media_type")
	expect(t, f.do(req{method: http.MethodPost, path: powerPath, session: &s, host: "evil.example", body: b}), http.StatusForbidden, "host_rejected")
	if n := f.log.count("power"); n != 0 {
		t.Fatalf("a refused request reached the broker: %s", f.log.list())
	}
	var csrf, origin int
	entries := 0
	f.audit.Close()
	for _, l := range f.auditLines(t) {
		switch {
		case l["action"] == "request_rejected" && l["result"] == "csrf_failed":
			csrf += int(l["count"].(float64))
		case l["action"] == "request_rejected" && l["result"] == "origin_rejected":
			origin += int(l["count"].(float64))
		case l["action"] == "system_reboot":
			entries++
		}
	}
	if csrf != 2 || origin != 3 || entries != 0 {
		t.Errorf("audited csrf_failed %d, origin_rejected %d, power entries %d", csrf, origin, entries)
	}
}

// The body: at most 1 KiB, JSON, exactly action and current_password; the
// action from the closed set. Every refusal is 400 or 413, is not audited as a
// power action, checks no password and reaches no broker.
func TestPowerBody(t *testing.T) {
	f, s := newPowerFixture(t, clock.System())
	big := `{"action":"reboot","current_password":"` + strings.Repeat("x", 1100) + `"}`
	expect(t, f.do(req{method: http.MethodPost, path: powerPath, session: &s, body: big}), http.StatusRequestEntityTooLarge, "request_too_large")
	for _, b := range []string{
		`{"action":"reboot","current_password":"x","force":true}`,
		`{"action":"reboot","current_password":"x","unit":"a.service"}`,
		`{"action":"reboot","current_password":"x"} {}`,
		`{"action":"reboot","current_password":7}`,
		`{"action":["reboot"],"current_password":"x"}`,
		`[]`, `not json`, ``,
	} {
		expect(t, f.do(req{method: http.MethodPost, path: powerPath, session: &s, body: b}), http.StatusBadRequest, "invalid_request")
	}
	for _, a := range []string{"", "halt", "kexec", "Reboot", "REBOOT", "reboot ", "poweroff\n", "shutdown", "soft-reboot", "system.reboot"} {
		expect(t, f.do(req{method: http.MethodPost, path: powerPath, session: &s, body: powerBody(a, testPassword)}), http.StatusBadRequest, "invalid_request")
		// A wrong password with an invalid action is not checked either.
		expect(t, f.do(req{method: http.MethodPost, path: powerPath, session: &s, body: powerBody(a, "Sentinel-Wrong-9d")}), http.StatusBadRequest, "invalid_request")
	}
	if f.log.list() != "" {
		t.Errorf("calls %s", f.log.list())
	}
	if e := powerEntries(t, f); len(e) != 0 {
		t.Errorf("audited %v", e)
	}
	// The limiter was never touched: the failures above were not counted, so
	// the right password works at once.
	f2, s2 := newPowerFixture(t, clock.System())
	for i := 0; i < 8; i++ {
		f2.do(req{method: http.MethodPost, path: powerPath, session: &s2, body: powerBody("halt", "Sentinel-Wrong-9d")})
	}
	expect(t, f2.do(req{method: http.MethodPost, path: powerPath, session: &s2, body: powerBody("reboot", testPassword)}), http.StatusOK, "")
}

// BF23: a wrong password is wrong_password, repeated it is the shared
// limiter's rate_limited; both are audited with the action and the target
// system, both use the status codes and bodies of the update route's password
// step, and nothing reaches the update check, the service check or the broker.
func TestPowerWrongPasswordNeverReachesTheBroker(t *testing.T) {
	f, s := newPowerFixture(t, clock.System())
	for i := 0; i < auth.FailuresBeforeBackoff; i++ {
		rec := f.do(req{method: http.MethodPost, path: powerPath, session: &s, body: powerBody("poweroff", "Sentinel-Wrong-9d")})
		expect(t, rec, http.StatusForbidden, "wrong_password")
	}
	rec := f.do(req{method: http.MethodPost, path: powerPath, session: &s, body: powerBody("poweroff", "Sentinel-Wrong-9d")})
	expect(t, rec, http.StatusTooManyRequests, "rate_limited")
	var body struct {
		Error apiError `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Error.RetryAfterSeconds < 1 || rec.Header().Get("Retry-After") == "" {
		t.Errorf("no wait time: %s", rec.Body.String())
	}
	// Limited: even the right password does not reach the broker now.
	expect(t, f.do(req{method: http.MethodPost, path: powerPath, session: &s, body: powerBody("reboot", testPassword)}), http.StatusTooManyRequests, "rate_limited")
	if f.log.list() != "" {
		t.Fatalf("calls after wrong or limited passwords: %s", f.log.list())
	}
	e := powerEntries(t, f)
	if len(e) != 7 {
		t.Fatalf("entries %v", e)
	}
	for i, l := range e[:5] {
		if l["action"] != "system_poweroff" || l["target"] != "system" || l["result"] != "wrong_password" || l["user"] != testUser {
			t.Errorf("%d: %v", i, l)
		}
	}
	if e[5]["result"] != "rate_limited" || e[6]["result"] != "rate_limited" || e[6]["action"] != "system_reboot" {
		t.Errorf("limited %v %v", e[5], e[6])
	}
}

// The order of the checks after the password: the socket, the update, a
// running service action, then the broker. Each refusal stops there and is
// audited once.
func TestPowerOrderOfChecks(t *testing.T) {
	f, s := newPowerFixture(t, clock.System())
	rec := f.do(req{method: http.MethodPost, path: powerPath, session: &s, body: powerBody("reboot", testPassword)})
	expect(t, rec, http.StatusOK, "")
	cid := rec.Header().Get("X-Correlation-ID")
	if got, want := f.log.list(), "stat,updates,services,power system.reboot "+cid; got != want {
		t.Fatalf("order %s\nwant  %s", got, want)
	}
	if rec.Body.String() != `{"accepted":true,"schema_version":1}`+"\n" {
		t.Errorf("body %s", rec.Body.String())
	}

	for _, tc := range []struct {
		name   string
		set    func(*powerFixture)
		status int
		code   string
		calls  string
	}{
		{"socket missing", func(p *powerFixture) { p.broker.available = false }, http.StatusServiceUnavailable, "unavailable", "stat"},
		{"update running (service source)", func(p *powerFixture) { p.updates.a.Running = true }, http.StatusConflict, "update_running", "stat,updates"},
		{"service action running", func(p *powerFixture) { p.svc.running = true }, http.StatusConflict, "busy", "stat,updates,services"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, ps := newPowerFixture(t, clock.System())
			tc.set(p)
			expect(t, p.do(req{method: http.MethodPost, path: powerPath, session: &ps, body: powerBody("poweroff", testPassword)}), tc.status, tc.code)
			if got := p.log.list(); got != tc.calls {
				t.Errorf("calls %s, want %s", got, tc.calls)
			}
			e := powerEntries(t, p)
			if len(e) != 1 || e[0]["action"] != "system_poweroff" || e[0]["target"] != "system" || e[0]["result"] != tc.code || e[0]["user"] != testUser {
				t.Errorf("audit %v", e)
			}
		})
	}
	// A staged update does not refuse: it is a waiting item that the reboot discards.
	p, ps := newPowerFixture(t, clock.System())
	p.updates.a.Staged = true
	expect(t, p.do(req{method: http.MethodPost, path: powerPath, session: &ps, body: powerBody("reboot", testPassword)}), http.StatusOK, "")
}

// Every broker code reaches the page as a typed error with a fixed status and
// is audited with its result: accepted as success, the broker's own
// update_running mapped as the service's, bad_request and internal as error.
func TestPowerBrokerCodes(t *testing.T) {
	for _, tc := range []struct {
		code   brokerclient.Code
		status int
		result string
	}{
		{brokerclient.CodeAccepted, http.StatusOK, "success"},
		{brokerclient.CodeBusy, http.StatusConflict, "busy"},
		{brokerclient.CodeUpdateRunning, http.StatusConflict, "update_running"},
		{brokerclient.CodeAlreadyRequested, http.StatusConflict, "already_requested"},
		{brokerclient.CodeUnavailable, http.StatusServiceUnavailable, "unavailable"},
		{brokerclient.CodeJobFailed, http.StatusBadGateway, "job_failed"},
		{brokerclient.CodeTimeout, http.StatusBadGateway, "timeout"},
		{brokerclient.CodeBadRequest, http.StatusBadGateway, "error"},
		{brokerclient.CodeInternal, http.StatusBadGateway, "error"},
		{"something_new", http.StatusBadGateway, "error"},
	} {
		t.Run(string(tc.code), func(t *testing.T) {
			p, ps := newPowerFixture(t, clock.System())
			p.broker.code = tc.code
			rec := p.do(req{method: http.MethodPost, path: powerPath, session: &ps, body: powerBody("reboot", testPassword)})
			wantCode := string(tc.code)
			switch tc.code {
			case brokerclient.CodeAccepted:
				wantCode = ""
			case "something_new":
				wantCode = "internal"
			}
			expect(t, rec, tc.status, wantCode)
			e := powerEntries(t, p)
			if len(e) != 1 || e[0]["result"] != tc.result || e[0]["correlation_id"] != rec.Header().Get("X-Correlation-ID") || e[0]["plugin"] != "" || e[0]["count"] != float64(1) {
				t.Errorf("audit %v", e)
			}
			if strings.Contains(rec.Body.String(), "systemctl") || strings.Contains(rec.Body.String(), "/run/") {
				t.Errorf("a cause leaked: %s", rec.Body.String())
			}
		})
	}
}

// BF28 service side: a second request after accepted is sent to the broker as
// usual (the service keeps no flag of its own), and the broker's
// already_requested is 409 and audited.
func TestPowerRepeatedRequestGoesToTheBroker(t *testing.T) {
	f, s := newPowerFixture(t, clock.System())
	expect(t, f.do(req{method: http.MethodPost, path: powerPath, session: &s, body: powerBody("reboot", testPassword)}), http.StatusOK, "")
	f.broker.code = brokerclient.CodeAlreadyRequested
	expect(t, f.do(req{method: http.MethodPost, path: powerPath, session: &s, body: powerBody("poweroff", testPassword)}), http.StatusConflict, "already_requested")
	if n := f.log.count("power"); n != 2 {
		t.Errorf("broker calls %d: %s", n, f.log.list())
	}
	e := powerEntries(t, f)
	if len(e) != 2 || e[0]["result"] != "success" || e[1]["result"] != "already_requested" || e[1]["action"] != "system_poweroff" {
		t.Errorf("audit %v", e)
	}
}

// B5: the entry of an accepted operation is in the audit file when the answer
// is written, without any clock movement and without Close; while the broker
// works, there is no entry yet (it is written when the broker has answered).
func TestPowerAcceptedIsWrittenBeforeTheAnswer(t *testing.T) {
	f, s := newPowerFixture(t, clocktest.New())
	path := filepath.Join(f.auth.stateDir, audit.DirName, audit.FileName)
	powerLines := func() int {
		data, _ := os.ReadFile(path)
		return strings.Count(string(data), `"system_reboot"`)
	}
	f.broker.during = func(context.Context) {
		f.audit.Flush()
		if n := powerLines(); n != 0 {
			t.Errorf("%d power entries before the broker answered", n)
		}
	}
	var rec *httptest.ResponseRecorder
	rec = f.do(req{method: http.MethodPost, path: powerPath, session: &s, body: powerBody("reboot", testPassword)})
	expect(t, rec, http.StatusOK, "")
	if n := powerLines(); n != 1 {
		t.Fatalf("%d power entries in the file when the answer came", n)
	}
	f.audit.Close()
	if n := powerLines(); n != 1 {
		t.Errorf("%d power entries after Close", n)
	}
}

// When the write of the entry fails, the failure is counted by the log and
// the answer stays accepted: systemd has already queued the job. (A failed
// fsync is the same path; it is tested in the audit package.)
func TestPowerAuditFailureKeepsAccepted(t *testing.T) {
	f, s := newPowerFixture(t, clock.System())
	file := filepath.Join(f.auth.stateDir, audit.DirName, audit.FileName)
	_ = os.Remove(file)
	if err := os.Mkdir(file, 0o700); err != nil {
		t.Fatal(err)
	}
	rec := f.do(req{method: http.MethodPost, path: powerPath, session: &s, body: powerBody("poweroff", testPassword)})
	expect(t, rec, http.StatusOK, "")
	if !strings.Contains(rec.Body.String(), `"accepted":true`) || strings.Contains(rec.Body.String(), "audit") {
		t.Errorf("body %s", rec.Body.String())
	}
	if st := f.audit.Stats(); st.Failures < 1 {
		t.Errorf("stats %+v", st)
	}
}

// A browser that closes while the broker works does not cancel the broker
// call: the operation and its audit entry belong together.
func TestPowerBrokerCallOutlivesTheRequest(t *testing.T) {
	f, s := newPowerFixture(t, clock.System())
	ctx, cancel := context.WithCancel(context.Background())
	var brokerCtxErr error
	f.broker.during = func(bctx context.Context) { cancel(); brokerCtxErr = bctx.Err() }
	hr := newRequest(http.MethodPost, powerPath, strings.NewReader(powerBody("reboot", testPassword))).WithContext(ctx)
	hr.Header.Set("Origin", "http://"+hr.Host)
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set(csrfHeader, s.csrf)
	hr.AddCookie(&http.Cookie{Name: cookieName, Value: s.token})
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, hr)
	if brokerCtxErr != nil {
		t.Errorf("the broker's context ended with the request: %v", brokerCtxErr)
	}
	if e := powerEntries(t, f); len(e) != 1 || e[0]["result"] != "success" {
		t.Errorf("audit %v", e)
	}
}

// ORB-PWR-002, B6, D-C87 item 11: the waiting actions, from memory and the
// update read only: no broker connection, no password, no program. Other
// sessions do not count the caller's own; login sessions of the device are not
// counted at all.
func TestPowerWaiting(t *testing.T) {
	f, s := newPowerFixture(t, clock.System())
	get := func() map[string]any {
		t.Helper()
		rec := f.do(req{method: http.MethodGet, path: powerPath, session: &s})
		expect(t, rec, http.StatusOK, "")
		return decode(t, rec.Body.String())
	}
	m := get()
	if m["schema_version"] != float64(1) || m["broker_available"] != true || len(m["waiting"].([]any)) != 0 {
		t.Errorf("empty: %v", m)
	}
	if raw := f.do(req{method: http.MethodGet, path: powerPath, session: &s}).Body.String(); !strings.Contains(raw, `"waiting":[]`) {
		t.Errorf("an empty list is not []: %s", raw)
	}

	f.updates.a = updater.Activity{Staged: true, Running: true}
	f.svc.running = true
	f.views = 3
	f.auth.login(t)
	f.auth.login(t)
	var raw struct {
		Waiting json.RawMessage `json:"waiting"`
	}
	_ = json.Unmarshal([]byte(f.do(req{method: http.MethodGet, path: powerPath, session: &s}).Body.String()), &raw)
	b := raw.Waiting
	want := `[{"kind":"update_staged"},{"kind":"update_running"},{"kind":"service_action_running"},{"kind":"other_sessions","count":2},{"kind":"other_views","count":3}]`
	if string(b) != want {
		t.Errorf("waiting %s\nwant    %s", b, want)
	}
	f.broker.available = false
	if m := get(); m["broker_available"] != false {
		t.Error("broker_available")
	}
	if n := f.log.count("power"); n != 0 {
		t.Errorf("the list reached the broker: %s", f.log.list())
	}
	if e := powerEntries(t, f); len(e) != 0 {
		t.Errorf("the list was audited: %v", e)
	}
}

// One other session is counted as one; the caller alone is no item.
func TestPowerOtherSessionsCount(t *testing.T) {
	f, s := newPowerFixture(t, clock.System())
	items := func() string {
		rec := f.do(req{method: http.MethodGet, path: powerPath, session: &s})
		var raw struct {
			Waiting json.RawMessage `json:"waiting"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &raw)
		return string(raw.Waiting)
	}
	if got := items(); got != `[]` {
		t.Errorf("alone: %s", got)
	}
	f.auth.login(t)
	if got := items(); got != `[{"kind":"other_sessions","count":1}]` {
		t.Errorf("one other: %s", got)
	}
}

// Both routes are session activity (E2).
func TestPowerRoutesAreActivity(t *testing.T) {
	c := clocktest.New()
	f, s := newPowerFixture(t, c)
	for i := 0; i < 3; i++ {
		c.Advance(auth.IdleTimeout / 2)
		expect(t, f.do(req{method: http.MethodGet, path: powerPath, session: &s}), http.StatusOK, "")
	}
	c.Advance(auth.IdleTimeout / 2)
	expect(t, f.do(req{method: http.MethodPost, path: powerPath, session: &s, body: powerBody("halt", "x")}), http.StatusBadRequest, "invalid_request")
	c.Advance(auth.IdleTimeout / 2)
	expect(t, f.do(req{method: http.MethodGet, path: powerPath, session: &s}), http.StatusOK, "")
}

// Without WithPower both routes answer 503, after the session check.
func TestPowerRoutesWithoutTheOption(t *testing.T) {
	f := newAccessFixture(t, clock.System())
	assets := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}
	f.handler = NewHandler(assets, WithAuth(f.auth.m), WithAudit(f.audit))
	s := f.setupViaAPI()
	expect(t, f.do(req{method: http.MethodGet, path: powerPath}), http.StatusUnauthorized, "unauthenticated")
	expect(t, f.do(req{method: http.MethodGet, path: powerPath, session: &s}), http.StatusServiceUnavailable, "power_unavailable")
	expect(t, f.do(req{method: http.MethodPost, path: powerPath, session: &s, body: powerBody("reboot", testPassword)}), http.StatusServiceUnavailable, "power_unavailable")
}

// ORB-SEC-007: a recognisable password and the request body appear in no
// answer, no audit line and no broker call, in every failing and succeeding
// path.
func TestPowerRedaction(t *testing.T) {
	const marker = "Sentinel-Power-Pw-4b1"
	run := func(set func(*powerFixture), body string) (*powerFixture, string) {
		p, ps := newPowerFixture(t, clock.System())
		set(p)
		p.do(req{method: http.MethodPost, path: powerPath, session: &ps, body: body})
		p.do(req{method: http.MethodGet, path: powerPath, session: &ps})
		p.audit.Close()
		dump, _ := json.Marshal(p.auditLines(t))
		return p, string(dump) + strings.Join(p.bodies, "\n") + p.log.list()
	}
	none := func(*powerFixture) {}
	cases := map[string]struct {
		set  func(*powerFixture)
		body string
	}{
		"wrong password":    {none, powerBody("reboot", marker)},
		"invalid action":    {none, powerBody("halt-"+marker, marker)},
		"unknown member":    {none, `{"action":"reboot","current_password":"` + marker + `","x":"` + marker + `"}`},
		"too large":         {none, `{"action":"reboot","current_password":"` + marker + strings.Repeat("y", 1100) + `"}`},
		"accepted":          {none, powerBody("reboot", testPassword)},
		"socket missing":    {func(p *powerFixture) { p.broker.available = false }, powerBody("reboot", testPassword)},
		"update running":    {func(p *powerFixture) { p.updates.a.Running = true }, powerBody("poweroff", testPassword)},
		"service running":   {func(p *powerFixture) { p.svc.running = true }, powerBody("poweroff", testPassword)},
		"already requested": {func(p *powerFixture) { p.broker.code = brokerclient.CodeAlreadyRequested }, powerBody("reboot", testPassword)},
		"internal":          {func(p *powerFixture) { p.broker.code = brokerclient.CodeInternal }, powerBody("reboot", testPassword)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, all := run(tc.set, tc.body)
			if strings.Contains(all, marker) || strings.Contains(all, testPassword) || strings.Contains(all, "current_password") {
				t.Fatalf("a password or the body leaked:\n%s", all)
			}
		})
	}
}
