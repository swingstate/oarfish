package updater

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// The service side of a component update (ADR 0010 U15, amendments A15 and
// A16; G6.10 step b): what the service must accept from a helper that replaces
// its own components. The dialog and its texts are step c.

func withPhase(phase string) string {
	return strings.Replace(statusAnswer, `"phase":"idle"`, `"phase":"`+phase+`"`, 1)
}

// A status answer with a component phase is valid: the job and the page see
// the phase instead of an internal error.
func TestHelperStatusAcceptsTheComponentPhases(t *testing.T) {
	for _, phase := range []string{"c_switching", "c_switched", "c_verified", "c_committed", "c_rolling_back"} {
		st, e := helperWith(fixed(withPhase(phase))).Status(context.Background())
		if e != nil || st.Phase != phase {
			t.Errorf("%s: %v %+v", phase, e, st)
		}
	}
	for _, phase := range []string{"c_unknown", "C_SWITCHED", ""} {
		if _, e := helperWith(fixed(withPhase(phase))).Status(context.Background()); e == nil || e.Code != CodeInternal {
			t.Errorf("%q: %v", phase, e)
		}
	}
}

// The names that the helper reports are decoded through the closed list of U2:
// an unknown name, a repeated one and anything that is not a name never reach
// the job or the page.
func TestComponentNamesAreDecodedThroughTheClosedList(t *testing.T) {
	answer := `{"v":1,"ok":true,"version":"` + v9 + `","commit":"0123456789abcdef0123456789abcdef01234567","components_differ":true,` +
		`"differs":["oarfish-update-helper","/etc/passwd","oarfish.service","oarfish-update-helper","<b>x</b>",""]}` + "\n"
	si, e := helperWith(fixed(answer)).Stage(context.Background(), v9, "0123456789abcdef")
	if e != nil || !si.ComponentsDiffer || !slices.Equal(si.Differs, []string{"oarfish-update-helper", "oarfish.service"}) {
		t.Fatalf("%v %+v", e, si)
	}
	// An older helper sends no names.
	old := `{"v":1,"ok":true,"version":"` + v9 + `","commit":"","components_differ":false}` + "\n"
	if si, e := helperWith(fixed(old)).Stage(context.Background(), v9, "0123456789abcdef"); e != nil || si.Differs != nil {
		t.Fatalf("%v %+v", e, si)
	}
	// status: the names of a staged release, the socket flag and components_updated.
	status := strings.Replace(statusAnswer, `"new_member":"ignored"`,
		`"staged_differs":["oarfish-update-recover.service","nonsense"],"socket_restart_pending":true`, 1)
	status = strings.Replace(status, `"acknowledged":false`, `"acknowledged":false,"components_updated":true`, 1)
	st, e := helperWith(fixed(status)).Status(context.Background())
	if e != nil || !slices.Equal(st.StagedDiffers, []string{"oarfish-update-recover.service"}) || !st.SocketRestartPending || !st.LastResult.ComponentsUpdated {
		t.Fatalf("%v %+v %+v", e, st, st.LastResult)
	}
}

// The hand-over (exit code 75): the socket stays, nobody accepts for a moment,
// and the next process answers. A request made meanwhile waits and succeeds.
func TestHelperRequestWaitsForTheNextHelperProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix sockets")
	}
	dir, err := os.MkdirTemp("", "ou")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "h.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skip("cannot listen on a unix socket here")
	}
	defer ln.Close()
	const gap = 300 * time.Millisecond
	go func() {
		time.Sleep(gap) // the old helper is gone, the new one is not there yet
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.Read(make([]byte, 64))
		c.Write([]byte(withPhase("c_switched")))
	}()
	start := time.Now()
	st, e := NewHelper(sock).Status(context.Background())
	if e != nil || st.Phase != "c_switched" {
		t.Fatalf("%v %+v", e, st)
	}
	if time.Since(start) < gap/2 {
		t.Fatal("the request did not wait for the next process")
	}
}

// componentFlow starts an apply whose stage reports a component set, checks
// the pause for the second confirmation and confirms it.
func componentFlow(t *testing.T) *env {
	t.Helper()
	e, _ := ready(t, relOpts{})
	names := []string{"oarfish-update-helper", "oarfish-update-recover.service"}
	e.helper.stage = func(_ context.Context, v, _ string) (StageInfo, *Error) {
		return StageInfo{Version: v, ComponentsDiffer: true, Differs: names}, nil
	}
	slot, _ := e.u.Reserve()
	if err := slot.Start(ApplyRequest{Version: v9, User: "admin", CorrelationID: "0123456789abcdef"}); err != nil {
		t.Fatal(err)
	}
	var job *JobView
	for i := 0; i < 2000; i++ {
		if job = e.u.State(context.Background()).Job; job != nil && job.Step == "awaiting_components_confirmation" {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if job == nil || job.Step != "awaiting_components_confirmation" || !job.Running || !slices.Equal(job.Differs, names) {
		t.Fatalf("the job does not pause with the names: %+v", job)
	}
	if e.helper.count("activate") != 0 {
		t.Fatal("activate ran before the second confirmation")
	}
	if err := e.u.Resume(v9); err != nil {
		t.Fatal(err)
	}
	e.settle()
	if e.jobErr() != nil || len(e.helper.confirmed) != 1 || !e.helper.confirmed[0] {
		t.Fatalf("job error %v, confirmed %v", e.jobErr(), e.helper.confirmed)
	}
	return e
}

// setStatus is what the helper answers from now on.
func (f *fakeHelper) setStatus(st HelperStatus, err *Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.statusErr = st, err
}

func finishing(e *env) []string {
	var out []string
	for _, ev := range e.audits() {
		if ev.Action == "update_finished" {
			out = append(out, string(ev.Result)+" "+ev.CorrelationID+" "+ev.User)
		}
	}
	return out
}

// The whole flow with the fake helper: stage with components, the pause, the
// second confirmation, activate, the restart of the helper, the restart of the
// service by the release, and the finished result.
func TestComponentUpdateFlowWithTheFakeHelper(t *testing.T) {
	e := componentFlow(t)
	base := HelperStatus{ProtocolMin: 1, ProtocolMax: 1, InstalledVersion: v6}
	poll := func(e *env) State {
		t.Helper()
		st := e.u.State(context.Background())
		if st.Job != nil && (st.Job.Error != nil || st.Job.Step == "failed") {
			t.Fatalf("the job ended in a failure during the hand-over: %+v", st.Job)
		}
		return st
	}
	// The helper hands over: the old process still answers, then nobody answers
	// (the client's one retry was used up or the call timed out), then the new
	// helper answers with its phases.
	for _, phase := range []string{"c_switching", "c_switched"} {
		st := base
		st.Phase = phase
		e.helper.setStatus(st, nil)
		if got := poll(e); !got.Helper.Present || got.Helper.Phase != phase || got.Job.Step != "done" {
			t.Fatalf("%s: %+v %+v", phase, got.Helper, got.Job)
		}
	}
	for _, code := range []Code{CodeUnavailable, CodeTimeout} {
		e.helper.setStatus(HelperStatus{}, fail(code))
		if got := poll(e); got.Helper.Present || got.Helper.Error == nil || got.Helper.Error.Code != code || got.Job.Step != "done" {
			t.Fatalf("%s: %+v %+v", code, got.Helper, got.Job)
		}
	}
	for _, phase := range []string{"c_verified", "c_committed", "switching", "verifying"} {
		st := base
		st.Phase = phase
		e.helper.setStatus(st, nil)
		if got := poll(e); got.Helper.Phase != phase || got.LastResult != nil {
			t.Fatalf("%s: %+v", phase, got)
		}
	}
	if len(finishing(e)) != 0 || e.helper.count("ack") != 0 {
		t.Fatalf("a finishing entry before the result: %v", finishing(e))
	}

	// The release restarts the service: a new process reads the result.
	e2 := newEnv(t, v9)
	done := base
	done.Phase, done.InstalledVersion, done.PreviousVersion, done.SocketRestartPending = "idle", v9, v6, true
	done.LastResult = &HelperResult{TransactionID: "00112233445566ff", To: v9, From: v6, Outcome: "success",
		CorrelationID: "0123456789abcdef", FinishedAt: "2026-11-11T10:00:00Z", ComponentsUpdated: true}
	e2.helper.setStatus(done, nil)
	got := e2.u.State(context.Background())
	if got.LastResult == nil || got.LastResult.Outcome != "success" || !got.LastResult.ComponentsUpdated || !got.Helper.SocketRestartPending {
		t.Fatalf("%+v %+v", got.LastResult, got.Helper)
	}
	e2.u.State(context.Background())
	if fin := finishing(e2); !slices.Equal(fin, []string{"success 0123456789abcdef -"}) || e2.helper.count("ack 00112233445566ff") != 1 {
		t.Fatalf("finishing entries %v, acks %v", fin, e2.helper.calls)
	}
	// update_started was written once, by the process that took the confirmation.
	started := 0
	for _, ev := range e.audits() {
		if ev.Action == "update_started" && ev.User == "admin" && ev.CorrelationID == "0123456789abcdef" {
			started++
		}
	}
	if started != 1 {
		t.Fatalf("%d update_started entries", started)
	}
}

// The new helper fails its self-check: the components are rolled back, the
// service was never restarted, and the same process reads the result.
func TestComponentsRolledBackFlowWithTheFakeHelper(t *testing.T) {
	e := componentFlow(t)
	st := HelperStatus{ProtocolMin: 1, ProtocolMax: 1, InstalledVersion: v6, Phase: "c_switched"}
	e.helper.setStatus(st, nil)
	e.u.State(context.Background())
	e.helper.setStatus(HelperStatus{}, fail(CodeUnavailable)) // the hand-over and the hand-back
	e.u.State(context.Background())
	st.Phase, st.CooldownVersion, st.CooldownRemainingS = "idle", v9, 890
	st.LastResult = &HelperResult{TransactionID: "00112233445566ff", To: v9, From: v6, Outcome: "components_rolled_back",
		ReasonCode: "key_changed", CorrelationID: "0123456789abcdef", FinishedAt: "2026-11-11T10:00:00Z"}
	e.helper.setStatus(st, nil)
	got := e.u.State(context.Background())
	if got.Job.Error != nil || got.Job.Step != "done" {
		t.Fatalf("the job: %+v", got.Job)
	}
	if r := got.LastResult; r == nil || r.Outcome != "components_rolled_back" || r.ReasonCode != "key_changed" || r.ComponentsUpdated || got.Helper.CooldownVersion != v9 {
		t.Fatalf("%+v %+v", got.LastResult, got.Helper)
	}
	e.u.State(context.Background())
	if fin := finishing(e); !slices.Equal(fin, []string{"components_rolled_back 0123456789abcdef -"}) || e.helper.count("ack") != 1 {
		t.Fatalf("finishing entries %v", fin)
	}
}

// F31: the components committed and the release was rolled back. The result is
// rolled_back, and components_updated stays with it.
func TestReleaseRolledBackAfterTheComponentsFlowWithTheFakeHelper(t *testing.T) {
	e := componentFlow(t)
	// The failing release restarted the service twice (to the new version and
	// back), so a new process of the old version reads the result.
	e2 := newEnv(t, v6)
	e2.helper.setStatus(HelperStatus{ProtocolMin: 1, ProtocolMax: 1, InstalledVersion: v6, Phase: "idle", CooldownVersion: v9, CooldownRemainingS: 880,
		LastResult: &HelperResult{TransactionID: "00112233445566ff", To: v9, From: v6, Outcome: "rolled_back", ReasonCode: "start_failed",
			CorrelationID: "0123456789abcdef", FinishedAt: "2026-11-11T10:00:00Z", ComponentsUpdated: true}}, nil)
	got := e2.u.State(context.Background())
	if r := got.LastResult; r == nil || r.Outcome != "rolled_back" || r.ReasonCode != "start_failed" || !r.ComponentsUpdated {
		t.Fatalf("%+v", got.LastResult)
	}
	if fin := finishing(e2); !slices.Equal(fin, []string{"rolled_back 0123456789abcdef -"}) {
		t.Fatalf("finishing entries %v", fin)
	}
	if len(finishing(e)) != 0 {
		t.Fatalf("the old process wrote a finishing entry: %v", finishing(e))
	}
}
