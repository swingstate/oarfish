package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// The Argon2id parameters of ADR 0009, E1. They are the only place the cost of
// a new hash is decided; a stored hash carries its own parameters (PHC string),
// so changing these needs no migration: the next login re-hashes.
const (
	ArgonMemoryKiB = 19 * 1024 // m = 19 MiB
	ArgonTime      = 2         // t
	ArgonThreads   = 1         // p
	saltLen        = 16
	keyLen         = 32
)

// Upper bounds for a parsed hash. The account file is trusted only as far as
// these allow: a tampered file must not make a login allocate gigabytes or run
// for minutes.
const (
	maxMemoryKiB = 64 * 1024
	maxTime      = 10
	maxThreads   = 4
	minSaltLen   = 8
	maxSaltLen   = 64
	minKeyLen    = 16
	maxKeyLen    = 64
	maxPHCLen    = 256
)

// Params are the cost parameters of one Argon2id hash.
type Params struct {
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
}

// CurrentParams are the parameters new hashes use.
var CurrentParams = Params{MemoryKiB: ArgonMemoryKiB, Time: ArgonTime, Threads: ArgonThreads}

// ErrMalformedHash means a stored hash is not a PHC string this code accepts.
var ErrMalformedHash = errors.New("malformed password hash")

// phc is a parsed `$argon2id$v=19$m=...,t=...,p=...$salt$hash` string.
type phc struct {
	params Params
	salt   []byte
	key    []byte
}

// encodePHC writes the PHC string form (unpadded standard base64).
func encodePHC(p Params, salt, key []byte) string {
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoryKiB, p.Time, p.Threads, b64.EncodeToString(salt), b64.EncodeToString(key))
}

// parsePHC is strict: exactly the form encodePHC writes, version 19, parameters
// in the order m, t, p, every number within the bounds above, canonical
// base64. Anything else is ErrMalformedHash; it never panics.
func parsePHC(s string) (phc, error) {
	var out phc
	if len(s) > maxPHCLen {
		return out, ErrMalformedHash
	}
	parts := strings.Split(s, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return out, ErrMalformedHash
	}
	kv := strings.Split(parts[3], ",")
	if len(kv) != 3 {
		return out, ErrMalformedHash
	}
	m, okM := phcUint(kv[0], "m=", maxMemoryKiB)
	t, okT := phcUint(kv[1], "t=", maxTime)
	p, okP := phcUint(kv[2], "p=", maxThreads)
	// Argon2 needs at least 8 KiB of memory per lane.
	if !okM || !okT || !okP || t < 1 || p < 1 || m < 8*p {
		return out, ErrMalformedHash
	}
	salt, okS := phcBase64(parts[4], minSaltLen, maxSaltLen)
	key, okK := phcBase64(parts[5], minKeyLen, maxKeyLen)
	if !okS || !okK {
		return out, ErrMalformedHash
	}
	out.params = Params{MemoryKiB: uint32(m), Time: uint32(t), Threads: uint8(p)}
	out.salt, out.key = salt, key
	return out, nil
}

// phcUint parses "<prefix><decimal>" with no sign, no leading zero and value <= max.
func phcUint(s, prefix string, max uint64) (uint64, bool) {
	digits, ok := strings.CutPrefix(s, prefix)
	if !ok || digits == "" || len(digits) > 10 || (len(digits) > 1 && digits[0] == '0') {
		return 0, false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil || n > max {
		return 0, false
	}
	return n, true
}

func phcBase64(s string, minLen, maxLen int) ([]byte, bool) {
	if len(s) > base64.RawStdEncoding.EncodedLen(maxLen) {
		return nil, false
	}
	// The decoder skips \r and \n even in strict mode; a stored hash has
	// neither, so only the alphabet itself is accepted.
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/') {
			return nil, false
		}
	}
	b, err := base64.RawStdEncoding.Strict().DecodeString(s)
	if err != nil || len(b) < minLen || len(b) > maxLen {
		return nil, false
	}
	return b, true
}

// Hasher computes and verifies Argon2id hashes, one at a time (ADR 0009, E1):
// a hash needs m KiB of RAM while it runs, and on a 512 MB device a burst of requests
// must not start several in parallel. Callers wait for their turn; the rate
// limit bounds how many can wait.
type Hasher struct {
	params Params
	sem    chan struct{}
	// afterHash returns the hash memory to the operating system, so that the
	// 19 MiB block does not stay in the resident size of an idle process.
	afterHash func()
	dummy     phc
}

// NewHasher returns a Hasher with CurrentParams.
func NewHasher() *Hasher { return newHasher(CurrentParams, debug.FreeOSMemory) }

// NewHasherWithParams returns a Hasher that creates hashes with p. Production
// uses NewHasher; tests use cheap parameters so that they run fast, and the
// parameter upgrade test uses old ones.
func NewHasherWithParams(p Params) *Hasher { return newHasher(p, debug.FreeOSMemory) }

func newHasher(p Params, afterHash func()) *Hasher {
	h := &Hasher{params: p, sem: make(chan struct{}, 1), afterHash: afterHash}
	// The dummy hash is verified when a login names a user that does not exist,
	// so that the response takes as long as a wrong password (E6). Its value
	// does not matter; its cost does: it always has the current parameters.
	h.dummy = phc{params: p, salt: make([]byte, saltLen), key: make([]byte, keyLen)}
	return h
}

// Params returns the parameters new hashes get.
func (h *Hasher) Params() Params { return h.params }

func (h *Hasher) acquire(ctx context.Context) error {
	select {
	case h.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hasher) release() {
	if h.afterHash != nil {
		h.afterHash()
	}
	<-h.sem
}

func (h *Hasher) key(password string, p phc) []byte {
	return argon2.IDKey([]byte(password), p.salt, p.params.Time, p.params.MemoryKiB, p.params.Threads, uint32(len(p.key)))
}

// Hash returns the PHC string of a new hash of password with a random salt.
func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.release()
	key := argon2.IDKey([]byte(password), salt, h.params.Time, h.params.MemoryKiB, h.params.Threads, keyLen)
	return encodePHC(h.params, salt, key), nil
}

// Verify reports whether password matches the stored PHC string, and whether
// the stored hash uses other parameters than the current ones (the caller then
// re-hashes). A malformed hash is an error, not a mismatch.
func (h *Hasher) Verify(ctx context.Context, stored, password string) (ok, rehash bool, err error) {
	p, err := parsePHC(stored)
	if err != nil {
		return false, false, err
	}
	if err := h.acquire(ctx); err != nil {
		return false, false, err
	}
	defer h.release()
	got := h.key(password, p)
	ok = subtle.ConstantTimeCompare(got, p.key) == 1
	return ok, p.params != h.params || len(p.salt) != saltLen || len(p.key) != keyLen, nil
}

// VerifyDummy does the work of one verification and always fails. It is used
// when the user name is unknown, so that both cases take the same time.
func (h *Hasher) VerifyDummy(ctx context.Context, password string) {
	if h.acquire(ctx) != nil {
		return
	}
	defer h.release()
	_ = subtle.ConstantTimeCompare(h.key(password, h.dummy), h.dummy.key)
}
