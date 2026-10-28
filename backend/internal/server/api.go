package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/swingstate/oarfish/backend/internal/audit"
	"github.com/swingstate/oarfish/backend/internal/auth"
)

// apiError is the typed error of the /api/v1 surface (Pflichtenheft 8.7): a
// user-readable message, a technical code, a recovery hint and the server's
// correlation ID of the request. RetryAfterSeconds is set on rate_limited only.
type apiError struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	Recovery          string `json:"recovery"`
	CorrelationID     string `json:"correlation_id"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, e apiError) {
	e.CorrelationID = correlationID(r)
	writeJSON(w, status, map[string]apiError{"error": e})
}

// access says who may call a route.
type access int

const (
	// needsSession: a valid session; state-changing methods also need the CSRF header.
	needsSession access = iota
	// public: no session (session status and login). Still refused in setup mode
	// unless setupOpen is set.
	public
)

// endpoint is one method of one route.
type endpoint struct {
	access access
	// setupOpen endpoints stay reachable while no account exists (session
	// status and setup); every other one answers setup_required (E5).
	setupOpen bool
	// activity: a request here counts as use of the session and moves its idle
	// deadline. The event stream and the session status do not (E2).
	activity bool
	handle   func(w http.ResponseWriter, r *http.Request, s *auth.Session)
}

type route struct {
	target    audit.Target
	endpoints map[string]endpoint
}

func (rt route) allow() string {
	methods := make([]string, 0, len(rt.endpoints))
	for m := range rt.endpoints {
		methods = append(methods, m)
	}
	sort.Strings(methods)
	return strings.Join(methods, ", ")
}

// stateChanging is every method except GET, HEAD and OPTIONS (E3).
func stateChanging(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// apiHandler serves /api/v1. Only the four account endpoints accept a
// state-changing method; everything else is GET only and takes no body. Every
// route except session status, login and setup needs a session (D-C46).
func apiHandler(cfg config) http.Handler {
	a := &authAPI{cfg: cfg}
	routes := map[string]route{
		"/api/v1/session": {target: audit.TargetSession, endpoints: map[string]endpoint{
			http.MethodGet:    {access: public, setupOpen: true, handle: a.sessionStatus},
			http.MethodPost:   {access: public, handle: a.login},
			http.MethodDelete: {access: needsSession, activity: true, handle: a.logout},
		}},
		"/api/v1/setup": {target: audit.TargetSetup, endpoints: map[string]endpoint{
			http.MethodPost: {access: public, setupOpen: true, handle: a.setup},
		}},
		"/api/v1/account/password": {target: audit.TargetPassword, endpoints: map[string]endpoint{
			http.MethodPost: {access: needsSession, activity: true, handle: a.changePassword},
		}},
		"/api/v1/device": {target: audit.TargetDevice, endpoints: map[string]endpoint{
			http.MethodGet: {access: needsSession, activity: true, handle: func(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
				if cfg.device == nil {
					writeError(w, r, http.StatusServiceUnavailable, apiError{
						Code:     "device_info_unavailable",
						Message:  "Device detection is not available in this build.",
						Recovery: "Restart Oarfish.",
					})
					return
				}
				handleDevice(w, cfg.device)
			}},
		}},
		"/api/v1/metrics": {target: audit.TargetMetrics, endpoints: map[string]endpoint{
			http.MethodGet: {access: needsSession, activity: true, handle: func(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
				handleMetrics(w, r, cfg)
			}},
		}},
		"/api/v1/events": {target: audit.TargetEvents, endpoints: map[string]endpoint{
			http.MethodGet: {access: needsSession, handle: func(w http.ResponseWriter, r *http.Request, s *auth.Session) {
				handleEvents(w, r, cfg, s.Done())
			}},
		}},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rt, ok := routes[r.URL.Path]
		if !ok {
			writeError(w, r, http.StatusNotFound, apiError{
				Code:     "not_found",
				Message:  "This API endpoint does not exist.",
				Recovery: "Use GET /api/v1/session, GET /api/v1/device, GET /api/v1/metrics or GET /api/v1/events.",
			})
			return
		}
		ep, ok := rt.endpoints[r.Method]
		if !ok {
			w.Header().Set("Allow", rt.allow())
			writeError(w, r, http.StatusMethodNotAllowed, apiError{
				Code:     "method_not_allowed",
				Message:  "This endpoint does not accept this method.",
				Recovery: "Use " + rt.allow() + ".",
			})
			return
		}
		changing := stateChanging(r.Method)
		if changing {
			if !originAllowed(r) {
				a.reject(r, rt.target, audit.OriginRejected)
				writeError(w, r, http.StatusForbidden, apiError{
					Code:     "origin_rejected",
					Message:  "The request does not come from the Oarfish page.",
					Recovery: "Open Oarfish in the browser and use it there.",
				})
				return
			}
			if !jsonContentType(r) {
				writeError(w, r, http.StatusUnsupportedMediaType, apiError{
					Code:     "unsupported_media_type",
					Message:  "This endpoint only accepts JSON.",
					Recovery: "Send Content-Type: application/json.",
				})
				return
			}
		} else if r.ContentLength > 0 || len(r.TransferEncoding) > 0 {
			writeError(w, r, http.StatusBadRequest, apiError{
				Code:     "request_body_not_allowed",
				Message:  "This endpoint does not take a request body.",
				Recovery: "Send the request without a body.",
			})
			return
		}
		m := cfg.auth
		if m == nil && ep.access == public {
			writeError(w, r, http.StatusServiceUnavailable, apiError{
				Code:     "auth_unavailable",
				Message:  "Sign-in is not available in this build.",
				Recovery: "Restart Oarfish.",
			})
			return
		}
		if m != nil && !ep.setupOpen && m.SetupRequired() {
			writeError(w, r, http.StatusUnauthorized, errSetupRequired)
			return
		}
		var sess *auth.Session
		if ep.access == needsSession {
			var ok bool
			if sess, ok = a.session(w, r, ep.activity); !ok {
				writeError(w, r, http.StatusUnauthorized, apiError{
					Code:     "unauthenticated",
					Message:  "You are not signed in, or the session has ended.",
					Recovery: "Sign in again.",
				})
				return
			}
			if changing && !csrfValid(r, sess) {
				a.reject(r, rt.target, audit.CSRFFailed)
				writeError(w, r, http.StatusForbidden, apiError{
					Code:     "csrf_failed",
					Message:  "The request is missing its protection token.",
					Recovery: "Reload the page and try again.",
				})
				return
			}
		}
		ep.handle(w, r, sess)
	})
}

var errSetupRequired = apiError{
	Code:     "setup_required",
	Message:  "Oarfish has no administrator account yet.",
	Recovery: "Create the account on the setup page with the setup token from the device.",
}

// retryAfter writes the Retry-After header (whole seconds, rounded up) and
// returns the same number for the error body.
func retryAfter(w http.ResponseWriter, d time.Duration) int {
	s := int((d + time.Second - 1) / time.Second)
	if s < 1 {
		s = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(s))
	return s
}
