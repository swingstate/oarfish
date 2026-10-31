// Package server implements the Oarfish HTTP surface.
package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/swingstate/oarfish/backend/internal/audit"
	"github.com/swingstate/oarfish/backend/internal/auth"
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

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
