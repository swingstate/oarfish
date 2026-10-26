package operating_system

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"unicode"

	"github.com/swingstate/oarfish/backend/internal/platform/platformtest"
)

// ORB-HW-003: ID, ID_LIKE, VERSION_ID and PRETTY_NAME are read from os-release.
func TestParseOSReleaseFields(t *testing.T) {
	const ubuntu = `PRETTY_NAME="Ubuntu 24.04.1 LTS"
NAME="Ubuntu"
VERSION_ID="24.04"
ID=ubuntu
ID_LIKE=debian
SOMETHING_NEW="unknown keys are skipped"
`
	got := ParseOSRelease([]byte(ubuntu))
	want := Release{ID: "ubuntu", IDLike: []string{"debian"}, VersionID: "24.04", PrettyName: "Ubuntu 24.04.1 LTS", Name: "Ubuntu"}
	if got.ID != want.ID || !slices.Equal(got.IDLike, want.IDLike) || got.VersionID != want.VersionID ||
		got.PrettyName != want.PrettyName || got.Name != want.Name {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// ORB-HW-003: strict about quoting, tolerant about everything else.
func TestParseOSReleaseQuoting(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		wantID string
		wantPN string
	}{
		{"bare", "ID=debian\n", "debian", ""},
		{"double quoted", `ID="debian"` + "\n", "debian", ""},
		{"single quoted", "ID='debian'\n", "debian", ""},
		{"escapes in double quotes", "PRETTY_NAME=\"A \\\"quoted\\\" \\$HOME \\\\ \\`x\\`\"\n", "", "A \"quoted\" $HOME \\ `x`"},
		{"no escapes in single quotes", `PRETTY_NAME='a\b'` + "\n", "", `a\b`},
		{"comment and blank lines", "# comment\n\n  # indented comment\nID=debian\n", "debian", ""},
		{"leading blanks before key", "  ID=debian\n", "debian", ""},
		{"trailing blanks", "ID=debian  \n", "debian", ""},
		{"last assignment wins", "ID=debian\nID=ubuntu\n", "ubuntu", ""},
		{"no final newline", "ID=debian", "debian", ""},
		{"empty value", "ID=\n", "", ""},

		{"unterminated double quote", "ID=\"debian\n", "", ""},
		{"unterminated single quote", "ID='debian\n", "", ""},
		{"junk after closing quote", `ID="debian" extra` + "\n", "", ""},
		{"bad escape", `PRETTY_NAME="a\nb"` + "\n", "", ""},
		{"unescaped dollar", `PRETTY_NAME="a $HOME"` + "\n", "", ""},
		{"command substitution", "ID=$(reboot)\n", "", ""},
		{"backtick", "ID=`reboot`\n", "", ""},
		{"bare value with space", "ID=de bian\n", "", ""},
		{"bare value with quote", "ID=de\"bian\n", "", ""},
		{"lower-case key", "id=debian\n", "", ""},
		{"key with space", "I D=debian\n", "", ""},
		{"no equals sign", "debian\n", "", ""},
		{"CRLF is not LF", "ID=debian\r\n", "", ""},
		{"NUL in value", "ID=deb\x00ian\n", "", ""},
		{"escape in value", "PRETTY_NAME=\"a\x1b[0m\"\n", "", ""},
		{"invalid UTF-8", "PRETTY_NAME=\"a\xff\"\n", "", ""},
		{"upper case ID", "ID=Debian\n", "", ""},
		{"ID with slash", "ID=../etc\n", "", ""},

		{"a broken line does not hide later lines", "ID=\"broken\nID=debian\n", "debian", ""},
		{"a bad line does not hide earlier lines", "ID=debian\nID=\"broken\n", "debian", ""},
	}
	for _, c := range cases {
		got := ParseOSRelease([]byte(c.in))
		if got.ID != c.wantID || got.PrettyName != c.wantPN {
			t.Errorf("%s: ID=%q PRETTY_NAME=%q, want ID=%q PRETTY_NAME=%q", c.name, got.ID, got.PrettyName, c.wantID, c.wantPN)
		}
	}
}

func TestParseOSReleaseBounds(t *testing.T) {
	long := "ID=debian\nPRETTY_NAME=\"" + strings.Repeat("x", maxOSReleaseLine) + "\"\n"
	if got := ParseOSRelease([]byte(long)); got.ID != "debian" || got.PrettyName != "" {
		t.Errorf("over-long line: %+v", got)
	}
	cut := ParseOSRelease([]byte(`PRETTY_NAME="` + strings.Repeat("y", 600) + `"` + "\n"))
	if len(cut.PrettyName) != 128 {
		t.Errorf("PRETTY_NAME must be cut to 128, got %d", len(cut.PrettyName))
	}
	many := ParseOSRelease([]byte("ID_LIKE=\"" + strings.Repeat("a ", 100) + "\"\n"))
	if len(many.IDLike) != maxIDLike {
		t.Errorf("ID_LIKE entries = %d, want %d", len(many.IDLike), maxIDLike)
	}
	badLike := ParseOSRelease([]byte(`ID_LIKE="debian Bad/Entry ubuntu"` + "\n"))
	if !slices.Equal(badLike.IDLike, []string{"debian", "ubuntu"}) {
		t.Errorf("IDLike = %q", badLike.IDLike)
	}
	if v := ParseOSRelease([]byte(`VERSION_ID="1 2"` + "\n")); v.VersionID != "" {
		t.Errorf("VERSION_ID with a space accepted: %q", v.VersionID)
	}
}

// AT-002: Raspberry Pi OS is recognised from /etc/os-release plus the image marker.
func TestDetectRaspberryPiOS(t *testing.T) {
	info := Detect(platformtest.Fixture(t, "pi-zero-2w-raspios"))
	if info.ID != "debian" || info.VersionID != "13" || info.PrettyName != "Debian GNU/Linux 13 (trixie)" {
		t.Errorf("identity = %+v", info)
	}
	if info.Adapter != "raspberry_pi_os" || info.AdapterStatus != StatusInitialTarget {
		t.Errorf("adapter = %q (%s)", info.Adapter, info.AdapterStatus)
	}
	if info.Kernel != "6.18.50+rpt-rpi-v8" {
		t.Errorf("kernel = %q", info.Kernel)
	}
}

// ORB-HW-010, AT-027: Ubuntu has its own adapter, recognised and not yet qualified.
func TestDetectUbuntuAdapterBoundary(t *testing.T) {
	for _, fixture := range []string{"pi-zero-2w-ubuntu", "generic-arm64-ubuntu"} {
		info := Detect(platformtest.Fixture(t, fixture))
		if info.ID != "ubuntu" || !slices.Equal(info.IDLike, []string{"debian"}) || info.VersionID != "24.04" {
			t.Errorf("%s: identity = %+v", fixture, info)
		}
		if info.Adapter != "ubuntu" || info.AdapterStatus != "not-yet-qualified" {
			t.Errorf("%s: adapter = %q (%s)", fixture, info.Adapter, info.AdapterStatus)
		}
	}
}

func TestDetectDebianFamily(t *testing.T) {
	info := Detect(platformtest.Fixture(t, "generic-arm64-debian"))
	if info.Adapter != "debian_family" || info.AdapterStatus != StatusGeneric || info.ID != "debian" {
		t.Errorf("debian = %+v", info)
	}
}

// os-release is primary; /usr/lib/os-release is the fallback.
func TestDetectFallsBackToUsrLibOSRelease(t *testing.T) {
	info := Detect(platformtest.Fixture(t, "unknown-arm64-minimal"))
	if info.ID != "debian" || info.VersionID != "12" || info.Adapter != "debian_family" {
		t.Errorf("fallback = %+v", info)
	}
	m := fstest.MapFS{
		"etc/os-release":     {Data: []byte("ID=ubuntu\n")},
		"usr/lib/os-release": {Data: []byte("ID=debian\n")},
	}
	if got := Detect(m).ID; got != "ubuntu" {
		t.Errorf("primary must win, got %q", got)
	}
	m["etc/os-release"] = &fstest.MapFile{Data: []byte("ID=\"broken\n")}
	if got := Detect(m).ID; got != "debian" {
		t.Errorf("unusable primary must fall back, got %q", got)
	}
	m["etc/os-release"] = &fstest.MapFile{Data: []byte("ID=ubuntu\n" + strings.Repeat("#", maxOSReleaseFile))}
	if got := Detect(m).ID; got != "debian" {
		t.Errorf("oversized primary must fall back, got %q", got)
	}
}

func TestRaspbianIDIsRaspberryPiOS(t *testing.T) {
	m := fstest.MapFS{"etc/os-release": {Data: []byte("ID=raspbian\nID_LIKE=debian\n")}}
	if got := Detect(m).Adapter; got != "raspberry_pi_os" {
		t.Errorf("adapter = %q", got)
	}
}

// Debian without the image marker is not reported as Raspberry Pi OS, and
// Ubuntu is not either when the marker file happens to exist.
func TestRaspberryPiOSNeedsTheImageMarker(t *testing.T) {
	m := platformtest.Clone(t, "pi-zero-2w-raspios")
	delete(m, "etc/rpi-issue")
	if got := Detect(m).Adapter; got != "debian_family" {
		t.Errorf("debian without marker: adapter = %q", got)
	}
	u := platformtest.Clone(t, "pi-zero-2w-ubuntu")
	u["etc/rpi-issue"] = &fstest.MapFile{Data: []byte("Raspberry Pi reference 2026-10-06\n")}
	if got := Detect(u).Adapter; got != "ubuntu" {
		t.Errorf("ubuntu with marker: adapter = %q", got)
	}
}

// ORB-HW-002: OS detection never looks at hardware files.
func TestOSDetectionNeverReadsHardwareFiles(t *testing.T) {
	rec := platformtest.Recording(platformtest.Fixture(t, "pi-zero-2w-raspios"))
	Detect(rec)
	for _, name := range rec.Names() {
		if strings.HasPrefix(name, "proc/device-tree") || name == "proc/cpuinfo" || strings.HasPrefix(name, "sys") {
			t.Errorf("OS detection opened %q", name)
		}
	}
}

// ORB-HW-002: OS detection works on a system without any board.
func TestOSDetectionIsIndependentOfHardware(t *testing.T) {
	for _, fixture := range []string{"non-arm", "generic-arm64-ubuntu", "pi-zero-2w-ubuntu"} {
		if got := Detect(platformtest.Fixture(t, fixture)); got.Adapter != "ubuntu" {
			t.Errorf("%s: adapter = %q", fixture, got.Adapter)
		}
	}
}

func TestDetectHostileFiles(t *testing.T) {
	info := Detect(platformtest.Fixture(t, "broken-hostile"))
	if info.ID != "" || info.Adapter != "generic_linux" || info.AdapterStatus != StatusUnrecognised {
		t.Errorf("hostile os-release gave %+v", info)
	}
	for _, s := range []string{info.PrettyName, info.Kernel, info.VersionID} {
		for _, r := range s {
			if unicode.IsControl(r) {
				t.Errorf("control character in %q", s)
			}
		}
	}
	if info.Kernel != "" {
		t.Errorf("kernel release with a space/NUL must be rejected, got %q", info.Kernel)
	}
}

func TestDetectEmptyFileSource(t *testing.T) {
	info := Detect(fstest.MapFS{})
	if info.Adapter != "generic_linux" || info.ID != "" || info.Kernel != "" {
		t.Errorf("empty = %+v", info)
	}
}

func TestAdapterNamesIncludeUbuntu(t *testing.T) {
	names := AdapterNames()
	for _, want := range []string{"raspberry_pi_os", "ubuntu", "debian_family", "generic_linux"} {
		if !slices.Contains(names, want) {
			t.Errorf("AdapterNames() lacks %q: %v", want, names)
		}
	}
}

func FuzzParseOSRelease(f *testing.F) {
	for _, seed := range []string{
		"ID=debian\nID_LIKE=\"a b\"\nPRETTY_NAME='x'\n",
		"PRETTY_NAME=\"Ubuntu 24.04 LTS\"\nVERSION_ID=\"24.04\"\nID=ubuntu\n",
		"ID=\"unterminated\n",
		"ID=$(x)\n`\"\\",
		"\x00\xff=\n\n#",
		"ID_LIKE=" + strings.Repeat("a ", 50),
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		rel := ParseOSRelease(data)
		if rel.ID != "" && validID(rel.ID) != rel.ID {
			t.Fatalf("invalid ID %q", rel.ID)
		}
		if len(rel.IDLike) > maxIDLike {
			t.Fatalf("%d ID_LIKE entries", len(rel.IDLike))
		}
		for _, l := range rel.IDLike {
			if validID(l) != l {
				t.Fatalf("invalid ID_LIKE entry %q", l)
			}
		}
		if rel.VersionID != "" && validVersionID(rel.VersionID) != rel.VersionID {
			t.Fatalf("invalid VERSION_ID %q", rel.VersionID)
		}
		for _, s := range []string{rel.PrettyName, rel.Name} {
			for _, r := range s {
				if unicode.IsControl(r) {
					t.Fatalf("control character in %q", s)
				}
			}
			if len([]rune(s)) > 128 {
				t.Fatalf("string too long: %d", len(s))
			}
		}
	})
}
