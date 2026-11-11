//go:build faulthooks

package main

import (
	"io"
	"strings"
	"sync"
	"testing"
)

// Run with: go test -tags faulthooks -run TestFaultHooks ./cmd/oarfish-update-helper

// newFaultProcess starts the counters of a new process of the helper.
func newFaultProcess() {
	faultCount.Store(0)
	faultProc.Store(nil)
}

// OARFISH_FAULT_PROC names the process a fault applies to.
func TestFaultHooksProcessSelection(t *testing.T) {
	defer newFaultProcess()
	t.Setenv("OARFISH_FAULT_AT", "1")
	t.Setenv("OARFISH_FAULT_MODE", "error")

	// Without the variable the fault applies to every process, as before.
	newFaultProcess()
	if faultPoint("x") != errFaultInjected {
		t.Fatal("no fault without a process selection")
	}

	t.Setenv("OARFISH_FAULT_PROC", "serve:c_switched")
	// A process that has not looked at its state yet is not selected.
	newFaultProcess()
	if faultPoint("x") != nil {
		t.Fatal("fired before the process was named")
	}
	// Another process counts its calls and is left alone.
	for _, other := range [][2]string{{"serve", "staged"}, {"recover", "c_switched"}, {"serve", "idle"}, {"serve", "locked"}} {
		newFaultProcess()
		faultProcess(other[0], other[1])
		if faultPoint("x") != nil || faultCount.Load() != 1 {
			t.Fatalf("fired in %s:%s", other[0], other[1])
		}
	}
	// The named process is hit at its own call number.
	newFaultProcess()
	faultProcess("serve", "c_switched")
	faultProcess("serve", "idle") // only the first call names the process
	if faultPoint("x") != errFaultInjected {
		t.Fatal("did not fire in the named process")
	}
	if faultPoint("x") != nil {
		t.Fatal("fired twice")
	}
}

// phaseCalls records, for every journal line, how many numbered calls the
// process had made when it wrote the line.
type phaseCalls struct {
	mu    sync.Mutex
	inner io.Writer
	at    map[string]int64
	order []string
}

func (p *phaseCalls) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	line := strings.TrimSpace(string(b))
	if name, _, ok := strings.Cut(strings.TrimPrefix(line, "phase="), " "); ok && strings.HasPrefix(line, "phase=") {
		if _, seen := p.at[name]; !seen {
			p.at[name] = faultCount.Load()
			p.order = append(p.order, name)
		}
	}
	return p.inner.Write(b)
}

func recordPhases(h *handler) *phaseCalls {
	p := &phaseCalls{inner: h.journal, at: map[string]int64{}}
	h.journal = p
	return p
}

func wantCalls(t *testing.T, what string, got *phaseCalls, want map[string]int64) {
	t.Helper()
	for name, n := range want {
		if got.at[name] != n {
			t.Errorf("%s: the journal line of %s was written after call %d, the documented number is %d (docs/DEVELOPMENT.md)", what, name, got.at[name], n)
		}
	}
	if len(got.at) != len(want) {
		t.Errorf("%s: phases %v, documented %d", what, got.order, len(want))
	}
}

// The call numbers of each phase of a component transaction, per process, as
// docs/DEVELOPMENT.md lists them for the device test kit. A journal line is
// written when the state write of its phase is complete, so the action of a
// phase starts with the next call. The numbers hold for a service that is
// healthy at every poll: each `systemctl show` is a numbered call.
func TestFaultHooksCallNumbersOfTheComponentTransaction(t *testing.T) {
	defer newFaultProcess()
	t.Setenv("OARFISH_FAULT_AT", "1000000") // count, never fire
	v, s := newComponentEnv(t)
	v.h.wrapFS = nil // the plain file system: every flush is a numbered call

	// serve:staged, the helper that receives activate after an idle exit.
	newFaultProcess()
	h := s.process(linkActive)
	v.h = h
	p := recordPhases(h)
	h.startupRecovery()
	wantOK(t, v.confirm(newVersion))
	wantCalls(t, "serve:staged", p, map[string]int64{"c_switching": 7, "c_switched": 15})
	if got := faultCount.Load(); got != 16 { // the daemon-reload is the last call before exit 75
		t.Errorf("serve:staged made %d calls, documented 16", got)
	}

	// serve:c_switched, the new helper at its first start.
	newFaultProcess()
	h = s.process(linkActive)
	v.h = h
	p = recordPhases(h)
	h.startupRecovery()
	wantCalls(t, "serve:c_switched", p, map[string]int64{"c_verified": 5, "c_committed": 14, "switching": 20, "verifying": 28, "committed": 45, "idle": 50})
	if st := checkSettled(t, v, false); st.Last.Outcome != outcomeSuccess {
		t.Fatalf("result %+v", st.Last)
	}

	// recover:c_switched, the recover unit that finds the new set active.
	for _, onFailure := range []bool{false, true} {
		v, s := newComponentEnv(t)
		v.h.wrapFS = nil
		wantOK(t, v.confirm(newVersion))
		newFaultProcess()
		h := s.process(linkKnownGood)
		h.onFailure = onFailure
		p := recordPhases(h)
		if code := h.recoverBoot(io.Discard); code != 0 {
			t.Fatalf("exit %d", code)
		}
		done := int64(18) // 5 intent, 3 link, reload, 2 release removal, 2 set removal, 5 state
		if onFailure {
			done = 19 // and reset-failed of the helper service
		}
		wantCalls(t, "recover:c_switched", p, map[string]int64{"c_rolling_back": 5, "components_rolled_back": done})
	}
}
