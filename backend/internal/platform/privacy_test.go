package platform

import (
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/swingstate/oarfish/backend/internal/metrics"
	"github.com/swingstate/oarfish/backend/internal/metrics/metricstest"
	"github.com/swingstate/oarfish/backend/internal/platform/detector"
	"github.com/swingstate/oarfish/backend/internal/platform/hardware"
	"github.com/swingstate/oarfish/backend/internal/platform/platformtest"
	"github.com/swingstate/oarfish/backend/internal/platform/profiles"
)

var fixtureNames = []string{
	"pi-zero-2w-raspios", "pi-zero-2w-ubuntu", "pi4-raspios", "generic-arm64-debian", "generic-arm64-ubuntu",
	"unknown-arm64-minimal", "non-arm", "broken-hostile",
}

// privatePatterns look for values that identify one device or its owner.
var privatePatterns = map[string]*regexp.Regexp{
	"MAC address":     regexp.MustCompile(`(?i)\b[0-9a-f]{2}([:-][0-9a-f]{2}){5}\b`),
	"IPv4 address":    regexp.MustCompile(`\b(\d{1,3}\.){3}\d{1,3}\b`),
	"IPv6 address":    regexp.MustCompile(`(?i)\b[0-9a-f]{1,4}(:[0-9a-f]{1,4}){7}\b`),
	"machine id":      regexp.MustCompile(`(?i)\b[0-9a-f]{32}\b`),
	"serial number":   regexp.MustCompile(`(?i)\bserial\b`),
	"host name":       regexp.MustCompile(`(?i)\bhostname\b`),
	"home directory":  regexp.MustCompile(`/home/`),
	"SSH data":        regexp.MustCompile(`(?i)ssh-(rsa|ed25519)|BEGIN [A-Z ]*PRIVATE KEY|authorized_keys|known_hosts`),
	"environment":     regexp.MustCompile(`(?i)\b(PATH|HOME|USER|LOGNAME)=`),
	"user name entry": regexp.MustCompile(`(?m)^[a-z_][a-z0-9_-]*:x:\d+:\d+:`),
}

// documentationRanges are the only IP addresses a fixture or a document may
// contain besides the loopback listen address: RFC 5737 (IPv4) and RFC 3849 (IPv6).
var documentationRanges = []netip.Prefix{
	netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("2001:db8::/32"),
}

func inDocumentationRange(a netip.Addr) bool {
	for _, p := range documentationRanges {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// scanPrivate returns a description of every private-looking value in text.
// An IP address is private unless it lies in a documentation range; a text
// that only looks like an address (999.999.999.999) is not an address.
func scanPrivate(text string) []string {
	var found []string
	for name, re := range privatePatterns {
		for _, m := range re.FindAllString(text, -1) {
			switch name {
			case "IPv4 address", "IPv6 address":
				if a, err := netip.ParseAddr(m); err != nil || inDocumentationRange(a) {
					continue
				}
			case "machine id":
				// /proc/net/if_inet6 prints an IPv6 address as 32 hex digits.
				if raw, err := hex.DecodeString(m); err == nil && len(raw) == 16 && inDocumentationRange(netip.AddrFrom16([16]byte(raw))) {
					continue
				}
			}
			found = append(found, name+": "+m)
			break
		}
	}
	return found
}

// The synthetic host name of the metrics fixtures.
const fixtureHostName = "oarfish-test"

// Privacy rule: the fixtures are synthetic and hold no identifying values. The
// detection fixtures hold no IP address and no host name at all. The metrics
// fixtures, which stand for live readings, may hold exactly two kinds of
// values: IP addresses from the documentation ranges, and the host name
// "oarfish-test" in proc/sys/kernel/hostname.
func TestFixturesContainNoPrivateData(t *testing.T) {
	roots := []struct {
		dir     string
		metrics bool
		minimum int
	}{
		{platformtest.Dir(), false, 50},
		{metricstest.Dir(), true, 60},
	}
	for _, root := range roots {
		count := 0
		err := filepath.WalkDir(root.dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root.dir, path)
			if rel == "README.md" {
				return nil // it names the categories it excludes
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			count++
			for _, f := range scanPrivate(string(data)) {
				t.Errorf("fixture %s: %s", rel, f)
			}

			// Path rules. rel is <fixture>/<path in the fixture tree>.
			_, inTree, _ := strings.Cut(filepath.ToSlash(rel), "/")
			for _, bad := range []string{"machine-id", "environ", "passwd", ".ssh", "cmdline", "shadow"} {
				if strings.Contains(inTree, bad) {
					t.Errorf("fixture path %s contains %q", rel, bad)
				}
			}
			if filepath.Base(path) == "address" {
				t.Errorf("fixture %s is a hardware address file (MAC address)", rel)
			}
			hostNamePath := inTree == "proc/sys/kernel/hostname"
			if strings.Contains(inTree, "hostname") && !(root.metrics && hostNamePath) {
				t.Errorf("fixture path %s contains \"hostname\"", rel)
			}
			netPath := strings.HasPrefix(inTree, "proc/net/") || strings.HasPrefix(inTree, "sys/class/net/")
			if strings.Contains("/"+inTree, "/net/") && !(root.metrics && netPath) {
				t.Errorf("fixture path %s contains \"/net/\"", rel)
			}
			if root.metrics && hostNamePath {
				name := strings.TrimSpace(string(data))
				if strings.HasPrefix(rel, "hostile"+string(filepath.Separator)) {
					if !strings.HasPrefix(name, "oarfish") {
						t.Errorf("fixture %s: host name %q is not the synthetic one", rel, name)
					}
				} else if name != fixtureHostName {
					t.Errorf("fixture %s: host name %q, want only %q", rel, name, fixtureHostName)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if count < root.minimum {
			t.Fatalf("only %d fixture files scanned in %s; is the fixture directory right?", count, root.dir)
		}
	}

	// Positive controls: the scanner does find what it is meant to find, and
	// lets through exactly the documentation ranges.
	for _, sample := range []string{
		"Serial\t\t: 10000000deadbeef", "aa:bb:cc:dd:ee:ff", "192.168.1.20", "0123456789abcdef0123456789abcdef",
		"/home/pi/.ssh", "ssh-ed25519 AAAA", "USER=bob", "bob:x:1000:1000:", "192.0.3.1", "198.51.101.1", "203.0.114.1",
		"10.0.0.1", "127.0.0.1", "fe80000000000000021122fffe334455", "20010db9000000000000000000000001",
		"2001:db9:0:0:0:0:0:1",
	} {
		if len(scanPrivate(sample)) == 0 {
			t.Errorf("scanner missed %q", sample)
		}
	}
	for _, sample := range []string{
		"192.0.2.1", "198.51.100.255", "203.0.113.9", "20010db8000000000000000000000015", "2001:db8:0:0:0:0:0:1",
		"999.999.999.999", // not an address
	} {
		if got := scanPrivate(sample); len(got) != 0 {
			t.Errorf("scanner rejects %q: %v", sample, got)
		}
	}
}

// readAllowList is every file detection may open. Anything else — in
// particular machine-id, hostname, network addresses, environ, /home and SSH
// data — is a privacy violation. Also see docs/DETECTION.md.
var readAllowList = []*regexp.Regexp{
	regexp.MustCompile(`^proc/(meminfo|cpuinfo|sys/kernel/osrelease|self/mountinfo)$`),
	regexp.MustCompile(`^proc/device-tree/(model|compatible)$`),
	regexp.MustCompile(`^etc/(os-release|rpi-issue)$`),
	regexp.MustCompile(`^usr/lib/os-release$`),
	regexp.MustCompile(`^sys/class/dmi/id/(sys_vendor|product_name)$`),
	regexp.MustCompile(`^sys/class$`),
	regexp.MustCompile(`^sys/class/(thermal|hwmon)$`),
	regexp.MustCompile(`^sys/class/thermal/thermal_zone[0-9]+/(temp|type)$`),
	regexp.MustCompile(`^sys/class/hwmon/hwmon[0-9]+/(temp1_input|name)$`),
	regexp.MustCompile(`^run/systemd/(system|journal/socket)$`),
	regexp.MustCompile(`^sys/fs/cgroup/cgroup\.controllers$`),
	regexp.MustCompile(`^sys/dev/block/[0-9]+:[0-9]+(/uevent)?$`),
}

func allowed(name string) bool {
	for _, re := range readAllowList {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

// Privacy rule, ORB-HW-006: detection opens only the allow-listed files, on
// every fixture, including the per-request temperature read.
func TestDetectionOpensOnlyAllowListedFiles(t *testing.T) {
	reg, err := profiles.Load()
	if err != nil {
		t.Fatal(err)
	}
	opened := map[string]bool{}
	for _, name := range fixtureNames {
		rec := platformtest.Recording(platformtest.Fixture(t, name))
		d := detector.New(rec, hardware.Env{GOOS: "linux", GOARCH: "arm64"}, reg)
		d.CPUTemperature()
		for _, n := range rec.Names() {
			opened[n] = true
			if !allowed(n) {
				t.Errorf("%s: detection opened %q, which is not on the allow-list", name, n)
			}
		}
	}
	// Positive control: the recorder sees reads, and the main files are covered.
	for _, want := range []string{"proc/cpuinfo", "etc/os-release", "proc/device-tree/compatible", "sys/class/thermal/thermal_zone0/temp"} {
		if !opened[want] {
			t.Errorf("expected %q to be read", want)
		}
	}
	if allowed("etc/machine-id") || allowed("etc/hostname") || allowed("proc/self/environ") || allowed("home/x/.ssh/id_ed25519") || allowed("sys/class/net/eth0/address") {
		t.Fatal("the allow-list admits a private file")
	}
}

// ---- metrics ----

// metricsReadAllowList is every file the metrics collector may open (Gate G5,
// docs/DASHBOARD.md). The hardware address of an interface, the machine id, the
// environment, process data, user databases and SSH data are not on it.
var metricsReadAllowList = []*regexp.Regexp{
	regexp.MustCompile(`^proc/(stat|loadavg|uptime|meminfo|self/mountinfo|sys/kernel/hostname)$`),
	regexp.MustCompile(`^proc/net/(fib_trie|route|if_inet6)$`),
	regexp.MustCompile(`^sys/devices/system/cpu/cpu0/cpufreq/(scaling_cur_freq|cpuinfo_max_freq|scaling_max_freq|scaling_governor)$`),
	regexp.MustCompile(`^sys/class/(thermal|hwmon|net)$`),
	regexp.MustCompile(`^sys/class/thermal/thermal_zone[0-9]{1,4}/(type|temp)$`),
	regexp.MustCompile(`^sys/class/hwmon/hwmon[0-9]{1,4}/(name|temp[1-8]_input|temp[1-8]_label)$`),
	regexp.MustCompile(`^sys/class/net/[A-Za-z0-9_.:@-]{1,15}/(operstate|type|speed|wireless|phy80211|statistics/rx_bytes|statistics/tx_bytes)$`),
}

func metricsAllowed(name string) bool {
	for _, re := range metricsReadAllowList {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

// Privacy rule (docs/DASHBOARD.md): the metrics collector opens only the
// allow-listed files, on every fixture, through both ticks of the sampler.
func TestMetricsOpenOnlyAllowListedFiles(t *testing.T) {
	sizes := metricstest.Statfs(map[string]metrics.Statfs{
		"/": {BlockSize: 4096, Blocks: 100, Free: 50, Available: 50}, "/boot/firmware": {BlockSize: 4096, Blocks: 100, Free: 50, Available: 50},
		"/boot/efi": {BlockSize: 4096, Blocks: 100, Free: 50, Available: 50},
	})
	opened := map[string]bool{}
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	for _, name := range []string{"pi-zero-2w", "generic-arm64", "minimal", "hostile"} {
		rec := platformtest.Recording(metricstest.Fixture(t, name))
		c := metrics.NewCollector(rec, sizes)
		c.Collect(now)
		c.Collect(now.Add(2 * time.Second))
		c.Collect(now.Add(metrics.SlowInterval + time.Second)) // the slow tier again
		for _, n := range rec.Names() {
			opened[n] = true
			if !metricsAllowed(n) {
				t.Errorf("%s: the collector opened %q, which is not on the allow-list", name, n)
			}
		}
	}
	// The detection fixtures hold none of these files; the collector opens
	// only what is on the list there too.
	for _, name := range fixtureNames {
		rec := platformtest.Recording(platformtest.Fixture(t, name))
		metrics.NewCollector(rec, sizes).Collect(now)
		for _, n := range rec.Names() {
			if !metricsAllowed(n) {
				t.Errorf("%s: the collector opened %q", name, n)
			}
		}
	}

	// Positive control: the recorder sees reads, and the main files are covered.
	for _, want := range []string{
		"proc/stat", "proc/loadavg", "proc/uptime", "proc/meminfo", "proc/sys/kernel/hostname", "proc/self/mountinfo",
		"proc/net/fib_trie", "proc/net/route", "proc/net/if_inet6",
		"sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq", "sys/class/thermal/thermal_zone0/temp",
		"sys/class/hwmon/hwmon0/temp1_input", "sys/class/net/wlan0/operstate", "sys/class/net/eth0/statistics/rx_bytes",
	} {
		if !opened[want] {
			t.Errorf("expected %q to be read", want)
		}
	}
	for _, private := range []string{
		"sys/class/net/eth0/address", "sys/class/net/eth0/addr_assign_type", "sys/class/net/eth0/phys_port_id", "etc/machine-id",
		"etc/hostname", "proc/self/environ", "proc/cpuinfo", "proc/1/cmdline", "home/x/.ssh/id_ed25519", "etc/passwd",
		"proc/net/arp", "proc/net/dev", "sys/class/dmi/id/product_serial", "sys/class/net/../../x/operstate",
	} {
		if metricsAllowed(private) {
			t.Errorf("the allow-list admits %q", private)
		}
	}
}

// A tree that holds every kind of identifying file: the collector must not
// open one of them, and nothing of them reaches the output.
func TestMetricsNeverReadsIdentifyingFiles(t *testing.T) {
	const (
		mac       = "aa:bb:cc:dd:ee:ff"
		serial    = "10000000cafe1234"
		machineID = "0123456789abcdef0123456789abcdef"
		user      = "alice-secret"
	)
	fsys := fstest.MapFS{}
	base := metricstest.Fixture(t, "pi-zero-2w")
	if err := fs.WalkDir(base, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(base, p)
		fsys[p] = &fstest.MapFile{Data: data}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	traps := map[string]string{
		"sys/class/net/wlan0/address":        mac + "\n",
		"sys/class/net/wlan0/broadcast":      "ff:ff:ff:ff:ff:ff\n",
		"sys/class/net/wlan0/phys_switch_id": mac + "\n",
		"proc/cpuinfo":                       "Serial\t\t: " + serial + "\n",
		"etc/machine-id":                     machineID + "\n",
		"etc/hostname":                       "real-host-name\n",
		"proc/self/environ":                  "USER=" + user + "\x00HOME=/home/" + user + "\x00",
		"proc/1/cmdline":                     "/sbin/init\x00",
		"proc/self/status":                   "Name: x\nUid: 1000\n",
		"proc/net/arp":                       "IP address HW address\n192.0.2.1 0x1 0x2 " + mac + " * wlan0\n",
		"home/" + user + "/.ssh/id_ed25519":  "-----BEGIN OPENSSH PRIVATE KEY-----\n",
		"etc/passwd":                         user + ":x:1000:1000::/home/" + user + ":/bin/sh\n",
		"sys/class/dmi/id/product_serial":    serial + "\n",
	}
	for name, content := range traps {
		fsys[name] = &fstest.MapFile{Data: []byte(content)}
	}
	rec := platformtest.Recording(fsys)
	s := metrics.NewCollector(rec, metricstest.Statfs(map[string]metrics.Statfs{"/": {BlockSize: 4096, Blocks: 100, Free: 50, Available: 50}})).Collect(time.Now())

	for _, n := range rec.Names() {
		if _, isTrap := traps[n]; isTrap {
			t.Errorf("the collector opened %q", n)
		}
	}
	out, err := jsonString(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{mac, strings.ToUpper(mac), serial, machineID, user, "real-host-name", "BEGIN OPENSSH"} {
		if strings.Contains(out, secret) {
			t.Errorf("%q reached the output", secret)
		}
	}
	// The only host name in the output is the one from proc/sys/kernel/hostname.
	if !strings.Contains(out, fixtureHostName) {
		t.Error("the host name from /proc/sys/kernel/hostname is missing from the output")
	}
}

func jsonString(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}
