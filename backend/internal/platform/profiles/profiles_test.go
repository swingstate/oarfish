package profiles

import (
	"strings"
	"testing"
	"testing/fstest"
)

// validBoard is a complete, valid board profile that tests change one field of.
const validBoard = `{
  "schema_version": 1, "kind": "board", "profile_id": "test-board", "version": "1.0.0",
  "vendor": "Test", "model": "Test Board", "soc_family": "BCM2837",
  "compatible_patterns": ["test,board"],
  "os_constraints": {"adapters": ["raspberry_pi_os"]},
  "support_tier": "compatible", "maintainers": ["Tester"]
}`

func with(t *testing.T, old, new string) string {
	t.Helper()
	if !strings.Contains(validBoard, old) {
		t.Fatalf("validBoard does not contain %q", old)
	}
	return strings.Replace(validBoard, old, new, 1)
}

// ORB-HW-008: every shipped profile validates, and the registry loads.
func TestShippedProfilesValidate(t *testing.T) {
	reg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reg.All()) < 3 {
		t.Fatalf("expected at least a SoC, a board and the generic profile, got %d", len(reg.All()))
	}
	for _, p := range reg.All() {
		if err := p.Validate(); err != nil {
			t.Errorf("%s: %v", p.ProfileID, err)
		}
	}
	if reg.Generic() == nil || reg.Generic().ProfileID != GenericProfileID {
		t.Fatal("the built-in generic ARM64 profile is missing")
	}
}

// ORB-HW-009, Pflichtenheft 12.4: no shipped profile claims Verified.
func TestNoShippedProfileClaimsVerified(t *testing.T) {
	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range reg.All() {
		if p.SupportTier == TierVerified {
			t.Errorf("%s claims verified", p.ProfileID)
		}
		for _, tc := range p.TestedCombinations {
			if hasAll(tc.FunctionsTested, CoreFunctions) {
				t.Errorf("%s lists a complete test of all core functions; only the G3 install test exists", p.ProfileID)
			}
		}
	}
}

func TestShippedPiZero2WProfile(t *testing.T) {
	reg, _ := Load()
	m := reg.Match([]string{"raspberrypi,model-zero-2-w", "brcm,bcm2837"}, "Raspberry Pi Zero 2 W Rev 1.0")
	if !m.IsBoard() || m.Profile.ProfileID != "raspberry-pi-zero-2-w" || m.MatchedBy != MatchedByCompatible {
		t.Fatalf("match = %+v", m)
	}
	if m.SoC == nil || m.SoC.SoCFamily != "BCM2837" || m.Profile.SupportTier != TierCompatible {
		t.Fatalf("soc/tier wrong: %+v", m)
	}
}

// ORB-HW-008: the validator rejects what it must reject.
func TestParseRejects(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string // substring of the error
	}{
		{"unknown field", with(t, `"vendor": "Test"`, `"vendor": "Test", "unexpected": 1`), "unknown field"},
		{"command field", with(t, `"vendor": "Test"`, `"vendor": "Test", "command": "rm -rf /"`), "unknown field"},
		{"exec field", with(t, `"vendor": "Test"`, `"vendor": "Test", "post_install": ["sh", "-c", "x"]`), "unknown field"},
		{"shell in handler binding", with(t, `"maintainers": ["Tester"]`, `"maintainers": ["Tester"], "sensor_bindings": [{"sensor": "cpu_temperature", "handler": "sh -c reboot"}]`), "not a known core handler"},
		{"unknown handler", with(t, `"maintainers": ["Tester"]`, `"maintainers": ["Tester"], "sensor_bindings": [{"sensor": "cpu_temperature", "handler": "sensor.made_up.v1"}]`), "not a known core handler"},
		{"unversioned handler", with(t, `"maintainers": ["Tester"]`, `"maintainers": ["Tester"], "sensor_bindings": [{"sensor": "cpu_temperature", "handler": "sensor.thermal_zone"}]`), "not a known core handler"},
		{"unknown sensor", with(t, `"maintainers": ["Tester"]`, `"maintainers": ["Tester"], "sensor_bindings": [{"sensor": "fan_speed", "handler": "sensor.hwmon.v1"}]`), "not a known sensor"},
		{"command substitution in text", with(t, `"vendor": "Test"`, `"vendor": "$(reboot)"`), "vendor"},
		{"semicolon in text", with(t, `"model": "Test Board"`, `"model": "Board; reboot"`), "model"},
		{"backtick in maintainer", with(t, `"Tester"`, "\"`id`\""), "maintainer"},
		{"control character", with(t, `"vendor": "Test"`, `"vendor": "Te\u0007st"`), "vendor"},
		{"wrong schema version", with(t, `"schema_version": 1`, `"schema_version": 2`), "schema_version"},
		{"missing schema version", with(t, `"schema_version": 1,`, ``), "schema_version"},
		{"unknown kind", with(t, `"kind": "board"`, `"kind": "plugin"`), "kind"},
		{"bad id", with(t, `"profile_id": "test-board"`, `"profile_id": "../etc"`), "profile_id"},
		{"bad version", with(t, `"version": "1.0.0"`, `"version": "1"`), "version"},
		{"bad tier", with(t, `"support_tier": "compatible"`, `"support_tier": "gold"`), "support_tier"},
		{"unsupported is computed, not declared", with(t, `"support_tier": "compatible"`, `"support_tier": "unsupported"`), "support_tier"},
		{"board with tier generic", with(t, `"support_tier": "compatible"`, `"support_tier": "generic"`), "generic"},
		{"verified without tested combination", with(t, `"support_tier": "compatible"`, `"support_tier": "verified"`), "verified"},
		{"free text as limitation", with(t, `"maintainers"`, `"known_limitations": ["The Wi-Fi is slow."], "maintainers"`), "known limitation"},
		{"bad documentation link", with(t, `"maintainers"`, `"documentation_links": ["http://evil.example/x"], "maintainers"`), "documentation link"},
		{"unknown capability", with(t, `"maintainers"`, `"capabilities": ["warp_drive"], "maintainers"`), "capability"},
		{"bad pattern", with(t, `"test,board"`, `"test board"`), "pattern"},
		{"two wildcards", with(t, `"test,board"`, `"test,*bo*"`), "pattern"},
		{"board without os constraints", with(t, `"os_constraints": {"adapters": ["raspberry_pi_os"]},`, ``), "os_constraints"},
		{"board without model", with(t, `"model": "Test Board",`, ``), "model"},
		{"board without soc", with(t, `"soc_family": "BCM2837",`, ``), "soc_family"},
		{"bad kernel constraint", with(t, `"maintainers"`, `"kernel_constraints": {"min_release": "new"}, "maintainers"`), "min_release"},
		{"bad boot medium", with(t, `"maintainers"`, `"storage_hints": {"boot_media": ["floppy"]}, "maintainers"`), "boot_media"},
		{"trailing data", validBoard + ` {}`, "unexpected data"},
		{"not json", `profile_id: x`, "decode"},
		{"empty", ``, "decode"},
		{"too large", validBoard + strings.Repeat(" ", maxProfileFile), "too large"},
	}
	for _, c := range cases {
		_, err := Parse([]byte(c.json))
		if err == nil {
			t.Errorf("%s: accepted", c.name)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not mention %q", c.name, err, c.want)
		}
	}
	if _, err := Parse([]byte(validBoard)); err != nil {
		t.Fatalf("the baseline profile must be valid: %v", err)
	}
}

// Pflichtenheft 12.4: Verified needs a tested combination with all core
// functions and an evidence reference.
func TestVerifiedRule(t *testing.T) {
	combo := func(functions, evidence string) string {
		return with(t, `"support_tier": "compatible"`, `"support_tier": "verified", "tested_combinations": [{
		  "os_adapter": "raspberry_pi_os", "oarfish_version": "v1.0.0",
		  "functions_tested": [`+functions+`], "evidence_ref": "`+evidence+`"}]`)
	}
	all := `"install","dashboard","system_functions","self_update","rollback","plugins"`

	if _, err := Parse([]byte(combo(all, "docs/evidence.md#pi"))); err != nil {
		t.Errorf("complete evidence must be accepted: %v", err)
	}
	for name, js := range map[string]string{
		"only install":        combo(`"install"`, "docs/evidence.md"),
		"missing rollback":    combo(`"install","dashboard","system_functions","self_update","plugins"`, "docs/evidence.md"),
		"no evidence":         combo(all, ""),
		"evidence not a ref":  combo(all, "trust me"),
		"unknown function":    combo(all+`,"magic"`, "docs/evidence.md"),
		"no functions listed": combo(``, "docs/evidence.md"),
	} {
		if _, err := Parse([]byte(js)); err == nil {
			t.Errorf("%s: verified accepted", name)
		}
	}
	// A partial test is fine for a non-verified tier.
	partial := with(t, `"maintainers"`, `"tested_combinations": [{"os_adapter": "raspberry_pi_os", "oarfish_version": "v0.1.0-alpha.2",
	  "functions_tested": ["install"], "evidence_ref": "docs/INSTALL.md#hardware-test-results"}], "maintainers"`)
	if _, err := Parse([]byte(partial)); err != nil {
		t.Errorf("partial evidence for a compatible profile: %v", err)
	}
}

func TestKindSpecificRules(t *testing.T) {
	soc := `{"schema_version":1,"kind":"soc_family","profile_id":"soc-x","version":"1.0.0","vendor":"V","soc_family":"X1",
	  "compatible_patterns":["v,x1"],"maintainers":["M"]}`
	if _, err := Parse([]byte(soc)); err != nil {
		t.Fatalf("valid soc: %v", err)
	}
	for name, js := range map[string]string{
		"soc with model":        strings.Replace(soc, `"soc_family":"X1"`, `"soc_family":"X1","model":"M"`, 1),
		"soc with tier":         strings.Replace(soc, `"soc_family":"X1"`, `"soc_family":"X1","support_tier":"compatible"`, 1),
		"soc without patterns":  strings.Replace(soc, `"compatible_patterns":["v,x1"]`, `"compatible_patterns":[]`, 1),
		"soc without family":    strings.Replace(soc, `"soc_family":"X1",`, ``, 1),
		"generic with patterns": `{"schema_version":1,"kind":"generic","profile_id":"generic-arm64","version":"1.0.0","vendor":"V","compatible_patterns":["a,b"],"support_tier":"generic","maintainers":["M"]}`,
		"generic with wrong id": `{"schema_version":1,"kind":"generic","profile_id":"other","version":"1.0.0","vendor":"V","compatible_patterns":[],"support_tier":"generic","maintainers":["M"]}`,
		"generic with tier":     `{"schema_version":1,"kind":"generic","profile_id":"generic-arm64","version":"1.0.0","vendor":"V","compatible_patterns":[],"support_tier":"verified","maintainers":["M"]}`,
	} {
		if _, err := Parse([]byte(js)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func registryFS(boards, socs map[string]string, generic string) fstest.MapFS {
	m := fstest.MapFS{"soc/.keep": {}, "boards/.keep": {}, "generic/.keep": {}}
	for n, c := range socs {
		m["soc/"+n] = &fstest.MapFile{Data: []byte(c)}
	}
	for n, c := range boards {
		m["boards/"+n] = &fstest.MapFile{Data: []byte(c)}
	}
	if generic != "" {
		m["generic/generic-arm64.json"] = &fstest.MapFile{Data: []byte(generic)}
	}
	return m
}

const (
	testSoC     = `{"schema_version":1,"kind":"soc_family","profile_id":"soc-bcm2837","version":"1.0.0","vendor":"B","soc_family":"BCM2837","compatible_patterns":["brcm,bcm2837"],"maintainers":["M"]}`
	testGeneric = `{"schema_version":1,"kind":"generic","profile_id":"generic-arm64","version":"1.0.0","vendor":"V","compatible_patterns":[],"support_tier":"generic","maintainers":["M"]}`
)

func TestRegistryRelationChecks(t *testing.T) {
	ok := registryFS(map[string]string{"b.json": validBoard}, map[string]string{"s.json": testSoC}, testGeneric)
	if _, err := LoadFS(ok); err != nil {
		t.Fatalf("baseline registry: %v", err)
	}
	cases := map[string]struct {
		fs   fstest.MapFS
		want string
	}{
		"no generic profile":   {registryFS(nil, map[string]string{"s.json": testSoC}, ""), "generic"},
		"board unknown soc":    {registryFS(map[string]string{"b.json": validBoard}, nil, testGeneric), "unknown soc_family"},
		"duplicate id":         {registryFS(map[string]string{"b.json": validBoard, "c.json": validBoard}, map[string]string{"s.json": testSoC}, testGeneric), "duplicate"},
		"kind in wrong folder": {registryFS(map[string]string{"s.json": testSoC}, nil, testGeneric), "does not belong"},
		"invalid file":         {registryFS(map[string]string{"b.json": `{"kind":"board"}`}, map[string]string{"s.json": testSoC}, testGeneric), "b.json"},
		"unknown field":        {registryFS(map[string]string{"b.json": strings.Replace(validBoard, `"vendor"`, `"cmd":"x","vendor"`, 1)}, map[string]string{"s.json": testSoC}, testGeneric), "unknown field"},
	}
	for name, c := range cases {
		if _, err := LoadFS(c.fs); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want mention of %q", name, err, c.want)
		}
	}
	if _, err := LoadFS(fstest.MapFS{}); err == nil {
		t.Error("an empty registry must not load")
	}
}

// Matching: compatible_patterns first, the model string is secondary.
func TestMatch(t *testing.T) {
	zero2w := strings.Replace(validBoard, `"test,board"`, `"raspberrypi,model-zero-2-w", "raspberrypi,wild-*"`, 1)
	zero2w = strings.Replace(zero2w, `"Test Board"`, `"Raspberry Pi Zero 2 W"`, 1)
	reg, err := LoadFS(registryFS(map[string]string{"b.json": zero2w}, map[string]string{"s.json": testSoC}, testGeneric))
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name       string
		compatible []string
		model      string
		wantID     string
		wantBy     string
		wantSoC    bool
	}{
		{"exact compatible", []string{"raspberrypi,model-zero-2-w", "brcm,bcm2837"}, "", "test-board", MatchedByCompatible, true},
		{"wildcard compatible", []string{"raspberrypi,wild-one"}, "", "test-board", MatchedByCompatible, true},
		{"compatible beats model", []string{"raspberrypi,model-zero-2-w"}, "Something else", "test-board", MatchedByCompatible, true},
		{"model only", nil, "Raspberry Pi Zero 2 W Rev 1.0", "test-board", MatchedByModel, true},
		{"model without revision", nil, "Raspberry Pi Zero 2 W", "test-board", MatchedByModel, true},
		{"model with other suffix", nil, "Raspberry Pi Zero 2 W Plus", "generic-arm64", MatchedByFallback, false},
		{"model with odd revision", nil, "Raspberry Pi Zero 2 W Rev x;y", "generic-arm64", MatchedByFallback, false},
		{"model prefix only", nil, "Raspberry Pi Zero 2", "generic-arm64", MatchedByFallback, false},
		{"soc only", []string{"vendor,board", "brcm,bcm2837"}, "Unknown", "generic-arm64", MatchedByFallback, true},
		{"nothing", []string{"a,b"}, "", "generic-arm64", MatchedByFallback, false},
		{"similar but different", []string{"raspberrypi,model-zero-2-wx"}, "", "generic-arm64", MatchedByFallback, false},
	}
	for _, c := range cases {
		m := reg.Match(c.compatible, c.model)
		if m.Profile.ProfileID != c.wantID || m.MatchedBy != c.wantBy || (m.SoC != nil) != c.wantSoC {
			t.Errorf("%s: got %s by %s soc=%v; want %s by %s soc=%v", c.name,
				m.Profile.ProfileID, m.MatchedBy, m.SoC != nil, c.wantID, c.wantBy, c.wantSoC)
		}
	}
}

func TestKernelAtLeast(t *testing.T) {
	cases := []struct {
		release, min string
		want         bool
	}{
		{"6.18.50+rpt-rpi-v8", "5.10", true},
		{"5.10.0", "5.10", true},
		{"5.9.9", "5.10", false},
		{"4.19", "5.10", false},
		{"6.1.0-28-arm64", "6.1", true},
		{"6.0", "6.1", false},
		{"anything", "", true},
		{"", "", true},
		{"", "5.10", false},
		{"garbage", "5.10", false},
		{"6.x", "5.10", false},
	}
	for _, c := range cases {
		if got := KernelAtLeast(c.release, c.min); got != c.want {
			t.Errorf("KernelAtLeast(%q, %q) = %v, want %v", c.release, c.min, got, c.want)
		}
	}
}
