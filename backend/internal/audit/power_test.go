package audit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Every code a power request can end with maps as A7 says: accepted is
// success; the service's own busy, unavailable and update_running and the
// broker codes update_running, already_requested, busy, job_failed and
// timeout map to themselves; bad_request, internal, the service-only broker
// codes and anything unknown become error.
func TestPowerResultMapping(t *testing.T) {
	if got := PowerResult("accepted"); got != Success {
		t.Errorf("accepted -> %s", got)
	}
	for _, c := range []string{"busy", "unavailable", "update_running", "already_requested", "job_failed", "timeout"} {
		if got := PowerResult(c); got != Result(c) || !results[got] {
			t.Errorf("%s -> %s", c, got)
		}
	}
	for _, c := range []string{"bad_request", "internal", "success", "unit_denied", "rate_limited", "wrong_password",
		"state_unexpected", "already_in_state", "", "x", "ACCEPTED"} {
		if got := PowerResult(c); got != Failed {
			t.Errorf("%s -> %s, want error", c, got)
		}
	}
}

// The target system is exact: no prefix form, no other letter case.
func TestPowerTargetVectors(t *testing.T) {
	ok := Event{User: "admin-test", Action: SystemReboot, Target: TargetSystem, Result: Success, CorrelationID: cid}
	if ok.validate() != nil {
		t.Fatal("valid power event refused")
	}
	for _, a := range []Action{SystemReboot, SystemPoweroff} {
		for _, r := range []Result{Success, WrongPassword, RateLimited, UpdateRunning, AlreadyRequested, Busy, Unavailable, Failed, JobFailed, Timeout} {
			e := ok
			e.Action, e.Result = a, r
			if e.validate() != nil {
				t.Errorf("%s %s refused", a, r)
			}
		}
	}
	for _, tg := range []Target{"System", "system/", "system/reboot", "systems", " system", "service/system", ""} {
		e := ok
		e.Target = tg
		if e.validate() == nil {
			t.Errorf("accepted target %q", tg)
		}
	}
	for _, a := range []Action{"system_halt", "system_kexec", "System_reboot", "reboot"} {
		e := ok
		e.Action = a
		if e.validate() == nil {
			t.Errorf("accepted action %q", a)
		}
	}
	for _, r := range []Result{"accepted", "Update_running"} {
		e := ok
		e.Result = r
		if e.validate() == nil {
			t.Errorf("accepted result %q", r)
		}
	}
}

// recordingFile stands in for audit.jsonl and records the order of the calls
// on it; syncErr makes the sync fail.
type recordingFile struct {
	steps   *[]string
	mu      *sync.Mutex
	syncErr error
	data    *strings.Builder
}

func (r recordingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.steps = append(*r.steps, "write")
	r.data.Write(p)
	return len(p), nil
}

func (r recordingFile) Sync() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.steps = append(*r.steps, "sync")
	return r.syncErr
}

func (r recordingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.steps = append(*r.steps, "close")
	return nil
}

type recorder struct {
	mu      sync.Mutex
	steps   []string
	data    strings.Builder
	syncErr error
}

func (rc *recorder) install(l *Log) {
	l.openFile = func(path string) (auditFile, error) {
		rc.mu.Lock()
		rc.steps = append(rc.steps, "open:"+filepath.Base(path))
		rc.mu.Unlock()
		return recordingFile{steps: &rc.steps, mu: &rc.mu, syncErr: rc.syncErr, data: &rc.data}, nil
	}
}

func (rc *recorder) got() []string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return append([]string(nil), rc.steps...)
}

var power = Event{User: "admin-test", Action: SystemReboot, Target: TargetSystem, Result: Success, CorrelationID: cid}

// RecordDurable takes the entry into the buffer, writes the buffer (the flush)
// and then fsyncs the file, in this order, before it returns. Entries that
// waited in the buffer are written in the same flush, before the power entry.
func TestRecordDurableWritesThenSyncs(t *testing.T) {
	f := newFixture(t)
	rc := &recorder{}
	rc.install(f.log)
	f.log.Record(Event{User: "admin-test", Action: Logout, Target: TargetSession, Result: Success, CorrelationID: cid})
	if len(rc.got()) != 0 {
		t.Fatalf("Record wrote at once: %v", rc.got())
	}
	if !f.log.RecordDurable(power) {
		t.Fatal("RecordDurable failed")
	}
	want := []string{"open:" + FileName, "write", "write", "sync", "close"}
	if got := rc.got(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("steps %v, want %v", got, want)
	}
	lines := strings.Split(strings.TrimSpace(rc.data.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"logout"`) || !strings.Contains(lines[1], `"system_reboot"`) ||
		!strings.Contains(lines[1], `"target":"system"`) || !strings.Contains(lines[1], `"result":"success"`) {
		t.Fatalf("written %q", rc.data.String())
	}
	// Nothing is left for the timer, and the timer is stopped.
	f.log.mu.Lock()
	left, timer := f.log.buf.Len(), f.log.flushTimer
	f.log.mu.Unlock()
	if left != 0 || timer != nil {
		t.Errorf("buffer %d, timer %v", left, timer)
	}
	if s := f.log.Stats(); s.Failures != 0 || s.Dropped != 0 {
		t.Errorf("stats %+v", s)
	}
}

// When a concurrent flush took the entry first, the buffer is empty: the file
// is still opened and fsynced, so the earlier write is durable.
func TestRecordDurableSyncsAnEmptyBuffer(t *testing.T) {
	f := newFixture(t)
	rc := &recorder{}
	rc.install(f.log)
	if !f.log.flush(true) {
		t.Fatal("flush")
	}
	if got := strings.Join(rc.got(), ","); got != "open:"+FileName+",sync,close" {
		t.Fatalf("steps %s", got)
	}
	// A plain Flush of an empty buffer still does nothing at all.
	rc2 := &recorder{}
	rc2.install(f.log)
	f.log.Flush()
	if len(rc2.got()) != 0 {
		t.Errorf("Flush of nothing: %v", rc2.got())
	}
}

// A failed sync is counted as a failed write and reported without data; the
// call says it failed. Nothing of the entry is in the journal message.
func TestRecordDurableSyncFailureIsCounted(t *testing.T) {
	f := newFixture(t)
	rc := &recorder{syncErr: errors.New("EIO from the disk")}
	rc.install(f.log)
	if f.log.RecordDurable(power) {
		t.Fatal("a failed sync was reported as durable")
	}
	if got := strings.Join(rc.got(), ","); got != "open:"+FileName+",write,sync,close" {
		t.Errorf("steps %s", got)
	}
	if s := f.log.Stats(); s.Failures != 1 || s.Dropped != 0 {
		t.Errorf("stats %+v", s)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.warns) != 1 || !strings.Contains(f.warns[0], "syncing") || strings.Contains(f.warns[0], "EIO") ||
		strings.Contains(f.warns[0], "admin-test") || strings.Contains(f.warns[0], cid) {
		t.Errorf("warnings %q", f.warns)
	}
}

// A failed write before the sync is counted too, and the sync is not reached.
func TestRecordDurableWriteFailureIsCounted(t *testing.T) {
	f := newFixture(t)
	if err := os.Mkdir(filepath.Join(f.dir, DirName, FileName), 0o700); err != nil {
		t.Fatal(err)
	}
	if f.log.RecordDurable(power) {
		t.Fatal("a failed write was reported as durable")
	}
	if s := f.log.Stats(); s.Failures != 1 {
		t.Errorf("stats %+v", s)
	}
}

// An invalid event, an aggregated action and a closed log are refused without
// a write; the first two are counted as dropped.
func TestRecordDurableRefusals(t *testing.T) {
	f := newFixture(t)
	rc := &recorder{}
	rc.install(f.log)
	bad := power
	bad.Target = "system/x"
	if f.log.RecordDurable(bad) {
		t.Error("invalid event")
	}
	if f.log.RecordDurable(Event{User: "-", Action: LoginFailed, Target: TargetSession, Result: InvalidCredentials, CorrelationID: cid}) {
		t.Error("aggregated event")
	}
	if s := f.log.Stats(); s.Dropped != 2 {
		t.Errorf("dropped %d", s.Dropped)
	}
	f.log.Close()
	if f.log.RecordDurable(power) {
		t.Error("closed log")
	}
	if len(rc.got()) != 0 {
		t.Errorf("steps %v", rc.got())
	}
}

// The real file: the entry is on disk when RecordDurable returns, without any
// clock movement, and Close afterwards writes nothing twice.
func TestRecordDurableRealFile(t *testing.T) {
	f := newFixture(t)
	if !f.log.RecordDurable(power) {
		t.Fatal("RecordDurable")
	}
	lines := f.lines(t, FileName)
	if len(lines) != 1 || lines[0]["action"] != "system_reboot" || lines[0]["target"] != "system" || lines[0]["count"] != float64(1) {
		t.Fatalf("lines %v", lines)
	}
	f.log.Close()
	if n := len(f.lines(t, FileName)); n != 1 {
		t.Errorf("%d lines after Close", n)
	}
	fi, err := os.Stat(filepath.Join(f.dir, DirName, FileName))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v %v", fi, err)
	}
}
