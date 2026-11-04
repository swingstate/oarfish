package main

import (
	"runtime"
	"testing"
)

// The copy streams: staging a bundle of at least 32 MiB allocates a small
// fraction of its size (32 KiB buffers, the gzip window), not the size itself
// (ADR 0010 U12, A4: the memory limit of the unit is not enforced on the
// reference image, so the program has to stay small).
func TestStageDoesNotAllocateInProportionToTheBundle(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 32 MiB bundle")
	}
	v := newTenv(t)
	r := v.release("1.1.0")
	r.setFile("oarfish", append(elfStub("huge"), incompressible(33<<20)...))
	b := r.build()
	if len(b.Bundle) < 32<<20 || len(b.Bundle) > maxBundleFile {
		t.Fatalf("bundle of %d bytes", len(b.Bundle))
	}
	v.place("1.1.0", b)
	r, b = nil, built{} // the fixture is not part of the measurement
	runtime.GC()

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	raw := v.stage("1.1.0")
	runtime.ReadMemStats(&after)
	wantOK(t, raw)

	grown := after.TotalAlloc - before.TotalAlloc
	t.Logf("stage of a %d byte bundle allocated %d bytes in total", 33<<20, grown)
	if grown > 4<<20 {
		t.Fatalf("allocated %d bytes for a bundle of more than 32 MiB", grown)
	}
}
