package auth

import (
	"testing"
	"time"

	"github.com/swingstate/oarfish/backend/internal/clock/clocktest"
)

func TestBackoffDoublesFromOneSecondToFifteenMinutes(t *testing.T) {
	want := map[int]time.Duration{5: time.Second, 6: 2 * time.Second, 7: 4 * time.Second, 14: 512 * time.Second, 15: 15 * time.Minute, 40: 15 * time.Minute, 64: 15 * time.Minute}
	for n, d := range want {
		if got := backoff(n); got != d {
			t.Errorf("backoff(%d) = %v, want %v", n, got, d)
		}
	}
}

// ORB-AUTH-003, E6: from the fifth consecutive failure the next attempt is
// refused for a delay that doubles; a refused attempt changes nothing; a
// success resets the count; there is no hard lockout.
func TestLimiterBackoff(t *testing.T) {
	c := clocktest.New()
	l := NewLimiter(c)
	fail := func() {
		t.Helper()
		if ok, wait := l.Allow(); !ok {
			t.Fatalf("refused (wait %v) before the backoff", wait)
		}
		l.Failure()
	}
	for i := 0; i < 4; i++ {
		fail()
	}
	fail() // the fifth failure
	ok, wait := l.Allow()
	if ok || wait != time.Second {
		t.Fatalf("after five failures: ok %v, wait %v; want refused for 1 s", ok, wait)
	}
	for i := 0; i < 20; i++ { // refused attempts neither count nor extend the wait
		l.Allow()
	}
	c.Advance(time.Second)
	fail() // the sixth
	if _, wait := l.Allow(); wait != 2*time.Second {
		t.Fatalf("after six failures wait %v, want 2 s", wait)
	}
	c.Advance(2 * time.Second)
	if ok, _ := l.Allow(); !ok {
		t.Fatal("still refused after the wait")
	}
	l.Success()
	for i := 0; i < 4; i++ {
		c.Advance(time.Minute) // stay clear of the per-minute window
		fail()
	}
	if ok, _ := l.Allow(); !ok {
		t.Fatal("the success did not reset the count")
	}
}

// E6: no hard lockout: even after many failures the wait is at most 15 minutes.
func TestLimiterHasNoHardLockout(t *testing.T) {
	c := clocktest.New()
	l := NewLimiter(c)
	for i := 0; i < 100; i++ {
		c.Advance(BackoffMax)
		if ok, _ := l.Allow(); !ok {
			t.Fatalf("refused after waiting %v at failure %d", BackoffMax, i)
		}
		l.Failure()
	}
	if _, wait := l.Allow(); wait > BackoffMax {
		t.Fatalf("wait %v exceeds %v", wait, BackoffMax)
	}
}

// E6: at most ten attempts per minute overall, failed or not.
func TestLimiterTenAttemptsPerMinute(t *testing.T) {
	c := clocktest.New()
	l := NewLimiter(c)
	for i := 0; i < AttemptsPerWindow; i++ {
		if ok, _ := l.Allow(); !ok {
			t.Fatalf("attempt %d refused", i+1)
		}
		l.Success()
		c.Advance(time.Second)
	}
	ok, wait := l.Allow()
	if ok || wait != 50*time.Second {
		t.Fatalf("the 11th attempt in a minute: ok %v, wait %v; want refused for 50 s", ok, wait)
	}
	c.Advance(wait)
	if ok, _ := l.Allow(); !ok {
		t.Fatal("refused after the oldest attempt left the window")
	}
}
