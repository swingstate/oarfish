//go:build !linux

package main

// The helper is a Linux program. Other systems only compile it so that the
// repository's tests run on a development machine; the tests inject the clock.
func bootSeconds() (int64, error) { return 0, errNoClock }
