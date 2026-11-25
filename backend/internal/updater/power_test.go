package updater

import (
	"context"
	"testing"
)

// Activity for the waiting list and the refusal update_running of a reboot or
// shutdown (ADR 0011 B6, G7.5): from the in-memory job and the helper's
// status, without an audit entry, an acknowledgement or any other helper call.
func TestActivity(t *testing.T) {
	e := newEnv(t, "0.1.0-alpha.6")
	set := func(j *jobState, phase string, err *Error) {
		e.u.mu.Lock()
		e.u.job = j
		e.u.mu.Unlock()
		e.helper.mu.Lock()
		e.helper.status.Phase, e.helper.statusErr = phase, err
		e.helper.mu.Unlock()
	}
	apply := func(s step, done bool) *jobState { return &jobState{kind: jobApply, step: s, version: "0.1.0-alpha.7", done: done} }
	for _, tc := range []struct {
		name  string
		job   *jobState
		phase string
		err   *Error
		want  Activity
	}{
		{"nothing", nil, "idle", nil, Activity{}},
		{"a check job is no update", &jobState{kind: jobCheck, step: stepChecking}, "idle", nil, Activity{}},
		{"awaiting the second confirmation", apply(stepAwaiting, false), "staged", nil, Activity{Staged: true}},
		{"downloading", apply(stepDownloading, false), "idle", nil, Activity{Running: true}},
		{"staging", apply(stepStaging, false), "staging", nil, Activity{Running: true}},
		{"activating", apply(stepActivating, false), "switching", nil, Activity{Running: true}},
		{"a finished job", apply(stepDone, true), "committed", nil, Activity{}},
		{"a failed job", apply(stepFailed, true), "rolled_back", nil, Activity{}},
		{"staged by the helper without a job", nil, "staged", nil, Activity{Staged: true}},
		{"verifying without a job (after a restart)", nil, "verifying", nil, Activity{Running: true}},
		{"rolling back", nil, "rolling_back", nil, Activity{Running: true}},
		{"rollback_failed is finished", nil, "rollback_failed", nil, Activity{}},
		{"c_switched (the hand-over gap)", nil, "c_switched", nil, Activity{Running: true}},
		{"c_committed is still a transaction", nil, "c_committed", nil, Activity{Running: true}},
		{"helper unreachable, no job", nil, "idle", fail(CodeUnavailable), Activity{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set(tc.job, tc.phase, tc.err)
			if got := e.u.Activity(context.Background()); got != tc.want {
				t.Errorf("%+v, want %+v", got, tc.want)
			}
		})
	}

	// The hand-over of a component update: the helper cannot be reached right
	// after activate, within the bound of F4.
	j := apply(stepActivating, false)
	j.activated = e.clk.Now()
	j.differs = []string{"oarfish-update-helper"}
	set(j, "idle", fail(CodeUnavailable))
	if got := e.u.Activity(context.Background()); !got.Running {
		t.Errorf("hand-over: %+v", got)
	}

	// No audit entry, no acknowledgement: only status calls reached the helper.
	e.helper.mu.Lock()
	calls := append([]string(nil), e.helper.calls...)
	e.helper.mu.Unlock()
	for _, c := range calls {
		if c != "status" {
			t.Errorf("helper call %q", c)
		}
	}
	if n := len(e.audits()); n != 0 {
		t.Errorf("%d audit entries", n)
	}
}

// Without a helper the job alone decides.
func TestActivityWithoutHelper(t *testing.T) {
	u := newUpdater(Config{Version: "0.1.0-alpha.6"}, nil)
	defer u.Close()
	if got := u.Activity(context.Background()); got != (Activity{}) {
		t.Errorf("%+v", got)
	}
	u.mu.Lock()
	u.job = &jobState{kind: jobApply, step: stepAwaiting}
	u.mu.Unlock()
	if got := u.Activity(context.Background()); got != (Activity{Staged: true}) {
		t.Errorf("%+v", got)
	}
}
