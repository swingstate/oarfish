// Package metricstest helps tests of the metrics and server packages: it opens
// the synthetic fixture trees under internal/metrics/testdata as file sources,
// offers a file source a test can change while a sampler reads it, a statfs
// stand-in, and a clock the test moves by hand. It is only imported by tests.
package metricstest

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/swingstate/oarfish/backend/internal/metrics"
)

// Dir returns the absolute path of the testdata directory.
func Dir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "testdata")
}

// Fixture returns the named fixture as a read-only file source.
func Fixture(t testing.TB, name string) fs.FS {
	t.Helper()
	dir := filepath.Join(Dir(), name)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("metrics fixture %q not found at %s", name, dir)
	}
	return os.DirFS(dir)
}

// Live is a file source that starts as a copy of a fixture and that a test
// can change while a sampler reads it from another goroutine.
type Live struct {
	mu sync.RWMutex
	m  fstest.MapFS
}

// NewLive copies the named fixture into memory.
func NewLive(t testing.TB, name string) *Live {
	t.Helper()
	src := Fixture(t, name)
	m := fstest.MapFS{}
	err := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(src, p)
		m[p] = &fstest.MapFile{Data: data}
		return err
	})
	if err != nil {
		t.Fatalf("clone fixture %q: %v", name, err)
	}
	return &Live{m: m}
}

// Set replaces the content of one file.
func (l *Live) Set(name, content string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.m[name] = &fstest.MapFile{Data: []byte(content)}
}

// Open implements fs.FS.
func (l *Live) Open(name string) (fs.File, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.m.Open(name)
}

// Statfs answers for the mount points it knows and fails for all others.
func Statfs(sizes map[string]metrics.Statfs) metrics.StatfsFunc {
	return func(path string) (metrics.Statfs, error) {
		if s, ok := sizes[path]; ok {
			return s, nil
		}
		return metrics.Statfs{}, errors.New("no such mount")
	}
}

// Clock is a metrics.Clock the test moves by hand.
type Clock struct {
	mu     sync.Mutex
	now    time.Time
	timers map[*timer]struct{}
}

type timer struct {
	c        *Clock
	deadline time.Time
	ch       chan time.Time
}

// NewClock starts a clock at a fixed instant.
func NewClock() *Clock {
	return &Clock{now: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC), timers: map[*timer]struct{}{}}
}

// Now implements metrics.Clock.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// NewTimer implements metrics.Clock.
func (c *Clock) NewTimer(d time.Duration) metrics.Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &timer{c: c, deadline: c.now.Add(d), ch: make(chan time.Time, 1)}
	c.timers[t] = struct{}{}
	return t
}

func (t *timer) C() <-chan time.Time { return t.ch }
func (t *timer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	_, active := t.c.timers[t]
	delete(t.c.timers, t)
	return active
}

// Pending is the number of timers that could still fire.
func (c *Clock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

// Advance moves the clock and fires every timer that is due.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*timer
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
