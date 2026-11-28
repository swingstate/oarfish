package services

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The unit-file states of ADR 0011 A9: their own store, their own single
// flight, their own deadline; renewed only at a list request.

// filesRunner is a Go runner: list-units answers at once with fixtureUnits;
// list-unit-files waits for release (when set) and answers with out.
type filesRunner struct {
	mu        sync.Mutex
	units     int
	files     int
	release   chan struct{}
	out       string
	exit      int
	deadlines map[string]time.Duration
}

func (fr *filesRunner) run(ctx context.Context, _ string, deadline time.Duration, args []string) (runResult, error) {
	fr.mu.Lock()
	if fr.deadlines == nil {
		fr.deadlines = map[string]time.Duration{}
	}
	fr.deadlines[args[0]] = deadline
	if args[0] == "list-units" {
		fr.units++
		fr.mu.Unlock()
		return runResult{Out: []byte(fixtureUnits)}, nil
	}
	fr.files++
	rel, out, exit := fr.release, fr.out, fr.exit
	fr.mu.Unlock()
	if rel != nil {
		select {
		case <-rel:
		case <-ctx.Done():
			return runResult{TimedOut: true, Exit: -1}, nil
		}
	}
	return runResult{Out: []byte(out), Exit: exit}, nil
}

func (fr *filesRunner) counts() (units, files int) {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	return fr.units, fr.files
}

func (fr *filesRunner) set(release chan struct{}, out string, exit int) {
	fr.mu.Lock()
	fr.release, fr.out, fr.exit = release, out, exit
	fr.mu.Unlock()
}

func (r *reader) filesRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.filesRun != nil
}

// A list-unit-files slower than the grace: the request is answered after the
// grace from list-units alone (pending, every start mode unknown, the rows that
// only the unit files name missing, the count line counting what is shown);
// while the read runs, a later request starts no list-units call and gets the
// last list (PID 1 would hold it, A9); when the read ends, the full list comes
// without a second unit-file process. Through the process fake.
func TestUnitFilesPendingThenKnown(t *testing.T) {
	f := newFakeSystemctl(t)
	f.set(0, "out", fixtureUnits)
	f.set(2, "out", fixtureFiles)
	f.set(2, "sleep", "1")
	r, c := testReader(t, f)
	done := make(chan List)
	go func() { l, _ := r.read(context.Background()); done <- l }()
	waitFor(t, "the grace timer", func() bool { return c.Pending() == 1 && f.count() == 2 })
	c.Advance(filesGrace - time.Millisecond)
	select {
	case <-done:
		t.Fatal("answered before the grace")
	case <-time.After(20 * time.Millisecond):
	}
	c.Advance(time.Millisecond)
	first := <-done
	if first.StartModes != StartModesPending || !first.StartModesReadAt.IsZero() {
		t.Fatalf("first: %q %v", first.StartModes, first.StartModesReadAt)
	}
	if len(first.Rows) != 10 || slices.Contains(names(first.Rows), "demo-idle.service") || slices.Contains(names(first.Rows), "demo-masked.service") {
		t.Errorf("first rows %v", names(first.Rows))
	}
	for _, row := range first.Rows {
		if row.StartMode != "" {
			t.Errorf("%s: start mode %q while pending", row.Name, row.StartMode)
		}
	}
	if first.Counts != (Counts{Total: 10, Active: 7, Failed: 1, Inactive: 2}) {
		t.Errorf("first counts %+v", first.Counts)
	}
	if !slices.Equal(f.args(2), shapeListUnitFiles) {
		t.Errorf("call 2: %q", f.args(2))
	}

	// The cache of the unit list has expired, but the unit-file read runs:
	// no list-units call, the last list, still pending (and a short wait).
	c.Advance(cacheFor)
	go func() { l, _ := r.read(context.Background()); done <- l }()
	waitFor(t, "the grace timer", func() bool { return c.Pending() == 1 })
	c.Advance(filesGrace)
	second := <-done
	if f.count() != 2 || second.StartModes != StartModesPending || !second.ReadAt.Equal(first.ReadAt) {
		t.Errorf("second: %d calls, %q, read at %v", f.count(), second.StartModes, second.ReadAt)
	}

	waitFor(t, "the unit-file read to end", func() bool { return !r.filesRunning() })
	third, e := r.read(context.Background())
	if e != nil || third.StartModes != StartModesKnown || len(third.Rows) != 12 {
		t.Fatalf("third: %v %q %d rows", e, third.StartModes, len(third.Rows))
	}
	if !third.StartModesReadAt.Equal(c.Now()) || rowNamed(t, third.Rows, "demo-app.service").StartMode != "enabled" {
		t.Errorf("third: %v %+v", third.StartModesReadAt, rowNamed(t, third.Rows, "demo-app.service"))
	}
	if f.count() != 3 || !slices.Equal(f.args(3), shapeListUnits) {
		t.Errorf("%d calls; want one list-units more and no second list-unit-files", f.count())
	}
	if c.Pending() != 0 {
		t.Errorf("%d timers left", c.Pending())
	}
}

// A unit-file read within the grace: the first request waits for it and gets
// the full list, as before.
func TestUnitFilesWithinGrace(t *testing.T) {
	fr := &filesRunner{out: fixtureFiles}
	fr.set(make(chan struct{}), fixtureFiles, 0)
	r, c := testReader(t, newFakeSystemctl(t))
	r.run = fr.run
	done := make(chan List)
	go func() { l, _ := r.read(context.Background()); done <- l }()
	waitFor(t, "the grace timer", func() bool { return c.Pending() == 1 })
	c.Advance(filesGrace - time.Second)
	close(fr.release)
	l := <-done
	if l.StartModes != StartModesKnown || len(l.Rows) != 12 {
		t.Errorf("%q %d rows", l.StartModes, len(l.Rows))
	}
	if c.Pending() != 0 {
		t.Errorf("%d timers left", c.Pending())
	}
}

// One unit-file process for many page loads within 30 minutes; at 30 minutes a
// renewal starts at the next request, and the old states are served while it
// runs; then the new ones.
func TestUnitFilesOneReadFor30Minutes(t *testing.T) {
	fr := &filesRunner{out: fixtureFiles}
	r, c := testReader(t, newFakeSystemctl(t))
	r.run = fr.run
	first, _ := r.read(context.Background())
	if first.StartModes != StartModesKnown {
		t.Fatalf("%q", first.StartModes)
	}
	for i := 0; i < 29; i++ {
		c.Advance(time.Minute)
		if l, e := r.read(context.Background()); e != nil || l.StartModes != StartModesKnown || !l.StartModesReadAt.Equal(first.StartModesReadAt) {
			t.Fatalf("minute %d: %v %q", i+1, e, l.StartModes)
		}
	}
	if u, fl := fr.counts(); u != 30 || fl != 1 {
		t.Fatalf("%d list-units, %d list-unit-files; want 30 and 1", u, fl)
	}
	c.Advance(time.Minute - time.Nanosecond)
	r.read(context.Background())
	if _, fl := fr.counts(); fl != 1 {
		t.Fatalf("renewed before 30 minutes: %d", fl)
	}

	c.Advance(time.Nanosecond)
	rel := make(chan struct{})
	fr.set(rel, "demo-app.service disabled enabled\n", 0)
	old, e := r.read(context.Background())
	if e != nil || old.StartModes != StartModesKnown || !old.StartModesReadAt.Equal(first.StartModesReadAt) || rowNamed(t, old.Rows, "demo-app.service").StartMode != "enabled" {
		t.Fatalf("during the renewal: %v %q %v", e, old.StartModes, old.StartModesReadAt)
	}
	if !r.filesRunning() {
		t.Fatal("no renewal")
	}
	waitFor(t, "the renewal's process", func() bool { _, fl := fr.counts(); return fl == 2 })
	if c.Pending() != 0 {
		t.Error("a request waited for a renewal although states were known")
	}
	close(rel)
	waitFor(t, "the renewal", func() bool { return !r.filesRunning() })
	c.Advance(cacheFor)
	renewed, _ := r.read(context.Background())
	if renewed.StartModes != StartModesKnown || !renewed.StartModesReadAt.After(first.StartModesReadAt) ||
		rowNamed(t, renewed.Rows, "demo-app.service").StartMode != "disabled" {
		t.Errorf("after the renewal: %q %v %+v", renewed.StartModes, renewed.StartModesReadAt, rowNamed(t, renewed.Rows, "demo-app.service"))
	}
	if _, fl := fr.counts(); fl != 2 {
		t.Errorf("%d unit-file reads", fl)
	}
}

// Many concurrent requests start one unit-file read.
func TestUnitFilesSingleFlight(t *testing.T) {
	fr := &filesRunner{}
	rel := make(chan struct{})
	fr.set(rel, fixtureFiles, 0)
	r, c := testReader(t, newFakeSystemctl(t))
	r.run = fr.run
	var wg sync.WaitGroup
	var pending atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l, _ := r.read(context.Background()); l.StartModes == StartModesPending {
				pending.Add(1)
			}
		}()
	}
	waitFor(t, "eight grace timers", func() bool { return c.Pending() == 8 })
	waitFor(t, "the unit-file process", func() bool { _, fl := fr.counts(); return fl >= 1 })
	c.Advance(filesGrace)
	wg.Wait()
	if _, fl := fr.counts(); fl != 1 || pending.Load() != 8 {
		t.Errorf("%d unit-file reads, %d pending answers", fl, pending.Load())
	}
	close(rel)
}

// Never sooner than 60 s after the previous unit-file read ended, also after a
// failure: the list is answered meanwhile with the status unavailable.
func TestUnitFilesFloorAfterFailure(t *testing.T) {
	fr := &filesRunner{}
	fr.set(nil, "", 1)
	r, c := testReader(t, newFakeSystemctl(t))
	r.run = fr.run
	if l, e := r.read(context.Background()); e != nil || l.StartModes != StartModesUnavailable || len(l.Rows) != 10 {
		t.Fatalf("%v %q %d rows", e, l.StartModes, len(l.Rows))
	}
	for i := 0; i < 11; i++ {
		c.Advance(5 * time.Second)
		if l, e := r.read(context.Background()); e != nil || l.StartModes != StartModesUnavailable {
			t.Fatalf("at %d s: %v %q", (i+1)*5, e, l.StartModes)
		}
	}
	c.Advance(5*time.Second - time.Nanosecond)
	r.read(context.Background())
	if _, fl := fr.counts(); fl != 1 {
		t.Fatalf("a second read before the floor: %d", fl)
	}
	c.Advance(time.Nanosecond)
	fr.set(nil, fixtureFiles, 0)
	if l, _ := r.read(context.Background()); l.StartModes != StartModesKnown || len(l.Rows) != 12 {
		t.Errorf("after the floor: %q %d rows", l.StartModes, len(l.Rows))
	}
	if _, fl := fr.counts(); fl != 2 {
		t.Errorf("%d unit-file reads", fl)
	}

	// A failed renewal keeps the known states in use.
	c.Advance(filesMaxAge)
	fr.set(nil, "", 1)
	l, _ := r.read(context.Background())
	waitFor(t, "the renewal", func() bool { return !r.filesRunning() })
	l, _ = r.read(context.Background())
	if _, fl := fr.counts(); fl != 3 || l.StartModes != StartModesKnown || len(l.Rows) != 12 {
		t.Errorf("after a failed renewal: %d reads, %q, %d rows", fl, l.StartModes, len(l.Rows))
	}
	// And the floor holds after it too.
	c.Advance(filesFloor - time.Second)
	r.read(context.Background())
	if _, fl := fr.counts(); fl != 3 {
		t.Errorf("renewed before the floor: %d", fl)
	}
}

// invalidateFiles marks the states out of date: they stay in use, and the next
// request after the floor renews them. invalidate (the end of an action) does
// not touch them.
func TestInvalidateUnitFiles(t *testing.T) {
	fr := &filesRunner{}
	fr.set(nil, fixtureFiles, 0)
	r, c := testReader(t, newFakeSystemctl(t))
	r.run = fr.run
	r.read(context.Background())
	r.invalidate()
	c.Advance(filesFloor)
	r.read(context.Background())
	if _, fl := fr.counts(); fl != 1 {
		t.Fatalf("invalidate dropped the unit-file states: %d reads", fl)
	}
	r.invalidateFiles()
	c.Advance(time.Second)
	if l, _ := r.read(context.Background()); l.StartModes != StartModesKnown || len(l.Rows) != 12 {
		t.Errorf("states not kept after invalidateFiles: %q", l.StartModes)
	}
	waitFor(t, "the renewal after invalidateFiles", func() bool { _, fl := fr.counts(); return fl == 2 && !r.filesRunning() })

	// Invalidated while a read runs: that read's states are used, and the next
	// request after the floor reads again.
	rel := make(chan struct{})
	fr.set(rel, fixtureFiles, 0)
	r.invalidateFiles()
	c.Advance(filesFloor)
	r.read(context.Background())
	waitFor(t, "the read", func() bool { return r.filesRunning() })
	r.invalidateFiles()
	close(rel)
	waitFor(t, "the read to end", func() bool { return !r.filesRunning() })
	c.Advance(filesFloor)
	fr.set(nil, fixtureFiles, 0)
	r.read(context.Background())
	waitFor(t, "a read after the invalidation during a read", func() bool { _, fl := fr.counts(); return fl == 4 && !r.filesRunning() })

	m := New(Config{})
	defer m.Close()
	m.reader.run = fr.run
	m.reader.systemctl, m.reader.systemdDir = r.systemctl, r.systemdDir
	m.InvalidateUnitFiles()
	if !m.reader.filesStale {
		t.Error("the entry point does not reach the store")
	}
}

// The deadlines: 5 s for list-units, 45 s for list-unit-files only.
func TestUnitFilesDeadline(t *testing.T) {
	if callDeadline != 5*time.Second || filesCallDeadline != 45*time.Second || filesGrace != 4*time.Second ||
		filesFloor != 60*time.Second || filesMaxAge != 30*time.Minute || cacheFor != 5*time.Second {
		t.Fatal("a bound of A9 changed")
	}
	fr := &filesRunner{}
	fr.set(nil, fixtureFiles, 0)
	r, _ := testReader(t, newFakeSystemctl(t))
	if r.filesDeadline != filesCallDeadline {
		t.Fatalf("filesDeadline %v", r.filesDeadline)
	}
	r.run = fr.run
	r.read(context.Background())
	if fr.deadlines["list-units"] != callDeadline || fr.deadlines["list-unit-files"] != filesCallDeadline {
		t.Errorf("deadlines %v", fr.deadlines)
	}
}

// A list-unit-files that never ends is killed at its deadline (shortened here)
// through the production runner and the process fake, and the list stays
// usable: pending while it runs, unavailable after it.
func TestUnitFilesHangingReadIsKilled(t *testing.T) {
	f := newFakeSystemctl(t)
	f.set(1, "out", fixtureUnits)
	f.set(2, "sleep", "30")
	r, c := testReader(t, f)
	r.filesDeadline = 300 * time.Millisecond
	done := make(chan List)
	go func() { l, _ := r.read(context.Background()); done <- l }()
	waitFor(t, "the grace timer", func() bool { return c.Pending() == 1 && f.count() == 2 })
	start := time.Now()
	c.Advance(filesGrace)
	if l := <-done; l.StartModes != StartModesPending || len(l.Rows) != 10 {
		t.Fatalf("%q %d rows", l.StartModes, len(l.Rows))
	}
	waitFor(t, "the kill", func() bool { return !r.filesRunning() })
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("killed after %v", d)
	}
	l, e := r.read(context.Background())
	if e != nil || l.StartModes != StartModesUnavailable || len(l.Rows) != 10 {
		t.Errorf("after the kill: %v %q %d rows", e, l.StartModes, len(l.Rows))
	}
}

// close ends a running unit-file read: the child is killed, close returns.
func TestCloseEndsAUnitFileRead(t *testing.T) {
	f := newFakeSystemctl(t)
	f.set(1, "out", fixtureUnits)
	f.set(2, "sleep", "30")
	r, c := testReader(t, f)
	go r.read(context.Background())
	waitFor(t, "the unit-file read", func() bool { return c.Pending() == 1 && f.count() == 2 })
	start := time.Now()
	r.close()
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("close took %v", d)
	}
	if r.filesRunning() {
		t.Error("the read still runs")
	}
}

// No offer depends on the start mode, so a row without a known start mode
// gets the same buttons as with one (the broker decides again).
func TestOffersWithoutStartModes(t *testing.T) {
	fr := &filesRunner{}
	fr.set(nil, "", 1)
	r, c := testReader(t, newFakeSystemctl(t))
	r.run = fr.run
	without, _ := r.read(context.Background())
	c.Advance(filesFloor)
	fr.set(nil, fixtureFiles, 0)
	with, _ := r.read(context.Background())
	if without.StartModes != StartModesUnavailable || with.StartModes != StartModesKnown {
		t.Fatalf("%q %q", without.StartModes, with.StartModes)
	}
	for _, a := range without.Rows {
		b := rowNamed(t, with.Rows, a.Name)
		if !slices.Equal(a.Actions, b.Actions) || a.Denied != b.Denied || a.RestartOnly != b.RestartOnly || a.Critical != b.Critical {
			t.Errorf("%s: %+v without, %+v with start modes", a.Name, a, b)
		}
	}
}
