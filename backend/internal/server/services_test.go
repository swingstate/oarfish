package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/swingstate/oarfish/backend/internal/auth"
	"github.com/swingstate/oarfish/backend/internal/brokerclient"
	"github.com/swingstate/oarfish/backend/internal/clock"
	"github.com/swingstate/oarfish/backend/internal/clock/clocktest"
	"github.com/swingstate/oarfish/backend/internal/services"
)

// fakeServiceControl is the service side behind the routes. It records what
// the routes ask of it.
type fakeServiceControl struct {
	mu        sync.Mutex
	list      services.List
	listErr   *services.Error
	broker    bool
	submitted []services.Request
	submit    string
	last      *services.ActionView
	calls     []string
}

func (f *fakeServiceControl) rec(s string) { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }

func (f *fakeServiceControl) List(context.Context) (services.List, *services.Error) {
	f.rec("list")
	return f.list, f.listErr
}
func (f *fakeServiceControl) BrokerAvailable() bool { f.rec("stat"); return f.broker }
func (f *fakeServiceControl) Submit(r services.Request) string {
	f.rec("submit")
	f.mu.Lock()
	f.submitted = append(f.submitted, r)
	f.mu.Unlock()
	return f.submit
}
func (f *fakeServiceControl) Last() *services.ActionView { f.rec("last"); return f.last }

func (f *fakeServiceControl) callList() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, ",")
}

func servicesFixture(t *testing.T, c clock.Clock, svc ServiceControl) (*accessFixture, session) {
	t.Helper()
	f := newAccessFixture(t, c)
	assets := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}
	f.handler = NewHandler(assets, WithAuth(f.auth.m), WithAudit(f.audit), WithServices(svc))
	return f, f.setupViaAPI()
}

var readAt = time.Date(2026, 11, 25, 9, 30, 0, 0, time.UTC)

func sampleList() services.List {
	return services.List{ReadAt: readAt, Counts: services.Counts{Total: 2, Active: 1, Inactive: 1}, Rows: []services.Row{
		{Name: "demo-app.service", Description: "Demo <b>app</b>", LoadState: "loaded", ActiveState: "active", SubState: "running", StartMode: "enabled", Actions: []string{"stop", "restart"}},
		{Name: "ssh.service", Description: "OpenBSD Secure Shell server", LoadState: "loaded", ActiveState: "inactive", SubState: "dead", StartMode: "enabled", RestartOnly: true, Critical: "ssh", Actions: []string{"restart"}},
	}}
}

func decode(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("not JSON: %q", body)
	}
	return m
}

const actionPath = "/api/v1/services/action"

// The list: schema_version 1, read_at, broker_available from the stat, the
// counts and the rows with every field, sorted as the service sorted them.
func TestServicesList(t *testing.T) {
	fake := &fakeServiceControl{list: sampleList(), broker: true}
	f, s := servicesFixture(t, clock.System(), fake)
	rec := f.do(req{method: http.MethodGet, path: "/api/v1/services", session: &s})
	expect(t, rec, http.StatusOK, "")
	want := `{"schema_version":1,"read_at":"2026-11-25T09:30:00Z","broker_available":true,"counts":{"total":2,"active":1,"failed":0,"inactive":1},"services":[` +
		`{"name":"demo-app.service","description":"Demo \u003cb\u003eapp\u003c/b\u003e","load_state":"loaded","active_state":"active","sub_state":"running","start_mode":"enabled","denied":false,"restart_only":false,"critical":"","actions":["stop","restart"]},` +
		`{"name":"ssh.service","description":"OpenBSD Secure Shell server","load_state":"loaded","active_state":"inactive","sub_state":"dead","start_mode":"enabled","denied":false,"restart_only":true,"critical":"ssh","actions":["restart"]}]}` + "\n"
	if rec.Body.String() != want {
		t.Fatalf("body\n%s\nwant\n%s", rec.Body.String(), want)
	}
	fake.broker = false
	if m := decode(t, f.do(req{method: http.MethodGet, path: "/api/v1/services", session: &s}).Body.String()); m["broker_available"] != false {
		t.Error("broker_available")
	}
	// An empty list is an empty array, not null.
	fake.list = services.List{ReadAt: readAt}
	if m := decode(t, f.do(req{method: http.MethodGet, path: "/api/v1/services", session: &s}).Body.String()); m["services"] == nil {
		t.Error("services is null")
	}
}

// No systemd: services_unavailable; any other failure: services_read_failed;
// both 503 with fixed English text and nothing of the cause.
func TestServicesListErrors(t *testing.T) {
	fake := &fakeServiceControl{listErr: &services.Error{Code: services.CodeUnavailable}}
	f, s := servicesFixture(t, clock.System(), fake)
	rec := f.do(req{method: http.MethodGet, path: "/api/v1/services", session: &s})
	expect(t, rec, http.StatusServiceUnavailable, "services_unavailable")
	fake.listErr = &services.Error{Code: services.CodeReadFailed}
	expect(t, f.do(req{method: http.MethodGet, path: "/api/v1/services", session: &s}), http.StatusServiceUnavailable, "services_read_failed")
	if strings.Contains(rec.Body.String(), "systemctl") || strings.Contains(rec.Body.String(), "/usr") {
		t.Errorf("a cause leaked: %s", rec.Body.String())
	}
}

// D-C46, B4, BF22: every route needs a session; the POST also needs the CSRF
// layers, JSON and a body of at most 1 KiB. A refused request reaches nothing
// and is audited as today.
func TestServicesRoutesNeedASessionAndTheCSRFLayers(t *testing.T) {
	fake := &fakeServiceControl{list: sampleList(), broker: true, submit: services.SubmitAccepted}
	f, s := servicesFixture(t, clock.System(), fake)
	for _, p := range []string{"/api/v1/services", actionPath} {
		expect(t, f.do(req{method: http.MethodGet, path: p}), http.StatusUnauthorized, "unauthenticated")
		expect(t, f.do(req{method: http.MethodGet, path: p, session: &session{token: "forged-" + s.token}}), http.StatusUnauthorized, "unauthenticated")
		expect(t, f.do(req{method: http.MethodGet, path: p, session: &s, body: "{}"}), http.StatusBadRequest, "request_body_not_allowed")
		for _, m := range []string{http.MethodPut, http.MethodDelete} {
			expect(t, f.do(req{method: m, path: p, session: &s, body: "{}"}), http.StatusMethodNotAllowed, "method_not_allowed")
		}
	}
	expect(t, f.do(req{method: http.MethodPost, path: "/api/v1/services", session: &s, body: "{}"}), http.StatusMethodNotAllowed, "method_not_allowed")
	b := `{"unit":"demo-app.service","action":"restart"}`
	expect(t, f.do(req{method: http.MethodPost, path: actionPath, body: b}), http.StatusUnauthorized, "unauthenticated")
	expect(t, f.do(req{method: http.MethodPost, path: actionPath, session: &s, noCSRF: true, body: b}), http.StatusForbidden, "csrf_failed")
	expect(t, f.do(req{method: http.MethodPost, path: actionPath, session: &s, csrf: "wrong", body: b}), http.StatusForbidden, "csrf_failed")
	expect(t, f.do(req{method: http.MethodPost, path: actionPath, session: &s, origin: "http://evil.example", body: b}), http.StatusForbidden, "origin_rejected")
	expect(t, f.do(req{method: http.MethodPost, path: actionPath, session: &s, origin: "-", secFetchSite: "-", body: b}), http.StatusForbidden, "origin_rejected")
	expect(t, f.do(req{method: http.MethodPost, path: actionPath, session: &s, secFetchSite: "cross-site", body: b}), http.StatusForbidden, "origin_rejected")
	expect(t, f.do(req{method: http.MethodPost, path: actionPath, session: &s, contentType: "text/plain", body: b}), http.StatusUnsupportedMediaType, "unsupported_media_type")
	expect(t, f.do(req{method: http.MethodPost, path: actionPath, session: &s, contentType: "application/x-www-form-urlencoded", body: "unit=a.service&action=stop"}), http.StatusUnsupportedMediaType, "")
	expect(t, f.do(req{method: http.MethodPost, path: actionPath, session: &s, host: "evil.example", body: b}), http.StatusForbidden, "host_rejected")
	if strings.Contains(fake.callList(), "submit") {
		t.Fatalf("a refused request reached the job: %s", fake.callList())
	}
	f.audit.Flush()
	f.audit.Close()
	var csrf, origin int
	for _, l := range f.auditLines(t) {
		if l["action"] == "request_rejected" && l["target"] == "-" {
			switch l["result"] {
			case "csrf_failed":
				csrf += int(l["count"].(float64))
			case "origin_rejected":
				origin += int(l["count"].(float64))
			}
		}
	}
	if csrf != 2 || origin != 3 {
		t.Errorf("audited csrf_failed %d, origin_rejected %d", csrf, origin)
	}
}

// The body: at most 1 KiB, JSON, exactly unit and action; the unit by rule 1
// of B3 and the action from the closed set, both 400 invalid_request and not
// audited as a service action.
func TestServicesActionBody(t *testing.T) {
	fake := &fakeServiceControl{broker: true, submit: services.SubmitAccepted}
	f, s := servicesFixture(t, clock.System(), fake)
	expect(t, f.do(req{method: http.MethodPost, path: actionPath, session: &s, body: `{"unit":"` + strings.Repeat("a", 1100) + `.service","action":"stop"}`}), http.StatusRequestEntityTooLarge, "request_too_large")
	for _, b := range []string{
		``, `{}`, `[]`, `null`, `{"unit":"demo-app.service"}`, `{"action":"stop"}`,
		`{"unit":"demo-app.service","action":"stop","password":"x"}`,
		`{"unit":"demo-app.service","action":"stop","path":"/etc"}`,
		`{"unit":"demo-app.service","action":"stop"} {}`,
		`{"unit":"demo-app.service","action":"reload"}`, `{"unit":"demo-app.service","action":"Stop"}`,
		`{"unit":"demo-app.service","action":"enable"}`, `{"unit":"demo-app.service","action":1}`,
		`{"unit":"-demo.service","action":"stop"}`, `{"unit":"demo.socket","action":"stop"}`,
		`{"unit":"../demo.service","action":"stop"}`, `{"unit":"a/b.service","action":"stop"}`,
		`{"unit":"demo app.service","action":"stop"}`, `{"unit":"démo.service","action":"stop"}`,
		`{"unit":"` + strings.Repeat("a", 248) + `.service","action":"stop"}`,
		`{"unit":null,"action":"stop"}`,
	} {
		rec := f.do(req{method: http.MethodPost, path: actionPath, session: &s, body: b})
		expect(t, rec, http.StatusBadRequest, "invalid_request")
		if strings.Contains(rec.Body.String(), "demo") || strings.Contains(rec.Body.String(), "/etc") {
			t.Errorf("%q echoed: %s", b, rec.Body.String())
		}
	}
	if strings.Contains(fake.callList(), "submit") {
		t.Fatal("an invalid request reached the job")
	}
	f.audit.Close()
	for _, l := range f.auditLines(t) {
		if strings.HasPrefix(l["action"].(string), "service_") {
			t.Errorf("audited: %v", l)
		}
	}
	// 255 bytes is the longest name.
	expect(t, f.do(req{method: http.MethodPost, path: actionPath, session: &s, body: `{"unit":"` + strings.Repeat("a", 247) + `.service","action":"stop"}`}), http.StatusAccepted, "")
}

// The answers of the job's own checks, and what the job is given: the unit,
// the action, the session's account name and the request's correlation ID.
func TestServicesActionSubmit(t *testing.T) {
	fake := &fakeServiceControl{broker: true, submit: services.SubmitAccepted}
	f, s := servicesFixture(t, clock.System(), fake)
	rec := f.do(req{method: http.MethodPost, path: actionPath, session: &s, body: `{"action":"restart","unit":"getty@tty1.service"}`})
	expect(t, rec, http.StatusAccepted, "")
	if rec.Body.String() != `{"accepted":true,"schema_version":1}`+"\n" {
		t.Errorf("body %s", rec.Body.String())
	}
	got := fake.submitted[0]
	if got.Unit != "getty@tty1.service" || got.Action != "restart" || got.User != testUser || got.CorrelationID != rec.Header().Get("X-Correlation-ID") || len(got.CorrelationID) != 16 {
		t.Errorf("%+v", got)
	}
	// A name with a \xHH escape reaches the job byte for byte (ADR 0011 A8).
	expect(t, f.do(req{method: http.MethodPost, path: actionPath, session: &s, body: `{"unit":"demo-disk@dev-disk-by\\x2dlabel-data.service","action":"restart"}`}), http.StatusAccepted, "")
	if got := fake.submitted[1].Unit; got != `demo-disk@dev-disk-by\x2dlabel-data.service` {
		t.Errorf("unit %q", got)
	}
	fake.submit = services.SubmitBusy
	expect(t, f.do(req{method: http.MethodPost, path: actionPath, session: &s, body: `{"unit":"demo-app.service","action":"stop"}`}), http.StatusConflict, "busy")
	fake.submit = services.SubmitUnavailable
	expect(t, f.do(req{method: http.MethodPost, path: actionPath, session: &s, body: `{"unit":"demo-app.service","action":"start"}`}), http.StatusServiceUnavailable, "unavailable")
	fake.submit = services.SubmitInvalid
	expect(t, f.do(req{method: http.MethodPost, path: actionPath, session: &s, body: `{"unit":"demo-app.service","action":"start"}`}), http.StatusBadRequest, "invalid_request")
}

// GET /api/v1/services/action: null before the first action, then the last
// one; running has no result fields, done has them.
func TestServicesLastAction(t *testing.T) {
	fake := &fakeServiceControl{}
	f, s := servicesFixture(t, clock.System(), fake)
	if b := f.do(req{method: http.MethodGet, path: actionPath, session: &s}).Body.String(); b != `{"schema_version":1,"action":null}`+"\n" {
		t.Errorf("%s", b)
	}
	fake.last = &services.ActionView{Unit: "demo-app.service", Action: "restart", Step: services.StepRunning, StartedAt: readAt}
	if b := f.do(req{method: http.MethodGet, path: actionPath, session: &s}).Body.String(); b != `{"schema_version":1,"action":{"unit":"demo-app.service","action":"restart","step":"running","started_at":"2026-11-25T09:30:00Z"}}`+"\n" {
		t.Errorf("%s", b)
	}
	fake.last = &services.ActionView{Unit: "demo-app.service", Action: "restart", Step: services.StepDone, StartedAt: readAt, FinishedAt: readAt.Add(3 * time.Second), Code: "rate_limited", RetryAfterS: 12}
	if b := f.do(req{method: http.MethodGet, path: actionPath, session: &s}).Body.String(); b != `{"schema_version":1,"action":{"unit":"demo-app.service","action":"restart","step":"done","started_at":"2026-11-25T09:30:00Z","finished_at":"2026-11-25T09:30:03Z","code":"rate_limited","retry_after_s":12}}`+"\n" {
		t.Errorf("%s", b)
	}
	if strings.Contains(fake.callList(), "list") {
		t.Error("the last action read the list")
	}
}

// The three routes count as use of the session (activity) and extend it.
func TestServicesRoutesAreActivity(t *testing.T) {
	for _, p := range []string{"/api/v1/services", actionPath} {
		c := clocktest.New()
		f, s := servicesFixture(t, c, &fakeServiceControl{list: sampleList()})
		for i := 0; i < 3; i++ {
			c.Advance(auth.IdleTimeout / 2)
			expect(t, f.do(req{method: http.MethodGet, path: p, session: &s}), http.StatusOK, "")
		}
	}
}

func TestServicesRoutesWithoutTheOption(t *testing.T) {
	f := newAccessFixture(t, clock.System())
	s := f.setupViaAPI()
	for _, c := range []struct{ m, p, b string }{
		{http.MethodGet, "/api/v1/services", ""}, {http.MethodGet, actionPath, ""},
		{http.MethodPost, actionPath, `{"unit":"demo-app.service","action":"stop"}`},
	} {
		expect(t, f.do(req{method: c.m, path: c.p, session: &s, body: c.b}), http.StatusServiceUnavailable, "services_unavailable")
	}
}

// fakeBrokerAPI is the broker client behind a real services.Manager.
type fakeBrokerAPI struct {
	mu      sync.Mutex
	release chan struct{}
	started chan struct{}
	calls   []string
}

func (b *fakeBrokerAPI) Available() bool { return true }
func (b *fakeBrokerAPI) Do(ctx context.Context, op brokerclient.Op, unit, cid string) brokerclient.Answer {
	b.mu.Lock()
	b.calls = append(b.calls, string(op)+" "+unit+" "+cid)
	b.mu.Unlock()
	b.started <- struct{}{}
	select {
	case <-b.release:
		return brokerclient.Answer{Code: brokerclient.CodeSuccess, State: "active"}
	case <-ctx.Done():
		return brokerclient.Answer{Code: brokerclient.CodeUnavailable}
	}
}

// The action is a job (B4): the request answers 202 at once and the job runs
// on its own context, so a request context that ends (the page closed) does
// not cancel it; a second action meanwhile is busy; each request is audited
// exactly once, with the account name and the request's correlation ID.
func TestServicesActionIsAJobAuditedOnce(t *testing.T) {
	b := &fakeBrokerAPI{release: make(chan struct{}), started: make(chan struct{}, 1)}
	f := newAccessFixture(t, clock.System())
	mgr := services.New(services.Config{Broker: b, Audit: f.audit.Record})
	defer mgr.Close()
	assets := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}
	f.handler = NewHandler(assets, WithAuth(f.auth.m), WithAudit(f.audit), WithServices(mgr))
	s := f.setupViaAPI()

	ctx, cancel := context.WithCancel(context.Background())
	hr := newRequest(http.MethodPost, actionPath, strings.NewReader(`{"unit":"demo-app.service","action":"restart"}`)).WithContext(ctx)
	hr.Header.Set("Origin", "http://"+hr.Host)
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set(csrfHeader, s.csrf)
	hr.AddCookie(&http.Cookie{Name: cookieName, Value: s.token})
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, hr)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	cid := rec.Header().Get("X-Correlation-ID")
	<-b.started
	cancel() // the page is closed

	busy := f.do(req{method: http.MethodPost, path: actionPath, session: &s, body: `{"unit":"demo-batch.service","action":"start"}`})
	expect(t, busy, http.StatusConflict, "busy")
	running := decode(t, f.do(req{method: http.MethodGet, path: actionPath, session: &s}).Body.String())
	if running["action"].(map[string]any)["step"] != "running" {
		t.Errorf("%v", running)
	}
	close(b.release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		m := decode(t, f.do(req{method: http.MethodGet, path: actionPath, session: &s}).Body.String())
		if a := m["action"].(map[string]any); a["step"] == "done" {
			if a["code"] != "success" || a["state"] != "active" || a["unit"] != "demo-app.service" {
				t.Errorf("%v", a)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the job did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.audit.Close()
	var service []map[string]any
	for _, l := range f.auditLines(t) {
		if strings.HasPrefix(l["action"].(string), "service_") {
			service = append(service, l)
		}
	}
	if len(service) != 2 {
		t.Fatalf("service entries %v", service)
	}
	if e := service[0]; e["action"] != "service_start" || e["target"] != "service/demo-batch.service" || e["result"] != "busy" || e["user"] != testUser {
		t.Errorf("busy entry %v", e)
	}
	if e := service[1]; e["action"] != "service_restart" || e["target"] != "service/demo-app.service" || e["result"] != "success" ||
		e["user"] != testUser || e["correlation_id"] != cid || e["plugin"] != "" || e["count"] != float64(1) {
		t.Errorf("job entry %v", e)
	}
	if len(b.calls) != 1 || b.calls[0] != "service.restart demo-app.service "+cid {
		t.Errorf("broker calls %v", b.calls)
	}
}

// The 30 s write timeout of the server stays as it is: the action is a job,
// not a long request (B4, the G7.4 constraint).
func TestWriteTimeoutUntouched(t *testing.T) {
	if DefaultTimeouts.Write != 30*time.Second {
		t.Fatalf("write timeout %v", DefaultTimeouts.Write)
	}
}

// ORB-SEC-007: no request body, path or cause in an answer or the audit file.
func TestServicesRedaction(t *testing.T) {
	fake := &fakeServiceControl{broker: true, submit: services.SubmitUnavailable, listErr: &services.Error{Code: services.CodeReadFailed}}
	f, s := servicesFixture(t, clock.System(), fake)
	f.do(req{method: http.MethodPost, path: actionPath, session: &s, body: `{"unit":"demo-app.service","action":"stop","secret":"Sentinel-Body-77"}`})
	f.do(req{method: http.MethodPost, path: actionPath, session: &s, body: `{"unit":"Sentinel-Body-77.socket","action":"stop"}`})
	f.do(req{method: http.MethodGet, path: "/api/v1/services", session: &s})
	f.audit.Close()
	dump, _ := json.Marshal(f.auditLines(t))
	all := string(dump) + strings.Join(f.bodies, "\n")
	if strings.Contains(all, "Sentinel-Body-77") || strings.Contains(all, "/run/") || strings.Contains(all, "systemctl") {
		t.Fatal("a body, path or cause leaked")
	}
}
