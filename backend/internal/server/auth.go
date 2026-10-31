package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/swingstate/oarfish/backend/internal/audit"
	"github.com/swingstate/oarfish/backend/internal/auth"
)

// Session cookie (ADR 0009, E3). On a TLS connection the name carries the
// __Host- prefix, which browsers only accept with Secure, Path=/ and no
// Domain. Secure follows the connection itself (r.TLS), never a forwarded
// header.
const (
	cookieName    = "oarfish_session"
	cookieNameTLS = "__Host-oarfish_session"
	csrfHeader    = "X-Oarfish-CSRF"
	// maxAuthBody is the body limit of login, setup and password change (E4).
	maxAuthBody = 1 << 10
	// SessionSchemaVersion is the version of the session status response.
	SessionSchemaVersion = 1
)

// WithAuth puts every /api/v1 route except session status, login and setup
// behind a session of m (D-C46). Without it those routes answer 401.
func WithAuth(m *auth.Manager) Option { return func(c *config) { c.auth = m } }

// WithAudit records account events and rejected requests in l.
func WithAudit(l *audit.Log) Option { return func(c *config) { c.audit = l } }

// WithAllowedHosts adds host names the Host header may carry besides the
// loopback names. G5.1 passes none; it exists for the LAN design (D-D10).
func WithAllowedHosts(names ...string) Option {
	return func(c *config) {
		for _, n := range names {
			c.allowedHosts = append(c.allowedHosts, strings.ToLower(n))
		}
	}
}

// ---- correlation ID ------------------------------------------------------

type ctxKey int

const correlationKey ctxKey = 0

// withCorrelation gives every request a server-made 64-bit correlation ID,
// returned in X-Correlation-ID and in typed errors. A value the client sends
// is ignored.
func withCorrelation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b [8]byte
		_, _ = rand.Read(b[:])
		id := hex.EncodeToString(b[:])
		w.Header().Set("X-Correlation-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), correlationKey, id)))
	})
}

func correlationID(r *http.Request) string {
	id, _ := r.Context().Value(correlationKey).(string)
	return id
}

// ---- Host, Origin, Content-Type, CSRF ------------------------------------

// hostOf returns the host part of a Host header or URL host, without port and
// brackets, in lower case.
func hostOf(hostport string) string {
	h := hostport
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		h = host
	}
	return strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(h, "["), "]"))
}

// hostAllowed is the DNS-rebinding guard (E3): the Host header must name the
// device by a loopback address or "localhost", or by a configured name. The
// port is ignored, because an SSH tunnel may use another local port.
func hostAllowed(r *http.Request, allowed []string) bool {
	h := hostOf(r.Host)
	if h == "localhost" {
		return true
	}
	// A literal loopback address cannot be rebound; this covers 127.0.0.1,
	// ::1 and the rest of 127.0.0.0/8, which the listen guard also accepts.
	if ip, err := netip.ParseAddr(h); err == nil && ip.Zone() == "" && ip.Unmap().IsLoopback() {
		return true
	}
	for _, a := range allowed {
		if h == a {
			return true
		}
	}
	return false
}

// originAllowed is the first CSRF layer: a browser that sends Sec-Fetch-Site
// must say same-origin; otherwise Origin must be there and name the host of
// the request (port ignored). A request with neither is refused. When both
// are present, both must pass.
func originAllowed(r *http.Request) bool {
	sfs := r.Header.Get("Sec-Fetch-Site")
	origin := r.Header.Get("Origin")
	if sfs == "" && origin == "" {
		return false
	}
	if sfs != "" && sfs != "same-origin" {
		return false
	}
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" {
		return false
	}
	return hostOf(u.Host) == hostOf(r.Host)
}

// jsonContentType is the third CSRF layer: an HTML form cannot send JSON.
func jsonContentType(r *http.Request) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mt == "application/json"
}

// csrfValid is the second layer: the per-session token in X-Oarfish-CSRF.
func csrfValid(r *http.Request, s *auth.Session) bool {
	got := r.Header.Get(csrfHeader)
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.CSRFToken)) == 1
}

// hostGuard refuses every request whose Host header is not allowed, before
// any route sees it.
func hostGuard(cfg config, next http.Handler) http.Handler {
	a := &authAPI{cfg: cfg}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostAllowed(r, cfg.allowedHosts) {
			if t, ok := apiTarget(r.URL.Path); ok {
				a.reject(r, t, audit.HostRejected)
			}
			writeError(w, r, http.StatusForbidden, apiError{
				Code:     "host_rejected",
				Message:  "Oarfish answers only to its loopback address.",
				Recovery: "Open http://127.0.0.1:8484 on the device or through an SSH tunnel.",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// apiTarget maps a known API path to its audit target. Paths that are not
// routes are not audited: their text comes from the client.
func apiTarget(path string) (audit.Target, bool) {
	switch path {
	case "/api/v1/session":
		return audit.TargetSession, true
	case "/api/v1/setup":
		return audit.TargetSetup, true
	case "/api/v1/account/password":
		return audit.TargetPassword, true
	case "/api/v1/device":
		return audit.TargetDevice, true
	case "/api/v1/metrics":
		return audit.TargetMetrics, true
	case "/api/v1/events":
		return audit.TargetEvents, true
	}
	return "", false
}

// ---- cookie --------------------------------------------------------------

func sessionCookieName(r *http.Request) string {
	if r.TLS != nil {
		return cookieNameTLS
	}
	return cookieName
}

func sessionToken(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName(r))
	if err != nil {
		return ""
	}
	return c.Value
}

// setSessionCookie sets a session cookie without Max-Age or Expires: the
// browser drops it when it closes, and the server enforces the lifetimes.
func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName(r), Value: token, Path: "/",
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName(r), Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode,
	})
}

// ---- the account endpoints -----------------------------------------------

type authAPI struct{ cfg config }

// session returns the live session of the request's cookie. A cookie that
// names no live session is cleared.
func (a *authAPI) session(w http.ResponseWriter, r *http.Request, activity bool) (*auth.Session, bool) {
	token := sessionToken(r)
	if token == "" {
		return nil, false
	}
	if a.cfg.auth != nil {
		if s, _, ok := a.cfg.auth.Sessions().Lookup(token, activity); ok {
			return s, true
		}
	}
	clearSessionCookie(w, r)
	return nil, false
}

func (a *authAPI) record(r *http.Request, user string, action audit.Action, target audit.Target, result audit.Result) {
	if a.cfg.audit == nil {
		return
	}
	a.cfg.audit.Record(audit.Event{User: user, Action: action, Target: target, Result: result, CorrelationID: correlationID(r)})
}

// reject audits a refused request (aggregated by the audit log).
func (a *authAPI) reject(r *http.Request, target audit.Target, result audit.Result) {
	a.record(r, audit.NoUser, audit.RequestRejected, target, result)
}

// sessionResponse is GET /api/v1/session and the answer to login and setup.
// Username, CSRF token and expiry are present only when authenticated.
type sessionResponse struct {
	SchemaVersion      int    `json:"schema_version"`
	State              string `json:"state"` // setup_required | login_required | authenticated
	Username           string `json:"username,omitempty"`
	CSRFToken          string `json:"csrf_token,omitempty"`
	ExpiresAt          string `json:"expires_at,omitempty"`
	IdleExpiresAt      string `json:"idle_expires_at,omitempty"`
	IdleTimeoutSeconds int    `json:"idle_timeout_seconds,omitempty"`
}

func authenticated(s *auth.Session, e auth.Expiry) sessionResponse {
	return sessionResponse{
		SchemaVersion: SessionSchemaVersion, State: "authenticated", Username: s.Username, CSRFToken: s.CSRFToken,
		ExpiresAt:          e.ExpiresAt.UTC().Format(time.RFC3339),
		IdleExpiresAt:      e.IdleExpiresAt.UTC().Format(time.RFC3339),
		IdleTimeoutSeconds: int(auth.IdleTimeout / time.Second),
	}
}

func (a *authAPI) sessionStatus(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	m := a.cfg.auth
	if m.SetupRequired() {
		writeJSON(w, http.StatusOK, sessionResponse{SchemaVersion: SessionSchemaVersion, State: "setup_required"})
		return
	}
	if token := sessionToken(r); token != "" {
		// Asking for the status is not activity: it does not extend the session.
		if s, e, ok := m.Sessions().Lookup(token, false); ok {
			writeJSON(w, http.StatusOK, authenticated(s, e))
			return
		}
		clearSessionCookie(w, r)
	}
	writeJSON(w, http.StatusOK, sessionResponse{SchemaVersion: SessionSchemaVersion, State: "login_required"})
}

// decodeBody reads a small JSON object with exactly the known fields.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAuthBody))
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil && dec.More() {
		err = errors.New("trailing data")
	}
	if err == nil {
		return true
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, r, http.StatusRequestEntityTooLarge, apiError{
			Code:     "request_too_large",
			Message:  "The request is too large.",
			Recovery: "Use a shorter user name and password.",
		})
		return false
	}
	writeError(w, r, http.StatusBadRequest, apiError{
		Code:     "invalid_request",
		Message:  "The request could not be read.",
		Recovery: "Reload the page and try again.",
	})
	return false
}

// startSession sets the cookie and answers like GET /api/v1/session. A
// session the request already had is ended first, so a browser holds one.
func (a *authAPI) startSession(w http.ResponseWriter, r *http.Request, token string, s *auth.Session) {
	if old := sessionToken(r); old != "" {
		a.cfg.auth.Logout(old)
	}
	_, e, _ := a.cfg.auth.Sessions().Lookup(token, false)
	setSessionCookie(w, r, token)
	writeJSON(w, http.StatusOK, authenticated(s, e))
}

func (a *authAPI) login(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	token, s, err := a.cfg.auth.Login(r.Context(), body.Username, body.Password)
	if err != nil {
		a.accountError(w, r, audit.LoginFailed, audit.TargetSession, audit.NoUser, err)
		return
	}
	a.record(r, s.Username, audit.LoginSucceeded, audit.TargetSession, audit.Success)
	a.startSession(w, r, token, s)
}

func (a *authAPI) setup(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	var body struct {
		SetupToken string `json:"setup_token"`
		Username   string `json:"username"`
		Password   string `json:"password"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	token, s, err := a.cfg.auth.Setup(r.Context(), body.SetupToken, body.Username, body.Password)
	if err != nil {
		a.accountError(w, r, audit.LoginFailed, audit.TargetSetup, audit.NoUser, err)
		return
	}
	a.record(r, s.Username, audit.SetupCompleted, audit.TargetSetup, audit.Success)
	a.startSession(w, r, token, s)
}

func (a *authAPI) logout(w http.ResponseWriter, r *http.Request, s *auth.Session) {
	a.cfg.auth.Logout(sessionToken(r))
	clearSessionCookie(w, r)
	a.record(r, s.Username, audit.Logout, audit.TargetSession, audit.Success)
	writeJSON(w, http.StatusOK, sessionResponse{SchemaVersion: SessionSchemaVersion, State: "login_required"})
}

func (a *authAPI) changePassword(w http.ResponseWriter, r *http.Request, s *auth.Session) {
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if err := a.cfg.auth.ChangePassword(r.Context(), s, body.CurrentPassword, body.NewPassword); err != nil {
		a.accountError(w, r, audit.PasswordChangeFailed, audit.TargetPassword, s.Username, err)
		return
	}
	a.record(r, s.Username, audit.PasswordChanged, audit.TargetPassword, audit.Success)
	_, e, _ := a.cfg.auth.Sessions().Lookup(sessionToken(r), false)
	writeJSON(w, http.StatusOK, authenticated(s, e))
}

// accountError maps an error of the auth package to a typed API error and
// audits the failure. Only failures that come from a guess are audited as
// failed attempts; a policy error on the password is the user's typo.
func (a *authAPI) accountError(w http.ResponseWriter, r *http.Request, action audit.Action, target audit.Target, user string, err error) {
	var limited *auth.RateLimitedError
	var policy *auth.PolicyError
	switch {
	case errors.As(err, &limited):
		a.record(r, user, action, target, audit.RateLimited)
		secs := retryAfter(w, limited.RetryAfter)
		writeError(w, r, http.StatusTooManyRequests, apiError{
			Code:              "rate_limited",
			Message:           "Too many attempts.",
			Recovery:          "Wait and try again.",
			RetryAfterSeconds: secs,
		})
	case errors.Is(err, auth.ErrInvalidCredentials):
		a.record(r, user, action, target, audit.InvalidCredentials)
		writeError(w, r, http.StatusUnauthorized, apiError{
			Code:     "invalid_credentials",
			Message:  "The user name or the password is wrong.",
			Recovery: "Check both and try again.",
		})
	case errors.Is(err, auth.ErrInvalidSetupToken):
		a.record(r, user, action, target, audit.InvalidSetupToken)
		writeError(w, r, http.StatusForbidden, apiError{
			Code:     "invalid_setup_token",
			Message:  "The setup token is wrong.",
			Recovery: "Read it again on the device with: sudo cat /var/lib/oarfish/setup-token",
		})
	case errors.Is(err, auth.ErrWrongPassword):
		a.record(r, user, action, target, audit.WrongPassword)
		writeError(w, r, http.StatusForbidden, apiError{
			Code:     "wrong_password",
			Message:  "The current password is wrong.",
			Recovery: "Enter your current password again.",
		})
	case errors.As(err, &policy):
		if action == audit.PasswordChangeFailed {
			a.record(r, user, action, target, audit.PolicyRejected)
		}
		writeError(w, r, http.StatusBadRequest, apiError{
			Code:     policy.Code,
			Message:  "The user name or the new password does not meet the rules.",
			Recovery: "User name: 1 to 32 characters a-z, 0-9, dot, dash, underscore. Password: 12 to 128 characters, not a common password.",
		})
	case errors.Is(err, auth.ErrSetupDone):
		writeError(w, r, http.StatusConflict, apiError{
			Code:     "setup_not_available",
			Message:  "The administrator account exists already.",
			Recovery: "Sign in.",
		})
	case errors.Is(err, auth.ErrSetupRequired):
		writeError(w, r, http.StatusUnauthorized, errSetupRequired)
	case r.Context().Err() != nil:
		// The client left while waiting for the hash; nobody reads an answer.
	default:
		a.record(r, user, action, target, audit.Failed)
		writeError(w, r, http.StatusServiceUnavailable, apiError{
			Code:     "account_unavailable",
			Message:  "The account could not be read or saved.",
			Recovery: "Try again. If it repeats, look at the service log on the device.",
		})
	}
}
