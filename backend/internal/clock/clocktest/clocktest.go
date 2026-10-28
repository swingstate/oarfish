// Package clocktest provides a clock that tests move by hand. It is imported by
// tests only (an architecture test checks that).
package clocktest

import (
	"sync"
	"time"

	"github.com/swingstate/oarfish/backend/internal/clock"
)

// Start is the time a new Fake begins at.
var Start = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

// Fake is a clock.Clock whose time only moves when Advance is called. Due
// timers run synchronously inside Advance, in deadline order, so a test sees
// their effect as soon as Advance returns.
type Fake struct {
	mu     sync.Mutex
	now    time.Time
	timers map[*timer]struct{}
}

type timer struct {
	c        *Fake
	deadline time.Time
	f        func()
}

// New returns a Fake at Start.
func New() *Fake { return &Fake{now: Start, timers: map[*timer]struct{}{}} }

// Now returns the fake time.
func (c *Fake) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// AfterFunc registers f to run when the clock passes now+d.
func (c *Fake) AfterFunc(d time.Duration, f func()) clock.Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &timer{c: c, deadline: c.now.Add(d), f: f}
	c.timers[t] = struct{}{}
	return t
}

func (t *timer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	_, active := t.c.timers[t]
	delete(t.c.timers, t)
	return active
}

// Pending is the number of timers that could still fire. A test that expects
// no background work checks that it is zero.
func (c *Fake) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

// Advance moves the clock forward by d and runs every timer that became due,
// including timers that those timers register within the same span.
func (c *Fake) Advance(d time.Duration) {
	c.mu.Lock()
	target := c.now.Add(d)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		var next *timer
		for t := range c.timers {
			if !t.deadline.After(target) && (next == nil || t.deadline.Before(next.deadline)) {
				next = t
			}
		}
		if next == nil {
			c.now = target
			c.mu.Unlock()
			return
		}
		delete(c.timers, next)
		if next.deadline.After(c.now) {
			c.now = next.deadline
		}
		c.mu.Unlock()
		next.f()
	}
}
