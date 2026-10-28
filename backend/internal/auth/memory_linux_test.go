package auth

import (
	"context"
	"testing"
)

// E1, ORB-NFR-001: on Linux, the resident size returns close to where it was
// after a hash with the production parameters, because the hasher hands the
// 19 MiB block back to the kernel at once (FreeOSMemory; Go releases with
// MADV_DONTNEED on Linux). CI runs this without the race detector and logs the
// numbers; the device measurement is in docs/AUTH.md.
func TestHashMemoryIsReturnedToTheOS(t *testing.T) {
	if raceEnabled {
		t.Skip("resident size is meaningless under the race detector")
	}
	if testing.Short() {
		t.Skip("runs real hashes")
	}
	before, ok := procStatusKB("VmRSS")
	if !ok {
		t.Skip("no /proc/self/status")
	}
	h := NewHasher()
	ctx := context.Background()
	stored, err := h.Hash(ctx, sentinelPassword)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if ok, _, err := h.Verify(ctx, stored, sentinelPassword); !ok || err != nil {
			t.Fatal("verification failed")
		}
	}
	after, _ := procStatusKB("VmRSS")
	peak, _ := procStatusKB("VmHWM")
	t.Logf("VmRSS before %.0f kB, after four hashes %.0f kB (delta %.0f kB), VmHWM %.0f kB", before, after, after-before, peak)
	if peak-before < 15*1024 {
		t.Logf("the peak did not see the 19 MiB block; the measurement may be blind")
	}
	if after-before > 6*1024 {
		t.Errorf("VmRSS grew by %.0f kB after the hashes; the hash memory was not returned", after-before)
	}
}
