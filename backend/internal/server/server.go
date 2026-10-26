// Package server implements the Oarfish HTTP surface.
package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

// Option configures NewHandler.
type Option func(*config)

type config struct {
	device DeviceSource
}

// WithDevice enables GET /api/v1/device, backed by src.
func WithDevice(src DeviceSource) Option {
	return func(c *config) { c.device = src }
}

// NewHandler returns the HTTP handler serving /healthz, the read-only API
// under /api/ and the given frontend assets.
func NewHandler(assets fs.FS, opts ...Option) http.Handler {
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.Handle("GET /", staticHandler(assets))
	api := apiHandler(cfg)
	return securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /api/ is routed by prefix, not by the mux, so that every method and
		// every unknown path under it gets the same typed JSON error.
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	}))
}

// New returns an http.Server for addr with conservative timeouts.
func New(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
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
