package auth

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/swingstate/oarfish/backend/internal/clock/clocktest"
)

type counted struct {
	m      *Manager
	clock  *clocktest.Fake
	hashes *atomic.Int32
	dir    string
}

func newCounted(t *testing.T) counted {
	t.Helper()
	var n atomic.Int32
	dir := t.TempDir()
	c := clocktest.New()
	m, err := Open(Config{StateDir: dir, Clock: c, Hasher: newHasher(cheap, func() { n.Add(1) })})
	if err != nil {
		t.Fatal(err)
	}
	return counted{m: m, clock: c, hashes: &n, dir: dir}
}

func (c counted) setup(t *testing.T) {
	t.Helper()
	if _, _, err := c.m.Setup(context.Background(), readToken(t, c.dir), "admin-test", sentinelPassword); err != nil {
		t.Fatal(err)
	}
}

// ORB-ONB-002: without an account there is no login; a wrong token is
// refused and counted by the limiter; the token is checked before anything
// is said about user name or password.
func TestSetupNeedsTheToken(t *testing.T) {
	c := newCounted(t)
	ctx := context.Background()
	if _, _, err := c.m.Login(ctx, "admin-test", sentinelPassword); !errors.Is(err, ErrSetupRequired) {
		t.Fatalf("login before setup: %v", err)
	}
	if _, _, err := c.m.Setup(ctx, strings.Repeat("0", 32), "Bad Name", "short"); !errors.Is(err, ErrInvalidSetupToken) {
		t.Fatalf("wrong token: %v", err)
	}
	if c.hashes.Load() != 0 {
		t.Fatal("a wrong token cost a hash")
	}
	tok := readToken(t, c.dir)
	if _, _, err := c.m.Setup(ctx, tok, "Bad Name", sentinelPassword); policyCode(err) != CodeUsernameInvalid {
		t.Fatalf("bad user name: %v", err)
	}
	if _, _, err := c.m.Setup(ctx, tok, "admin-test", "short"); policyCode(err) != CodePasswordTooShort {
		t.Fatalf("short password: %v", err)
	}
	raw, s, err := c.m.Setup(ctx, tok, "admin-test", sentinelPassword)
	if err != nil || raw == "" || s.Username != "admin-test" || c.m.SetupRequired() {
		t.Fatalf("setup: %v", err)
	}
	if _, _, ok := c.m.Sessions().Lookup(raw, false); !ok {
		t.Fatal("setup did not start a session")
	}
}

// E6: wrong user name and wrong password give the same error after the same
// work: exactly one hash each.
func TestLoginFailuresLookTheSame(t *testing.T) {
	c := newCounted(t)
	c.setup(t)
	ctx := context.Background()
	before := c.hashes.Load()
	_, _, errUser := c.m.Login(ctx, "someone-else", sentinelPassword)
	afterUser := c.hashes.Load()
	_, _, errPass := c.m.Login(ctx, "admin-test", "wrong-password-123")
	afterPass := c.hashes.Load()
	if !errors.Is(errUser, ErrInvalidCredentials) || errUser != errPass {
		t.Fatalf("errors differ: %v / %v", errUser, errPass)
	}
	if afterUser-before != 1 || afterPass-afterUser != 1 {
		t.Fatalf("hashes: unknown user %d, wrong password %d; want 1 and 1", afterUser-before, afterPass-afterUser)
	}
}

// ORB-AUTH-003, E6: after five failures the correct password is refused too,
// and the rate limit is checked before any hash.
func TestLoginRateLimitRefusesEvenTheRightPassword(t *testing.T) {
	c := newCounted(t)
	c.setup(t)
	ctx := context.Background()
	for i := 0; i < FailuresBeforeBackoff; i++ {
		if _, _, err := c.m.Login(ctx, "admin-test", "wrong-password-123"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("failure %d: %v", i+1, err)
		}
	}
	hashes := c.hashes.Load()
	_, _, err := c.m.Login(ctx, "admin-test", sentinelPassword)
	var limited *RateLimitedError
	if !errors.As(err, &limited) || limited.RetryAfter != time.Second {
		t.Fatalf("the right password during the backoff: %v", err)
	}
	if c.hashes.Load() != hashes {
		t.Fatal("a refused attempt computed a hash")
	}
	c.clock.Advance(time.Second)
	if _, _, err := c.m.Login(ctx, "admin-test", sentinelPassword); err != nil {
		t.Fatalf("after the wait: %v", err)
	}
	// The success reset the count: one more failure does not block.
	_, _, _ = c.m.Login(ctx, "admin-test", "wrong-password-123")
	if _, _, err := c.m.Login(ctx, "admin-test", sentinelPassword); err != nil {
		t.Fatalf("after reset: %v", err)
	}
}

// E6: wrong setup tokens count like failed logins.
func TestSetupTokenAttemptsAreRateLimited(t *testing.T) {
	c := newCounted(t)
	ctx := context.Background()
	for i := 0; i < FailuresBeforeBackoff; i++ {
		_, _, _ = c.m.Setup(ctx, "wrong", "admin-test", sentinelPassword)
	}
	_, _, err := c.m.Setup(ctx, readToken(t, c.dir), "admin-test", sentinelPassword)
	var limited *RateLimitedError
	if !errors.As(err, &limited) {
		t.Fatalf("the right token during the backoff: %v", err)
	}
}

// E1: a hash with an old parameter set is replaced at the next login, and the
// account keeps working.
func TestLoginUpgradesAnOldHash(t *testing.T) {
	dir := t.TempDir()
	old := Params{MemoryKiB: 32, Time: 1, Threads: 1}
	m := openManager(t, dir, NewHasherWithParams(old))
	if _, _, err := m.Setup(context.Background(), readToken(t, dir), "admin-test", sentinelPassword); err != nil {
		t.Fatal(err)
	}
	m = openManager(t, dir, NewHasherWithParams(cheap)) // a new release with new parameters
	if _, _, err := m.Login(context.Background(), "admin-test", sentinelPassword); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(AccountPath(dir))
	if !strings.Contains(string(data), "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Fatalf("the hash was not upgraded:\n%s", data)
	}
	m = openManager(t, dir, NewHasherWithParams(cheap))
	if _, _, err := m.Login(context.Background(), "admin-test", sentinelPassword); err != nil {
		t.Fatalf("login after the upgrade: %v", err)
	}
}

// ORB-AUTH-001, E4: changing the password needs the current one and ends
// every other session; the old password stops working.
func TestChangePassword(t *testing.T) {
	c := newCounted(t)
	c.setup(t)
	ctx := context.Background()
	_, keep, _ := c.m.Login(ctx, "admin-test", sentinelPassword)
	_, other, _ := c.m.Login(ctx, "admin-test", sentinelPassword)
	const next = "Another-Sentinel-55"
	if err := c.m.ChangePassword(ctx, keep, "wrong-password-123", next); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong current password: %v", err)
	}
	if err := c.m.ChangePassword(ctx, keep, sentinelPassword, "short"); policyCode(err) != CodePasswordTooShort {
		t.Fatalf("short new password: %v", err)
	}
	if closed(other) {
		t.Fatal("a failed change ended a session")
	}
	if err := c.m.ChangePassword(ctx, keep, sentinelPassword, next); err != nil {
		t.Fatal(err)
	}
	if !closed(other) || closed(keep) {
		t.Fatal("the change must end the other sessions and keep this one")
	}
	if _, _, err := c.m.Login(ctx, "admin-test", sentinelPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password: %v", err)
	}
	if _, _, err := c.m.Login(ctx, "admin-test", next); err != nil {
		t.Fatalf("new password: %v", err)
	}
}

// E2: a restart ends every session: nothing is persisted.
func TestRestartEndsAllSessions(t *testing.T) {
	dir := t.TempDir()
	m := openManager(t, dir, NewHasherWithParams(cheap))
	raw, _, err := m.Setup(context.Background(), readToken(t, dir), "admin-test", sentinelPassword)
	if err != nil {
		t.Fatal(err)
	}
	m = openManager(t, dir, NewHasherWithParams(cheap))
	if _, _, ok := m.Sessions().Lookup(raw, true); ok {
		t.Fatal("a session survived the restart")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != AuthDirName {
			t.Errorf("unexpected file %s in the state directory", e.Name())
		}
	}
}
