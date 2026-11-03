package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The bundle has two shapes (ADR 0010, U14 row G6.2):
//
//	legacy:     the root directory and LICENSE, THIRD_PARTY_NOTICES.txt, oarfish,
//	            oarfish.service (five entries). The default.
//	components: the five above plus the update helper and its three unit files
//	            (nine entries). The manifest then carries the object "components".
//
// There is no third shape: a bundle has either all four component files and the
// "components" object, or none of them.
const (
	helperName         = "oarfish-update-helper"
	helperServiceName  = "oarfish-update-helper.service"
	helperSocketName   = "oarfish-update-helper.socket"
	recoverServiceName = "oarfish-update-recover.service"
)

type layoutFile struct {
	name string
	mode int64
}

var legacyLayout = []layoutFile{
	{"LICENSE", 0o644},
	{"THIRD_PARTY_NOTICES.txt", 0o644},
	{"oarfish", 0o755},
	{"oarfish.service", 0o644},
}

var componentLayout = []layoutFile{
	{helperName, 0o755},
	{helperServiceName, 0o644},
	{helperSocketName, 0o644},
	{recoverServiceName, 0o644},
}

func isComponentFile(name string) bool {
	for _, f := range componentLayout {
		if f.name == name {
			return true
		}
	}
	return false
}

// expectedLayout is the exact set of files, with modes, that a bundle of the
// given shape holds.
func expectedLayout(components bool) []layoutFile {
	l := append([]layoutFile(nil), legacyLayout...)
	if components {
		l = append(l, componentLayout...)
	}
	return l
}

var keyIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// Components is the manifest object that describes the helper a bundle delivers.
// It is present if and only if the four component files are in the bundle.
type Components struct {
	// KeyID is the 16-digit id of the key embedded in the delivered helper.
	KeyID string `json:"key_id"`
	// Protocol is the highest protocol the delivered helper implements.
	Protocol int `json:"protocol"`
}

// componentsInput is the all-or-nothing set of inputs for the components shape.
type componentsInput struct {
	Helper         []byte
	HelperService  []byte
	HelperSocket   []byte
	RecoverService []byte
	KeyID          string
	Protocol       int
}

func (c *componentsInput) files() map[string][]byte {
	return map[string][]byte{
		helperName:         c.Helper,
		helperServiceName:  c.HelperService,
		helperSocketName:   c.HelperSocket,
		recoverServiceName: c.RecoverService,
	}
}

func (c *componentsInput) validate() error {
	for _, f := range componentLayout {
		if len(c.files()[f.name]) == 0 {
			return fmt.Errorf("components: %s must not be empty", f.name)
		}
	}
	if err := checkARM64ELF(c.Helper); err != nil {
		return fmt.Errorf("components: %s: %w", helperName, err)
	}
	return nil
}

// checkARM64ELF applies the installer's check_binary (install/install.sh): ELF
// magic, 64-bit class, little-endian data, machine 0xB7 (AArch64).
func checkARM64ELF(b []byte) error {
	if len(b) < 20 {
		return errors.New("too short for an ELF header")
	}
	if string(b[:6]) != "\x7fELF\x02\x01" {
		return errors.New("not a 64-bit little-endian ELF file")
	}
	if b[18] != 0xb7 || b[19] != 0x00 {
		return errors.New("not a 64-bit ARM (aarch64) executable")
	}
	return nil
}

// checkContents requires the manifest's contents to be exactly the files, with
// exactly the modes, of the shape the manifest declares through "components".
func checkContents(mf *Manifest) error {
	root := strings.TrimSuffix(mf.Bundle.File, ".tar.gz")
	hasComponents := mf.Components != nil
	want := map[string]int64{}
	for _, f := range expectedLayout(hasComponents) {
		want[root+"/"+f.name] = f.mode
	}
	seen := map[string]bool{}
	for _, e := range mf.Contents {
		mode, ok := want[e.Path]
		if !ok {
			if !hasComponents && isComponentFile(strings.TrimPrefix(e.Path, root+"/")) {
				return fmt.Errorf("manifest: contents lists %s but there is no components object", e.Path)
			}
			return fmt.Errorf("manifest: contents lists unexpected entry %s", e.Path)
		}
		if seen[e.Path] {
			return fmt.Errorf("manifest: contents lists %s twice", e.Path)
		}
		seen[e.Path] = true
		if e.Mode != fmt.Sprintf("%04o", mode) {
			return fmt.Errorf("manifest: %s has mode %s, want %04o", e.Path, e.Mode, mode)
		}
	}
	for p := range want {
		if !seen[p] {
			if hasComponents && isComponentFile(strings.TrimPrefix(p, root+"/")) {
				return fmt.Errorf("manifest: components object is present but contents does not list %s", p)
			}
			return fmt.Errorf("manifest: contents does not list %s", p)
		}
	}
	return nil
}

// checkExtras validates the additive manifest fields of ADR 0010 (U4). The
// version of the manifest must already be valid.
func checkExtras(mf *Manifest) error {
	r := mf.Requirements
	if r.MinInstalledVersion != nil {
		v := *r.MinInstalledVersion
		if !versionPattern.MatchString(v) {
			return fmt.Errorf("manifest: requirements.min_installed_version %q is not a SemVer version", v)
		}
		if compareSemver(v, mf.Version) > 0 {
			return fmt.Errorf("manifest: requirements.min_installed_version %s is newer than version %s", v, mf.Version)
		}
	}
	if r.HelperProtocol != nil && *r.HelperProtocol < 1 {
		return fmt.Errorf("manifest: requirements.helper_protocol is %d, want at least 1", *r.HelperProtocol)
	}
	if c := mf.Components; c != nil {
		if !keyIDPattern.MatchString(c.KeyID) {
			return fmt.Errorf("manifest: components.key_id %q is not 16 lowercase hexadecimal digits", c.KeyID)
		}
		if c.Protocol < 1 {
			return fmt.Errorf("manifest: components.protocol is %d, want at least 1", c.Protocol)
		}
		if r.HelperProtocol != nil && *r.HelperProtocol > c.Protocol {
			return fmt.Errorf("manifest: requirements.helper_protocol %d is greater than components.protocol %d", *r.HelperProtocol, c.Protocol)
		}
	}
	return nil
}

// compareSemver orders two versions that match versionPattern by SemVer 2.0
// precedence: -1, 0 or 1. Numeric parts are compared as digit strings (length,
// then text), so no integer can overflow.
func compareSemver(a, b string) int {
	core := func(v string) (string, string) {
		c, pre, _ := strings.Cut(v, "-")
		return c, pre
	}
	ac, ap := core(a)
	bc, bp := core(b)
	as, bs := strings.Split(ac, "."), strings.Split(bc, ".")
	for i := range as {
		if c := compareNumeric(as[i], bs[i]); c != 0 {
			return c
		}
	}
	switch {
	case ap == bp:
		return 0
	case ap == "":
		return 1 // a release is newer than its own prereleases
	case bp == "":
		return -1
	}
	ai, bi := strings.Split(ap, "."), strings.Split(bp, ".")
	for i := 0; i < len(ai) && i < len(bi); i++ {
		if c := compareIdentifier(ai[i], bi[i]); c != 0 {
			return c
		}
	}
	return compareInt(len(ai), len(bi))
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func compareNumeric(a, b string) int {
	if c := compareInt(len(a), len(b)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

func compareIdentifier(a, b string) int {
	an, bn := isDigits(a), isDigits(b)
	switch {
	case an && bn:
		return compareNumeric(a, b)
	case an:
		return -1 // numeric identifiers sort below alphanumeric ones
	case bn:
		return 1
	}
	return strings.Compare(a, b)
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
