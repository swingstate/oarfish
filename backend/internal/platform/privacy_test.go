package platform

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

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

// scanPrivate returns a description of every private-looking value in text.
func scanPrivate(text string) []string {
	var found []string
	for name, re := range privatePatterns {
		if m := re.FindString(text); m != "" {
			found = append(found, name+": "+m)
		}
	}
	return found
}

// Privacy rule: the fixtures are synthetic and hold no identifying values.
func TestFixturesContainNoPrivateData(t *testing.T) {
	root := platformtest.Dir()
	count := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
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
		// The path may not name a machine-specific file either.
		for _, bad := range []string{"machine-id", "hostname", "environ", "/net/", "passwd", ".ssh"} {
			if strings.Contains(rel, bad) {
				t.Errorf("fixture path %s contains %q", rel, bad)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count < 50 {
		t.Fatalf("only %d fixture files scanned; is the fixture directory right?", count)
	}
	// Positive control: the scanner does find what it is meant to find.
	for _, sample := range []string{
		"Serial\t\t: 10000000deadbeef", "aa:bb:cc:dd:ee:ff", "192.168.1.20", "0123456789abcdef0123456789abcdef",
		"/home/pi/.ssh", "ssh-ed25519 AAAA", "USER=bob", "bob:x:1000:1000:",
	} {
		if len(scanPrivate(sample)) == 0 {
			t.Errorf("scanner missed %q", sample)
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
