package auth

import (
	"bufio"
	"context"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
)

// procStatusKB reads one "Name:   1234 kB" line of /proc/self/status. It is
// absent outside Linux; the benchmark then reports the Go heap figures only.
func procStatusKB(name string) (float64, bool) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(sc.Text(), name+":"); ok {
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				n, err := strconv.ParseFloat(fields[0], 64)
				return n, err == nil
			}
		}
	}
	return 0, false
}

func benchmarkVerify(b *testing.B, h *Hasher) {
	ctx := context.Background()
	stored, err := h.Hash(ctx, sentinelPassword)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if ok, _, err := h.Verify(ctx, stored, sentinelPassword); !ok || err != nil {
			b.Fatal("verification failed")
		}
	}
	b.StopTimer()
	// What the idle process keeps after the last hash: heap the runtime holds
	// but does not use, and on Linux the resident size and its peak.
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	b.ReportMetric(float64(ms.HeapIdle-ms.HeapReleased)/(1<<20), "retained-MiB")
	if kb, ok := procStatusKB("VmHWM"); ok {
		b.ReportMetric(kb, "VmHWM-kB")
	}
	if kb, ok := procStatusKB("VmRSS"); ok {
		b.ReportMetric(kb, "VmRSS-kB")
	}
}

// BenchmarkLoginVerify is one login-equivalent Argon2id verification with the
// production parameters (ADR 0009, E1: target below about 500 ms on the
// Raspberry Pi Zero 2 W). Run it on the device with the binary that
// `make bench-arm64` builds (docs/AUTH.md, "Measurements on the device").
func BenchmarkLoginVerify(b *testing.B) {
	benchmarkVerify(b, NewHasher())
}

// BenchmarkLoginVerifyWithoutFreeOSMemory is the same without returning the
// memory after each hash, to show what debug.FreeOSMemory changes.
func BenchmarkLoginVerifyWithoutFreeOSMemory(b *testing.B) {
	debug.FreeOSMemory() // start from the same state as the other benchmark
	benchmarkVerify(b, newHasher(CurrentParams, nil))
}
