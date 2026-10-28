package auth

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// cheap parameters keep the tests fast; production uses CurrentParams.
var cheap = Params{MemoryKiB: 64, Time: 1, Threads: 1}

const sentinelPassword = "Sentinel-Pw-3e9f1c7a"

var phcPattern = regexp.MustCompile(`^\$argon2id\$v=19\$m=\d+,t=\d+,p=\d+\$[A-Za-z0-9+/]+\$[A-Za-z0-9+/]+$`)

// ORB-AUTH-001, ADR 0009 E1: the hash is Argon2id as a PHC string with a
// 16-byte random salt and a 32-byte key, and it verifies.
func TestHashRoundTrip(t *testing.T) {
	h := NewHasherWithParams(cheap)
	ctx := context.Background()
	a, err := h.Hash(ctx, sentinelPassword)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := h.Hash(ctx, sentinelPassword)
	if !phcPattern.MatchString(a) || a == b {
		t.Fatalf("hashes %q and %q: want two different PHC strings (random salt)", a, b)
	}
	p, err := parsePHC(a)
	if err != nil || len(p.salt) != 16 || len(p.key) != 32 || p.params != cheap {
		t.Fatalf("parsed %+v, %v", p, err)
	}
	if strings.Contains(a, sentinelPassword) {
		t.Fatal("the hash contains the password")
	}
	if ok, rehash, err := h.Verify(ctx, a, sentinelPassword); !ok || rehash || err != nil {
		t.Errorf("Verify(right) = %v, %v, %v", ok, rehash, err)
	}
	if ok, _, err := h.Verify(ctx, a, sentinelPassword+"x"); ok || err != nil {
		t.Errorf("Verify(wrong) = %v, %v", ok, err)
	}
	if _, _, err := h.Verify(ctx, "$argon2id$garbage", sentinelPassword); !errors.Is(err, ErrMalformedHash) {
		t.Errorf("Verify(malformed) err = %v", err)
	}
}

// E1: the start parameters are m = 19 MiB, t = 2, p = 1, in one place, and a
// production hash carries them.
func TestProductionParametersAreTheADRValues(t *testing.T) {
	if CurrentParams != (Params{MemoryKiB: 19456, Time: 2, Threads: 1}) || NewHasher().Params() != CurrentParams {
		t.Fatalf("CurrentParams = %+v", CurrentParams)
	}
	if testing.Short() {
		t.Skip("one real hash takes a moment")
	}
	s, err := NewHasher().Hash(context.Background(), sentinelPassword)
	if err != nil || !strings.HasPrefix(s, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("hash = %q, %v", s, err)
	}
}

func TestParsePHCRejectsMalformedAndOutOfBoundsHashes(t *testing.T) {
	salt := "AAAAAAAAAAAAAAAAAAAAAA"                     // 16 bytes
	key := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 32 bytes
	ok := "$argon2id$v=19$m=19456,t=2,p=1$" + salt + "$" + key
	if _, err := parsePHC(ok); err != nil {
		t.Fatalf("valid hash refused: %v", err)
	}
	if _, err := parsePHC("$argon2id$v=19$m=65536,t=10,p=4$" + salt + "$" + key); err != nil {
		t.Fatalf("the upper bounds themselves must be accepted: %v", err)
	}
	bad := map[string]string{
		"empty":               "",
		"argon2i":             "$argon2i$v=19$m=19456,t=2,p=1$" + salt + "$" + key,
		"argon2d":             "$argon2d$v=19$m=19456,t=2,p=1$" + salt + "$" + key,
		"old version":         "$argon2id$v=16$m=19456,t=2,p=1$" + salt + "$" + key,
		"no version":          "$argon2id$m=19456,t=2,p=1$" + salt + "$" + key,
		"memory above 64 MiB": "$argon2id$v=19$m=65537,t=2,p=1$" + salt + "$" + key,
		"huge memory":         "$argon2id$v=19$m=4294967295,t=2,p=1$" + salt + "$" + key,
		"overflowing memory":  "$argon2id$v=19$m=99999999999999999999,t=2,p=1$" + salt + "$" + key,
		"time zero":           "$argon2id$v=19$m=19456,t=0,p=1$" + salt + "$" + key,
		"time above 10":       "$argon2id$v=19$m=19456,t=11,p=1$" + salt + "$" + key,
		"threads zero":        "$argon2id$v=19$m=19456,t=2,p=0$" + salt + "$" + key,
		"threads above 4":     "$argon2id$v=19$m=19456,t=2,p=5$" + salt + "$" + key,
		"memory below 8p":     "$argon2id$v=19$m=15,t=2,p=2$" + salt + "$" + key,
		"leading zero":        "$argon2id$v=19$m=019456,t=2,p=1$" + salt + "$" + key,
		"plus sign":           "$argon2id$v=19$m=+19456,t=2,p=1$" + salt + "$" + key,
		"negative":            "$argon2id$v=19$m=-1,t=2,p=1$" + salt + "$" + key,
		"order":               "$argon2id$v=19$t=2,m=19456,p=1$" + salt + "$" + key,
		"extra parameter":     "$argon2id$v=19$m=19456,t=2,p=1,k=1$" + salt + "$" + key,
		"missing parameter":   "$argon2id$v=19$m=19456,t=2$" + salt + "$" + key,
		"spaces":              "$argon2id$v=19$m=19456, t=2,p=1$" + salt + "$" + key,
		"padded base64":       "$argon2id$v=19$m=19456,t=2,p=1$" + salt + "==$" + key,
		"url base64":          "$argon2id$v=19$m=19456,t=2,p=1$" + "______________________" + "$" + key,
		"salt too short":      "$argon2id$v=19$m=19456,t=2,p=1$AAAAAAA$" + key,
		"key too short":       "$argon2id$v=19$m=19456,t=2,p=1$" + salt + "$AAAAAAA",
		"key too long":        "$argon2id$v=19$m=19456,t=2,p=1$" + salt + "$" + strings.Repeat("A", 200),
		"no salt":             "$argon2id$v=19$m=19456,t=2,p=1$$" + key,
		"extra field":         ok + "$x",
		"missing field":       "$argon2id$v=19$m=19456,t=2,p=1$" + salt,
		"no leading dollar":   ok[1:],
		"very long":           ok + strings.Repeat("A", 1000),
	}
	for name, s := range bad {
		if _, err := parsePHC(s); !errors.Is(err, ErrMalformedHash) {
			t.Errorf("%s: parsePHC(%q) err = %v", name, s, err)
		}
	}
}

// A tampered account file cannot make one login allocate more than 64 MiB or
// run more than ten passes: parsePHC has hard bounds, and every accepted hash
// is the canonical form encodePHC writes.
func FuzzParsePHC(f *testing.F) {
	f.Add("$argon2id$v=19$m=19456,t=2,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	f.Add("$argon2id$v=19$m=65536,t=10,p=4$c2FsdHNhbHQ$a2V5a2V5a2V5a2V5a2V5aw")
	f.Add("$argon2id$v=19$m=1,t=1,p=1$$")
	f.Add("$$$$$$")
	f.Fuzz(func(t *testing.T, s string) {
		p, err := parsePHC(s)
		if err != nil {
			return
		}
		if p.params.MemoryKiB > maxMemoryKiB || p.params.Time < 1 || p.params.Time > maxTime || p.params.Threads < 1 || p.params.Threads > maxThreads {
			t.Fatalf("out-of-bounds parameters accepted: %+v", p.params)
		}
		if got := encodePHC(p.params, p.salt, p.key); got != s {
			t.Fatalf("accepted a non-canonical form: %q re-encodes as %q", s, got)
		}
	})
}

// E1: a login with an old parameter set verifies and tells the caller to
// re-hash with the current parameters.
func TestVerifyReportsAnOldParameterSet(t *testing.T) {
	ctx := context.Background()
	old, _ := NewHasherWithParams(Params{MemoryKiB: 32, Time: 1, Threads: 1}).Hash(ctx, sentinelPassword)
	ok, rehash, err := NewHasherWithParams(cheap).Verify(ctx, old, sentinelPassword)
	if !ok || !rehash || err != nil {
		t.Fatalf("Verify = %v, %v, %v; want true, true, nil", ok, rehash, err)
	}
}

// E1: at most one hash runs at a time; a second caller waits.
func TestHasherRunsOneHashAtATime(t *testing.T) {
	h := NewHasherWithParams(cheap)
	if cap(h.sem) != 1 {
		t.Fatalf("semaphore size %d, want 1", cap(h.sem))
	}
	h.sem <- struct{}{} // someone is hashing
	done := make(chan struct{})
	go func() {
		_, _ = h.Hash(context.Background(), sentinelPassword)
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("a second hash ran while the first held the semaphore")
	case <-time.After(50 * time.Millisecond):
	}
	<-h.sem
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting hash never ran")
	}
	// A caller that gives up while waiting gets its context error.
	h.sem <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.Hash(ctx, sentinelPassword); !errors.Is(err, context.Canceled) {
		t.Errorf("Hash with a cancelled context = %v", err)
	}
	<-h.sem
}

// E1: after every hash the memory goes back to the operating system, so the
// hash block does not stay in the idle resident size.
func TestHasherReturnsMemoryAfterEveryHash(t *testing.T) {
	var calls atomic.Int32
	h := newHasher(cheap, func() { calls.Add(1) })
	ctx := context.Background()
	s, _ := h.Hash(ctx, sentinelPassword)
	_, _, _ = h.Verify(ctx, s, sentinelPassword)
	h.VerifyDummy(ctx, sentinelPassword)
	if calls.Load() != 3 {
		t.Errorf("afterHash ran %d times for three hashes", calls.Load())
	}
}

// E6: the dummy verification for an unknown user does the same work as a real
// one: same parameters, salt and key length as a current hash.
func TestDummyHashHasTheCurrentCost(t *testing.T) {
	for _, p := range []Params{cheap, CurrentParams} {
		h := NewHasherWithParams(p)
		if h.dummy.params != p || len(h.dummy.salt) != saltLen || len(h.dummy.key) != keyLen {
			t.Errorf("dummy for %+v is %+v, salt %d, key %d", p, h.dummy.params, len(h.dummy.salt), len(h.dummy.key))
		}
	}
}
