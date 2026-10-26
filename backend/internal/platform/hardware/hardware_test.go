package hardware

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"unicode"

	"github.com/swingstate/oarfish/backend/internal/platform/platformtest"
)

var linuxARM64 = Env{GOOS: "linux", GOARCH: "arm64"}

// AT-003, ORB-HW-001: the Raspberry Pi Zero 2 W is recognised from the device
// tree and cpuinfo alone.
func TestDetectPiZero2W(t *testing.T) {
	info := Detect(platformtest.Fixture(t, "pi-zero-2w-raspios"), linuxARM64)

	want := Info{
		Class:            ClassRaspberryPi,
		GOOS:             "linux",
		Arch:             "arm64",
		Vendor:           "Raspberry Pi",
		Model:            "Raspberry Pi Zero 2 W Rev 1.0",
		SoCFamily:        "BCM2837",
		Compatible:       []string{"raspberrypi,model-zero-2-w", "brcm,bcm2837"},
		RevisionCode:     "902120",
		MemoryTotalBytes: 415404 * 1024,
	}
	if !reflect.DeepEqual(info, want) {
		t.Fatalf("Detect = %+v\nwant     %+v", info, want)
	}
}

// ORB-HW-002, AT-004: the same Raspberry Pi hardware is classified identically
// whatever operating system runs on it.
func TestSameHardwareUnderRaspberryPiOSAndUbuntu(t *testing.T) {
	a := Detect(platformtest.Fixture(t, "pi-zero-2w-raspios"), linuxARM64)
	b := Detect(platformtest.Fixture(t, "pi-zero-2w-ubuntu"), linuxARM64)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("hardware differs between OS fixtures:\n%+v\n%+v", a, b)
	}
	if a.Class != ClassRaspberryPi {
		t.Fatalf("class = %q, want raspberry_pi", a.Class)
	}
}

// ORB-HW-002, AT-004: hardware detection does not depend on the OS. It runs on a
// file source that has no /etc at all, and it never opens anything there.
func TestHardwareDetectionNeverReadsOSFiles(t *testing.T) {
	m := platformtest.Clone(t, "pi-zero-2w-raspios")
	for name := range m {
		if strings.HasPrefix(name, "etc/") || strings.HasPrefix(name, "usr/") {
			delete(m, name)
		}
	}
	rec := platformtest.Recording(m)
	info := Detect(rec, linuxARM64)
	if info.Class != ClassRaspberryPi || info.Model != "Raspberry Pi Zero 2 W Rev 1.0" {
		t.Fatalf("hardware detection needs OS files: %+v", info)
	}
	for _, name := range rec.Names() {
		if strings.HasPrefix(name, "etc") || strings.HasPrefix(name, "usr") || strings.HasPrefix(name, "home") {
			t.Errorf("hardware detection opened %q", name)
		}
	}
}

// ORB-HW-001: another Raspberry Pi model is recognised as Raspberry Pi hardware.
func TestDetectPi4(t *testing.T) {
	info := Detect(platformtest.Fixture(t, "pi4-raspios"), linuxARM64)
	if info.Class != ClassRaspberryPi || info.Model != "Raspberry Pi 4 Model B Rev 1.5" ||
		info.SoCFamily != "BCM2711" || info.RevisionCode != "d03115" {
		t.Fatalf("pi4 = %+v", info)
	}
}

// ORB-HW-007: an unknown ARM64 board is generic hardware, and gets no
// Raspberry Pi knowledge (no revision code).
func TestDetectGenericARM64WithDeviceTree(t *testing.T) {
	info := Detect(platformtest.Fixture(t, "generic-arm64-debian"), linuxARM64)
	if info.Class != ClassGenericARM || info.Model != "Synthetic Board A1" || info.Vendor != "" ||
		info.RevisionCode != "" || info.SoCFamily != "" {
		t.Fatalf("generic = %+v", info)
	}
	if !slices.Equal(info.Compatible, []string{"synthetic,board-a1", "synthetic,soc-a"}) {
		t.Errorf("compatible = %v", info.Compatible)
	}
}

func TestDetectGenericARM64WithDMIOnly(t *testing.T) {
	info := Detect(platformtest.Fixture(t, "generic-arm64-ubuntu"), linuxARM64)
	if info.Class != ClassGenericARM || info.Vendor != "Synthetic Vendor" || info.Model != "Synthetic ARM64 Server" {
		t.Fatalf("generic dmi = %+v", info)
	}
}

func TestDetectUnknownARM64Minimal(t *testing.T) {
	info := Detect(platformtest.Fixture(t, "unknown-arm64-minimal"), linuxARM64)
	if info.Class != ClassGenericARM || info.Model != "" || info.Arch != "arm64" || info.MemoryTotalBytes != 1000000*1024 {
		t.Fatalf("minimal = %+v", info)
	}
}

// Board knowledge is applied to ARM64 only.
func TestDetectNonARMGetsNoBoard(t *testing.T) {
	info := Detect(platformtest.Fixture(t, "non-arm"), Env{GOOS: "linux", GOARCH: "amd64"})
	if info.Class != ClassOther || info.Arch != "amd64" || info.Model != "" || info.MemoryTotalBytes != 32000000*1024 {
		t.Fatalf("non-arm = %+v", info)
	}
}

// Hostile files: no panic, nothing unclean comes out.
func TestDetectHostileFiles(t *testing.T) {
	info := Detect(platformtest.Fixture(t, "broken-hostile"), linuxARM64)

	if !slices.Equal(info.Compatible, []string{"good,entry", "also.good-1"}) {
		t.Errorf("compatible = %q", info.Compatible)
	}
	for _, s := range append([]string{info.Model, info.Vendor, info.SoCFamily}, info.Compatible...) {
		for _, r := range s {
			if unicode.IsControl(r) {
				t.Errorf("control character in %q", s)
			}
		}
	}
	if info.MemoryTotalBytes != 0 {
		t.Errorf("absurd MemTotal must be ignored, got %d", info.MemoryTotalBytes)
	}
	if info.RevisionCode != "" {
		t.Errorf("revision code %q", info.RevisionCode)
	}
}

func TestDetectEmptyFileSource(t *testing.T) {
	info := Detect(fstest.MapFS{}, linuxARM64)
	if info.Class != ClassGenericARM || info.Arch != "arm64" {
		t.Fatalf("empty = %+v", info)
	}
}

func TestOversizedFilesAreIgnored(t *testing.T) {
	m := platformtest.Clone(t, "pi-zero-2w-raspios")
	m["proc/cpuinfo"] = &fstest.MapFile{Data: []byte(strings.Repeat("a", maxCPUInfo+1))}
	m["proc/meminfo"] = &fstest.MapFile{Data: []byte(strings.Repeat("a", maxMemInfo+1))}
	m["proc/device-tree/model"] = &fstest.MapFile{Data: []byte(strings.Repeat("a", maxDeviceTreeFile+1))}
	info := Detect(m, linuxARM64)
	if info.RevisionCode != "" || info.MemoryTotalBytes != 0 {
		t.Fatalf("oversized files were used: %+v", info)
	}
	if info.Model != "" && len(info.Model) > maxTextRunes {
		t.Fatalf("model not bounded: %d", len(info.Model))
	}
}

// Privacy: the serial number in /proc/cpuinfo is never read into the result.
func TestCPUInfoSerialIsNeverKept(t *testing.T) {
	const serial = "0123456789abcdef"
	data := []byte("Hardware\t: BCM2835\nRevision\t: 902120\nSerial\t\t: " + serial + "\nModel\t\t: Raspberry Pi Zero 2 W Rev 1.0\n")
	ci := parseCPUInfo(data)
	if strings.Contains(ci.Hardware+ci.Revision+ci.Model, serial) {
		t.Fatalf("serial leaked into %+v", ci)
	}
	if ci.Revision != "902120" || ci.Model != "Raspberry Pi Zero 2 W Rev 1.0" {
		t.Fatalf("fields lost: %+v", ci)
	}
	for _, f := range reflect.VisibleFields(reflect.TypeOf(Info{})) {
		if strings.Contains(strings.ToLower(f.Name), "serial") {
			t.Errorf("Info has a field %q", f.Name)
		}
	}
}

func TestParseCompatible(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"two entries", "a,b\x00c,d\x00", []string{"a,b", "c,d"}},
		{"no trailing NUL", "a,b\x00c,d", []string{"a,b", "c,d"}},
		{"empty", "", nil},
		{"only NULs", "\x00\x00\x00", nil},
		{"drops invalid", "ok,1\x00bad entry\x00sh;ls\x00\xff\x00fine.2", []string{"ok,1", "fine.2"}},
		{"drops too long", strings.Repeat("a", 129) + "\x00x,y", []string{"x,y"}},
	}
	for _, c := range cases {
		if got := ParseCompatible([]byte(c.in)); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	many := strings.Repeat("a,b\x00", 100)
	if got := ParseCompatible([]byte(many)); len(got) != maxCompatible {
		t.Errorf("entries = %d, want cap %d", len(got), maxCompatible)
	}
}

func TestParseModel(t *testing.T) {
	cases := map[string]string{
		"Raspberry Pi Zero 2 W Rev 1.0\x00": "Raspberry Pi Zero 2 W Rev 1.0",
		"Board\x1b[2J\x07 Name\x00junk":     "Board[2J Name",
		"":                                  "",
		"\x00after":                         "",
	}
	for in, want := range cases {
		if got := ParseModel([]byte(in)); got != want {
			t.Errorf("ParseModel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseMemTotal(t *testing.T) {
	cases := map[string]uint64{
		"MemTotal:       415404 kB\nMemFree: 1 kB\n": 415404 * 1024,
		"MemFree: 1 kB\n":                            0,
		"MemTotal: 12 MB\n":                          0,
		"MemTotal: abc kB\n":                         0,
		"MemTotal: 0 kB\n":                           0,
		"MemTotal: 99999999999999999999 kB\n":        0,
		"":                                           0,
	}
	for in, want := range cases {
		if got := ParseMemTotal([]byte(in)); got != want {
			t.Errorf("ParseMemTotal(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestRevisionCode(t *testing.T) {
	cases := map[string]string{" 902120 ": "902120", "D03115": "d03115", "zzzz": "", "": "", "123456789": ""}
	for in, want := range cases {
		if got := revisionCode(in); got != want {
			t.Errorf("revisionCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHostEnv(t *testing.T) {
	if e := HostEnv(); e.GOOS == "" || e.GOARCH == "" {
		t.Fatalf("HostEnv = %+v", e)
	}
}

func FuzzParseCompatible(f *testing.F) {
	for _, seed := range []string{
		"raspberrypi,model-zero-2-w\x00brcm,bcm2837\x00",
		"a,b\x00\x00c",
		"\xff\xfe\x00",
		strings.Repeat("x,", 100),
		"",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		got := ParseCompatible(data)
		if len(got) > maxCompatible {
			t.Fatalf("%d entries", len(got))
		}
		for _, s := range got {
			if !validCompatible([]byte(s)) {
				t.Fatalf("invalid entry %q from %q", s, data)
			}
		}
		_ = ParseModel(data)
		_ = ParseMemTotal(data)
		_ = parseCPUInfo(data)
	})
}
