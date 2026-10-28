package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/swingstate/oarfish/backend/internal/auth"
	"github.com/swingstate/oarfish/backend/internal/clock"
)

// Synthetic credentials. The password is a sentinel: redaction tests search
// every output for it.
const (
	testUser     = "admin-test"
	testPassword = "Sentinel-Pw-3e9f1c7a"
)

// cheapParams keep the tests fast; production uses auth.CurrentParams.
var cheapParams = auth.Params{MemoryKiB: 64, Time: 1, Threads: 1}

// testAuth is an auth manager in a fresh state directory.
type testAuth struct {
	m        *auth.Manager
	stateDir string
}

func newTestAuth(t testing.TB, c clock.Clock) *testAuth {
	t.Helper()
	dir := t.TempDir()
	m, err := auth.Open(auth.Config{StateDir: dir, Clock: c, Hasher: auth.NewHasherWithParams(cheapParams)})
	if err != nil {
		t.Fatal(err)
	}
	return &testAuth{m: m, stateDir: dir}
}

// setupToken reads the token the way the administrator does.
func (a *testAuth) setupToken(t testing.TB) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(a.stateDir, "setup-token"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

// session is a signed-in browser: the raw cookie value and the CSRF token.
type session struct {
	token, csrf string
}

// completeSetup creates the account and returns its first session.
func (a *testAuth) completeSetup(t testing.TB) session {
	t.Helper()
	token, s, err := a.m.Setup(context.Background(), a.setupToken(t), testUser, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	return session{token: token, csrf: s.CSRFToken}
}

// login starts one more session.
func (a *testAuth) login(t testing.TB) session {
	t.Helper()
	token, s, err := a.m.Login(context.Background(), testUser, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	return session{token: token, csrf: s.CSRFToken}
}

// signedIn wraps a handler so that every request carries a valid session
// cookie, and state-changing ones also the Origin and CSRF headers a browser
// on the Oarfish page sends. The tests of the read-only API use it; the
// access-control tests talk to the bare handler.
type signedIn struct {
	http.Handler
	s session
}

func (h signedIn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.AddCookie(&http.Cookie{Name: cookieName, Value: h.s.token})
	if stateChanging(r.Method) {
		r.Header.Set("Origin", "http://"+r.Host)
		r.Header.Set(csrfHeader, h.s.csrf)
	}
	h.Handler.ServeHTTP(w, r)
}

// CloseStreams forwards the shutdown hook, so New registers it for the wrapped handler.
func (h signedIn) CloseStreams() {
	if c, ok := h.Handler.(interface{ CloseStreams() }); ok {
		c.CloseStreams()
	}
}

// newSignedInHandler builds NewHandler with access control and returns it
// wrapped with the first session of a freshly set up account.
func newSignedInHandler(t testing.TB, newHandler func(opts ...Option) http.Handler) (signedIn, *testAuth) {
	t.Helper()
	a := newTestAuth(t, clock.System())
	s := a.completeSetup(t)
	return signedIn{Handler: newHandler(WithAuth(a.m)), s: s}, a
}

// signedInHandler is NewHandler without frontend files, with opts and access
// control, used through a signed-in session.
func signedInHandler(t testing.TB, opts ...Option) signedIn {
	t.Helper()
	h, _ := newSignedInHandler(t, func(auth ...Option) http.Handler {
		return NewHandler(fstest.MapFS{}, append(opts, auth...)...)
	})
	return h
}

// newRequest is httptest.NewRequest for the device: the Host header is the
// loopback listen address, as a browser on the device or behind an SSH tunnel
// sends it (httptest's default, example.com, is refused by the Host guard).
func newRequest(method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	r.Host = "127.0.0.1:8484"
	return r
}
