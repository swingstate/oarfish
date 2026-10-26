package server

import (
	"encoding/json"
	"net/http"
)

// apiError is the typed error of the /api/v1 surface (Pflichtenheft 8.7): a
// user-readable message, a technical code and a recovery hint.
type apiError struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Recovery string `json:"recovery"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, e apiError) {
	writeJSON(w, status, map[string]apiError{"error": e})
}

// apiHandler serves /api/v1. Everything on it is read-only: only GET is
// accepted and a request body is refused.
func apiHandler(cfg config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/device" {
			writeError(w, http.StatusNotFound, apiError{
				Code:     "not_found",
				Message:  "This API endpoint does not exist.",
				Recovery: "Use GET /api/v1/device.",
			})
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeError(w, http.StatusMethodNotAllowed, apiError{
				Code:     "method_not_allowed",
				Message:  "This endpoint is read-only.",
				Recovery: "Use GET.",
			})
			return
		}
		if r.ContentLength > 0 || len(r.TransferEncoding) > 0 {
			writeError(w, http.StatusBadRequest, apiError{
				Code:     "request_body_not_allowed",
				Message:  "This endpoint does not take a request body.",
				Recovery: "Send the request without a body.",
			})
			return
		}
		if cfg.device == nil {
			writeError(w, http.StatusServiceUnavailable, apiError{
				Code:     "device_info_unavailable",
				Message:  "Device detection is not available in this build.",
				Recovery: "Restart Oarfish.",
			})
			return
		}
		handleDevice(w, cfg.device)
	})
}
