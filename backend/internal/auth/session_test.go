package auth

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/swingstate/oarfish/backend/internal/clock/clocktest"
)

func closed(s *Session) bool {
	select {
	case <-s.Done():
		return true
	default:
		return false
	}
}

// ORB-AUTH-002, E2: a session ends after one hour without an ordinary request.
func TestSessionIdleTimeout(t *testing.T) {
	c := clocktest.New()
	st := NewSessions(c)
	tok, s, _ := st.Create("admin-test")
	c.Advance(59 * time.Minute)
	if _, _, ok := st.Lookup(tok, true); !ok {
		t.Fatal("ended before the idle timeout")
	}
	c.Advance(59 * time.Minute) // 1 h 58 min since start, 59 min since the last use
	if _, _, ok := st.Lookup(tok, false); !ok {
		t.Fatal("activity did not move the idle deadline")
	}
	c.Advance(time.Minute) // the look-up above was not activity
	if _, _, ok := st.Lookup(tok, true); ok || !closed(s) {
		t.Fatal("a session without activity for an hour is still alive")
	}
}

// E2: however active, a session ends 12 hours after login.
func TestSessionAbsoluteTimeout(t *testing.T) {
	c := clocktest.New()
	st := NewSessions(c)
	tok, s, _ := st.Create("admin-test")
	for i := 0; i < 23; i++ {
		c.Advance(30 * time.Minute)
		if _, _, ok := st.Lookup(tok, true); !ok {
			t.Fatalf("ended after %v", time.Duration(i+1)*30*time.Minute)
		}
	}
	c.Advance(30 * time.Minute)
	if _, _, ok := st.Lookup(tok, true); ok || !closed(s) {
		t.Fatal("alive after 12 hours")
	}
}

// E2: an expired session ends on time without any request, so its event
// streams close; the sweeper exists only while there are sessions.
func TestSessionSweeperEndsExpiredSessionsAndOnlyRunsWhileSessionsExist(t *testing.T) {
	c := clocktest.New()
	st := NewSessions(c)
	if c.Pending() != 0 {
		t.Fatal("a timer without sessions")
	}
	_, a, _ := st.Create("admin-test")
	c.Advance(30 * time.Minute)
	tokB, b, _ := st.Create("admin-test")
	if c.Pending() != 1 {
		t.Fatalf("%d timers, want one sweeper", c.Pending())
	}
	c.Advance(31 * time.Minute) // a idle for 61 min, b for 31
	if !closed(a) || closed(b) || st.Len() != 1 {
		t.Fatalf("after the first idle deadline: a closed %v, b closed %v, %d sessions", closed(a), closed(b), st.Len())
	}
	st.Delete(tokB)
	if !closed(b) || st.Len() != 0 || c.Pending() != 0 {
		t.Fatalf("after the last logout: %d sessions, %d timers", st.Len(), c.Pending())
	}
	_, d, _ := st.Create("admin-test")
	c.Advance(IdleTimeout)
	if !closed(d) || c.Pending() != 0 {
		t.Fatalf("after expiry by the sweeper: closed %v, %d timers", closed(d), c.Pending())
	}
}

// E2: at most eight sessions; the ninth evicts the oldest and closes it.
func TestSessionEvictionAtNine(t *testing.T) {
	c := clocktest.New()
	st := NewSessions(c)
	var toks []string
	var sessions []*Session
	for i := 0; i < MaxSessions+1; i++ {
		tok, s, err := st.Create("admin-test")
		if err != nil {
			t.Fatal(err)
		}
		toks, sessions = append(toks, tok), append(sessions, s)
		if i%2 == 1 {
			c.Advance(time.Second) // pairs share a creation time; the first of a pair is older
		}
	}
	if st.Len() != MaxSessions {
		t.Fatalf("%d sessions", st.Len())
	}
	if _, _, ok := st.Lookup(toks[0], false); ok || !closed(sessions[0]) {
		t.Fatal("the oldest session was not evicted")
	}
	for i := 1; i <= MaxSessions; i++ {
		if _, _, ok := st.Lookup(toks[i], false); !ok || closed(sessions[i]) {
			t.Errorf("session %d was evicted", i)
		}
	}
}

// E2: the store keeps only the SHA-256 of a token, never the token itself.
func TestSessionTokenIsNotStoredRaw(t *testing.T) {
	st := NewSessions(clocktest.New())
	tok, s, _ := st.Create("admin-test")
	if len(tok) != 43 { // 32 bytes, unpadded base64url
		t.Fatalf("token length %d", len(tok))
	}
	dump := fmt.Sprintf("%#v %+v", st.byDigest, *s)
	if strings.Contains(dump, tok) {
		t.Fatal("the raw token is in the store")
	}
	if _, ok := st.byDigest[sha256.Sum256([]byte(tok))]; !ok || len(st.byDigest) != 1 {
		t.Fatal("the store is not keyed by the token's SHA-256")
	}
	if s.CSRFToken == "" || s.CSRFToken == tok {
		t.Fatal("the CSRF token must be its own random value")
	}
	if _, _, ok := st.Lookup(tok+"x", true); ok {
		t.Fatal("a different token found the session")
	}
	if _, _, ok := st.Lookup("", true); ok {
		t.Fatal("an empty token found a session")
	}
}

// E2: logout and a password change close the sessions they end.
func TestDeleteAndDeleteOthersCloseSessions(t *testing.T) {
	st := NewSessions(clocktest.New())
	t1, s1, _ := st.Create("admin-test")
	_, s2, _ := st.Create("admin-test")
	_, s3, _ := st.Create("admin-test")
	st.DeleteOthers(s2)
	if !closed(s1) || closed(s2) || !closed(s3) || st.Len() != 1 {
		t.Fatal("DeleteOthers must end every session but the kept one")
	}
	if _, _, ok := st.Lookup(t1, true); ok {
		t.Fatal("an ended session was found")
	}
	st.CloseAll()
	if !closed(s2) || st.Len() != 0 {
		t.Fatal("CloseAll left a session")
	}
}
