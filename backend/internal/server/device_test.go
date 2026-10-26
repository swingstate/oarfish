package server

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/swingstate/oarfish/backend/internal/platform/detector"
	"github.com/swingstate/oarfish/backend/internal/platform/hardware"
	"github.com/swingstate/oarfish/backend/internal/platform/platformtest"
	"github.com/swingstate/oarfish/backend/internal/platform/profiles"
)

func deviceHandler(t *testing.T, fsys fs.FS, env hardware.Env) http.Handler {
	t.Helper()
	reg, err := profiles.Load()
	if err != nil {
		t.Fatal(err)
	}
	return NewHandler(fstest.MapFS{}, WithDevice(detector.New(fsys, env, reg)))
}

var linuxARM64 = hardware.Env{GOOS: "linux", GOARCH: "arm64"}

func getDevice(t *testing.T, h http.Handler) (*httptest.ResponseRecorder, deviceResponse) {
	t.Helper()
	rec := get(t, h, http.MethodGet, "/api/v1/device")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	// Strict decoding: the response has exactly the documented fields.
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	var resp deviceResponse
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, rec.Body)
	}
	return rec, resp
}

func str(p *string) string {
	if p == nil {
		return "<null>"
	}
	return *p
}

// ORB-HW-001, AT-003: the API shows model, SoC, architecture, OS, kernel,
// sensors, boot medium, support status and capabilities for the Pi.
func TestDeviceEndpointPiZero2W(t *testing.T) {
	rec, r := getDevice(t, deviceHandler(t, platformtest.Fixture(t, "pi-zero-2w-raspios"), linuxARM64))

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	if r.SchemaVersion != 1 {
		t.Errorf("schema_version = %d", r.SchemaVersion)
	}
	h := r.Hardware
	if str(h.Vendor) != "Raspberry Pi" || str(h.Model) != "Raspberry Pi Zero 2 W Rev 1.0" || str(h.SoCFamily) != "BCM2837" ||
		h.Arch != "arm64" || str(h.RevisionCode) != "902120" || h.MemoryTotalBytes == nil || *h.MemoryTotalBytes != 415404*1024 {
		t.Errorf("hardware = %+v", h)
	}
	o := r.OS
	if str(o.ID) != "debian" || len(o.IDLike) != 0 || str(o.VersionID) != "13" || str(o.PrettyName) != "Debian GNU/Linux 13 (trixie)" ||
		o.Adapter != "raspberry_pi_os" || o.AdapterStatus != "initial-target" {
		t.Errorf("os = %+v", o)
	}
	if str(r.Kernel.Release) != "6.18.50+rpt-rpi-v8" {
		t.Errorf("kernel = %+v", r.Kernel)
	}
	if r.Profile != (deviceProfile{ID: "raspberry-pi-zero-2-w", Version: "1.0.0", MatchedBy: "compatible"}) {
		t.Errorf("profile = %+v", r.Profile)
	}
	if r.Support.Tier != "compatible" || r.Support.Reason != "profile_not_verified" || !r.Support.BoardActionsAllowed ||
		r.Support.ExplanationKey != "support.compatible.profile_not_verified" {
		t.Errorf("support = %+v", r.Support)
	}
	if r.Sensors.CPUTemperatureCelsius == nil || *r.Sensors.CPUTemperatureCelsius != 43.3 {
		t.Errorf("sensors = %+v", r.Sensors)
	}
	if r.BootMedium.Type != "mmc" {
		t.Errorf("boot_medium = %+v", r.BootMedium)
	}
	if len(r.Capabilities) != 7 {
		t.Fatalf("capabilities = %+v", r.Capabilities)
	}
	for _, c := range r.Capabilities {
		if !c.Available || c.Reason != nil || c.Source == "" {
			t.Errorf("capability %+v", c)
		}
	}
}

// ORB-HW-007, ORB-NFR-015, AT-005: the response for an unknown ARM64 system is
// complete and offers no board-specific action.
func TestDeviceEndpointGenericARM64(t *testing.T) {
	_, r := getDevice(t, deviceHandler(t, platformtest.Fixture(t, "generic-arm64-debian"), linuxARM64))

	if r.Profile.ID != "generic-arm64" || r.Profile.MatchedBy != "generic_fallback" {
		t.Errorf("profile = %+v", r.Profile)
	}
	if r.Support.Tier != "generic" || r.Support.BoardActionsAllowed || r.Support.ExplanationKey != "support.generic.no_board_profile" {
		t.Errorf("support = %+v", r.Support)
	}
	if str(r.Hardware.Model) != "Synthetic Board A1" || r.Hardware.Vendor != nil || r.Hardware.RevisionCode != nil || r.Hardware.SoCFamily != nil {
		t.Errorf("hardware = %+v", r.Hardware)
	}
	if r.OS.Adapter != "debian_family" || r.OS.AdapterStatus != "generic" || r.BootMedium.Type != "nvme" {
		t.Errorf("os = %+v, boot = %+v", r.OS, r.BootMedium)
	}
	if r.Sensors.CPUTemperatureCelsius == nil || *r.Sensors.CPUTemperatureCelsius != 45.3 {
		t.Errorf("sensors = %+v", r.Sensors)
	}
	if len(r.Capabilities) != 7 || r.Kernel.Release == nil {
		t.Errorf("incomplete: %+v", r)
	}
}

// Unknown values are null, lists are never null.
func TestDeviceEndpointUsesNullsAndEmptyLists(t *testing.T) {
	rec := get(t, deviceHandler(t, platformtest.Fixture(t, "unknown-arm64-minimal"), linuxARM64), http.MethodGet, "/api/v1/device")
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	hw := raw["hardware"].(map[string]any)
	for _, k := range []string{"vendor", "model", "soc_family", "revision_code"} {
		if v, ok := hw[k]; !ok || v != nil {
			t.Errorf("hardware.%s = %v, want null", k, v)
		}
	}
	if raw["sensors"].(map[string]any)["cpu_temperature_celsius"] != nil {
		t.Error("cpu_temperature_celsius must be null without a sensor")
	}
	if _, ok := raw["os"].(map[string]any)["id_like"].([]any); !ok {
		t.Error("id_like must be a list")
	}
	caps := raw["capabilities"].([]any)
	var unavailable int
	for _, c := range caps {
		m := c.(map[string]any)
		if m["available"] == false {
			unavailable++
			if m["reason"] == nil {
				t.Errorf("unavailable capability without reason: %v", m)
			}
		}
	}
	if unavailable == 0 {
		t.Error("expected unavailable capabilities in the minimal fixture")
	}
}

func TestDeviceEndpointUnsupportedSystemStillAnswers(t *testing.T) {
	_, r := getDevice(t, deviceHandler(t, platformtest.Fixture(t, "non-arm"), hardware.Env{GOOS: "linux", GOARCH: "amd64"}))
	if r.Support.Tier != "unsupported" || r.Support.Reason != "not_arm64" || r.Hardware.Arch != "amd64" || r.Support.Limitations == nil {
		t.Errorf("response = %+v", r)
	}
}

// The API is read-only: GET only, no request body.
func TestDeviceEndpointAcceptsOnlyGet(t *testing.T) {
	h := deviceHandler(t, platformtest.Fixture(t, "pi-zero-2w-raspios"), linuxARM64)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions} {
		rec := get(t, h, method, "/api/v1/device")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want 405", method, rec.Code)
		}
		if allow := rec.Header().Get("Allow"); allow != "GET" {
			t.Errorf("%s: Allow = %q", method, allow)
		}
		if method != http.MethodHead && !strings.Contains(rec.Body.String(), `"method_not_allowed"`) {
			t.Errorf("%s: body = %s", method, rec.Body)
		}
	}
}

func TestDeviceEndpointRejectsARequestBody(t *testing.T) {
	h := deviceHandler(t, platformtest.Fixture(t, "pi-zero-2w-raspios"), linuxARM64)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/device", strings.NewReader(`{"x":1}`)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "request_body_not_allowed") {
		t.Errorf("status = %d, body %s", rec.Code, rec.Body)
	}
}

func TestUnknownAPIPathsGetATypedNotFound(t *testing.T) {
	h := deviceHandler(t, platformtest.Fixture(t, "pi-zero-2w-raspios"), linuxARM64)
	for _, p := range []string{"/api", "/api/", "/api/v1", "/api/v1/", "/api/v1/device/", "/api/v1/other", "/api/v2/device", "/api/v1/device/extra"} {
		rec := get(t, h, http.MethodGet, p)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"not_found"`) {
			t.Errorf("%s = %d %s", p, rec.Code, rec.Body)
		}
	}
	// Other routes are untouched.
	if rec := get(t, h, http.MethodGet, "/healthz"); rec.Code != http.StatusOK {
		t.Errorf("/healthz = %d", rec.Code)
	}
}

func TestDeviceEndpointWithoutDetectionIs503(t *testing.T) {
	rec := get(t, NewHandler(fstest.MapFS{}), http.MethodGet, "/api/v1/device")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "device_info_unavailable") {
		t.Errorf("status = %d, body %s", rec.Code, rec.Body)
	}
}

func TestDeviceEndpointHasSecurityHeaders(t *testing.T) {
	rec := get(t, deviceHandler(t, platformtest.Fixture(t, "pi-zero-2w-raspios"), linuxARM64), http.MethodGet, "/api/v1/device")
	for _, name := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if rec.Header().Get(name) == "" {
			t.Errorf("missing header %s", name)
		}
	}
}

// Sensors are read per request; the rest is cached from startup.
func TestDeviceEndpointReadsSensorsPerRequest(t *testing.T) {
	m := platformtest.Clone(t, "pi-zero-2w-raspios")
	h := deviceHandler(t, m, linuxARM64)

	_, first := getDevice(t, h)
	m["sys/class/thermal/thermal_zone0/temp"] = &fstest.MapFile{Data: []byte("70123\n")}
	m["etc/os-release"] = &fstest.MapFile{Data: []byte("ID=ubuntu\n")} // must not change the cached result
	_, second := getDevice(t, h)

	if *first.Sensors.CPUTemperatureCelsius != 43.3 || *second.Sensors.CPUTemperatureCelsius != 70.1 {
		t.Errorf("temperatures: %v then %v", *first.Sensors.CPUTemperatureCelsius, *second.Sensors.CPUTemperatureCelsius)
	}
	if second.OS.Adapter != "raspberry_pi_os" {
		t.Errorf("detection must run once, adapter changed to %q", second.OS.Adapter)
	}
}

// Privacy: values that identify one device never reach the API, and detection
// does not even open the files that hold them.
func TestDeviceEndpointNeverExposesIdentifyingData(t *testing.T) {
	planted := map[string]string{
		"proc/cpuinfo":                   "Hardware\t: BCM2835\nRevision\t: 902120\nSerial\t\t: 10000000deadbeef\nModel\t\t: Raspberry Pi Zero 2 W Rev 1.0\n",
		"etc/machine-id":                 "0123456789abcdef0123456789abcdef\n",
		"etc/hostname":                   "synth-host-xyz\n",
		"sys/class/net/wlan0/address":    "aa:bb:cc:dd:ee:ff\n",
		"proc/self/environ":              "SYNTH_TOKEN=tok-9f8e7d\x00",
		"proc/1/environ":                 "SYNTH_TOKEN=tok-9f8e7d\x00",
		"proc/net/arp":                   "192.168.77.12 0x1 0x2 aa:bb:cc:00:11:22 * wlan0\n",
		"home/synthuser/.ssh/id_ed25519": "PRIVATE-KEY-MARKER-4711",
		"etc/passwd":                     "synthuser:x:1000:1000::/home/synthuser:/bin/sh\n",
	}
	m := platformtest.Clone(t, "pi-zero-2w-raspios")
	for name, content := range planted {
		m[name] = &fstest.MapFile{Data: []byte(content)}
	}
	rec := platformtest.Recording(m)
	body := get(t, deviceHandler(t, rec, linuxARM64), http.MethodGet, "/api/v1/device").Body.String()

	for _, secret := range []string{
		"10000000deadbeef", "0123456789abcdef0123456789abcdef", "synth-host-xyz", "aa:bb:cc:dd:ee:ff", "tok-9f8e7d",
		"192.168.77.12", "aa:bb:cc:00:11:22", "PRIVATE-KEY-MARKER-4711", "synthuser", "Serial",
	} {
		if strings.Contains(body, secret) {
			t.Errorf("response contains %q", secret)
		}
	}
	for _, name := range rec.Names() {
		for _, forbidden := range []string{"etc/machine-id", "etc/hostname", "sys/class/net", "environ", "proc/net", "home", ".ssh", "etc/passwd"} {
			if strings.Contains(name, forbidden) {
				t.Errorf("detection opened %q", name)
			}
		}
	}
}

var (
	reMAC     = regexp.MustCompile(`(?i)\b[0-9a-f]{2}([:-][0-9a-f]{2}){5}\b`)
	reIPv4    = regexp.MustCompile(`\b(\d{1,3}\.){3}\d{1,3}\b`)
	reMachine = regexp.MustCompile(`(?i)\b[0-9a-f]{32}\b`)
	reSerial  = regexp.MustCompile(`(?i)serial|hostname|machine-id|/home/|ssh`)
)

// Privacy: the response of every fixture is free of private-looking values.
func TestDeviceResponsesContainNoPrivatePatterns(t *testing.T) {
	for _, fixture := range []string{
		"pi-zero-2w-raspios", "pi-zero-2w-ubuntu", "pi4-raspios", "generic-arm64-debian", "generic-arm64-ubuntu",
		"unknown-arm64-minimal", "non-arm", "broken-hostile",
	} {
		body := get(t, deviceHandler(t, platformtest.Fixture(t, fixture), linuxARM64), http.MethodGet, "/api/v1/device").Body.String()
		for name, re := range map[string]*regexp.Regexp{"MAC": reMAC, "IPv4": reIPv4, "machine-id": reMachine, "identifier word": reSerial} {
			if m := re.FindString(body); m != "" {
				t.Errorf("%s: %s pattern matches %q", fixture, name, m)
			}
		}
	}
}
