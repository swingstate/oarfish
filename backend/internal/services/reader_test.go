package services

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func names(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Name
	}
	return out
}

func rowNamed(t *testing.T, rows []Row, name string) Row {
	t.Helper()
	for _, r := range rows {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no row %s", name)
	return Row{}
}

// The whole read through the production runner and the process fake: the two
// shapes in order, the join of A3.2, the classification, sorted by name.
func TestReadThroughProcessFake(t *testing.T) {
	f := newFakeSystemctl(t)
	f.set(1, "out", fixtureUnits)
	f.set(2, "out", fixtureFiles)
	r, c := testReader(t, f)
	list, e := r.read(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if f.count() != 2 || !slices.Equal(f.args(1), shapeListUnits) || !slices.Equal(f.args(2), shapeListUnitFiles) {
		t.Fatalf("calls %d: %q %q", f.count(), f.args(1), f.args(2))
	}
	want := []string{"ModemManager.service", "dbus.service", "demo-app.service", "demo-batch.service", "demo-broken.service",
		"demo-idle.service", "demo-masked.service", "demo-worker@one.service", "docker.service", "getty@tty1.service",
		"ssh.service", "systemd-journald.service"}
	if !slices.Equal(names(list.Rows), want) {
		t.Fatalf("rows %v\nwant %v", names(list.Rows), want)
	}
	if !list.ReadAt.Equal(c.Now()) {
		t.Errorf("read at %v", list.ReadAt)
	}
	app := rowNamed(t, list.Rows, "demo-app.service")
	if app.Description != "Demo application server" || app.LoadState != "loaded" || app.ActiveState != "active" || app.SubState != "running" ||
		app.StartMode != "enabled" || app.Denied || app.RestartOnly || app.Critical != "" || !slices.Equal(app.Actions, []string{"stop", "restart"}) {
		t.Errorf("%+v", app)
	}
	// Only in list-unit-files: inactive and not loaded.
	idle := rowNamed(t, list.Rows, "demo-idle.service")
	if idle.LoadState != NotLoaded || idle.ActiveState != "inactive" || idle.SubState != "dead" || idle.StartMode != "disabled" || idle.Description != "" ||
		!slices.Equal(idle.Actions, []string{"start", "restart"}) {
		t.Errorf("%+v", idle)
	}
	if m := rowNamed(t, list.Rows, "demo-masked.service"); m.StartMode != "masked" {
		t.Errorf("%+v", m)
	}
	// An instance takes the state of its template.
	if w := rowNamed(t, list.Rows, "demo-worker@one.service"); w.StartMode != "indirect" {
		t.Errorf("%+v", w)
	}
	if g := rowNamed(t, list.Rows, "getty@tty1.service"); !g.Denied || len(g.Actions) != 0 || g.StartMode != "enabled" {
		t.Errorf("%+v", g)
	}
	for _, n := range []string{"dbus.service", "systemd-journald.service"} {
		if d := rowNamed(t, list.Rows, n); !d.Denied || len(d.Actions) != 0 {
			t.Errorf("%+v", d)
		}
	}
	if s := rowNamed(t, list.Rows, "ssh.service"); !s.RestartOnly || s.Critical != KindSSH || !slices.Equal(s.Actions, []string{"restart"}) {
		t.Errorf("%+v", s)
	}
	if d := rowNamed(t, list.Rows, "docker.service"); d.Critical != KindContainer || !slices.Equal(d.Actions, []string{"stop", "restart"}) {
		t.Errorf("%+v", d)
	}
	if mm := rowNamed(t, list.Rows, "ModemManager.service"); mm.Critical != KindNetwork || mm.RestartOnly || !slices.Equal(mm.Actions, []string{"start", "restart"}) {
		t.Errorf("%+v", mm)
	}
	if list.Counts != (Counts{Total: 12, Active: 7, Failed: 1, Inactive: 4}) {
		t.Errorf("counts %+v", list.Counts)
	}
	// Aliases, templates and not-found entries are not rows.
	for _, n := range []string{"sshd.service", "getty@.service", "demo-worker@.service", "demo-gone.service"} {
		if slices.Contains(names(list.Rows), n) {
			t.Errorf("%s is a row", n)
		}
	}
}

// ADR 0011 A8: list-units and list-unit-files print a name with a \xHH
// escape as it is (systemd 257 prints both as table cells, unquoted; only
// show quotes it). R1 lists such an instance under its exact name, takes the
// start mode of its template, and offers what rules 10 and 11 allow.
func TestEscapedNamesOnReadPath(t *testing.T) {
	const inst = `demo-disk@dev-disk-by\x2dlabel-data.service`
	const fsck = `systemd-fsck@dev-disk-by\x2dpartuuid-00000000\x2d02.service`
	f := newFakeSystemctl(t)
	f.set(1, "out", inst+" loaded active running Demo disk worker\n"+
		fsck+" loaded active exited File System Check on /dev/disk/by-partuuid/00000000-02\n")
	f.set(2, "out", "demo-disk@.service static -\nsystemd-fsck@.service static -\n")
	r, _ := testReader(t, f)
	list, e := r.read(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if !slices.Equal(names(list.Rows), []string{inst, fsck}) {
		t.Fatalf("rows %q", names(list.Rows))
	}
	if d := rowNamed(t, list.Rows, inst); d.Denied || d.StartMode != "static" || !slices.Equal(d.Actions, []string{"stop", "restart"}) {
		t.Errorf("%+v", d)
	}
	if d := rowNamed(t, list.Rows, fsck); !d.Denied || len(d.Actions) != 0 {
		t.Errorf("%+v", d)
	}
}

// No systemd, or no systemctl: services_unavailable, and no process starts.
func TestReadUnavailable(t *testing.T) {
	f := newFakeSystemctl(t)
	r, _ := testReader(t, f)
	r.systemdDir = filepath.Join(t.TempDir(), "missing")
	if _, e := r.read(context.Background()); e == nil || e.Code != CodeUnavailable {
		t.Errorf("no systemd: %+v", e)
	}
	f2 := newFakeSystemctl(t)
	r2, _ := testReader(t, f2)
	r2.systemctl = filepath.Join(t.TempDir(), "systemctl")
	if _, e := r2.read(context.Background()); e == nil || e.Code != CodeUnavailable {
		t.Errorf("no systemctl: %+v", e)
	}
	if f.count() != 0 || f2.count() != 0 {
		t.Error("a process started")
	}
	// A program that exists but cannot be started (not executable).
	f3 := newFakeSystemctl(t)
	r3, _ := testReader(t, f3)
	writeFile(t, f3.path, "not a program", 0o644)
	if _, e := r3.read(context.Background()); e == nil || e.Code != CodeUnavailable {
		t.Errorf("not executable: %+v", e)
	}
}

// A non-zero exit, output over the bound, output this version does not
// accept, or the deadline: services_read_failed, and no half list.
func TestReadFailed(t *testing.T) {
	for name, setup := range map[string]func(f *fakeSystemctl){
		"exit units": func(f *fakeSystemctl) { f.set(1, "out", fixtureUnits); f.set(1, "exit", "1") },
		"exit files": func(f *fakeSystemctl) {
			f.set(1, "out", fixtureUnits)
			f.set(2, "out", fixtureFiles)
			f.set(2, "exit", "4")
		},
		"1 MiB":     func(f *fakeSystemctl) { f.set(1, "out", string(make([]byte, 1<<20))) },
		"bad state": func(f *fakeSystemctl) { f.set(1, "out", fixtureUnits); f.set(2, "out", "a.service bad enabled\n") },
		"bad name": func(f *fakeSystemctl) {
			f.set(1, "out", "../a.service loaded active running x\n")
			f.set(2, "out", fixtureFiles)
		},
		"two columns": func(f *fakeSystemctl) { f.set(1, "out", fixtureUnits); f.set(2, "out", "a.service enabled\n") },
		"bad sub state": func(f *fakeSystemctl) {
			f.set(1, "out", "a.service loaded active zombie x\n")
			f.set(2, "out", fixtureFiles)
		},
	} {
		f := newFakeSystemctl(t)
		setup(f)
		r, _ := testReader(t, f)
		list, e := r.read(context.Background())
		if e == nil || e.Code != CodeReadFailed || list.Rows != nil {
			t.Errorf("%s: %+v %+v", name, e, list)
		}
	}
	// The deadline, through a Go runner (the real one is tested in systemctl_test.go).
	f := newFakeSystemctl(t)
	r, _ := testReader(t, f)
	r.run = func(context.Context, string, time.Duration, []string) (runResult, error) {
		return runResult{TimedOut: true, Exit: -1}, nil
	}
	if _, e := r.read(context.Background()); e == nil || e.Code != CodeReadFailed {
		t.Errorf("deadline: %+v", e)
	}
}

// More than 1,000 rows (units and files together) fail the read.
func TestJoinBound(t *testing.T) {
	var units []unitLine
	files := map[string]string{}
	for i := 0; i < 600; i++ {
		units = append(units, unitLine{name: "u" + itoa(i) + ".service", load: "loaded", active: "active", sub: "running"})
		files["f"+itoa(i)+".service"] = "disabled"
	}
	if _, ok := join(units, files); ok {
		t.Error("accepted 1,200 rows")
	}
	if rows, ok := join(units[:500], map[string]string{}); !ok || len(rows) != 500 {
		t.Error("refused 500 rows")
	}
}

// The cache (B2): a second read within 5 s starts no process; after 5 s it
// reads again. A failure is kept for 5 s as well.
func TestCache(t *testing.T) {
	f := newFakeSystemctl(t)
	f.set(0, "out", "")
	f.set(1, "out", fixtureUnits)
	f.set(2, "out", fixtureFiles)
	f.set(3, "out", fixtureUnits)
	f.set(4, "out", fixtureFiles)
	r, c := testReader(t, f)
	first, _ := r.read(context.Background())
	c.Advance(4 * time.Second)
	second, _ := r.read(context.Background())
	if f.count() != 2 || !second.ReadAt.Equal(first.ReadAt) {
		t.Fatalf("calls %d", f.count())
	}
	c.Advance(time.Second)
	third, e := r.read(context.Background())
	if e != nil || f.count() != 4 || !third.ReadAt.After(first.ReadAt) {
		t.Fatalf("calls %d, %v", f.count(), e)
	}
	if c.Pending() != 0 {
		t.Errorf("%d timers: the read path has no background work", c.Pending())
	}

	g := newFakeSystemctl(t)
	g.set(1, "exit", "1")
	rg, cg := testReader(t, g)
	for i := 0; i < 3; i++ {
		if _, e := rg.read(context.Background()); e == nil {
			t.Fatal("no error")
		}
	}
	if g.count() != 1 {
		t.Errorf("failure not cached: %d calls", g.count())
	}
	cg.Advance(cacheFor)
	rg.read(context.Background()) // reads again: list-units and list-unit-files
	if g.count() != 3 {
		t.Errorf("failure kept too long: %d calls", g.count())
	}
}

// The cache is dropped when an action ends (invalidate); a read that was
// running when it was dropped serves its callers but does not fill the cache.
func TestInvalidate(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	block := atomic.Bool{}
	f := newFakeSystemctl(t)
	r, _ := testReader(t, f)
	r.run = func(_ context.Context, _ string, _ time.Duration, args []string) (runResult, error) {
		calls.Add(1)
		if block.Load() {
			<-release
		}
		if args[0] == "list-units" {
			return runResult{Out: []byte(fixtureUnits)}, nil
		}
		return runResult{Out: []byte(fixtureFiles)}, nil
	}
	r.read(context.Background())
	r.read(context.Background())
	if calls.Load() != 2 {
		t.Fatalf("%d calls", calls.Load())
	}
	r.invalidate()
	r.read(context.Background())
	if calls.Load() != 4 {
		t.Fatalf("after invalidate: %d calls", calls.Load())
	}
	// Dropped while a read runs.
	r.invalidate()
	block.Store(true)
	done := make(chan struct{})
	go func() { r.read(context.Background()); close(done) }()
	waitFor(t, "the read to start", func() bool { return calls.Load() == 5 })
	r.invalidate()
	block.Store(false)
	close(release)
	<-done
	r.read(context.Background())
	if calls.Load() != 8 {
		t.Errorf("the stale read filled the cache: %d calls", calls.Load())
	}
}

// One list read at a time: callers that come while it runs wait for it and
// get its result; a caller that leaves does not stop it.
func TestSingleFlight(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	f := newFakeSystemctl(t)
	r, _ := testReader(t, f)
	r.run = func(_ context.Context, _ string, _ time.Duration, args []string) (runResult, error) {
		calls.Add(1)
		<-release
		if args[0] == "list-units" {
			return runResult{Out: []byte(fixtureUnits)}, nil
		}
		return runResult{Out: []byte(fixtureFiles)}, nil
	}
	var wg sync.WaitGroup
	results := make([]List, 5)
	for i := range results {
		wg.Add(1)
		go func() { defer wg.Done(); results[i], _ = r.read(context.Background()) }()
	}
	ctx, cancel := context.WithCancel(context.Background())
	leaver := make(chan *Error)
	go func() { _, e := r.read(ctx); leaver <- e }()
	waitFor(t, "the read to start", func() bool { return calls.Load() == 1 })
	cancel()
	if e := <-leaver; e == nil || e.Code != CodeReadFailed {
		t.Errorf("leaver: %+v", e)
	}
	time.Sleep(20 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("%d reads started at once", calls.Load())
	}
	close(release)
	wg.Wait()
	if calls.Load() != 2 {
		t.Errorf("%d calls, want the two of one read", calls.Load())
	}
	for i, l := range results {
		if len(l.Rows) != 12 {
			t.Errorf("caller %d: %d rows", i, len(l.Rows))
		}
	}
}

// close ends a running read: the child is killed and nothing is left behind.
func TestCloseEndsARead(t *testing.T) {
	f := newFakeSystemctl(t)
	f.set(1, "sleep", "5")
	r, _ := testReader(t, f)
	done := make(chan *Error)
	go func() { _, e := r.read(context.Background()); done <- e }()
	waitFor(t, "the child to start", func() bool { return f.count() == 1 })
	start := time.Now()
	r.close()
	if e := <-done; e == nil {
		t.Error("no error")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("close took %v", d)
	}
}
