package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/swingstate/oarfish/backend/internal/metrics"
	"github.com/swingstate/oarfish/backend/internal/metrics/metricstest"
	"github.com/swingstate/oarfish/backend/internal/platform/detector"
	"github.com/swingstate/oarfish/backend/internal/platform/platformtest"
	"github.com/swingstate/oarfish/backend/internal/platform/profiles"
)

var piSizes = map[string]metrics.Statfs{
	"/":              {BlockSize: 4096, Blocks: 1000000, Free: 700000, Available: 650000},
	"/boot/firmware": {BlockSize: 512, Blocks: 1000000, Free: 900000, Available: 900000},
}

// metricsFixture wires a sampler on a metrics fixture, with short timing, to a
// handler that also has device detection on a detection fixture.
type metricsFixture struct {
	live    *metricstest.Live
	sampler *metrics.Sampler
	handler http.Handler
}

func newMetricsFixture(t *testing.T, fixture string, maxClients int) *metricsFixture {
	t.Helper()
	live := metricstest.NewLive(t, fixture)
	reg, err := profiles.Load()
	if err != nil {
		t.Fatal(err)
	}
	dev := detector.New(platformtest.Fixture(t, "pi-zero-2w-raspios"), linuxARM64, reg)
	s := metrics.NewSampler(metrics.Config{
		Collector:  metrics.NewCollector(live, metricstest.Statfs(piSizes)),
		Interval:   15 * time.Millisecond,
		Warmup:     5 * time.Millisecond,
		OnceWindow: 5 * time.Millisecond,
		MaxClients: maxClients,
		AlertContext: func() metrics.AlertContext {
			return metrics.AlertContext{SupportTier: dev.Result().Support.Tier}
		},
	})
	h := NewHandler(fstest.MapFS{}, WithDevice(dev), WithMetrics(s, WithHeartbeat(40*time.Millisecond), WithStreamWriteTimeout(time.Second)))
	f := &metricsFixture{live: live, sampler: s, handler: h}
	f.tickCPU(t)
	return f
}

// tickCPU makes the CPU counters of the fixture advance, as the kernel's do,
// so that usage can be computed from two samples.
func (f *metricsFixture) tickCPU(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		for n := 1; ; n++ {
			select {
			case <-stop:
				return
			case <-time.After(time.Millisecond):
			}
			var b strings.Builder
			const base = 100_000_000 // above the fixture, so the counters only go forward
			fmt.Fprintf(&b, "cpu %d 0 %d %d 0 0 0 0\n", base+400*n, base+200*n, base+1400*n)
			for i := 0; i < 4; i++ {
				fmt.Fprintf(&b, "cpu%d %d 0 %d %d 0 0 0 0\n", i, base/4+100*n, base/4+50*n, base/4+350*n)
			}
			f.live.Set("proc/stat", b.String())
		}
	}()
}

func getMetrics(t *testing.T, h http.Handler) (*httptest.ResponseRecorder, metricsResponse) {
	t.Helper()
	rec := get(t, h, http.MethodGet, "/api/v1/metrics")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields() // exactly the documented fields
	var resp metricsResponse
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, rec.Body)
	}
	return rec, resp
}

// ORB-DASH-001, AT-008: one snapshot with schema_version, the static device
// summary Home needs, and real values.
func TestMetricsEndpointPiZero2W(t *testing.T) {
	f := newMetricsFixture(t, "pi-zero-2w", 4)
	rec, resp := getMetrics(t, f.handler)

	if resp.SchemaVersion != 1 || resp.History.SchemaVersion != 1 {
		t.Errorf("schema versions %d / %d", resp.SchemaVersion, resp.History.SchemaVersion)
	}
	if resp.Hostname == nil || *resp.Hostname != "oarfish-test" {
		t.Errorf("hostname = %v", resp.Hostname)
	}
	if resp.UptimeSeconds == nil || resp.CPU.Cores == nil || *resp.CPU.Cores != 4 || resp.Memory.TotalBytes == nil {
		t.Errorf("core values missing: %s", rec.Body)
	}
	if len(resp.Temperatures) != 1 || *resp.Temperatures[0].Celsius != 43.3 || len(resp.Storage) != 2 || len(resp.Network) != 1 {
		t.Errorf("lists: %s", rec.Body)
	}
	if resp.CPU.UsagePercent == nil {
		t.Error("a one-shot snapshot reads the CPU twice and has a usage value")
	}

	d := resp.Device
	if str(d.Model) != "Raspberry Pi Zero 2 W Rev 1.0" || str(d.SoCFamily) != "BCM2837" || str(d.Arch) != "arm64" ||
		str(d.SupportTier) != "compatible" || d.GenericMode || str(d.KernelRelease) == "<null>" || str(d.OSPrettyName) == "<null>" {
		t.Errorf("device summary = %+v", d)
	}
	// The Compatible tier is not an alert (D-C44) and the healthy fixture raises nothing else.
	if resp.Alerts == nil || len(resp.Alerts) != 0 {
		t.Errorf("alerts = %+v (nil: %v), want an empty list", resp.Alerts, resp.Alerts == nil)
	}

	// The device endpoint is unchanged and still works next to it.
	if rec := get(t, f.handler, http.MethodGet, "/api/v1/device"); rec.Code != http.StatusOK {
		t.Errorf("/api/v1/device = %d", rec.Code)
	}
	if f.sampler.Running() {
		t.Error("a plain GET must not start sampling")
	}
}

func TestMetricsEndpointHeaders(t *testing.T) {
	f := newMetricsFixture(t, "pi-zero-2w", 4)
	rec := get(t, f.handler, http.MethodGet, "/api/v1/metrics")
	for name, want := range map[string]string{
		"Content-Type": "application/json", "Cache-Control": "no-store",
		"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY", "Referrer-Policy": "no-referrer",
	} {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("missing Content-Security-Policy")
	}
}

// The documented layout: required keys, null for unknown, lists never null.
func TestMetricsSchemaUsesNullsAndEmptyLists(t *testing.T) {
	f := newMetricsFixture(t, "minimal", 4)
	rec := get(t, f.handler, http.MethodGet, "/api/v1/metrics")
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema_version", "collected_at", "hostname", "uptime_seconds", "cpu", "memory", "temperatures", "storage", "network", "device", "alerts", "history"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q", key)
		}
	}
	for _, key := range []string{"temperatures", "storage", "network", "alerts"} {
		if l, ok := m[key].([]any); !ok || len(l) != 0 && key != "alerts" {
			t.Errorf("%s = %#v, want a list (empty for this fixture)", key, m[key])
		}
	}
	cpu := m["cpu"].(map[string]any)
	if cpu["governor"] != nil || cpu["frequency"].(map[string]any)["current_mhz"] != nil || cpu["frequency"].(map[string]any)["max_mhz"] != nil {
		t.Errorf("missing cpufreq must be null: %v", cpu)
	}
	mem := m["memory"].(map[string]any)
	if mem["swap_total_bytes"] != nil || mem["swap_used_bytes"] != nil {
		t.Errorf("missing swap must be null: %v", mem)
	}
	hist := m["history"].(map[string]any)
	for _, key := range []string{"cpu_percent", "memory_used_percent", "temperature_celsius"} {
		if l, ok := hist[key].([]any); !ok || l == nil {
			t.Errorf("history.%s = %#v, want a list", key, hist[key])
		}
	}
}

func TestMetricsEndpointsAreReadOnly(t *testing.T) {
	f := newMetricsFixture(t, "pi-zero-2w", 4)
	for _, path := range []string{"/api/v1/metrics", "/api/v1/events"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions} {
			rec := get(t, f.handler, method, path)
			if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET" {
				t.Errorf("%s %s = %d, Allow %q", method, path, rec.Code, rec.Header().Get("Allow"))
			}
			if method != http.MethodHead && !strings.Contains(rec.Body.String(), `"method_not_allowed"`) {
				t.Errorf("%s %s: body %s", method, path, rec.Body)
			}
		}
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, strings.NewReader(`{"x":1}`)))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "request_body_not_allowed") {
			t.Errorf("GET %s with a body = %d %s", path, rec.Code, rec.Body)
		}
	}
	if f.sampler.Running() || f.sampler.Clients() != 0 {
		t.Error("a refused request started sampling")
	}
	for _, p := range []string{"/api/v1/metrics/", "/api/v1/events/x", "/api/v1/metric"} {
		if rec := get(t, f.handler, http.MethodGet, p); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"not_found"`) {
			t.Errorf("%s = %d", p, rec.Code)
		}
	}
}

func TestMetricsEndpointsWithoutASamplerAre503(t *testing.T) {
	h := NewHandler(fstest.MapFS{})
	for _, path := range []string{"/api/v1/metrics", "/api/v1/events"} {
		rec := get(t, h, http.MethodGet, path)
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "metrics_unavailable") {
			t.Errorf("%s = %d %s", path, rec.Code, rec.Body)
		}
	}
}

// sseClient reads an event stream line by line.
type sseClient struct {
	t      *testing.T
	resp   *http.Response
	cancel context.CancelFunc
	lines  chan string
}

func openStream(t *testing.T, base string) *sseClient {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	c := &sseClient{t: t, resp: resp, cancel: cancel, lines: make(chan string, 1024)}
	go func() {
		defer close(c.lines)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			c.lines <- sc.Text()
		}
	}()
	t.Cleanup(c.close)
	return c
}

func (c *sseClient) close() {
	c.cancel()
	c.resp.Body.Close()
}

// next returns the next event: its name ("" for a comment) and data (or the comment text).
func (c *sseClient) next(timeout time.Duration) (name, data string, ok bool) {
	deadline := time.After(timeout)
	for {
		select {
		case line, open := <-c.lines:
			if !open {
				return "", "", false
			}
			switch {
			case strings.HasPrefix(line, ": "):
				return "", line[2:], true
			case strings.HasPrefix(line, "event: "):
				name = line[7:]
			case strings.HasPrefix(line, "data: "):
				data = line[6:]
			case strings.HasPrefix(line, "retry: "):
				name, data = "retry", line[7:]
				return name, data, true
			case line == "" && name != "":
				return name, data, true
			}
		case <-deadline:
			return "", "", false
		}
	}
}

func (c *sseClient) expect(name string) string {
	c.t.Helper()
	for i := 0; i < 50; i++ {
		n, data, ok := c.next(3 * time.Second)
		if !ok {
			c.t.Fatalf("stream ended or timed out while waiting for %q", name)
		}
		if n == name {
			return data
		}
	}
	c.t.Fatalf("no %q event", name)
	return ""
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// ORB-DASH-001, ORB-DASH-006: the stream sends the reconnect hint, the ring,
// the alerts, then metrics every interval, and a heartbeat comment when idle.
func TestEventStreamProtocol(t *testing.T) {
	f := newMetricsFixture(t, "pi-zero-2w", 4)
	srv := httptest.NewServer(f.handler)
	t.Cleanup(srv.Close)
	c := openStream(t, srv.URL)

	if ct := c.resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := c.resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	if c.resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing security headers on the stream")
	}

	if n, data, ok := c.next(2 * time.Second); !ok || n != "retry" || data != "3000" {
		t.Fatalf("first line = %q %q %v, want a retry hint", n, data, ok)
	}
	// Order at the start: history, alerts, metrics.
	var hist metrics.History
	if err := json.Unmarshal([]byte(c.expect("history")), &hist); err != nil || hist.SchemaVersion != 1 || hist.CPUPercent == nil {
		t.Fatalf("history event: %+v, %v", hist, err)
	}
	var al alertsEvent
	if err := json.Unmarshal([]byte(c.expect("alerts")), &al); err != nil || al.SchemaVersion != 1 || al.Alerts == nil || len(al.Alerts) != 0 {
		t.Fatalf("alerts event: %+v, %v", al, err)
	}
	var first metrics.Snapshot
	dec := json.NewDecoder(strings.NewReader(c.expect("metrics")))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&first); err != nil || first.SchemaVersion != 1 || first.Hostname == nil || first.CPU.UsagePercent == nil {
		t.Fatalf("metrics event: %+v, %v", first, err)
	}

	// Further metrics follow, alerts do not repeat while unchanged, and the
	// heartbeat comment arrives.
	sawHeartbeat, metricsEvents := false, 0
	for i := 0; i < 60 && (!sawHeartbeat || metricsEvents < 3); i++ {
		n, data, ok := c.next(2 * time.Second)
		if !ok {
			t.Fatal("the stream stopped")
		}
		switch {
		case n == "metrics":
			metricsEvents++
		case n == "alerts":
			t.Error("alerts were sent again although they did not change")
		case n == "" && data == "heartbeat":
			sawHeartbeat = true
		}
	}
	if !sawHeartbeat || metricsEvents < 3 {
		t.Errorf("heartbeat=%v metrics events=%d", sawHeartbeat, metricsEvents)
	}
}

// ORB-DASH-005: an alerts event follows when the alert list changes.
func TestEventStreamSendsAlertsWhenTheyChange(t *testing.T) {
	f := newMetricsFixture(t, "pi-zero-2w", 4)
	srv := httptest.NewServer(f.handler)
	t.Cleanup(srv.Close)
	c := openStream(t, srv.URL)
	c.expect("alerts")

	f.live.Set("sys/class/thermal/thermal_zone0/temp", "83000\n")
	var al alertsEvent
	if err := json.Unmarshal([]byte(c.expect("alerts")), &al); err != nil {
		t.Fatal(err)
	}
	if len(al.Alerts) != 1 || al.Alerts[0].ID != "cpu_temperature" || al.Alerts[0].Level != "critical" {
		t.Errorf("alerts = %+v", al.Alerts)
	}
}

// Sampling follows the clients: a connection starts it, a disconnect ends it,
// and nothing is left of the handler's goroutine.
func TestEventStreamDisconnectStopsSampling(t *testing.T) {
	f := newMetricsFixture(t, "pi-zero-2w", 4)
	srv := httptest.NewServer(f.handler)
	t.Cleanup(srv.Close)

	if f.sampler.Running() {
		t.Fatal("sampling before any client")
	}
	c := openStream(t, srv.URL)
	c.expect("metrics")
	if !f.sampler.Running() || f.sampler.Clients() != 1 {
		t.Fatalf("running=%v clients=%d", f.sampler.Running(), f.sampler.Clients())
	}
	c.close()
	waitUntil(t, "the sampler to stop", func() bool { return !f.sampler.Running() && f.sampler.Clients() == 0 })
	if h := f.sampler.History(); len(h.CPUPercent) != 0 {
		t.Errorf("history kept after the last client: %d points", len(h.CPUPercent))
	}
}

// The client cap: the next client gets a typed 503 and a Retry-After hint;
// the existing ones are not affected, and a freed slot works again.
func TestEventStreamClientCap(t *testing.T) {
	f := newMetricsFixture(t, "pi-zero-2w", 2)
	srv := httptest.NewServer(f.handler)
	t.Cleanup(srv.Close)

	a, b := openStream(t, srv.URL), openStream(t, srv.URL)
	a.expect("metrics")
	b.expect("metrics")

	resp, err := http.Get(srv.URL + "/api/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") != "3" ||
		resp.Header.Get("Content-Type") != "application/json" || !strings.Contains(string(body), `"too_many_stream_clients"`) {
		t.Fatalf("third client: %d %v %s", resp.StatusCode, resp.Header, body)
	}
	var e map[string]apiError
	if err := json.Unmarshal(body, &e); err != nil || e["error"].Message == "" || e["error"].Recovery == "" {
		t.Errorf("error is not typed: %s", body)
	}
	if _, _, ok := a.next(2 * time.Second); !ok {
		t.Error("the refusal disturbed an existing stream")
	}

	b.close()
	waitUntil(t, "a free slot", func() bool { return f.sampler.Clients() == 1 })
	c := openStream(t, srv.URL)
	c.expect("metrics")
}

// A client that stops reading must not keep a goroutine and a slot forever:
// the write deadline ends the handler.
type stuckWriter struct {
	header   http.Header
	deadline time.Time
	writes   int
}

func (w *stuckWriter) Header() http.Header { return w.header }
func (w *stuckWriter) WriteHeader(int)     {}
func (w *stuckWriter) Flush()              {}
func (w *stuckWriter) SetReadDeadline(time.Time) error {
	return nil
}
func (w *stuckWriter) SetWriteDeadline(t time.Time) error { w.deadline = t; return nil }

// Write blocks like a full TCP buffer until the deadline, then fails like a net.Conn.
func (w *stuckWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == 1 {
		return len(p), nil // the retry hint fits into the buffer
	}
	time.Sleep(time.Until(w.deadline))
	return 0, os.ErrDeadlineExceeded
}

func TestStuckStreamClientIsDroppedByTheWriteDeadline(t *testing.T) {
	f := newMetricsFixture(t, "pi-zero-2w", 4)
	h := NewHandler(fstest.MapFS{}, WithMetrics(f.sampler, WithStreamWriteTimeout(150*time.Millisecond), WithHeartbeat(time.Hour)))
	w := &stuckWriter{header: http.Header{}}
	done := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(done)
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/events", nil))
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler is stuck behind a client that does not read")
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("took %v to drop the client", time.Since(start))
	}
	waitUntil(t, "the slot to be released", func() bool { return f.sampler.Clients() == 0 && !f.sampler.Running() })
	if w.deadline.IsZero() {
		t.Error("no write deadline was set")
	}
}

// Deliberate fix for the server timeouts: ReadTimeout and WriteTimeout cover a
// whole request and a whole response, which would cut every stream. The stream
// handler lifts them for its own connection, other endpoints keep them.
func TestEventStreamOutlivesTheServerTimeouts(t *testing.T) {
	f := newMetricsFixture(t, "pi-zero-2w", 4)
	short := Timeouts{ReadHeader: 200 * time.Millisecond, Read: 200 * time.Millisecond, Write: 200 * time.Millisecond, Idle: time.Second}
	srv := NewWithTimeouts("127.0.0.1:0", f.handler, short)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	defer srv.Close()

	c := openStream(t, "http://"+ln.Addr().String())
	start := time.Now()
	events := 0
	for time.Since(start) < 1200*time.Millisecond { // six times the read and write timeouts
		n, _, ok := c.next(time.Second)
		if !ok {
			t.Fatalf("the stream ended after %v with %d events; the server timeouts cut it", time.Since(start), events)
		}
		if n == "metrics" {
			events++
		}
	}
	if events < 10 {
		t.Errorf("only %d metrics events in 1.2 s", events)
	}
	// The same server still answers an ordinary request.
	resp, err := http.Get("http://" + ln.Addr().String() + "/api/v1/metrics")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Errorf("GET /api/v1/metrics: %v %v", resp, err)
	}
	resp.Body.Close()
}

// A graceful shutdown must not wait for browsers that keep their stream open:
// New registers the handler's CloseStreams as the server's shutdown hook.
func TestShutdownEndsStreams(t *testing.T) {
	f := newMetricsFixture(t, "pi-zero-2w", 4)
	srv := New("127.0.0.1:0", f.handler)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	c := openStream(t, "http://"+ln.Addr().String())
	c.expect("metrics")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown with an open stream: %v after %v", err, time.Since(start))
	}
	if !errors.Is(<-served, http.ErrServerClosed) {
		t.Error("Serve did not report ErrServerClosed")
	}
	// The client sees the stream end.
	for i := 0; i < 1000; i++ {
		if _, _, ok := c.next(time.Second); !ok {
			break
		}
	}
	waitUntil(t, "the sampler to stop", func() bool { return !f.sampler.Running() })
}

// A stream requested after shutdown began is refused with a typed error.
func TestStreamIsRefusedWhileShuttingDown(t *testing.T) {
	f := newMetricsFixture(t, "pi-zero-2w", 4)
	f.handler.(interface{ CloseStreams() }).CloseStreams()
	rec := get(t, f.handler, http.MethodGet, "/api/v1/events")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "shutting_down") {
		t.Errorf("status = %d, body %s", rec.Code, rec.Body)
	}
	if f.sampler.Running() {
		t.Error("sampling started for a refused stream")
	}
}

// Privacy rules: no MAC address, serial number, machine id, user name,
// environment or process list in any API output; IP addresses and the host
// name only from the synthetic fixtures.
var privateInAPI = map[string]*regexp.Regexp{
	"MAC address":   regexp.MustCompile(`(?i)\b[0-9a-f]{2}([:-][0-9a-f]{2}){5}\b`),
	"machine id":    regexp.MustCompile(`(?i)\b[0-9a-f]{32}\b`),
	"serial number": regexp.MustCompile(`(?i)serial`),
	"user name":     regexp.MustCompile(`(?i)"(user|username|login|uid|gid|pid|cmdline|environ|env|process|processes)"`),
	"home path":     regexp.MustCompile(`/home/|/root\b|/Users/`),
	"someone":       regexp.MustCompile(`someone`), // the user name inside a mount path of the generic fixture
}

var docIPv4 = regexp.MustCompile(`\b(\d{1,3}\.){3}\d{1,3}\b`)

func TestMetricsOutputContainsNoPrivateData(t *testing.T) {
	for _, fixture := range []string{"pi-zero-2w", "generic-arm64", "minimal", "hostile"} {
		t.Run(fixture, func(t *testing.T) {
			// The generic fixture has more mounts than the Pi sizes know.
			sizes := map[string]metrics.Statfs{"/": piSizes["/"], "/boot/efi": piSizes["/"], "/run/media/someone/DATA DISK": piSizes["/"]}
			sampler := metrics.NewSampler(metrics.Config{
				Collector:  metrics.NewCollector(metricstest.NewLive(t, fixture), metricstest.Statfs(sizes)),
				OnceWindow: time.Millisecond,
			})
			h := NewHandler(fstest.MapFS{}, WithMetrics(sampler))
			body := get(t, h, http.MethodGet, "/api/v1/metrics").Body.String()

			for name, re := range privateInAPI {
				if m := re.FindString(body); m != "" {
					t.Errorf("%s in the response: %q", name, m)
				}
			}
			for _, ip := range docIPv4.FindAllString(body, -1) {
				if !strings.HasPrefix(ip, "192.0.2.") && !strings.HasPrefix(ip, "198.51.100.") && !strings.HasPrefix(ip, "203.0.113.") {
					t.Errorf("IPv4 address %s is outside the documentation ranges", ip)
				}
			}
			if strings.Contains(body, "fe80") || strings.Contains(strings.ToLower(body), "fffe") {
				t.Errorf("a link-local or EUI-64 IPv6 address (it contains the MAC address) is in the response")
			}
		})
	}
}
