package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// bootSeconds is CLOCK_BOOTTIME in whole seconds. It is compared with
// /proc/uptime, which the unit hides but a test may read.
func TestBootSecondsIsClockBoottime(t *testing.T) {
	a, err := bootSeconds()
	if err != nil || a <= 0 {
		t.Fatalf("bootSeconds = %d, %v", a, err)
	}
	b, _ := bootSeconds()
	if b < a || b-a > 5 {
		t.Fatalf("not monotonic: %d then %d", a, b)
	}
	raw, err := os.ReadFile("/proc/uptime")
	if err != nil {
		t.Skip("no /proc/uptime")
	}
	f, _ := strconv.ParseFloat(strings.Fields(string(raw))[0], 64)
	if d := float64(b) - f; d < -2 || d > 2 { // /proc/uptime is CLOCK_BOOTTIME as well
		t.Fatalf("bootSeconds %d, /proc/uptime %.2f", b, f)
	}
}
