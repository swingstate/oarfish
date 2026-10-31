// Package clock abstracts time for the access-control and audit code, so that
// expiry, backoff and flush timing can be tested without waiting. Tests use
// clocktest.Fake; production code uses System.
package clock

import "time"

// Clock is the time source. AfterFunc runs f in its own goroutine once d has
// passed, like time.AfterFunc.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer is the part of *time.Timer the callers use.
type Timer interface {
	// Stop prevents the timer from firing. It returns false if the timer has
	// already fired or been stopped.
	Stop() bool
}

// System is the production clock.
func System() Clock { return system{} }

type system struct{}

func (system) Now() time.Time { return time.Now() }

func (system) AfterFunc(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }
