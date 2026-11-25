package services

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/swingstate/oarfish/backend/internal/audit"
	"github.com/swingstate/oarfish/backend/internal/brokerclient"
	"github.com/swingstate/oarfish/backend/internal/clock/clocktest"
)

const cid = "0123456789abcdef"

func req(action string) Request {
	return Request{Unit: "demo-app.service", Action: action, User: "admin-test", CorrelationID: cid}
}

func testActions(t *testing.T, b *fakeBroker) (*actions, *auditSink, *atomic.Int32) {
	t.Helper()
	sink := &auditSink{}
	var dropped atomic.Int32
	c := clocktest.New()
	a := newActions(b, sink.record, c.Now, func() { dropped.Add(1) })
	t.Cleanup(a.close)
	return a, sink, &dropped
}

// The job: accepted, the broker is called once with the operation, the unit
// and the server's correlation ID, the result is kept, one audit entry is
// written when the answer is known, and the list cache is dropped.
func TestJobSuccess(t *testing.T) {
	b := &fakeBroker{available: true, answer: brokerclient.Answer{Code: brokerclient.CodeSuccess, State: "active"}}
	a, sink, dropped := testActions(t, b)
	if got := a.submit(req(ActionRestart)); got != SubmitAccepted {
		t.Fatal(got)
	}
	waitFor(t, "the job", func() bool { v := a.lastAction(); return v != nil && v.Step == StepDone })
	v := a.lastAction()
	if v.Unit != "demo-app.service" || v.Action != "restart" || v.Code != "success" || v.State != "active" || v.StartedAt.IsZero() || v.FinishedAt.IsZero() {
		t.Errorf("%+v", v)
	}
	if calls := b.callList(); len(calls) != 1 || calls[0] != "service.restart demo-app.service "+cid {
		t.Errorf("calls %v", calls)
	}
	ev := sink.list()
	if len(ev) != 1 || ev[0] != (audit.Event{User: "admin-test", Action: audit.ServiceRestart, Target: "service/demo-app.service", Result: audit.Success, CorrelationID: cid}) {
		t.Errorf("audit %+v", ev)
	}
	if dropped.Load() != 1 {
		t.Errorf("cache dropped %d times", dropped.Load())
	}
}

// Every code of the broker and the service's unavailable: kept on the last
// action and audited with the mapping of A7.
func TestJobAuditMapping(t *testing.T) {
	for _, c := range append(brokerclient.Codes, brokerclient.CodeUnavailable) {
		ans := brokerclient.Answer{Code: c}
		if c == brokerclient.CodeRateLimited {
			ans.RetryAfterS = 17
		}
		b := &fakeBroker{available: true, answer: ans}
		a, sink, _ := testActions(t, b)
		for _, act := range []string{ActionStart, ActionStop, ActionRestart} {
			if a.submit(req(act)) != SubmitAccepted {
				t.Fatal(c, act)
			}
			waitFor(t, "the job", func() bool { v := a.lastAction(); return v.Step == StepDone && v.Action == act })
		}
		ev := sink.list()
		want := audit.Result(c)
		if c == brokerclient.CodeBadRequest || c == brokerclient.CodeInternal {
			want = audit.Failed
		}
		if len(ev) != 3 || ev[0].Result != want || ev[0].Action != audit.ServiceStart || ev[1].Action != audit.ServiceStop || ev[2].Action != audit.ServiceRestart {
			t.Errorf("%s: %+v", c, ev)
		}
		if v := a.lastAction(); v.Code != string(c) || (c == brokerclient.CodeRateLimited && v.RetryAfterS != 17) {
			t.Errorf("%s: %+v", c, v)
		}
	}
}

// One action at a time: a second request while one runs is busy, audited, and
// does not reach the broker.
func TestJobBusy(t *testing.T) {
	b := &fakeBroker{available: true, answer: brokerclient.Answer{Code: brokerclient.CodeSuccess, State: "inactive"}, release: make(chan struct{}), started: make(chan struct{}, 1)}
	a, sink, _ := testActions(t, b)
	if a.submit(req(ActionStop)) != SubmitAccepted {
		t.Fatal("first")
	}
	<-b.started
	second := req(ActionStart)
	second.Unit, second.CorrelationID = "demo-batch.service", "fedcba9876543210"
	if got := a.submit(second); got != SubmitBusy {
		t.Fatalf("second: %q", got)
	}
	if v := a.lastAction(); v.Unit != "demo-app.service" || v.Step != StepRunning {
		t.Errorf("the busy request replaced the last action: %+v", v)
	}
	close(b.release)
	waitFor(t, "the job", func() bool { return a.lastAction().Step == StepDone })
	ev := sink.list()
	if len(ev) != 2 || ev[0] != (audit.Event{User: "admin-test", Action: audit.ServiceStart, Target: "service/demo-batch.service", Result: audit.Busy, CorrelationID: "fedcba9876543210"}) ||
		ev[1].Result != audit.Success || ev[1].Action != audit.ServiceStop {
		t.Errorf("audit %+v", ev)
	}
	if len(b.callList()) != 1 {
		t.Errorf("calls %v", b.callList())
	}
	// Free again afterwards.
	b.release = nil
	if a.submit(req(ActionStart)) != SubmitAccepted {
		t.Error("not free after the job")
	}
}

// The socket is missing: unavailable, audited, nothing sent, no job.
func TestJobUnavailable(t *testing.T) {
	b := &fakeBroker{available: false}
	a, sink, _ := testActions(t, b)
	if got := a.submit(req(ActionRestart)); got != SubmitUnavailable {
		t.Fatal(got)
	}
	if a.lastAction() != nil || len(b.callList()) != 0 {
		t.Error("a job ran")
	}
	if ev := sink.list(); len(ev) != 1 || ev[0].Result != audit.Unavailable || ev[0].Action != audit.ServiceRestart {
		t.Errorf("audit %+v", ev)
	}
	// Without a broker client at all.
	a2 := newActions(nil, sink.record, time.Now, nil)
	defer a2.close()
	if got := a2.submit(req(ActionStart)); got != SubmitUnavailable {
		t.Error(got)
	}
}

// A request the route should not have passed is refused without an entry.
func TestJobRefusesInvalid(t *testing.T) {
	b := &fakeBroker{available: true}
	a, sink, _ := testActions(t, b)
	for _, r := range []Request{
		{Unit: "demo-app.service", Action: "reload", User: "admin-test", CorrelationID: cid},
		{Unit: "../x.service", Action: "start", User: "admin-test", CorrelationID: cid},
		{Unit: "", Action: "stop", User: "admin-test", CorrelationID: cid},
	} {
		if got := a.submit(r); got != SubmitInvalid {
			t.Errorf("%+v: %q", r, got)
		}
	}
	if len(sink.list()) != 0 || len(b.callList()) != 0 {
		t.Error("audited or sent")
	}
}

// The job does not belong to the request: it runs to its end on its own
// context. close (service stop) ends the wait; the job is audited once, as
// unavailable, before close returns.
func TestJobOwnContextAndClose(t *testing.T) {
	b := &fakeBroker{available: true, answer: brokerclient.Answer{Code: brokerclient.CodeSuccess}, release: make(chan struct{}), started: make(chan struct{}, 1)}
	sink := &auditSink{}
	a := newActions(b, sink.record, time.Now, nil)
	if a.submit(req(ActionRestart)) != SubmitAccepted {
		t.Fatal("submit")
	}
	<-b.started
	a.close()
	ev := sink.list()
	if len(ev) != 1 || ev[0].Result != audit.Unavailable {
		t.Errorf("audit %+v", ev)
	}
	if v := a.lastAction(); v.Step != StepDone || v.Code != "unavailable" {
		t.Errorf("%+v", v)
	}
	if b.ctxErr == nil {
		t.Error("the job's context was not ended by close")
	}
}

// The Manager wires the job to the read path: the cache is dropped when an
// action ends, so the next read runs again.
func TestManagerDropsCacheAfterAction(t *testing.T) {
	f := newFakeSystemctl(t)
	f.set(0, "out", "")
	f.set(1, "out", fixtureUnits)
	f.set(2, "out", fixtureFiles)
	f.set(3, "out", fixtureUnits)
	f.set(4, "out", fixtureFiles)
	b := &fakeBroker{available: true, answer: brokerclient.Answer{Code: brokerclient.CodeSuccess, State: "active"}}
	c := clocktest.New()
	m := New(Config{Broker: b, Clock: c})
	m.reader.systemctl = f.path
	m.reader.systemdDir = t.TempDir()
	defer m.Close()
	if _, e := m.List(t.Context()); e != nil {
		t.Fatal(e)
	}
	m.List(t.Context())
	if f.count() != 2 {
		t.Fatalf("%d calls", f.count())
	}
	if !m.BrokerAvailable() {
		t.Error("broker")
	}
	if m.Submit(req(ActionRestart)) != SubmitAccepted {
		t.Fatal("submit")
	}
	waitFor(t, "the job", func() bool { v := m.Last(); return v != nil && v.Step == StepDone })
	if _, e := m.List(t.Context()); e != nil || f.count() != 4 {
		t.Errorf("after the action: %d calls, %v", f.count(), e)
	}
	if (&Manager{}).BrokerAvailable() {
		t.Error("nil broker counted as available")
	}
}

// ActionRunning is true exactly while a job runs (the waiting list and the
// refusal busy of a reboot or shutdown, ADR 0011 B6, G7.5).
func TestActionRunning(t *testing.T) {
	b := &fakeBroker{available: true, answer: brokerclient.Answer{Code: brokerclient.CodeSuccess, State: "active"}, release: make(chan struct{}), started: make(chan struct{}, 1)}
	m := New(Config{Broker: b})
	defer m.Close()
	if m.ActionRunning() {
		t.Fatal("running before any action")
	}
	if m.Submit(req(ActionRestart)) != SubmitAccepted {
		t.Fatal("submit")
	}
	<-b.started
	if !m.ActionRunning() {
		t.Error("not running while the broker works")
	}
	close(b.release)
	waitFor(t, "the job", func() bool { return !m.ActionRunning() })
	if m.Last().Step != StepDone {
		t.Errorf("%+v", m.Last())
	}
}
