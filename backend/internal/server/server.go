// Package server implements the Oarfish HTTP surface.
package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"time"
)

// NewHandler returns the HTTP handler serving /healthz and the given frontend assets.
func NewHandler(assets fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.Handle("GET /", staticHandler(assets))
	return securityHeaders(mux)
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
