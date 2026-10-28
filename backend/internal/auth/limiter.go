package auth

import (
	"sync"
	"time"

	"github.com/swingstate/oarfish/backend/internal/clock"
)

// Rate limit of login and setup attempts (ADR 0009, E6). One global limiter:
// behind an SSH tunnel every client is loopback, so per-address limits would
// not limit anything.
const (
	// FailuresBeforeBackoff consecutive failures start the backoff: after the
	// fifth failure the next attempt must wait BackoffStart, after the sixth
	// twice as long, and so on up to BackoffMax.
	FailuresBeforeBackoff = 5
	BackoffStart          = time.Second
	BackoffMax            = 15 * time.Minute
	// AttemptsPerWindow attempts are processed per Window overall.
	AttemptsPerWindow = 10
	Window            = time.Minute
)

// Limiter decides whether an attempt may be processed, before its hash is
// computed. Its state is a counter, a deadline and at most AttemptsPerWindow
// timestamps. There is no hard lockout: the wait is bounded by BackoffMax.
type Limiter struct {
	clock clock.Clock

	mu           sync.Mutex
	failures     int
	blockedUntil time.Time
	recent       [AttemptsPerWindow]time.Time // ring of the last processed attempts
	next         int
}

// NewLimiter returns a limiter with no history.
func NewLimiter(c clock.Clock) *Limiter { return &Limiter{clock: c} }

// Allow reports whether an attempt may be processed now. If it may, the
// attempt is counted against the window. If not, retryAfter is how long the
// caller must wait; a refused attempt changes nothing, so a refused attempt
// with the correct password is refused as well and does not extend the wait.
func (l *Limiter) Allow() (ok bool, retryAfter time.Duration) {
	now := l.clock.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Before(l.blockedUntil) {
		return false, l.blockedUntil.Sub(now)
	}
	// The oldest of the last AttemptsPerWindow attempts must have left the window.
	oldest := l.recent[l.next]
	if !oldest.IsZero() && now.Sub(oldest) < Window {
		return false, Window - now.Sub(oldest)
	}
	l.recent[l.next] = now
	l.next = (l.next + 1) % AttemptsPerWindow
	return true, 0
}

// Failure records a processed attempt that failed.
func (l *Limiter) Failure() {
	now := l.clock.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failures < 64 {
		l.failures++
	}
	if l.failures >= FailuresBeforeBackoff {
		l.blockedUntil = now.Add(backoff(l.failures))
	}
}

// Success resets the consecutive failure count.
func (l *Limiter) Success() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures = 0
	l.blockedUntil = time.Time{}
}

// backoff is the wait after the n-th consecutive failure (n >= 5).
func backoff(n int) time.Duration {
	d := BackoffStart
	for i := FailuresBeforeBackoff; i < n && d < BackoffMax; i++ {
		d *= 2
	}
	return min(d, BackoffMax)
}
