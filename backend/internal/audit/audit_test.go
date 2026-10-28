package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/swingstate/oarfish/backend/internal/clock/clocktest"
)

const cid = "0123456789abcdef"

type fixture struct {
	log   *Log
	clock *clocktest.Fake
	dir   string
	mu    sync.Mutex
	warns []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{clock: clocktest.New(), dir: t.TempDir()}
	l, err := Open(f.dir, f.clock, func(m string) { f.mu.Lock(); f.warns = append(f.warns, m); f.mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	f.log = l
	return f
}

func (f *fixture) lines(t *testing.T, name string) []map[string]any {
	t.Helper()
	file, err := os.Open(filepath.Join(f.dir, DirName, name))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var out []map[string]any
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("not a JSON line: %q", sc.Text())
		}
		out = append(out, m)
	}
	return out
}

// ORB-SEC-006, E7: an entry has exactly the fixed fields: time (UTC, RFC
// 3339), user, plugin (empty in the core), action, target, result,
// correlation ID, and a count. Directory 0700, file 0600.
func TestEntryHasTheFixedFields(t *testing.T) {
	f := newFixture(t)
	f.log.Record(Event{User: "admin-test", Action: LoginSucceeded, Target: TargetSession, Result: Success, CorrelationID: cid})
	f.clock.Advance(FlushDelay)
	got := f.lines(t, FileName)
	want := []map[string]any{{
		"time": "2026-10-28T12:00:00Z", "user": "admin-test", "plugin": "", "action": "login_succeeded",
		"target": "session", "result": "success", "correlation_id": cid, "count": float64(1),
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %v\nwant %v", got, want)
	}
	for path, mode := range map[string]os.FileMode{filepath.Join(f.dir, DirName): 0o700, filepath.Join(f.dir, DirName, FileName): 0o600} {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != mode {
			t.Errorf("%s mode %o, want %o", path, fi.Mode().Perm(), mode)
		}
	}
}

// ORB-SEC-007: every field is checked against its closed set; an event with
// anything else (a typed user name with a space, a password, an unknown
// action) is dropped, so request text cannot reach the file.
func TestInvalidEventsAreDropped(t *testing.T) {
	f := newFixture(t)
	for _, e := range []Event{
		{User: "Sentinel Pw 3e9f", Action: LoginSucceeded, Target: TargetSession, Result: Success, CorrelationID: cid},
		{User: strings.Repeat("a", 33), Action: LoginSucceeded, Target: TargetSession, Result: Success, CorrelationID: cid},
		{User: "admin-test", Action: "login_with_password=x", Target: TargetSession, Result: Success, CorrelationID: cid},
		{User: "admin-test", Action: Logout, Target: "/api/v1/x?token=y", Result: Success, CorrelationID: cid},
		{User: "admin-test", Action: Logout, Target: TargetSession, Result: "ok, password was x", CorrelationID: cid},
		{User: "admin-test", Action: Logout, Target: TargetSession, Result: Success, CorrelationID: "client-chosen"},
	} {
		f.log.Record(e)
	}
	f.log.Close()
	if lines := f.lines(t, FileName); len(lines) != 0 {
		t.Fatalf("invalid events were written: %v", lines)
	}
	if s := f.log.Stats(); s.Dropped != 6 {
		t.Errorf("dropped %d, want 6", s.Dropped)
	}
}

// E7, ORB-NFR-009: entries are buffered and written 30 s after the first one;
// the timer exists only while there is something to write.
func TestFlushTiming(t *testing.T) {
	f := newFixture(t)
	if f.clock.Pending() != 0 {
		t.Fatal("a timer while idle")
	}
	f.log.Record(Event{User: "admin-test", Action: Logout, Target: TargetSession, Result: Success, CorrelationID: cid})
	f.clock.Advance(10 * time.Second)
	f.log.Record(Event{User: "admin-test", Action: LoginSucceeded, Target: TargetSession, Result: Success, CorrelationID: cid})
	if f.clock.Pending() != 1 {
		t.Fatalf("%d timers, want one flush timer", f.clock.Pending())
	}
	f.clock.Advance(19 * time.Second)
	if len(f.lines(t, FileName)) != 0 {
		t.Fatal("written before 30 s")
	}
	f.clock.Advance(time.Second)
	if n := len(f.lines(t, FileName)); n != 2 {
		t.Fatalf("%d lines after 30 s, want 2", n)
	}
	if f.clock.Pending() != 0 {
		t.Fatal("a timer left after the flush")
	}
}

// E7: failed logins and rejected requests are aggregated: one entry per
// minute and kind, with a count. A failed login never records a typed name.
func TestFailedLoginsAndRejectionsAreAggregated(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 25; i++ {
		f.log.Record(Event{User: NoUser, Action: LoginFailed, Target: TargetSession, Result: InvalidCredentials, CorrelationID: cid})
	}
	for i := 0; i < 3; i++ {
		f.log.Record(Event{User: NoUser, Action: LoginFailed, Target: TargetSession, Result: RateLimited, CorrelationID: cid})
		f.log.Record(Event{User: NoUser, Action: RequestRejected, Target: TargetPassword, Result: CSRFFailed, CorrelationID: cid})
	}
	f.clock.Advance(AggregateWindow + FlushDelay)
	got := f.lines(t, FileName)
	if len(got) != 3 {
		t.Fatalf("%d entries, want 3 aggregated ones: %v", len(got), got)
	}
	counts := map[string]float64{}
	for _, e := range got {
		counts[e["action"].(string)+"/"+e["result"].(string)] = e["count"].(float64)
		if e["user"] != NoUser || e["time"] != "2026-10-28T12:00:00Z" {
			t.Errorf("entry %v", e)
		}
	}
	if counts["login_failed/invalid_credentials"] != 25 || counts["login_failed/rate_limited"] != 3 || counts["request_rejected/csrf_failed"] != 3 {
		t.Errorf("counts = %v", counts)
	}
	// The next failure opens a new window.
	f.log.Record(Event{User: NoUser, Action: LoginFailed, Target: TargetSession, Result: InvalidCredentials, CorrelationID: cid})
	f.clock.Advance(AggregateWindow + FlushDelay)
	if n := len(f.lines(t, FileName)); n != 4 {
		t.Fatalf("%d entries after a second window, want 4", n)
	}
	if f.clock.Pending() != 0 {
		t.Fatal("timers left while idle")
	}
}

// E7, ORB-STO-007: rotation at 1 MiB, four files, so never more than 4 MiB.
func TestRotationAndCap(t *testing.T) {
	f := newFixture(t)
	ev := Event{User: "admin-test", Action: PasswordChanged, Target: TargetPassword, Result: Success, CorrelationID: cid}
	line, _ := json.Marshal(entry{Time: "2026-10-28T12:00:00Z", User: ev.User, Action: ev.Action, Target: ev.Target, Result: ev.Result, CorrelationID: cid, Count: 1})
	perFile := MaxFileSize / (len(line) + 1)
	total := perFile*6 + 10 // more than the four files hold
	for i := 0; i < total; i++ {
		f.log.Record(ev)
		if i%200 == 0 {
			f.log.Flush() // the buffer is bounded; a real log flushes every 30 s
		}
	}
	f.log.Close()
	var sum int64
	for i := 0; i < Files; i++ {
		fi, err := os.Stat(f.log.file(i))
		if err != nil {
			t.Fatalf("file %d: %v", i, err)
		}
		if fi.Size() > MaxFileSize {
			t.Errorf("file %d is %d bytes, above %d", i, fi.Size(), MaxFileSize)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("file %d mode %o", i, fi.Mode().Perm())
		}
		sum += fi.Size()
	}
	if sum > Files*MaxFileSize {
		t.Errorf("total %d bytes above the cap", sum)
	}
	if _, err := os.Stat(f.log.file(Files)); !os.IsNotExist(err) {
		t.Errorf("a fifth file exists: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(f.dir, DirName))
	if len(entries) != Files {
		t.Errorf("%d files in audit/, want %d", len(entries), Files)
	}
	// Every line in every file is whole JSON.
	for i := 0; i < Files; i++ {
		f.lines(t, filepath.Base(f.log.file(i)))
	}
}

// E7: Close writes the open aggregation windows and the buffer (the service
// calls it on SIGTERM and SIGINT), and later events are ignored.
func TestCloseWritesEverything(t *testing.T) {
	f := newFixture(t)
	f.log.Record(Event{User: "admin-test", Action: Logout, Target: TargetSession, Result: Success, CorrelationID: cid})
	f.log.Record(Event{User: NoUser, Action: LoginFailed, Target: TargetSession, Result: InvalidCredentials, CorrelationID: cid})
	f.log.Close()
	if n := len(f.lines(t, FileName)); n != 2 {
		t.Fatalf("%d lines after Close, want 2", n)
	}
	if f.clock.Pending() != 0 {
		t.Fatal("timers after Close")
	}
	f.log.Record(Event{User: "admin-test", Action: Logout, Target: TargetSession, Result: Success, CorrelationID: cid})
	f.log.Flush()
	if n := len(f.lines(t, FileName)); n != 2 {
		t.Fatal("an event after Close was written")
	}
}

// E7: a failed write does not crash, does not block, and is reported to the
// journal as a plain message without data.
func TestFailedWriteIsCountedAndReportedWithoutData(t *testing.T) {
	f := newFixture(t)
	// A directory where the file should be makes every write fail.
	if err := os.Mkdir(filepath.Join(f.dir, DirName, FileName), 0o700); err != nil {
		t.Fatal(err)
	}
	f.log.Record(Event{User: "admin-test", Action: Logout, Target: TargetSession, Result: Success, CorrelationID: cid})
	f.clock.Advance(FlushDelay)
	if s := f.log.Stats(); s.Failures != 1 {
		t.Fatalf("failures = %d", s.Failures)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.warns) != 1 || strings.Contains(f.warns[0], "admin-test") || strings.Contains(f.warns[0], cid) {
		t.Fatalf("warnings = %q", f.warns)
	}
}

// ORB-SEC-005: existing audit files of the service user that are too open are
// tightened at start.
func TestOpenTightensExistingFiles(t *testing.T) {
	dir := t.TempDir()
	_ = os.Mkdir(filepath.Join(dir, DirName), 0o755)
	_ = os.WriteFile(filepath.Join(dir, DirName, FileName), nil, 0o644)
	if _, err := Open(dir, clocktest.New(), nil); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{filepath.Join(dir, DirName): 0o700, filepath.Join(dir, DirName, FileName): 0o600} {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != mode {
			t.Errorf("%s mode %o", path, fi.Mode().Perm())
		}
	}
}
