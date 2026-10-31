// Package auth is the local access control of ADR 0009: one administrator
// account with an Argon2id password hash (account.go, password.go, policy.go),
// the first-start setup token (setuptoken.go), in-memory sessions
// (session.go) and the global login rate limit (limiter.go). It knows nothing
// about HTTP; the server package maps its results to requests and responses.
package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/swingstate/oarfish/backend/internal/clock"
	"github.com/swingstate/oarfish/backend/internal/securefile"
)

// Errors of the account operations. None of them says whether a user name
// exists.
var (
	ErrSetupRequired      = errors.New("no administrator account exists yet")
	ErrSetupDone          = errors.New("the administrator account exists already")
	ErrInvalidSetupToken  = errors.New("invalid setup token")
	ErrInvalidCredentials = errors.New("invalid user name or password")
	ErrWrongPassword      = errors.New("the current password is wrong")
	ErrUnavailable        = errors.New("the account could not be read or written")
)

// RateLimitedError means the attempt was refused before any hash was computed.
type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("rate limited, retry after %s", e.RetryAfter)
}

// Config configures Open.
type Config struct {
	StateDir string
	Clock    clock.Clock // default clock.System()
	Hasher   *Hasher     // default NewHasher(); tests pass cheaper parameters
}

// Manager holds the account state of the running service.
type Manager struct {
	stateDir string
	hasher   *Hasher
	limiter  *Limiter
	sessions *Sessions
	store    *AccountStore
	clock    clock.Clock

	mu      sync.Mutex
	account *Account    // nil while setup is required
	setup   *SetupToken // non-nil exactly while account is nil
}

// Open checks the state directory, loads the account and, while none exists,
// creates a new setup token. An unusable state directory or account file is an
// error: the service does not start (ORB-SEC-005, E4).
func Open(cfg Config) (*Manager, error) {
	if cfg.StateDir == "" {
		return nil, errors.New("no state directory: systemd sets STATE_DIRECTORY, or pass -state-dir")
	}
	if err := securefile.CheckWritableDir(cfg.StateDir); err != nil {
		return nil, err
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.System()
	}
	if cfg.Hasher == nil {
		cfg.Hasher = NewHasher()
	}
	store, acc, err := OpenAccountStore(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	m := &Manager{
		stateDir: cfg.StateDir, hasher: cfg.Hasher, clock: cfg.Clock, store: store, account: acc,
		limiter: NewLimiter(cfg.Clock), sessions: NewSessions(cfg.Clock),
	}
	if acc == nil {
		if m.setup, err = NewSetupToken(cfg.StateDir); err != nil {
			return nil, fmt.Errorf("create the setup token: %w", err)
		}
	} else if err := RemoveSetupToken(cfg.StateDir); err != nil {
		return nil, fmt.Errorf("remove the stale setup token: %w", err)
	}
	return m, nil
}

// Sessions is the session store.
func (m *Manager) Sessions() *Sessions { return m.sessions }

// SetupRequired reports whether no account exists yet.
func (m *Manager) SetupRequired() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.account == nil
}

// SetupTokenPath is where the setup token is written; it is safe to log.
func (m *Manager) SetupTokenPath() string { return SetupTokenPath(m.stateDir) }

func (m *Manager) allow() error {
	if ok, wait := m.limiter.Allow(); !ok {
		return &RateLimitedError{RetryAfter: wait}
	}
	return nil
}

// Setup creates the administrator account with the setup token, removes the
// token file and starts a session. The token is checked before anything is
// said about the user name or password.
func (m *Manager) Setup(ctx context.Context, token, username, password string) (string, *Session, error) {
	m.mu.Lock()
	setup := m.setup
	m.mu.Unlock()
	if setup == nil {
		return "", nil, ErrSetupDone
	}
	if err := m.allow(); err != nil {
		return "", nil, err
	}
	if !setup.Matches(token) {
		m.limiter.Failure()
		return "", nil, ErrInvalidSetupToken
	}
	if err := ValidateUsername(username); err != nil {
		return "", nil, err
	}
	if err := ValidatePassword(password); err != nil {
		return "", nil, err
	}
	hash, err := m.hasher.Hash(ctx, password)
	if err != nil {
		return "", nil, ErrUnavailable
	}
	m.mu.Lock()
	if m.account != nil {
		m.mu.Unlock()
		return "", nil, ErrSetupDone // a parallel setup won
	}
	acc := Account{Username: username, PasswordHash: hash, CreatedAt: m.clock.Now().UTC().Truncate(time.Second)}
	if err := m.store.Save(acc); err != nil {
		m.mu.Unlock()
		return "", nil, ErrUnavailable
	}
	m.account, m.setup = &acc, nil
	m.mu.Unlock()
	_ = setup.Remove() // the token is dead in memory either way
	m.limiter.Success()
	raw, s, err := m.sessions.Create(username)
	if err != nil {
		return "", nil, ErrUnavailable
	}
	return raw, s, nil
}

// Login checks user name and password and starts a session. A wrong user name
// and a wrong password give the same error after the same amount of work.
func (m *Manager) Login(ctx context.Context, username, password string) (string, *Session, error) {
	m.mu.Lock()
	acc := m.account
	m.mu.Unlock()
	if acc == nil {
		return "", nil, ErrSetupRequired
	}
	if err := m.allow(); err != nil {
		return "", nil, err
	}
	if subtle.ConstantTimeCompare([]byte(username), []byte(acc.Username)) != 1 {
		m.hasher.VerifyDummy(ctx, password)
		m.limiter.Failure()
		return "", nil, ErrInvalidCredentials
	}
	ok, rehash, err := m.hasher.Verify(ctx, acc.PasswordHash, password)
	if err != nil {
		return "", nil, ErrUnavailable
	}
	if !ok {
		m.limiter.Failure()
		return "", nil, ErrInvalidCredentials
	}
	m.limiter.Success()
	if rehash {
		m.rehash(ctx, acc, password)
	}
	raw, s, err := m.sessions.Create(acc.Username)
	if err != nil {
		return "", nil, ErrUnavailable
	}
	return raw, s, nil
}

// rehash stores the password again with the current parameters. A failure is
// not an error for the login: the old hash still verifies.
func (m *Manager) rehash(ctx context.Context, old *Account, password string) {
	hash, err := m.hasher.Hash(ctx, password)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.account != old {
		return // changed meanwhile
	}
	acc := *old
	acc.PasswordHash = hash
	if m.store.Save(acc) == nil {
		m.account = &acc
	}
}

// ChangePassword replaces the password of the signed-in administrator. It
// needs the current password, goes through the same rate limit as a login, and
// ends every other session.
func (m *Manager) ChangePassword(ctx context.Context, s *Session, current, next string) error {
	m.mu.Lock()
	acc := m.account
	m.mu.Unlock()
	if acc == nil {
		return ErrSetupRequired
	}
	if err := ValidatePassword(next); err != nil {
		return err
	}
	if err := m.allow(); err != nil {
		return err
	}
	ok, _, err := m.hasher.Verify(ctx, acc.PasswordHash, current)
	if err != nil {
		return ErrUnavailable
	}
	if !ok {
		m.limiter.Failure()
		return ErrWrongPassword
	}
	m.limiter.Success()
	hash, err := m.hasher.Hash(ctx, next)
	if err != nil {
		return ErrUnavailable
	}
	m.mu.Lock()
	updated := *m.account
	updated.PasswordHash = hash
	if err := m.store.Save(updated); err != nil {
		m.mu.Unlock()
		return ErrUnavailable
	}
	m.account = &updated
	m.mu.Unlock()
	m.sessions.DeleteOthers(s)
	return nil
}

// Logout ends the session of a raw token.
func (m *Manager) Logout(token string) { m.sessions.Delete(token) }

// Username is the administrator's name, or "" while setup is required.
func (m *Manager) Username() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.account == nil {
		return ""
	}
	return m.account.Username
}
