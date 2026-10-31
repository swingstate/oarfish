package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"sort"
	"sync"
	"time"

	"github.com/swingstate/oarfish/backend/internal/clock"
)

// Session limits (ADR 0009, E2). They are not configurable in G5.1.
const (
	IdleTimeout     = time.Hour
	AbsoluteTimeout = 12 * time.Hour
	MaxSessions     = 8
	tokenBytes      = 32 // 256 bits
	csrfBytes       = 32
)

// Session is one signed-in browser. The token that identifies it is not stored
// anywhere in the process: the store keys sessions by the token's SHA-256.
type Session struct {
	Username  string
	CSRFToken string
	Created   time.Time

	lastSeen time.Time // guarded by the store's lock
	seq      uint64    // creation order; breaks ties between equal Created times
	ctx      context.Context
	cancel   context.CancelFunc
}

// Done is closed when the session ends: logout, expiry, eviction, a password
// change in another session, or shutdown. Event streams of the session select
// on it and end at once.
func (s *Session) Done() <-chan struct{} { return s.ctx.Done() }

// Expiry is the moment the session ends if nothing happens, and the hard end.
type Expiry struct {
	IdleExpiresAt time.Time
	ExpiresAt     time.Time
}

func (s *Session) expiry() Expiry {
	return Expiry{IdleExpiresAt: s.lastSeen.Add(IdleTimeout), ExpiresAt: s.Created.Add(AbsoluteTimeout)}
}

func (s *Session) expiredAt(now time.Time) bool {
	e := s.expiry()
	return !now.Before(e.IdleExpiresAt) || !now.Before(e.ExpiresAt)
}

// Sessions is the in-memory session store. Nothing is persisted; a restart
// ends every session (ORB-NFR-009). Expiry is checked on every access. A
// sweeper timer, armed only while at least one session exists, ends expired
// sessions on time so that their event streams close without a request.
type Sessions struct {
	clock clock.Clock

	mu       sync.Mutex
	byDigest map[[sha256.Size]byte]*Session
	sweep    clock.Timer
	sweepGen uint64 // identifies the armed sweep; a stale callback sees another value
	created  uint64 // sessions ever created, for Session.seq
}

// NewSessions returns an empty store.
func NewSessions(c clock.Clock) *Sessions {
	return &Sessions{clock: c, byDigest: map[[sha256.Size]byte]*Session{}}
}

func digest(token string) [sha256.Size]byte { return sha256.Sum256([]byte(token)) }

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Create starts a session for username and returns the raw token (for the
// cookie) and the session. When MaxSessions exist, the oldest is evicted.
func (st *Sessions) Create(username string) (string, *Session, error) {
	token, err := randomToken(tokenBytes)
	if err != nil {
		return "", nil, err
	}
	csrf, err := randomToken(csrfBytes)
	if err != nil {
		return "", nil, err
	}
	now := st.clock.Now()
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{Username: username, CSRFToken: csrf, Created: now, lastSeen: now, ctx: ctx, cancel: cancel}

	st.mu.Lock()
	defer st.mu.Unlock()
	st.removeExpiredLocked(now)
	for len(st.byDigest) >= MaxSessions {
		st.evictOldestLocked()
	}
	st.created++
	s.seq = st.created
	st.byDigest[digest(token)] = s
	st.armLocked(now)
	return token, s, nil
}

// Lookup returns the live session for a raw token. touch marks the request as
// activity (ordinary requests); the event stream and the session status
// request do not count as activity (E2).
func (st *Sessions) Lookup(token string, touch bool) (*Session, Expiry, bool) {
	if token == "" {
		return nil, Expiry{}, false
	}
	d := digest(token)
	now := st.clock.Now()
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.byDigest[d]
	if !ok {
		return nil, Expiry{}, false
	}
	if s.expiredAt(now) {
		st.removeLocked(d, s)
		return nil, Expiry{}, false
	}
	if touch {
		s.lastSeen = now
	}
	return s, s.expiry(), true
}

// Delete ends the session of a raw token (logout).
func (st *Sessions) Delete(token string) {
	d := digest(token)
	st.mu.Lock()
	defer st.mu.Unlock()
	if s, ok := st.byDigest[d]; ok {
		st.removeLocked(d, s)
	}
}

// DeleteOthers ends every session except keep (after a password change).
func (st *Sessions) DeleteOthers(keep *Session) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for d, s := range st.byDigest {
		if s != keep {
			st.removeLocked(d, s)
		}
	}
}

// CloseAll ends every session (shutdown).
func (st *Sessions) CloseAll() {
	st.DeleteOthers(nil)
}

// Len is the number of sessions, expired ones included until they are swept.
func (st *Sessions) Len() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return len(st.byDigest)
}

func (st *Sessions) removeLocked(d [sha256.Size]byte, s *Session) {
	delete(st.byDigest, d)
	s.cancel()
	if len(st.byDigest) == 0 && st.sweep != nil {
		st.sweep.Stop()
		st.sweep = nil
	}
}

func (st *Sessions) removeExpiredLocked(now time.Time) {
	for d, s := range st.byDigest {
		if s.expiredAt(now) {
			st.removeLocked(d, s)
		}
	}
}

func (st *Sessions) evictOldestLocked() {
	type entry struct {
		d [sha256.Size]byte
		s *Session
	}
	all := make([]entry, 0, len(st.byDigest))
	for d, s := range st.byDigest {
		all = append(all, entry{d, s})
	}
	sort.Slice(all, func(i, j int) bool {
		a, b := all[i].s, all[j].s
		if !a.Created.Equal(b.Created) {
			return a.Created.Before(b.Created)
		}
		return a.seq < b.seq
	})
	st.removeLocked(all[0].d, all[0].s)
}

// armLocked makes sure a sweep is scheduled for the earliest deadline. Idle
// deadlines only move later when a session is used, so a sweep that fires
// early simply schedules the next one.
func (st *Sessions) armLocked(now time.Time) {
	if st.sweep != nil || len(st.byDigest) == 0 {
		return
	}
	var next time.Time
	for _, s := range st.byDigest {
		e := s.expiry()
		for _, t := range []time.Time{e.IdleExpiresAt, e.ExpiresAt} {
			if next.IsZero() || t.Before(next) {
				next = t
			}
		}
	}
	st.sweepGen++
	gen := st.sweepGen
	st.sweep = st.clock.AfterFunc(max(next.Sub(now), 0), func() { st.onSweep(gen) })
}

func (st *Sessions) onSweep(gen uint64) {
	now := st.clock.Now()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.sweep == nil || gen != st.sweepGen {
		return // stopped, or replaced by a newer sweep, after this one fired
	}
	st.sweep = nil
	st.removeExpiredLocked(now)
	st.armLocked(now)
}
