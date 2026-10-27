package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/swingstate/oarfish/backend/internal/metrics"
)

// MetricsSource is what the metrics endpoints need from the sampler.
// *metrics.Sampler implements it.
type MetricsSource interface {
	Once(ctx context.Context) (metrics.Frame, error)
	Subscribe() (*metrics.Subscription, error)
	History() metrics.History
}

// Stream timing. The write timeout is the longest a single write to a stream
// client may take; a client that does not read for that long is dropped.
const (
	DefaultHeartbeat          = 15 * time.Second
	DefaultStreamWriteTimeout = 10 * time.Second
	// streamRetry is the reconnect delay the browser is told to use (SSE "retry").
	streamRetry = 3 * time.Second
)

type metricsConfig struct {
	src          MetricsSource
	heartbeat    time.Duration
	writeTimeout time.Duration
}

// MetricsOption tunes the metrics endpoints; tests use it to shorten the timing.
type MetricsOption func(*metricsConfig)

// WithHeartbeat sets the interval of the SSE heartbeat comments.
func WithHeartbeat(d time.Duration) MetricsOption { return func(c *metricsConfig) { c.heartbeat = d } }

// WithStreamWriteTimeout sets how long one write to a stream client may take.
func WithStreamWriteTimeout(d time.Duration) MetricsOption {
	return func(c *metricsConfig) { c.writeTimeout = d }
}

// WithMetrics enables GET /api/v1/metrics and GET /api/v1/events, backed by src.
func WithMetrics(src MetricsSource, opts ...MetricsOption) Option {
	return func(c *config) {
		m := &metricsConfig{src: src, heartbeat: DefaultHeartbeat, writeTimeout: DefaultStreamWriteTimeout}
		for _, o := range opts {
			o(m)
		}
		if m.heartbeat <= 0 {
			m.heartbeat = DefaultHeartbeat
		}
		if m.writeTimeout <= 0 {
			m.writeTimeout = DefaultStreamWriteTimeout
		}
		c.metrics = m
	}
}

// metricsResponse is the wire format of GET /api/v1/metrics: the snapshot
// (flattened), the static device summary Home needs, the current alerts and
// the recent history for the sparklines.
type metricsResponse struct {
	metrics.Snapshot
	Device  metricsDevice   `json:"device"`
	Alerts  []metrics.Alert `json:"alerts"`
	History metrics.History `json:"history"`
}

// metricsDevice is the part of the device detection result that Home shows. It
// does not change while the service runs.
type metricsDevice struct {
	Model          *string `json:"model"`
	SoCFamily      *string `json:"soc_family"`
	Arch           *string `json:"arch"`
	OSPrettyName   *string `json:"os_pretty_name"`
	KernelRelease  *string `json:"kernel_release"`
	SupportTier    *string `json:"support_tier"`
	SupportReason  *string `json:"support_reason"`
	ExplanationKey *string `json:"support_explanation_key"`
	GenericMode    bool    `json:"generic_mode"`
}

// alertsEvent is the payload of the "alerts" event.
type alertsEvent struct {
	SchemaVersion int             `json:"schema_version"`
	Alerts        []metrics.Alert `json:"alerts"`
}

func metricsUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, apiError{
		Code:     "metrics_unavailable",
		Message:  "Live metrics are not available in this build.",
		Recovery: "Restart Oarfish.",
	})
}

func handleMetrics(w http.ResponseWriter, r *http.Request, cfg config) {
	if cfg.metrics == nil {
		metricsUnavailable(w)
		return
	}
	frame, err := cfg.metrics.src.Once(r.Context())
	if err != nil {
		if r.Context().Err() != nil {
			return // the client left
		}
		writeError(w, http.StatusServiceUnavailable, apiError{
			Code:     "metrics_unavailable",
			Message:  "The device could not be read just now.",
			Recovery: "Try again in a few seconds.",
		})
		return
	}
	writeJSON(w, http.StatusOK, metricsResponse{
		Snapshot: frame.Snapshot,
		Device:   deviceSummary(cfg.device),
		Alerts:   frame.Alerts,
		History:  cfg.metrics.src.History(),
	})
}

func deviceSummary(src DeviceSource) metricsDevice {
	if src == nil {
		return metricsDevice{}
	}
	r := src.Result()
	arch := r.Hardware.Arch
	return metricsDevice{
		Model:          nullable(r.Hardware.Model),
		SoCFamily:      nullable(r.SoCFamily),
		Arch:           nullable(arch),
		OSPrettyName:   nullable(r.OS.PrettyName),
		KernelRelease:  nullable(r.OS.Kernel),
		SupportTier:    nullable(r.Support.Tier),
		SupportReason:  nullable(r.Support.Reason),
		ExplanationKey: nullable(r.Support.ExplanationKey),
		GenericMode:    r.Support.Tier == "generic",
	}
}

// handleEvents serves the Server-Sent Events stream. A client counts as a
// watcher for as long as the request lasts: sampling starts with it and stops
// after the last one. The stream carries
//
//	event: history   the ring, once at the start
//	event: alerts    at the start and whenever the alert list changes
//	event: metrics   about every two seconds
//	: heartbeat      a comment every 15 seconds, so proxies and the browser see the line is alive
func handleEvents(w http.ResponseWriter, r *http.Request, cfg config) {
	if cfg.metrics == nil {
		metricsUnavailable(w)
		return
	}
	select {
	case <-cfg.streamsDone:
		w.Header().Set("Retry-After", strconv.Itoa(int(streamRetry/time.Second)))
		writeError(w, http.StatusServiceUnavailable, apiError{
			Code:     "shutting_down",
			Message:  "Oarfish is shutting down.",
			Recovery: "Reload this page in a few seconds.",
		})
		return
	default:
	}
	m := cfg.metrics
	sub, err := m.src.Subscribe()
	if errors.Is(err, metrics.ErrTooManyClients) {
		w.Header().Set("Retry-After", strconv.Itoa(int(streamRetry/time.Second)))
		writeError(w, http.StatusServiceUnavailable, apiError{
			Code:     "too_many_stream_clients",
			Message:  "Too many browser windows are watching this device.",
			Recovery: "Close another Oarfish window and reload this page.",
		})
		return
	}
	if err != nil {
		metricsUnavailable(w)
		return
	}
	defer sub.Close()

	// http.Server.WriteTimeout covers the whole response and would end every
	// stream after 30 seconds. So each write sets its own deadline instead,
	// which replaces the server's: a client that stops reading is dropped
	// after writeTimeout and nothing waits for it forever. ReadTimeout needs
	// no change: it ends when the request (a GET without a body) is read, and
	// TestEventStreamOutlivesTheServerTimeouts checks that a stream outlives it.
	rc := http.NewResponseController(w)
	send := func(chunk string) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(m.writeTimeout))
		if _, err := w.Write([]byte(chunk)); err != nil {
			return false
		}
		return rc.Flush() == nil
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if !send(fmt.Sprintf("retry: %d\n\n", streamRetry.Milliseconds())) {
		return
	}
	if !sendEvent(send, "history", m.src.History()) {
		return
	}

	heartbeat := time.NewTicker(m.heartbeat)
	defer heartbeat.Stop()
	var sentAlerts uint64
	first := true
	for {
		select {
		case <-r.Context().Done():
			return
		case <-cfg.streamsDone:
			return
		case <-heartbeat.C:
			if !send(": heartbeat\n\n") {
				return
			}
		case f := <-sub.C():
			if first || f.AlertsVersion != sentAlerts {
				if !sendEvent(send, "alerts", alertsEvent{SchemaVersion: metrics.SchemaVersion, Alerts: f.Alerts}) {
					return
				}
				sentAlerts, first = f.AlertsVersion, false
			}
			if !sendEvent(send, "metrics", f.Snapshot) {
				return
			}
		}
	}
}

func sendEvent(send func(string) bool, name string, v any) bool {
	data, err := json.Marshal(v)
	if err != nil {
		return false
	}
	return send("event: " + name + "\ndata: " + string(data) + "\n\n")
}
