package metrics

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Defaults of the sampling model (ORB-DASH-006, ORB-NFR-003, ORB-NFR-009).
const (
	DefaultInterval    = 2 * time.Second        // between samples while somebody watches
	DefaultWarmup      = 500 * time.Millisecond // first sample after the baseline, so CPU usage exists at once
	DefaultMaxClients  = 4
	DefaultHistorySize = 60                     // points per series, 2 minutes at the default interval
	defaultOnceWindow  = 250 * time.Millisecond // between the two reads of a one-shot snapshot
	onceCacheTTL       = time.Second
)

// ErrTooManyClients is returned by Subscribe when the client limit is reached.
var ErrTooManyClients = errors.New("too many stream clients")

// Clock abstracts time so the sampler can be tested without waiting.
type Clock interface {
	Now() time.Time
	NewTimer(d time.Duration) Timer
}

// Timer is the part of *time.Timer the sampler uses.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
}

// SystemClock is the production Clock.
func SystemClock() Clock { return systemClock{} }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }
func (systemClock) NewTimer(d time.Duration) Timer {
	return systemTimer{time.NewTimer(d)}
}

type systemTimer struct{ *time.Timer }

func (t systemTimer) C() <-chan time.Time { return t.Timer.C }

// Config configures a Sampler.
type Config struct {
	Collector    *Collector
	Clock        Clock               // default SystemClock
	Interval     time.Duration       // default DefaultInterval
	Warmup       time.Duration       // default DefaultWarmup
	MaxClients   int                 // default DefaultMaxClients
	HistorySize  int                 // default DefaultHistorySize
	OnceWindow   time.Duration       // between the two reads of Once; default 250 ms
	AlertContext func() AlertContext // optional; read on every sample
}

// Frame is what a subscriber receives for each sample.
type Frame struct {
	Snapshot Snapshot
	Alerts   []Alert
	// AlertsVersion changes whenever the alert list differs from the one in the
	// previous frame, so a consumer can send alerts only when they changed.
	AlertsVersion uint64
}

// Sampler collects metrics only while at least one client is subscribed. With
// no client there is no goroutine, no timer and no file read. The recent
// samples are kept in memory only, and are dropped when the last client leaves.
type Sampler struct {
	cfg Config

	mu       sync.Mutex
	subs     map[*Subscription]struct{}
	cancel   context.CancelFunc
	gen      uint64 // identifies the running loop; changes when the loop stops
	done     chan struct{}
	latest   *Frame
	prevAl   []Alert
	alertVer uint64
	ring     ring

	onceMu     sync.Mutex
	onceFrame  Frame
	onceAt     time.Time
	onceCached bool
}

// NewSampler returns an idle Sampler.
func NewSampler(cfg Config) *Sampler {
	if cfg.Clock == nil {
		cfg.Clock = SystemClock()
	}
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.Warmup <= 0 {
		cfg.Warmup = DefaultWarmup
	}
	if cfg.MaxClients <= 0 {
		cfg.MaxClients = DefaultMaxClients
	}
	if cfg.HistorySize <= 0 {
		cfg.HistorySize = DefaultHistorySize
	}
	if cfg.OnceWindow <= 0 {
		cfg.OnceWindow = defaultOnceWindow
	}
	return &Sampler{cfg: cfg, subs: map[*Subscription]struct{}{}, ring: newRing(cfg.HistorySize)}
}

// Subscription is one watching client.
type Subscription struct {
	s    *Sampler
	ch   chan Frame
	once sync.Once
}

// C delivers frames. It holds at most one: when the client is slower than the
// sampler, the newest frame replaces the one it has not read. The channel is
// never closed; a client stops reading when it calls Close or leaves.
func (sub *Subscription) C() <-chan Frame { return sub.ch }

// Close ends the subscription. It is safe to call more than once. When the
// last subscription closes the sampler stops.
func (sub *Subscription) Close() { sub.once.Do(func() { sub.s.unsubscribe(sub) }) }

// Subscribe adds a watching client and starts sampling if it is the first. It
// fails with ErrTooManyClients when the limit is reached.
func (s *Sampler) Subscribe() (*Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.subs) >= s.cfg.MaxClients {
		return nil, ErrTooManyClients
	}
	sub := &Subscription{s: s, ch: make(chan Frame, 1)}
	s.subs[sub] = struct{}{}
	if s.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		s.gen++
		s.done = make(chan struct{})
		go s.run(ctx, s.gen, s.done)
	} else if s.latest != nil {
		sub.ch <- *s.latest // a client that joins a running session sees data at once
	}
	return sub, nil
}

func (s *Sampler) unsubscribe(sub *Subscription) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.subs[sub]; !ok {
		return
	}
	delete(s.subs, sub)
	if len(s.subs) > 0 {
		return
	}
	// Last client gone: stop the loop, forget the samples.
	s.cancel()
	s.cancel = nil
	s.gen++
	s.latest = nil
	s.prevAl = nil
	s.ring.reset()
}

// Clients returns the number of subscribed clients.
func (s *Sampler) Clients() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs)
}

// Running reports whether the sampling loop is active.
func (s *Sampler) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancel != nil
}

func (s *Sampler) run(ctx context.Context, gen uint64, done chan struct{}) {
	defer close(done)
	clock := s.cfg.Clock
	// The baseline read gives the CPU usage of the first real sample something
	// to compare with. It is not published.
	s.cfg.Collector.Collect(clock.Now())
	wait := s.cfg.Warmup
	for {
		timer := clock.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C():
		}
		snap := s.cfg.Collector.Collect(clock.Now())
		if ctx.Err() != nil {
			return
		}
		s.publish(gen, snap)
		wait = s.cfg.Interval
	}
}

func (s *Sampler) alertContext() AlertContext {
	if s.cfg.AlertContext == nil {
		return AlertContext{}
	}
	return s.cfg.AlertContext()
}

func (s *Sampler) publish(gen uint64, snap Snapshot) {
	alerts := Evaluate(snap, s.alertContext())

	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.gen {
		return // this loop was stopped (and maybe a new one started) meanwhile
	}
	if s.latest == nil || !sameAlerts(s.prevAl, alerts) {
		s.alertVer++
		s.prevAl = alerts
	}
	f := Frame{Snapshot: snap, Alerts: alerts, AlertsVersion: s.alertVer}
	s.latest = &f
	s.ring.add(snap)
	for sub := range s.subs {
		// Never block on a client. Only this method sends, and it holds s.mu,
		// so draining the old frame and sending the new one cannot race.
		select {
		case sub.ch <- f:
		default:
			select {
			case <-sub.ch:
			default:
			}
			sub.ch <- f
		}
	}
}

// History returns the recent samples, oldest first. It is empty when nobody is watching.
func (s *Sampler) History() History {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ring.history(s.cfg.Interval)
}

// Once returns one frame for GET /api/v1/metrics. While clients watch, it is
// the latest sample. Otherwise the device is read twice, a short window apart,
// so that the CPU usage exists; the result is reused for a second so that
// repeated requests cannot make the device work harder than the stream does.
func (s *Sampler) Once(ctx context.Context) (Frame, error) {
	s.mu.Lock()
	if s.latest != nil && s.cancel != nil {
		f := *s.latest
		s.mu.Unlock()
		return f, nil
	}
	s.mu.Unlock()

	s.onceMu.Lock()
	defer s.onceMu.Unlock()
	clock := s.cfg.Clock
	if s.onceCached && clock.Now().Sub(s.onceAt) < onceCacheTTL && !clock.Now().Before(s.onceAt) {
		return s.onceFrame, nil
	}
	s.cfg.Collector.Collect(clock.Now())
	timer := clock.NewTimer(s.cfg.OnceWindow)
	select {
	case <-ctx.Done():
		timer.Stop()
		return Frame{}, ctx.Err()
	case <-timer.C():
	}
	snap := s.cfg.Collector.Collect(clock.Now())
	f := Frame{Snapshot: snap, Alerts: Evaluate(snap, s.alertContext())}
	s.onceFrame, s.onceAt, s.onceCached = f, clock.Now(), true
	return f, nil
}

func sameAlerts(a, b []Alert) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameAlert(a[i], b[i]) {
			return false
		}
	}
	return true
}

func sameAlert(a, b Alert) bool {
	return a.ID == b.ID && a.Level == b.Level && sameMessage(a.Cause, b.Cause) &&
		sameMessage(a.Impact, b.Impact) && sameMessage(a.NextStep, b.NextStep)
}

func sameMessage(a, b Message) bool {
	if a.Key != b.Key || len(a.Params) != len(b.Params) {
		return false
	}
	for k, v := range a.Params {
		if w, ok := b.Params[k]; !ok || w != v {
			return false
		}
	}
	return true
}
