package services

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/swingstate/oarfish/backend/internal/audit"
	"github.com/swingstate/oarfish/backend/internal/brokerclient"
	"github.com/swingstate/oarfish/backend/internal/clock/clocktest"
)

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// fakeSystemctl is the process-level fake (ADR 0011 B9), as in the broker's
// tests: a small test program that records its arguments and environment and
// prints fixture output. Call n (from 1) prints dir/out.<n>, exits with
// dir/exit.<n> (default 0), or sleeps dir/sleep.<n> seconds first. When
// dir/out.<n> is missing it prints dir/out (the same output for every call).
type fakeSystemctl struct {
	t    *testing.T
	dir  string
	path string
}

func newFakeSystemctl(t *testing.T) *fakeSystemctl {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
d='` + dir + `'
n=$(cat "$d/count" 2>/dev/null || echo 0)
n=$((n + 1))
echo "$n" > "$d/count"
for a in "$@"; do printf '%s\n' "$a"; done > "$d/args.$n"
env > "$d/env.$n"
pwd > "$d/pwd.$n"
if [ -f "$d/sleep.$n" ]; then sleep "$(cat "$d/sleep.$n")"; fi
if [ -f "$d/out.$n" ]; then cat "$d/out.$n"; elif [ -f "$d/out" ]; then cat "$d/out"; fi
exit "$(cat "$d/exit.$n" 2>/dev/null || echo 0)"
`
	path := filepath.Join(dir, "systemctl")
	writeFile(t, path, script, 0o755)
	return &fakeSystemctl{t: t, dir: dir, path: path}
}

func (f *fakeSystemctl) set(n int, kind, value string) {
	name := kind
	if n > 0 {
		name += "." + strconv.Itoa(n)
	}
	writeFile(f.t, filepath.Join(f.dir, name), value, 0o644)
}

func (f *fakeSystemctl) count() int {
	b, err := os.ReadFile(filepath.Join(f.dir, "count"))
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

func (f *fakeSystemctl) file(kind string, n int) string {
	b, err := os.ReadFile(filepath.Join(f.dir, kind+"."+strconv.Itoa(n)))
	if err != nil {
		f.t.Fatalf("call %d: %v", n, err)
	}
	return string(b)
}

func (f *fakeSystemctl) args(n int) []string {
	return strings.Split(strings.TrimSuffix(f.file("args", n), "\n"), "\n")
}

// fixtureUnits is list-units output in the shape observed in G7.1 (A2 S7):
// column padding, not-found entries, an instance of a template, a failed unit.
// Every name that is not a list entry of rule 10 or 11 is invented.
const fixtureUnits = `demo-app.service                 loaded    active   running Demo application server
demo-batch.service               loaded    inactive dead    Demo nightly batch
demo-broken.service              loaded    failed   failed  Demo service that failed to start
demo-gone.service                not-found inactive dead    demo-gone.service
demo-worker@one.service          loaded    active   running Demo worker one
dbus.service                     loaded    active   running D-Bus System Message Bus
getty@tty1.service               loaded    active   running Getty on tty1
ssh.service                      loaded    active   running OpenBSD Secure Shell server
systemd-journald.service         loaded    active   running Journal Service
docker.service                   loaded    active   running Docker Application Container Engine
ModemManager.service             loaded    inactive dead    Modem Manager
`

// fixtureFiles is list-unit-files output with the third column (vendor preset).
const fixtureFiles = `demo-app.service                           enabled         enabled
demo-batch.service                         static          -
demo-broken.service                        disabled        enabled
demo-idle.service                          disabled        enabled
demo-masked.service                        masked          enabled
demo-worker@.service                       indirect        enabled
dbus.service                               static          -
getty@.service                             enabled         enabled
ssh.service                                enabled         enabled
sshd.service                               alias           -
systemd-journald.service                   static          -
docker.service                             enabled         enabled
ModemManager.service                       disabled        enabled
`

// testReader is a reader on a fake clock whose systemctl is the process fake.
func testReader(t *testing.T, f *fakeSystemctl) (*reader, *clocktest.Fake) {
	t.Helper()
	c := clocktest.New()
	r := newReader(c)
	r.systemctl = f.path
	r.systemdDir = t.TempDir()
	t.Cleanup(r.close)
	return r, c
}

// fakeBroker is the broker client of the job tests.
type fakeBroker struct {
	mu        sync.Mutex
	available bool
	answer    brokerclient.Answer
	calls     []string
	release   chan struct{} // when set, Do waits for it or for ctx
	started   chan struct{}
	ctxErr    error
}

func (b *fakeBroker) Available() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.available }

func (b *fakeBroker) Do(ctx context.Context, op brokerclient.Op, unit, cid string) brokerclient.Answer {
	b.mu.Lock()
	b.calls = append(b.calls, string(op)+" "+unit+" "+cid)
	rel, started, ans := b.release, b.started, b.answer
	b.mu.Unlock()
	if started != nil {
		started <- struct{}{}
	}
	if rel != nil {
		select {
		case <-rel:
		case <-ctx.Done():
			b.mu.Lock()
			b.ctxErr = ctx.Err()
			b.mu.Unlock()
			return brokerclient.Answer{Code: brokerclient.CodeUnavailable}
		}
	}
	return ans
}

func (b *fakeBroker) callList() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.calls...)
}

// auditSink collects audit events.
type auditSink struct {
	mu     sync.Mutex
	events []audit.Event
}

func (s *auditSink) record(e audit.Event) { s.mu.Lock(); s.events = append(s.events, e); s.mu.Unlock() }

func (s *auditSink) list() []audit.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]audit.Event(nil), s.events...)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}
