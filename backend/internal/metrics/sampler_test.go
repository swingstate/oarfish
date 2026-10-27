package metrics

import (
	"context"
	"errors"
	"io/fs"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

// fakeClock is a clock the test moves by hand. Timers fire only when the test
// advances the clock past their deadline, so the tests never wait.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers map[*fakeTimer]struct{}
	made   int // timers ever created
}

type fakeTimer struct {
	c        *fakeClock
	deadline time.Time
	ch       chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: t0, timers: map[*fakeTimer]struct{}{}}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) NewTimer(d time.Duration) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{c: c, deadline: c.now.Add(d), ch: make(chan time.Time, 1)}
	c.timers[t] = struct{}{}
	c.made++
	return t
}

func (t *fakeTimer) C() <-chan time.Time { return t.ch }
func (t *fakeTimer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	_, active := t.c.timers[t]
	delete(t.c.timers, t)
	return active
}

// Pending is the number of timers that could still fire.
func (c *fakeClock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

// waitForTimer blocks until a timer is pending: the goroutine under test has
// reached its wait.
func (c *fakeClock) waitForTimer(t testing.TB) {
	t.Helper()
	for i := 0; i < 2000; i++ {
		if c.Pending() > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no timer was created")
}

// Advance moves the clock and fires every timer that is due.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*fakeTimer
	for t := range c.timers {
		if !t.deadline.After(c.now) {
			due = append(due, t)
			delete(c.timers, t)
		}
	}
	now := c.now
	c.mu.Unlock()
	for _, t := range due {
		t.ch <- now
	}
}

// step lets the loop take exactly one sample: it waits for the loop's timer,
// fires it, and waits for the frame.
func step(t testing.TB, c *fakeClock, sub *Subscription, d time.Duration) Frame {
	t.Helper()
	c.waitForTimer(t)
	c.Advance(d)
	select {
	case f := <-sub.C():
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("no frame arrived")
		return Frame{}
	}
}

func newTestSampler(t testing.TB, clock Clock, cfg Config) (*Sampler, fstest.MapFS) {
	t.Helper()
	fsys := cloneFixture(t, "pi-zero-2w")
	cfg.Collector = NewCollector(fsys, fakeStatfs(map[string]Statfs{
		"/": {BlockSize: 4096, Blocks: 1000, Free: 500, Available: 500},
	}))
	cfg.Clock = clock
	return NewSampler(cfg), fsys
}

func waitDone(t testing.TB, s *Sampler) {
	t.Helper()
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the sampling goroutine did not stop")
	}
}

// countingFS counts every open, to prove that an idle sampler reads nothing.
type countingFS struct {
	inner fs.FS
	n     *atomic.Int64
}

func (c countingFS) Open(name string) (fs.File, error) {
	c.n.Add(1)
	return c.inner.Open(name)
}

// ORB-DASH-006, ORB-NFR-003: no client means no sampling and no timer wake-ups.
func TestNoSamplingWithoutClients(t *testing.T) {
	clock := newFakeClock()
	var reads atomic.Int64
	s := NewSampler(Config{Clock: clock, Collector: NewCollector(countingFS{cloneFixture(t, "pi-zero-2w"), &reads}, fakeStatfs(nil))})

	before := runtime.NumGoroutine()
	clock.Advance(time.Hour)
	if s.Running() || s.Clients() != 0 || clock.Pending() != 0 || clock.made != 0 || reads.Load() != 0 {
		t.Fatalf("an idle sampler is active: running=%v timers=%d made=%d reads=%d", s.Running(), clock.Pending(), clock.made, reads.Load())
	}
	if h := s.History(); len(h.CPUPercent) != 0 || h.CPUPercent == nil {
		t.Errorf("history while idle must be an empty list: %+v", h)
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Errorf("goroutines grew from %d to %d", before, n)
	}
}

// ORB-DASH-006: sampling starts with the first client, runs at the interval,
// and stops with the last one.
func TestSamplerStartsAndStopsWithClients(t *testing.T) {
	clock := newFakeClock()
	s, fsys := newTestSampler(t, clock, Config{})

	sub, err := s.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	if !s.Running() || s.Clients() != 1 {
		t.Fatalf("running=%v clients=%d", s.Running(), s.Clients())
	}

	// The loop has taken its baseline and waits for the warm-up. Make the CPU busy.
	clock.waitForTimer(t)
	fsys["proc/stat"] = &fstest.MapFile{Data: []byte("cpu  41400 310 15430 2291600 1210 0 2210 0 0 0\ncpu0 1\ncpu1 1\n")}
	f := step(t, clock, sub, DefaultWarmup)
	if f.Snapshot.CPU.UsagePercent == nil || *f.Snapshot.CPU.UsagePercent != 20 {
		t.Errorf("the first published sample must carry CPU usage, got %s", mustJSON(t, f.Snapshot.CPU.UsagePercent))
	}
	if !f.Snapshot.CollectedAt.Equal(t0.Add(DefaultWarmup)) {
		t.Errorf("collected at %v", f.Snapshot.CollectedAt)
	}

	// The next sample comes after the interval, not before.
	clock.waitForTimer(t)
	clock.Advance(DefaultInterval - time.Millisecond)
	select {
	case f := <-sub.C():
		t.Fatalf("a sample arrived early: %v", f.Snapshot.CollectedAt)
	case <-time.After(30 * time.Millisecond):
	}
	clock.Advance(time.Millisecond)
	select {
	case f = <-sub.C():
	case <-time.After(5 * time.Second):
		t.Fatal("no sample after the interval")
	}
	if !f.Snapshot.CollectedAt.Equal(t0.Add(DefaultWarmup + DefaultInterval)) {
		t.Errorf("collected at %v", f.Snapshot.CollectedAt)
	}

	sub.Close()
	waitDone(t, s)
	if s.Running() || s.Clients() != 0 || clock.Pending() != 0 {
		t.Errorf("after the last client left: running=%v clients=%d timers=%d", s.Running(), s.Clients(), clock.Pending())
	}
	if h := s.History(); len(h.CPUPercent) != 0 {
		t.Errorf("the history survived the last client: %+v", h)
	}
	sub.Close() // closing twice is harmless

	// A new client starts a new session.
	sub2, err := s.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer sub2.Close()
	step(t, clock, sub2, DefaultWarmup)
	if !s.Running() {
		t.Error("the sampler did not restart")
	}
}

// The sampler never reads the host after the last client left.
func TestNoReadsAfterTheLastClientLeaves(t *testing.T) {
	clock := newFakeClock()
	var reads atomic.Int64
	s := NewSampler(Config{Clock: clock, Collector: NewCollector(countingFS{cloneFixture(t, "pi-zero-2w"), &reads}, fakeStatfs(nil))})
	sub, _ := s.Subscribe()
	step(t, clock, sub, DefaultWarmup)
	sub.Close()
	waitDone(t, s)
	n := reads.Load()
	clock.Advance(time.Hour)
	time.Sleep(20 * time.Millisecond)
	if reads.Load() != n || clock.Pending() != 0 {
		t.Errorf("reads grew from %d to %d, timers %d", n, reads.Load(), clock.Pending())
	}
}

// The ring holds at most HistorySize points per series, oldest dropped first,
// and the three series stay aligned.
func TestHistoryRingIsBounded(t *testing.T) {
	clock := newFakeClock()
	s, fsys := newTestSampler(t, clock, Config{HistorySize: 5})
	sub, _ := s.Subscribe()
	defer sub.Close()

	var temps []float64
	for i := 0; i < 12; i++ {
		temp := 40000 + 1000*i
		fsys["sys/class/thermal/thermal_zone0/temp"] = &fstest.MapFile{Data: []byte(itoa(temp) + "\n")}
		d := DefaultInterval
		if i == 0 {
			d = DefaultWarmup
		}
		step(t, clock, sub, d)
		temps = append(temps, float64(temp)/1000)
	}
	h := s.History()
	if len(h.CPUPercent) != 5 || len(h.MemoryUsedPercent) != 5 || len(h.TemperatureCelsius) != 5 {
		t.Fatalf("lengths %d %d %d, want 5", len(h.CPUPercent), len(h.MemoryUsedPercent), len(h.TemperatureCelsius))
	}
	for i, p := range h.TemperatureCelsius {
		if p == nil || *p != temps[len(temps)-5+i] {
			t.Errorf("temperature point %d = %v, want %v", i, p, temps[len(temps)-5+i])
		}
	}
	if h.IntervalSeconds != 2 || h.SchemaVersion != SchemaVersion {
		t.Errorf("history header = %+v", h)
	}
	if h.MemoryUsedPercent[0] == nil || *h.MemoryUsedPercent[0] != 36.9 { // (415408-262144)/415408
		t.Errorf("memory point = %v", h.MemoryUsedPercent[0])
	}

	// The history is a copy: changing it does not change the ring.
	h.TemperatureCelsius[0] = nil
	if s.History().TemperatureCelsius[0] == nil {
		t.Error("History returned the ring itself")
	}
}

// The default history is about 60 points per series.
func TestDefaultHistorySize(t *testing.T) {
	if NewSampler(Config{Collector: NewCollector(fstest.MapFS{}, fakeStatfs(nil))}).cfg.HistorySize != 60 {
		t.Error("the default ring is not 60 points")
	}
}

// The client limit: the fifth client is refused, a free slot is reusable.
func TestClientCap(t *testing.T) {
	clock := newFakeClock()
	s, _ := newTestSampler(t, clock, Config{MaxClients: 2})
	a, err := s.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Subscribe(); !errors.Is(err, ErrTooManyClients) {
		t.Fatalf("third client: %v", err)
	}
	if s.Clients() != 2 {
		t.Errorf("clients = %d", s.Clients())
	}
	a.Close()
	c, err := s.Subscribe()
	if err != nil {
		t.Fatalf("a slot was freed but Subscribe failed: %v", err)
	}
	b.Close()
	c.Close()
	waitDone(t, s)
	if DefaultMaxClients != 4 {
		t.Errorf("the default cap is %d, the documented one is 4", DefaultMaxClients)
	}
}

// A client that stops reading must neither block the sampler nor hold more
// than one frame.
func TestSlowClientDoesNotBlockAndKeepsTheNewestFrame(t *testing.T) {
	clock := newFakeClock()
	s, _ := newTestSampler(t, clock, Config{})
	slow, _ := s.Subscribe()
	defer slow.Close()
	fast, _ := s.Subscribe()
	defer fast.Close()

	var last Frame
	for i := 0; i < 20; i++ {
		d := DefaultInterval
		if i == 0 {
			d = DefaultWarmup
		}
		last = step(t, clock, fast, d) // fast keeps reading
	}
	if len(slow.ch) != 1 {
		t.Fatalf("the slow client holds %d frames", len(slow.ch))
	}
	got := <-slow.C()
	if !got.Snapshot.CollectedAt.Equal(last.Snapshot.CollectedAt) {
		t.Errorf("slow client got %v, newest is %v", got.Snapshot.CollectedAt, last.Snapshot.CollectedAt)
	}
}

// A client that joins a running session gets the latest sample at once.
func TestLateClientGetsTheLatestFrame(t *testing.T) {
	clock := newFakeClock()
	s, _ := newTestSampler(t, clock, Config{})
	first, _ := s.Subscribe()
	defer first.Close()
	f := step(t, clock, first, DefaultWarmup)

	late, err := s.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer late.Close()
	select {
	case got := <-late.C():
		if !got.Snapshot.CollectedAt.Equal(f.Snapshot.CollectedAt) {
			t.Errorf("late client got %v", got.Snapshot.CollectedAt)
		}
	default:
		t.Error("the late client has no frame")
	}
}

// ORB-DASH-005: the alert list is part of each frame; its version changes only
// when the list changes.
func TestAlertsVersionChangesOnlyWhenAlertsChange(t *testing.T) {
	clock := newFakeClock()
	s, fsys := newTestSampler(t, clock, Config{AlertContext: func() AlertContext { return AlertContext{SupportTier: "compatible"} }})
	sub, _ := s.Subscribe()
	defer sub.Close()

	f1 := step(t, clock, sub, DefaultWarmup)
	f2 := step(t, clock, sub, DefaultInterval)
	if f1.AlertsVersion == 0 || f2.AlertsVersion != f1.AlertsVersion {
		t.Errorf("versions %d, %d: unchanged alerts must keep the version", f1.AlertsVersion, f2.AlertsVersion)
	}
	hasID := func(f Frame, id string) bool {
		for _, a := range f.Alerts {
			if a.ID == id {
				return true
			}
		}
		return false
	}
	if !hasID(f2, "support_compatible") || hasID(f2, "cpu_temperature") {
		t.Fatalf("alerts = %s", mustJSON(t, f2.Alerts))
	}

	fsys["sys/class/thermal/thermal_zone0/temp"] = &fstest.MapFile{Data: []byte("75000\n")}
	f3 := step(t, clock, sub, DefaultInterval)
	if f3.AlertsVersion == f2.AlertsVersion || !hasID(f3, "cpu_temperature") {
		t.Errorf("a new alert did not change the version: %d -> %d, %s", f2.AlertsVersion, f3.AlertsVersion, mustJSON(t, f3.Alerts))
	}
	// A different value of the same alert is a change too (the text shows it).
	fsys["sys/class/thermal/thermal_zone0/temp"] = &fstest.MapFile{Data: []byte("76000\n")}
	f4 := step(t, clock, sub, DefaultInterval)
	if f4.AlertsVersion == f3.AlertsVersion {
		t.Error("changed alert parameters kept the version")
	}
	f5 := step(t, clock, sub, DefaultInterval)
	if f5.AlertsVersion != f4.AlertsVersion {
		t.Error("identical alerts changed the version")
	}
}

// No goroutine is left behind after clients connect and disconnect, whether
// they read or not.
func TestNoGoroutineLeakAfterDisconnect(t *testing.T) {
	clock := newFakeClock()
	s, _ := newTestSampler(t, clock, Config{})
	runtime.GC()
	before := runtime.NumGoroutine()

	for i := 0; i < 50; i++ {
		a, _ := s.Subscribe()
		b, _ := s.Subscribe()
		if i%3 == 0 {
			clock.waitForTimer(t)
			clock.Advance(DefaultWarmup) // a frame is waiting that nobody reads
		}
		b.Close()
		a.Close()
		waitDone(t, s)
	}
	for i := 0; i < 200 && runtime.NumGoroutine() > before; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Errorf("goroutines: %d before, %d after", before, n)
	}
	if clock.Pending() != 0 || s.Running() {
		t.Errorf("timers=%d running=%v", clock.Pending(), s.Running())
	}
}

// Many clients connecting and leaving at once, with the real clock, under the
// race detector in CI.
func TestConcurrentSubscribeAndClose(t *testing.T) {
	fsys := cloneFixture(t, "pi-zero-2w")
	s := NewSampler(Config{
		Collector: NewCollector(fsys, fakeStatfs(nil)), Interval: time.Millisecond, Warmup: time.Millisecond, MaxClients: 1000,
	})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				sub, err := s.Subscribe()
				if err != nil {
					t.Error(err)
					return
				}
				if i%2 == 0 {
					select {
					case <-sub.C():
					case <-time.After(time.Millisecond):
					}
				}
				s.History()
				sub.Close()
			}
		}()
	}
	wg.Wait()
	waitDone(t, s)
	if s.Running() || s.Clients() != 0 {
		t.Errorf("running=%v clients=%d", s.Running(), s.Clients())
	}
}

// GET /api/v1/metrics without a stream: two reads a short window apart so CPU
// usage exists, reused for a second, and never started as a session.
func TestOnceWithoutClients(t *testing.T) {
	clock := newFakeClock()
	var reads atomic.Int64
	fsys := cloneFixture(t, "pi-zero-2w")
	s := NewSampler(Config{Clock: clock, Collector: NewCollector(countingFS{fsys, &reads}, fakeStatfs(nil))})

	type result struct {
		f   Frame
		err error
	}
	res := make(chan result, 1)
	go func() {
		f, err := s.Once(context.Background())
		res <- result{f, err}
	}()
	clock.waitForTimer(t)
	fsys["proc/stat"] = &fstest.MapFile{Data: []byte("cpu  41300 310 15330 2290800 1210 0 2210 0 0 0\ncpu0 1\n")}
	clock.Advance(defaultOnceWindow)
	r := <-res
	if r.err != nil || r.f.Snapshot.CPU.UsagePercent == nil || *r.f.Snapshot.CPU.UsagePercent != 20 {
		t.Fatalf("once = %+v, %v", r.f.Snapshot.CPU, r.err)
	}
	if s.Running() || clock.Pending() != 0 {
		t.Error("Once started a sampling session")
	}

	// A second request within a second is served from the cache.
	n := reads.Load()
	clock.Advance(500 * time.Millisecond)
	if f, err := s.Once(context.Background()); err != nil || !f.Snapshot.CollectedAt.Equal(r.f.Snapshot.CollectedAt) || reads.Load() != n {
		t.Errorf("not cached: %v %v, reads %d -> %d", f.Snapshot.CollectedAt, err, n, reads.Load())
	}

	// A cancelled request returns promptly and leaves no timer.
	clock.Advance(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Once(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled Once: %v", err)
	}
	if clock.Pending() != 0 {
		t.Error("a cancelled Once left a timer")
	}
}

// While clients watch, Once returns the stream's latest sample.
func TestOnceWhileWatchedReturnsTheLatestSample(t *testing.T) {
	clock := newFakeClock()
	s, _ := newTestSampler(t, clock, Config{})
	sub, _ := s.Subscribe()
	defer sub.Close()
	f := step(t, clock, sub, DefaultWarmup)
	got, err := s.Once(context.Background())
	if err != nil || !got.Snapshot.CollectedAt.Equal(f.Snapshot.CollectedAt) {
		t.Errorf("Once = %v, %v", got.Snapshot.CollectedAt, err)
	}
}
