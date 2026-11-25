package services

import (
	"context"
	"sync"
	"time"

	"github.com/swingstate/oarfish/backend/internal/audit"
	"github.com/swingstate/oarfish/backend/internal/brokerclient"
	"github.com/swingstate/oarfish/backend/internal/unitname"
)

// The action job (ADR 0011 B4, B5; step G7.4). One service action at a time,
// device-wide. The job does not belong to the request that started it: it has
// its own context, so closing the page cannot cancel or un-audit it, and the
// server's 30 s write timeout stays as it is. Exactly one audit entry is
// written per request that passed the route's checks: here, when the answer is
// known, including the service's own busy and unavailable.

// Request is one action as the route has checked it.
type Request struct {
	Unit          string
	Action        string // start, stop or restart
	User          string // the account name of the session at request time
	CorrelationID string // the server's own value of the request
}

// The steps of the last action.
const (
	StepRunning = "running"
	StepDone    = "done"
)

// The service's own refusals of Submit, besides acceptance.
const (
	SubmitAccepted    = ""
	SubmitUnavailable = string(brokerclient.CodeUnavailable)
	SubmitBusy        = string(brokerclient.CodeBusy)
	SubmitInvalid     = "invalid_request"
)

// ActionView is the last action, kept in memory only: a restart of the
// service clears it. Code is a broker code or unavailable, set when Step is
// done; State and RetryAfterS as the broker sent them.
type ActionView struct {
	Unit        string
	Action      string
	Step        string
	StartedAt   time.Time
	FinishedAt  time.Time
	Code        string
	State       string
	RetryAfterS int
}

type actions struct {
	broker brokerclient.API
	audit  func(audit.Event)
	now    func() time.Time
	done   func() // drops the list cache

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.Mutex
	running bool
	last    *ActionView
}

var (
	ops       = map[string]brokerclient.Op{ActionStart: brokerclient.OpStart, ActionStop: brokerclient.OpStop, ActionRestart: brokerclient.OpRestart}
	auditActs = map[string]audit.Action{ActionStart: audit.ServiceStart, ActionStop: audit.ServiceStop, ActionRestart: audit.ServiceRestart}
)

func newActions(b brokerclient.API, rec func(audit.Event), now func() time.Time, done func()) *actions {
	ctx, cancel := context.WithCancel(context.Background())
	if rec == nil {
		rec = func(audit.Event) {}
	}
	return &actions{broker: b, audit: rec, now: now, done: done, ctx: ctx, cancel: cancel}
}

func (a *actions) record(req Request, code string) {
	a.audit(audit.Event{User: req.User, Action: auditActs[req.Action], Target: audit.ServiceTarget(req.Unit),
		Result: audit.ServiceResult(code), CorrelationID: req.CorrelationID})
}

// submit checks, in this order, that the broker's socket exists (a stat, never
// a connection) and that no action runs, and starts the job. A refusal is
// audited here; an accepted request is audited by the job when the broker has
// answered. A request that the route should not have passed (an action
// outside the set, a unit that fails rule 1) is refused without an entry.
func (a *actions) submit(req Request) string {
	if !ValidAction(req.Action) || !unitname.Valid(req.Unit) {
		return SubmitInvalid
	}
	if a.broker == nil || !a.broker.Available() {
		a.record(req, SubmitUnavailable)
		return SubmitUnavailable
	}
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		a.record(req, SubmitBusy)
		return SubmitBusy
	}
	a.running = true
	a.last = &ActionView{Unit: req.Unit, Action: req.Action, Step: StepRunning, StartedAt: a.now().UTC()}
	view := a.last
	a.mu.Unlock()

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ans := a.broker.Do(a.ctx, ops[req.Action], req.Unit, req.CorrelationID)
		a.record(req, string(ans.Code))
		a.mu.Lock()
		view.Step, view.FinishedAt = StepDone, a.now().UTC()
		view.Code, view.State, view.RetryAfterS = string(ans.Code), ans.State, ans.RetryAfterS
		a.running = false
		a.mu.Unlock()
		if a.done != nil {
			a.done()
		}
	}()
	return SubmitAccepted
}

// isRunning reports whether an action job runs now.
func (a *actions) isRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running
}

// lastAction returns a copy of the last action, or nil.
func (a *actions) lastAction() *ActionView {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.last == nil {
		return nil
	}
	v := *a.last
	return &v
}

// close ends a running job: the broker client returns unavailable at once
// (the operation itself may continue inside systemd), the job writes its audit
// entry, and close returns when it has. The audit log is closed after this.
func (a *actions) close() {
	a.cancel()
	a.wg.Wait()
}
