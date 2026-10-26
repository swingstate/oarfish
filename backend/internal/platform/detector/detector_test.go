package detector

import (
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/swingstate/oarfish/backend/internal/platform/hardware"
	"github.com/swingstate/oarfish/backend/internal/platform/operating_system"
	"github.com/swingstate/oarfish/backend/internal/platform/platformtest"
	"github.com/swingstate/oarfish/backend/internal/platform/profiles"
)

var linuxARM64 = hardware.Env{GOOS: "linux", GOARCH: "arm64"}

func registry(t testing.TB) *profiles.Registry {
	t.Helper()
	reg, err := profiles.Load()
	if err != nil {
		t.Fatalf("profiles.Load: %v", err)
	}
	return reg
}

func detect(t testing.TB, fsys fs.FS, env hardware.Env) Result {
	t.Helper()
	return New(fsys, env, registry(t)).Result()
}

// AT-002, AT-003, ORB-HW-001, ORB-HW-009: the reference combination.
func TestPiZero2WWithRaspberryPiOSIsCompatible(t *testing.T) {
	r := detect(t, platformtest.Fixture(t, "pi-zero-2w-raspios"), linuxARM64)

	if r.Hardware.Model != "Raspberry Pi Zero 2 W Rev 1.0" || r.Hardware.Vendor != "Raspberry Pi" ||
		r.SoCFamily != "BCM2837" || r.Hardware.Arch != "arm64" || r.Hardware.RevisionCode != "902120" {
		t.Errorf("hardware = %+v soc=%q", r.Hardware, r.SoCFamily)
	}
	if r.OS.Adapter != "raspberry_pi_os" || r.OS.PrettyName != "Debian GNU/Linux 13 (trixie)" || r.OS.Kernel != "6.18.50+rpt-rpi-v8" {
		t.Errorf("os = %+v", r.OS)
	}
	if r.Profile.ID != "raspberry-pi-zero-2-w" || r.Profile.MatchedBy != profiles.MatchedByCompatible || r.Profile.Version != "1.0.0" {
		t.Errorf("profile = %+v", r.Profile)
	}
	s := r.Support
	if s.Tier != TierCompatible || s.Reason != ReasonProfileNotVerified || s.ExplanationKey != "support.compatible.profile_not_verified" || !s.BoardActionsAllowed {
		t.Errorf("support = %+v", s)
	}
	for _, want := range []string{"memory_512mb", "evidence_install_only"} {
		if !slices.Contains(s.Limitations, want) {
			t.Errorf("limitation %q missing from %v", want, s.Limitations)
		}
	}
	if r.BootMedium.Type != "mmc" {
		t.Errorf("boot medium = %q", r.BootMedium.Type)
	}
}

// ORB-HW-002, AT-004, AT-027: the same Raspberry Pi under Ubuntu has the same
// hardware result and the Ubuntu adapter, with no change to the core.
func TestSameHardwareUnderUbuntu(t *testing.T) {
	pios := detect(t, platformtest.Fixture(t, "pi-zero-2w-raspios"), linuxARM64)
	ubuntu := detect(t, platformtest.Fixture(t, "pi-zero-2w-ubuntu"), linuxARM64)

	if !reflect.DeepEqual(pios.Hardware, ubuntu.Hardware) || pios.SoCFamily != ubuntu.SoCFamily || pios.Profile != ubuntu.Profile {
		t.Errorf("hardware result differs:\n%+v\n%+v", pios.Hardware, ubuntu.Hardware)
	}
	if ubuntu.OS.Adapter != "ubuntu" || ubuntu.OS.AdapterStatus != "not-yet-qualified" || ubuntu.OS.ID != "ubuntu" {
		t.Errorf("os = %+v", ubuntu.OS)
	}
	if ubuntu.Support.Tier != TierCompatible || !slices.Contains(ubuntu.Support.Limitations, LimitOSNotQualified) {
		t.Errorf("support = %+v", ubuntu.Support)
	}
}

// ORB-HW-007, ORB-NFR-015, AT-005: unknown ARM64 starts in Generic Mode and
// offers no board-specific action.
func TestUnknownARM64IsGenericMode(t *testing.T) {
	for _, fixture := range []string{"generic-arm64-debian", "generic-arm64-ubuntu", "unknown-arm64-minimal"} {
		r := detect(t, platformtest.Fixture(t, fixture), linuxARM64)
		if r.Support.Tier != TierGeneric || r.Support.Reason != ReasonNoBoardProfile || r.Support.BoardActionsAllowed {
			t.Errorf("%s: support = %+v", fixture, r.Support)
		}
		if r.Profile.ID != profiles.GenericProfileID || r.Profile.MatchedBy != profiles.MatchedByFallback {
			t.Errorf("%s: profile = %+v", fixture, r.Profile)
		}
		if !slices.Contains(r.Support.Limitations, LimitGenericMode) {
			t.Errorf("%s: no generic_mode limitation: %v", fixture, r.Support.Limitations)
		}
		// The data is complete: OS, kernel and capabilities are all there.
		if r.OS.Adapter == "" || r.OS.Kernel == "" || len(r.Capabilities) != 7 || r.Hardware.Arch != "arm64" {
			t.Errorf("%s: incomplete result: %+v", fixture, r)
		}
	}
}

// A Raspberry Pi without a shipped board profile is recognised as one but
// stays in Generic Mode: no board profile, no board-specific action.
func TestRaspberryPi4HasNoBoardProfile(t *testing.T) {
	r := detect(t, platformtest.Fixture(t, "pi4-raspios"), linuxARM64)
	if r.Hardware.Vendor != "Raspberry Pi" || r.Hardware.Model != "Raspberry Pi 4 Model B Rev 1.5" || r.SoCFamily != "BCM2711" {
		t.Errorf("hardware = %+v", r.Hardware)
	}
	if r.Support.Tier != TierGeneric || r.Support.BoardActionsAllowed || !slices.Contains(r.Support.Limitations, LimitRaspberryPiNoBoard) {
		t.Errorf("support = %+v", r.Support)
	}
	if r.OS.Adapter != "raspberry_pi_os" {
		t.Errorf("adapter = %q", r.OS.Adapter)
	}
}

// The profile only applies to the OS adapters it lists. Debian on the same
// board is recognised, but runs in Generic Mode.
func TestBoardOnUncoveredOSFallsBackToGenericMode(t *testing.T) {
	m := platformtest.Clone(t, "pi-zero-2w-raspios")
	delete(m, "etc/rpi-issue") // now plain Debian
	r := detect(t, m, linuxARM64)
	if r.OS.Adapter != "debian_family" {
		t.Fatalf("adapter = %q", r.OS.Adapter)
	}
	if r.Support.Tier != TierGeneric || r.Support.Reason != ReasonOSNotCovered || r.Support.BoardActionsAllowed {
		t.Errorf("support = %+v", r.Support)
	}
	if r.Profile.ID != "raspberry-pi-zero-2-w" {
		t.Errorf("the board is still recognised: %+v", r.Profile)
	}
}

// Pflichtenheft 3.6: Unsupported with a diagnostic, never a crash.
func TestUnsupportedSystems(t *testing.T) {
	t.Run("x86_64", func(t *testing.T) {
		r := detect(t, platformtest.Fixture(t, "non-arm"), hardware.Env{GOOS: "linux", GOARCH: "amd64"})
		if r.Support.Tier != TierUnsupported || r.Support.Reason != ReasonNotARM64 || r.Support.BoardActionsAllowed {
			t.Errorf("support = %+v", r.Support)
		}
		if r.Hardware.Arch != "amd64" || r.OS.Adapter != "ubuntu" || r.Profile.ID != profiles.GenericProfileID {
			t.Errorf("result = %+v", r)
		}
	})
	t.Run("not Linux", func(t *testing.T) {
		r := detect(t, platformtest.Fixture(t, "pi-zero-2w-raspios"), hardware.Env{GOOS: "darwin", GOARCH: "arm64"})
		if r.Support.Tier != TierUnsupported || r.Support.Reason != ReasonNotLinux {
			t.Errorf("support = %+v", r.Support)
		}
	})
	t.Run("minimum capabilities missing", func(t *testing.T) {
		m := platformtest.Clone(t, "generic-arm64-debian")
		delete(m, "run/systemd/system/.gitkeep")
		r := detect(t, m, linuxARM64)
		if r.Support.Tier != TierUnsupported || r.Support.Reason != ReasonMissingMinimum ||
			!slices.Equal(r.Support.Limitations, []string{"capability_missing_systemd"}) {
			t.Errorf("support = %+v", r.Support)
		}
	})
	t.Run("empty system", func(t *testing.T) {
		r := detect(t, fstest.MapFS{}, linuxARM64)
		if r.Support.Tier != TierUnsupported || r.Support.Reason != ReasonMissingMinimum || len(r.Support.Limitations) != 3 {
			t.Errorf("support = %+v", r.Support)
		}
		if r.Support.Limitations == nil || r.Support.ExplanationKey != "support.unsupported.missing_minimum_capabilities" {
			t.Errorf("support = %+v", r.Support)
		}
	})
	t.Run("hostile files", func(t *testing.T) {
		r := detect(t, platformtest.Fixture(t, "broken-hostile"), linuxARM64)
		if r.Support.Tier == "" || r.Support.Reason == "" || r.Support.ExplanationKey == "" {
			t.Errorf("support = %+v", r.Support)
		}
		if r.Support.BoardActionsAllowed {
			t.Error("board actions must not be allowed on a system with hostile files")
		}
	})
}

// ORB-HW-005: nothing depends on a user called "pi" or on /home/pi. A system
// with another user (and no /home/pi) gives the same result, and no code path
// looks into /home.
func TestNoUserNameOrHomeDirectoryDependency(t *testing.T) {
	base := platformtest.Clone(t, "pi-zero-2w-raspios")
	want := detect(t, base, linuxARM64)

	other := platformtest.Clone(t, "pi-zero-2w-raspios")
	other["home/synthuser/.profile"] = &fstest.MapFile{Data: []byte("synthetic")}
	other["etc/passwd"] = &fstest.MapFile{Data: []byte("synthuser:x:1000:1000::/home/synthuser:/bin/sh\n")}
	rec := platformtest.Recording(other)
	got := detect(t, rec, linuxARM64)

	if !reflect.DeepEqual(want, got) {
		t.Errorf("result depends on the user:\n%+v\n%+v", want, got)
	}
	for _, name := range rec.Names() {
		if strings.HasPrefix(name, "home") || strings.HasPrefix(name, "root") || name == "etc/passwd" {
			t.Errorf("detection opened %q", name)
		}
	}
	if _, err := fs.Stat(other, "home/pi"); err == nil {
		t.Fatal("test setup: /home/pi must not exist")
	}
}

// ORB-HW-009, Pflichtenheft 12.4: Verified only for a combination a profile
// lists with evidence for all core functions.
func TestVerifiedOnlyForTestedCombination(t *testing.T) {
	board := func(tier, combos string) string {
		return `{"schema_version":1,"kind":"board","profile_id":"test-pi","version":"1.0.0","vendor":"Raspberry Pi",
		  "model":"Raspberry Pi Zero 2 W","soc_family":"BCM2837","compatible_patterns":["raspberrypi,model-zero-2-w"],
		  "os_constraints":{"adapters":["raspberry_pi_os","ubuntu"]},"support_tier":"` + tier + `","maintainers":["M"]` + combos + `}`
	}
	allFunctions := `"install","dashboard","system_functions","self_update","rollback","plugins"`
	verified := board("verified", `,"tested_combinations":[{"os_adapter":"raspberry_pi_os","os_version_id":"13","oarfish_version":"v1.0.0",
	  "functions_tested":[`+allFunctions+`],"evidence_ref":"docs/evidence.md"}]`)
	soc := `{"schema_version":1,"kind":"soc_family","profile_id":"soc-bcm2837","version":"1.0.0","vendor":"B","soc_family":"BCM2837","compatible_patterns":["brcm,bcm2837"],"maintainers":["M"]}`
	generic := `{"schema_version":1,"kind":"generic","profile_id":"generic-arm64","version":"1.0.0","vendor":"V","compatible_patterns":[],"support_tier":"generic","maintainers":["M"]}`
	reg, err := profiles.LoadFS(fstest.MapFS{
		"soc/s.json": {Data: []byte(soc)}, "boards/b.json": {Data: []byte(verified)}, "generic/g.json": {Data: []byte(generic)},
	})
	if err != nil {
		t.Fatal(err)
	}

	pios := New(platformtest.Fixture(t, "pi-zero-2w-raspios"), linuxARM64, reg).Result()
	if pios.Support.Tier != TierVerified || pios.Support.Reason != ReasonVerified {
		t.Errorf("tested combination: %+v", pios.Support)
	}
	ubuntu := New(platformtest.Fixture(t, "pi-zero-2w-ubuntu"), linuxARM64, reg).Result()
	if ubuntu.Support.Tier != TierCompatible || ubuntu.Support.Reason != ReasonCombinationUntested {
		t.Errorf("untested combination must not be verified: %+v", ubuntu.Support)
	}
	m := platformtest.Clone(t, "pi-zero-2w-raspios")
	m["etc/os-release"] = &fstest.MapFile{Data: []byte("ID=debian\nVERSION_ID=\"14\"\n")}
	if got := New(m, linuxARM64, reg).Result().Support.Tier; got != TierCompatible {
		t.Errorf("another OS version is not the tested combination, tier = %q", got)
	}
}

// Nothing the repository ships can come out Verified, on any fixture.
func TestShippedProfilesNeverProduceVerified(t *testing.T) {
	for _, fixture := range []string{
		"pi-zero-2w-raspios", "pi-zero-2w-ubuntu", "pi4-raspios", "generic-arm64-debian", "generic-arm64-ubuntu",
		"unknown-arm64-minimal", "non-arm", "broken-hostile",
	} {
		if got := detect(t, platformtest.Fixture(t, fixture), linuxARM64).Support.Tier; got == TierVerified {
			t.Errorf("%s is Verified", fixture)
		}
	}
}

func TestOtherDeclaredTiers(t *testing.T) {
	for tier, want := range map[string]string{"community": TierCommunity, "experimental": TierExperimental} {
		reg := tierRegistry(t, tier, "")
		got := New(platformtest.Fixture(t, "pi-zero-2w-raspios"), linuxARM64, reg).Result().Support.Tier
		if got != want {
			t.Errorf("profile tier %s gave %s", tier, got)
		}
	}
}

func TestKernelConstraint(t *testing.T) {
	reg := tierRegistry(t, "compatible", `,"kernel_constraints":{"min_release":"7.0"}`)
	r := New(platformtest.Fixture(t, "pi-zero-2w-raspios"), linuxARM64, reg).Result()
	if r.Support.Tier != TierGeneric || r.Support.Reason != ReasonKernelTooOld || r.Support.BoardActionsAllowed {
		t.Errorf("support = %+v", r.Support)
	}
}

func TestExpectedCapabilityMissingIsALimitation(t *testing.T) {
	m := platformtest.Clone(t, "pi-zero-2w-raspios")
	delete(m, "run/systemd/journal/socket") // the profile expects journald
	r := detect(t, m, linuxARM64)
	if r.Support.Tier != TierCompatible || !slices.Contains(r.Support.Limitations, LimitExpectedCapMissing) {
		t.Errorf("support = %+v", r.Support)
	}
}

func tierRegistry(t *testing.T, tier, extra string) *profiles.Registry {
	t.Helper()
	board := `{"schema_version":1,"kind":"board","profile_id":"test-pi","version":"1.0.0","vendor":"Raspberry Pi",
	  "model":"Raspberry Pi Zero 2 W","soc_family":"BCM2837","compatible_patterns":["raspberrypi,model-zero-2-w"],
	  "os_constraints":{"adapters":["raspberry_pi_os"]},"support_tier":"` + tier + `","maintainers":["M"]` + extra + `}`
	soc := `{"schema_version":1,"kind":"soc_family","profile_id":"soc-bcm2837","version":"1.0.0","vendor":"B","soc_family":"BCM2837","compatible_patterns":["brcm,bcm2837"],"maintainers":["M"]}`
	generic := `{"schema_version":1,"kind":"generic","profile_id":"generic-arm64","version":"1.0.0","vendor":"V","compatible_patterns":[],"support_tier":"generic","maintainers":["M"]}`
	reg, err := profiles.LoadFS(fstest.MapFS{
		"soc/s.json": {Data: []byte(soc)}, "boards/b.json": {Data: []byte(board)}, "generic/g.json": {Data: []byte(generic)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestTemperatureIsReadPerCall(t *testing.T) {
	m := platformtest.Clone(t, "pi-zero-2w-raspios")
	d := New(m, linuxARM64, registry(t))
	if c, ok := d.CPUTemperature(); !ok || c != 43.312 {
		t.Fatalf("temperature = %v, %v", c, ok)
	}
	m["sys/class/thermal/thermal_zone0/temp"] = &fstest.MapFile{Data: []byte("55000\n")}
	if c, ok := d.CPUTemperature(); !ok || c != 55 {
		t.Fatalf("after change = %v, %v", c, ok)
	}
	// The cached result does not change.
	if d.Result().Support.Tier != TierCompatible {
		t.Fatal("result changed")
	}
	g := New(platformtest.Fixture(t, "unknown-arm64-minimal"), linuxARM64, registry(t))
	if _, ok := g.CPUTemperature(); ok {
		t.Fatal("no sensor, no temperature")
	}
}

// Every profile refers to OS adapters that exist.
func TestProfilesReferenceKnownAdapters(t *testing.T) {
	reg := registry(t)
	known := operating_system.AdapterNames()
	for _, p := range reg.All() {
		if p.OSConstraints == nil {
			continue
		}
		for _, a := range p.OSConstraints.Adapters {
			if !slices.Contains(known, a) {
				t.Errorf("%s refers to unknown adapter %q", p.ProfileID, a)
			}
		}
	}
}

func TestEveryTierReasonHasAnExplanationKey(t *testing.T) {
	keys := ExplanationKeys()
	if len(keys) != len(TierReasons) || len(slices.Compact(slices.Sorted(slices.Values(keys)))) != len(keys) {
		t.Fatalf("keys: %v", keys)
	}
	for _, k := range keys {
		if !strings.HasPrefix(k, "support.") {
			t.Errorf("odd key %q", k)
		}
	}
}
