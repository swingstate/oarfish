package audit

// Durability tests (G5.1 closeout). They try to lose an audit entry: events
// from several goroutines, plain and aggregated, while timer flushes fire,
// while a flush is writing, across the 1 MiB rotation boundary, while Close
// runs, and across a restart. After Close every file is read back and every
// valid recorded event must be there exactly once, in whole lines.
//
// Every event carries a correlation ID that names it (generation, worker,
// sequence), so a missing, doubled or invented entry is found by its ID. An
// aggregated entry carries the ID of its first event only, so aggregated
// events are checked by the sum of the counts per kind.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const (
	stressWorkers = 8
	stressEvents  = 1400 // per worker: about 9000 plain lines, over 1 MiB, under 3 MiB
	gateLimit     = 150  // events in flight; keeps the 64 KiB buffer far from full
	advanceAt     = 20   // the clock moves whenever this many events are waiting
)

type kind struct {
	action Action
	target Target
	result Result
}

func kindOf(e Event) kind { return kind{e.Action, e.Target, e.Result} }

var (
	plainEvents = []Event{
		{User: "admin-test", Action: LoginSucceeded, Target: TargetSession, Result: Success},
		{User: "admin-test", Action: Logout, Target: TargetSession, Result: Success},
		{User: "admin-test", Action: PasswordChanged, Target: TargetPassword, Result: Success},
		{User: "admin-test", Action: PasswordChangeFailed, Target: TargetPassword, Result: WrongPassword},
		{User: "admin-test", Action: SetupCompleted, Target: TargetSetup, Result: Success},
	}
	aggregatedEvents = []Event{
		{User: NoUser, Action: LoginFailed, Target: TargetSession, Result: InvalidCredentials},
		{User: NoUser, Action: LoginFailed, Target: TargetSession, Result: RateLimited},
		{User: NoUser, Action: RequestRejected, Target: TargetPassword, Result: CSRFFailed},
	}
)

// eventID is a correlation ID (16 hex digits) that names one event.
func eventID(gen, worker, seq int) string { return fmt.Sprintf("%02x%02x%012x", gen, worker, seq) }

func withID(e Event, id string) Event { e.CorrelationID = id; return e }

// --- reading the files back ---------------------------------------------

// auditPaths lists the files oldest first: .3, .2, .1, then the current one.
func auditPaths(dir string) []string {
	var out []string
	for i := Files - 1; i >= 0; i-- {
		name := FileName
		if i > 0 {
			name += "." + strconv.Itoa(i)
		}
		out = append(out, filepath.Join(dir, DirName, name))
	}
	return out
}

type onDisk struct {
	entries []entry // oldest first
	files   int     // how many of the four files exist
}

// readAudit reads every audit file and fails the test on a torn, unknown or
// invalid line: a file must be whole lines, each one an entry with exactly
// the fixed fields and values from the closed sets.
func readAudit(t *testing.T, dir string) onDisk {
	t.Helper()
	var d onDisk
	for _, p := range auditPaths(dir) {
		data, err := os.ReadFile(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		d.files++
		if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %o, want 600", filepath.Base(p), fi.Mode().Perm())
		}
		if len(data) > MaxFileSize {
			t.Errorf("%s: %d bytes, above %d", filepath.Base(p), len(data), MaxFileSize)
		}
		if len(data) > 0 && data[len(data)-1] != '\n' {
			t.Errorf("%s: does not end with a newline (a torn last line)", filepath.Base(p))
		}
		for n, line := range bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n")) {
			if len(data) == 0 {
				break
			}
			var en entry
			dec := json.NewDecoder(bytes.NewReader(line))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&en); err != nil || dec.InputOffset() != int64(len(line)) {
				t.Errorf("%s line %d: torn or foreign line (%d bytes): %v", filepath.Base(p), n+1, len(line), err)
				continue
			}
			if _, err := time.Parse(time.RFC3339, en.Time); err != nil || en.Plugin != "" || en.Count < 1 ||
				(Event{User: en.User, Action: en.Action, Target: en.Target, Result: en.Result, CorrelationID: en.CorrelationID}).validate() != nil {
				t.Errorf("%s line %d: an entry outside the fixed fields and sets", filepath.Base(p), n+1)
				continue
			}
			d.entries = append(d.entries, en)
		}
	}
	return d
}

type tally struct {
	seen map[string]int // plain entries by event ID: how often each is in the files
	pos  map[string]int // position of the first sighting, in file order
	agg  map[kind]int   // aggregated entries: sum of the counts per kind
}

func tallyOf(t *testing.T, d onDisk) tally {
	t.Helper()
	ta := tally{seen: map[string]int{}, pos: map[string]int{}, agg: map[kind]int{}}
	for i, en := range d.entries {
		if aggregated[en.Action] {
			ta.agg[kind{en.Action, en.Target, en.Result}] += en.Count
			continue
		}
		if en.Count != 1 {
			t.Errorf("a plain entry with count %d", en.Count)
		}
		if ta.seen[en.CorrelationID] == 0 {
			ta.pos[en.CorrelationID] = i
		}
		ta.seen[en.CorrelationID]++
	}
	return ta
}

// --- the stress run -----------------------------------------------------

// ledger is what one worker recorded.
type ledger struct {
	plain     []string // IDs of its plain events, in order
	mustPlain int      // the first mustPlain of them were recorded before Close began
	agg       map[kind]int
	mustAgg   map[kind]int
	invalid   int
}

type stress struct {
	f        *fixture
	gen      int
	pending  atomic.Int64 // recorded since the clock last moved
	recorded atomic.Int64
	closing  atomic.Bool // set just before the first Close call
}

func newLedger() *ledger { return &ledger{agg: map[kind]int{}, mustAgg: map[kind]int{}} }

// work records n events: four plain ones, then an aggregated one, with an
// invalid event now and then. The gate keeps the number of unwritten events
// small; the production buffer is 64 KiB and a real flush runs every 30 s.
func (s *stress) work(w, n int, led *ledger) {
	for i := 0; i < n; i++ {
		for s.pending.Load() >= gateLimit {
			runtime.Gosched()
		}
		if i%97 == 96 {
			s.f.log.Record(Event{User: "Not A User", Action: Logout, Target: TargetSession, Result: Success, CorrelationID: eventID(s.gen, w, i)})
			led.invalid++
			continue
		}
		s.pending.Add(1)
		id := eventID(s.gen, w, i)
		if i%5 == 4 {
			ev := withID(aggregatedEvents[(i/5)%len(aggregatedEvents)], id)
			s.f.log.Record(ev)
			led.agg[kindOf(ev)]++
			if !s.closing.Load() {
				led.mustAgg[kindOf(ev)]++
			}
		} else {
			s.f.log.Record(withID(plainEvents[i%len(plainEvents)], id))
			led.plain = append(led.plain, id)
			if !s.closing.Load() {
				led.mustPlain = len(led.plain)
			}
		}
		s.recorded.Add(1)
	}
}

// tick moves the fake clock whenever events are waiting, by 30 s or by a
// minute, so timer flushes and aggregation windows fire while the workers run.
func (s *stress) tick(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	for n := 0; ; {
		select {
		case <-stop:
			return
		default:
		}
		p := s.pending.Load()
		if p < advanceAt {
			runtime.Gosched()
			continue
		}
		step := FlushDelay
		if n++; n%3 == 0 {
			step = AggregateWindow
		}
		s.f.clock.Advance(step)
		s.pending.Add(-p)
	}
}

// run starts the workers, the clock and a goroutine that flushes by hand,
// calls during for the concurrent part (it may be nil) and waits for all of
// them. It does not call Close.
func (s *stress) run(during func(total int64)) []*ledger {
	stop := make(chan struct{})
	tickDone := make(chan struct{})
	go s.tick(stop, tickDone)
	flushDone := make(chan struct{})
	go func() {
		defer close(flushDone)
		for {
			select {
			case <-stop:
				return
			default:
				s.f.log.Flush()
				runtime.Gosched()
			}
		}
	}()
	var wg sync.WaitGroup
	ledgers := make([]*ledger, stressWorkers)
	for w := range ledgers {
		ledgers[w] = newLedger()
		wg.Add(1)
		go func() { defer wg.Done(); s.work(w, stressEvents, ledgers[w]) }()
	}
	if during != nil {
		wg.Add(1)
		go func() { defer wg.Done(); during(int64(stressWorkers * stressEvents)) }()
	}
	wg.Wait()
	close(stop)
	<-tickDone
	<-flushDone
	return ledgers
}

// verify compares the files with what the workers recorded. Events whose
// Record returned before Close began (must) have to be there; events after
// that may or may not be (they are documented to be ignored), but then the
// ones that made it are a prefix of the worker's sequence, each exactly once.
// Without a concurrent Close, must and may are equal: everything is there.
func (s *stress) verify(t *testing.T, ledgers []*ledger) onDisk {
	t.Helper()
	d := readAudit(t, s.f.dir)
	ta := tallyOf(t, d)

	known := map[string]bool{}
	invalid := 0
	wantAgg, mustAgg := map[kind]int{}, map[kind]int{}
	lost := 0
	for w, led := range ledgers {
		invalid += led.invalid
		for k, c := range led.agg {
			wantAgg[k] += c
		}
		for k, c := range led.mustAgg {
			mustAgg[k] += c
		}
		present, last := 0, -1
		for i, id := range led.plain {
			known[id] = true
			switch c := ta.seen[id]; {
			case c > 1:
				t.Errorf("worker %d: event %d is in the files %d times", w, i, c)
			case c == 1:
				if present != i {
					t.Errorf("worker %d: event %d is present but an earlier one is missing", w, present)
				}
				if ta.pos[id] <= last {
					t.Errorf("worker %d: event %d is out of order in the files", w, i)
				}
				last = ta.pos[id]
				present++
			}
		}
		if present < led.mustPlain {
			t.Errorf("worker %d: %d of %d events recorded before Close are missing", w, led.mustPlain-present, led.mustPlain)
		}
		lost += len(led.plain) - present
	}
	for id := range ta.seen {
		if !known[id] {
			t.Errorf("an entry nobody recorded: %s", id)
		}
	}
	for k, c := range ta.agg {
		if c > wantAgg[k] {
			t.Errorf("%v: counts add up to %d, only %d events were recorded", k, c, wantAgg[k])
		}
	}
	for k, c := range mustAgg {
		if ta.agg[k] < c {
			t.Errorf("%v: counts add up to %d, %d events were recorded before Close", k, ta.agg[k], c)
		}
	}
	if st := s.f.log.Stats(); st.Failures != 0 || st.Dropped != invalid {
		t.Errorf("stats %+v, want no failed write and %d dropped invalid events", st, invalid)
	}
	s.f.mu.Lock()
	for _, w := range s.f.warns {
		if w != "audit: an event with invalid fields was dropped" {
			t.Errorf("warning %q", w)
		}
	}
	s.f.mu.Unlock()
	if s.f.clock.Pending() != 0 {
		t.Errorf("%d timers left after Close", s.f.clock.Pending())
	}
	if d.files < 2 {
		t.Errorf("only %d file: the run did not cross the rotation boundary", d.files)
	}
	if _, err := os.Stat(filepath.Join(s.f.dir, DirName, FileName+".3")); err == nil {
		t.Errorf("audit.jsonl.3 exists: the run was long enough to drop old entries, the test no longer counts all")
	}
	return d
}

// Many goroutines record plain and aggregated events while the clock fires
// timer flushes, another goroutine flushes by hand, and the files rotate.
// Close comes after the last event. All of it must be in the files, once.
func TestAuditDurabilityManyWritersAcrossRotation(t *testing.T) {
	s := &stress{f: newFixture(t)}
	ledgers := s.run(nil)
	s.f.log.Close()
	d := s.verify(t, ledgers)
	var plain int
	for _, led := range ledgers {
		plain += len(led.plain)
		if led.mustPlain != len(led.plain) {
			t.Fatal("test bug: nothing was recorded after Close here")
		}
	}
	if got := len(tallyOf(t, d).seen); got != plain {
		t.Errorf("%d plain entries in the files, %d recorded", got, plain)
	}
}

// The same, with two Close calls from other goroutines in the middle of the
// run. Events recorded before Close began must be written; the ones after are
// ignored by contract, never half written, doubled or reordered.
func TestAuditDurabilityCloseRunsConcurrently(t *testing.T) {
	s := &stress{f: newFixture(t)}
	var closers sync.WaitGroup
	ledgers := s.run(func(total int64) {
		for s.recorded.Load() < total*9/10 {
			runtime.Gosched()
		}
		s.closing.Store(true)
		for i := 0; i < 2; i++ {
			closers.Add(1)
			go func() { defer closers.Done(); s.f.log.Close() }()
		}
		closers.Wait()
	})
	s.f.log.Close() // a third, late call changes nothing
	s.verify(t, ledgers)
}

// --- deterministic cases ------------------------------------------------

// An event recorded right before Close, also right after a timer flush, as in
// a password change followed by a restart 2 s later. Close twice and a second
// Log on the same directory add nothing and lose nothing.
func TestAuditDurabilityEventRightBeforeClose(t *testing.T) {
	f := newFixture(t)
	id := func(i int) string { return eventID(0, 0, i) }
	f.log.Record(withID(plainEvents[2], id(0)))
	f.clock.Advance(FlushDelay) // the timer flush has written the first entry
	f.clock.Advance(time.Second)
	f.log.Record(withID(plainEvents[2], id(1))) // a new flush timer is armed
	for i := 0; i < 3; i++ {
		f.log.Record(withID(aggregatedEvents[0], id(10+i)))
	}
	f.clock.Advance(2 * time.Second)
	f.log.Close()
	f.log.Close()
	if f.clock.Pending() != 0 {
		t.Fatal("timers after Close")
	}
	ta := tallyOf(t, readAudit(t, f.dir))
	if ta.seen[id(0)] != 1 || ta.seen[id(1)] != 1 || len(ta.seen) != 2 || ta.agg[kindOf(aggregatedEvents[0])] != 3 {
		t.Fatalf("entries after Close: plain %v, aggregated %v", ta.seen, ta.agg)
	}

	// An event, then Close at once, with no clock movement at all.
	l2, err := Open(f.dir, f.clock, nil)
	if err != nil {
		t.Fatal(err)
	}
	l2.Record(withID(plainEvents[0], id(2)))
	l2.Record(withID(aggregatedEvents[2], id(20)))
	l2.Close()
	ta = tallyOf(t, readAudit(t, f.dir))
	if ta.seen[id(0)] != 1 || ta.seen[id(1)] != 1 || ta.seen[id(2)] != 1 || len(ta.seen) != 3 ||
		ta.agg[kindOf(aggregatedEvents[0])] != 3 || ta.agg[kindOf(aggregatedEvents[2])] != 1 {
		t.Fatalf("entries after the second Close: plain %v, aggregated %v", ta.seen, ta.agg)
	}
}

// blockedFlush starts a timer flush of one event and leaves it stuck in the
// open of audit.jsonl, which is a named pipe nobody reads yet. At that point
// the flush has taken its data out of the buffer and holds the write lock, as
// in the middle of a slow write. finish lets it go and turns what it wrote
// into the regular file the log would have written.
type blockedFlush struct {
	t    *testing.T
	f    *fixture
	path string
	done chan struct{}
}

func startBlockedFlush(t *testing.T, f *fixture, first Event) *blockedFlush {
	t.Helper()
	b := &blockedFlush{t: t, f: f, path: filepath.Join(f.dir, DirName, FileName), done: make(chan struct{})}
	if err := syscall.Mkfifo(b.path, 0o600); err != nil {
		t.Skipf("no named pipe here: %v", err)
	}
	f.log.Record(first)
	go func() { f.clock.Advance(FlushDelay); close(b.done) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		f.log.mu.Lock()
		taken := f.log.buf.Len() == 0 && f.log.flushTimer == nil
		f.log.mu.Unlock()
		if taken {
			return b
		}
		if time.Now().After(deadline) {
			t.Fatal("the timer flush did not start")
		}
		runtime.Gosched()
	}
}

// finish opens the pipe (which lets the blocked open go on), waits for every
// channel in wait, collects what was written, and replaces the pipe by a
// regular file with that content.
func (b *blockedFlush) finish(wait ...<-chan struct{}) {
	b.t.Helper()
	// Read and write at once: opening it lets the blocked open go on, and the
	// pipe never reports the end of the data while this descriptor is open.
	fd, err := syscall.Open(b.path, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		b.t.Fatal(err)
	}
	for _, w := range append([]<-chan struct{}{b.done}, wait...) {
		select {
		case <-w:
		case <-time.After(10 * time.Second):
			b.t.Fatal("a flush or Close is stuck")
		}
	}
	var data []byte
	buf := make([]byte, 4096)
	for {
		n, err := syscall.Read(fd, buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if err == syscall.EINTR {
			continue
		}
		if err != nil || n <= 0 {
			break // EAGAIN: the pipe is empty
		}
	}
	_ = syscall.Close(fd)
	if err := os.Remove(b.path); err != nil {
		b.t.Fatal(err)
	}
	if err := os.WriteFile(b.path, data, 0o600); err != nil {
		b.t.Fatal(err)
	}
}

// An event recorded while a timer flush is writing: Record does not wait for
// the disk, the event is not in the data of the running flush, and it is
// written by the next one.
func TestAuditDurabilityEventWhileFlushIsWriting(t *testing.T) {
	f := newFixture(t)
	b := startBlockedFlush(t, f, withID(plainEvents[0], eventID(0, 0, 0)))
	recorded := make(chan struct{})
	go func() {
		f.log.Record(withID(plainEvents[1], eventID(0, 0, 1)))
		f.log.Record(withID(aggregatedEvents[1], eventID(0, 0, 100)))
		f.log.Record(withID(aggregatedEvents[1], eventID(0, 0, 101)))
		f.log.Record(withID(plainEvents[2], eventID(0, 0, 2)))
		close(recorded)
	}()
	select {
	case <-recorded:
	case <-time.After(5 * time.Second):
		b.finish() // do not leave the flush stuck
		t.Fatal("Record waited for the running flush")
	}
	if f.clock.Pending() != 2 { // a new flush timer and the aggregation window
		t.Errorf("%d timers while the first flush is writing, want 2", f.clock.Pending())
	}
	b.finish()
	// Now the second flush, by its timer; then Close writes the open window.
	f.clock.Advance(FlushDelay)
	f.log.Close()
	ta := tallyOf(t, readAudit(t, f.dir))
	if len(ta.seen) != 3 || ta.seen[eventID(0, 0, 0)] != 1 || ta.seen[eventID(0, 0, 1)] != 1 || ta.seen[eventID(0, 0, 2)] != 1 ||
		ta.agg[kindOf(aggregatedEvents[1])] != 2 {
		t.Fatalf("entries: plain %v, aggregated %v", ta.seen, ta.agg)
	}
	if s := f.log.Stats(); s.Failures != 0 || s.Dropped != 0 {
		t.Errorf("stats %+v", s)
	}
}

// Close arrives while a timer flush is writing. It waits for that flush, then
// writes what was recorded in the meantime and the open aggregation window.
// An event after Close is ignored.
func TestAuditDurabilityCloseWhileFlushIsWriting(t *testing.T) {
	f := newFixture(t)
	b := startBlockedFlush(t, f, withID(plainEvents[0], eventID(0, 0, 0)))
	f.log.Record(withID(plainEvents[2], eventID(0, 0, 1)))
	f.log.Record(withID(aggregatedEvents[0], eventID(0, 0, 100)))
	closed := make(chan struct{})
	go func() { f.log.Close(); close(closed) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		f.log.mu.Lock()
		c := f.log.closed
		f.log.mu.Unlock()
		if c {
			break
		}
		if time.Now().After(deadline) {
			b.finish()
			t.Fatal("Close did not start")
		}
		runtime.Gosched()
	}
	select {
	case <-closed:
		b.finish()
		t.Fatal("Close returned while a flush was still writing")
	default:
	}
	f.log.Record(withID(plainEvents[3], eventID(0, 0, 2))) // after Close began: ignored
	b.finish(closed)
	ta := tallyOf(t, readAudit(t, f.dir))
	if len(ta.seen) != 2 || ta.seen[eventID(0, 0, 0)] != 1 || ta.seen[eventID(0, 0, 1)] != 1 || ta.agg[kindOf(aggregatedEvents[0])] != 1 {
		t.Fatalf("entries: plain %v, aggregated %v", ta.seen, ta.agg)
	}
	if f.clock.Pending() != 0 {
		t.Error("timers after Close")
	}
}

// A restart-like sequence on one directory: Close, a new Log, more events,
// Close, a third Log. The second Log continues the file where the first one
// stopped (it reads the size from disk), crosses the rotation boundary, and
// nothing is written twice.
func TestAuditDurabilityRestartLikeSequence(t *testing.T) {
	f := newFixture(t)
	line, _ := json.Marshal(entry{Time: "2026-10-28T12:00:00Z", User: "admin-test", Action: PasswordChanged, Target: TargetPassword,
		Result: Success, CorrelationID: eventID(0, 0, 0), Count: 1})
	perFile := MaxFileSize / (len(line) + 1)

	var want []string
	record := func(l *Log, gen, n int) {
		for i := 0; i < n; i++ {
			id := eventID(gen, 0, i)
			l.Record(withID(plainEvents[2], id))
			want = append(want, id)
			if i%100 == 99 {
				f.clock.Advance(FlushDelay)
			}
		}
	}
	// Generation 0: nearly one full file, a few aggregated events, then Close.
	record(f.log, 0, perFile-30)
	for i := 0; i < 4; i++ {
		f.log.Record(withID(aggregatedEvents[0], eventID(0, 1, i)))
	}
	f.log.Close()
	if d := readAudit(t, f.dir); d.files != 1 {
		t.Fatalf("%d files after the first generation, want 1", d.files)
	}

	// Generation 1: carries on in the same file, crosses the boundary, and
	// records one more event right before its Close.
	l1, err := Open(f.dir, f.clock, nil)
	if err != nil {
		t.Fatal(err)
	}
	record(l1, 1, 60)
	for i := 0; i < 2; i++ {
		l1.Record(withID(aggregatedEvents[2], eventID(1, 1, i)))
	}
	f.clock.Advance(AggregateWindow + FlushDelay)
	l1.Record(withID(plainEvents[2], eventID(1, 2, 0)))
	want = append(want, eventID(1, 2, 0))
	l1.Close()

	// Generation 2: a restart that finds both files.
	l2, err := Open(f.dir, f.clock, nil)
	if err != nil {
		t.Fatal(err)
	}
	record(l2, 2, 5)
	l2.Close()

	d := readAudit(t, f.dir)
	ta := tallyOf(t, d)
	if d.files != 2 {
		t.Errorf("%d files, want 2 (the second generation crossed the boundary once)", d.files)
	}
	if len(ta.seen) != len(want) {
		t.Errorf("%d plain entries in the files, %d recorded", len(ta.seen), len(want))
	}
	last := -1
	for _, id := range want {
		if ta.seen[id] != 1 {
			t.Errorf("event %s is in the files %d times", id, ta.seen[id])
		}
		if ta.pos[id] <= last {
			t.Errorf("event %s is out of order", id)
		}
		last = ta.pos[id]
	}
	if ta.agg[kindOf(aggregatedEvents[0])] != 4 || ta.agg[kindOf(aggregatedEvents[2])] != 2 {
		t.Errorf("aggregated counts %v, want 4 and 2", ta.agg)
	}
	if f.clock.Pending() != 0 {
		t.Error("timers after the last Close")
	}
}
