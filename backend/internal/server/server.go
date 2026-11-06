// Package server implements the Oarfish HTTP surface.
package server

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/swingstate/oarfish/backend/internal/audit"
	"github.com/swingstate/oarfish/backend/internal/auth"
	"github.com/swingstate/oarfish/backend/internal/version"
)

// Option configures NewHandler.
type Option func(*config)

type config struct {
	device  DeviceSource
	metrics *metricsConfig
	// streamsDone is closed when the server shuts down, so that open event
	// streams end and Shutdown does not wait for them.
	streamsDone  <-chan struct{}
	auth         *auth.Manager
	audit        *audit.Log
	allowedHosts []string
	updater      UpdateService
}

// WithDevice enables GET /api/v1/device, backed by src.
func WithDevice(src DeviceSource) Option {
	return func(c *config) { c.device = src }
}

// NewHandler returns the HTTP handler serving /healthz, the API under /api/
// and the given frontend assets. Every request gets a correlation ID and the
// security headers, and is refused unless its Host header names the device by
// a loopback name (DNS rebinding, ADR 0009 E3). /healthz and the frontend files
// are public; the API is behind the session rules of apiHandler.
func NewHandler(assets fs.FS, opts ...Option) http.Handler {
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}
	streamsCtx, closeStreams := context.WithCancel(context.Background())
	cfg.streamsDone = streamsCtx.Done()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /readyz", readyHandler(cfg, assets))
	mux.Handle("GET /", staticHandler(assets))
	api := apiHandler(cfg)
	return &handler{closeStreams: closeStreams, Handler: withCorrelation(securityHeaders(hostGuard(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /api/ is routed by prefix, not by the mux, so that every method and
		// every unknown path under it gets the same typed JSON error.
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	}))))}
}

// handler is the http.Handler NewHandler returns. New registers CloseStreams
// as the server's shutdown hook.
type handler struct {
	http.Handler
	closeStreams context.CancelFunc
}

// CloseStreams ends all open event streams. Without it a graceful Shutdown
// would wait for browsers that keep their stream open until its timeout.
func (h *handler) CloseStreams() { h.closeStreams() }

// Timeouts are the http.Server timeouts. WriteTimeout bounds a whole response,
// which suits every endpoint except the event stream; that handler sets its
// own write deadline before each write (see handleEvents).
type Timeouts struct {
	ReadHeader, Read, Write, Idle time.Duration
}

// DefaultTimeouts are the conservative production values.
var DefaultTimeouts = Timeouts{ReadHeader: 5 * time.Second, Read: 15 * time.Second, Write: 30 * time.Second, Idle: 60 * time.Second}

// New returns an http.Server for addr with conservative timeouts.
func New(addr string, handler http.Handler) *http.Server {
	return NewWithTimeouts(addr, handler, DefaultTimeouts)
}

// NewWithTimeouts is New with explicit timeouts; tests use short ones.
func NewWithTimeouts(addr string, handler http.Handler, t Timeouts) *http.Server {
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: t.ReadHeader,
		ReadTimeout:       t.Read,
		WriteTimeout:      t.Write,
		IdleTimeout:       t.Idle,
		MaxHeaderBytes:    16 << 10,
	}
	if c, ok := handler.(interface{ CloseStreams() }); ok {
		srv.RegisterOnShutdown(c.CloseStreams)
	}
	return srv
}

// healthz is the public health answer (ADR 0010 A11.2, D-C57, D-C68). The
// version is public (specification 10.3); the process ID lets the update
// helper compare it with the MainPID of systemd, so that another process on
// the loopback port cannot pass the check. The installer only looks for
// "status":"ok", so the order of the members matters and status stays first.
type healthz struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	PID     int    `json:"pid"`
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthz{Status: "ok", Version: version.Version, PID: os.Getpid()})
}

// readyHandler answers /readyz (specification 13.1, ADR 0010 A11.2): the
// configuration (the access store and the audit log were opened), the profiles
// (the detection result exists and names a profile, which needs the embedded
// profile registry) and
// the required local resources (the embedded frontend has its index.html) are
// ready. The checks look at memory only and do no disk access. Not ready is
// 503 with a body that does not contain "status":"ok".
func readyHandler(cfg config, assets fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		ready := cfg.auth != nil && cfg.audit != nil && cfg.device != nil && cfg.device.Result().Profile.ID != ""
		if ready {
			_, err := fs.Stat(assets, "index.html")
			ready = err == nil
		}
		if !ready {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// staticHandler serves the embedded frontend. A build without frontend assets
// answers 503 rather than a misleading 404.
func staticHandler(assets fs.FS) http.Handler {
	files := http.FileServerFS(assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(assets, "index.html"); err != nil {
			http.Error(w, "frontend assets are not embedded in this build", http.StatusServiceUnavailable)
			return
		}
		files.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
