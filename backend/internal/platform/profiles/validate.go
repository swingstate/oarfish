package profiles

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/swingstate/oarfish/backend/internal/platform/capabilities"
)

var (
	reID        = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	reVersion   = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	reKernelMin = regexp.MustCompile(`^[0-9]{1,3}\.[0-9]{1,3}$`)
	// Compatible strings and patterns: letters, digits and ",._+-", optional trailing "*".
	rePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9,._+-]{2,127}\*?$`)
	reRef     = regexp.MustCompile(`^(docs/[A-Za-z0-9_./#-]+|https://github\.com/swingstate/oarfish/[A-Za-z0-9_./#?=&-]+)$`)
	reOarfish = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$`)
)

// shellMeta are characters that make a text field look like a command. Profile
// text is data; none of these has a use in it.
const shellMeta = "`$;|&<>\\\n\r\t\"'(){}*?!"

// Validate checks a decoded profile against the schema and returns every
// problem found (nil if the profile is valid). Strict JSON decoding has already
// rejected unknown fields, so a field such as "command" or "script" never gets
// this far; Validate additionally restricts what the known fields may hold.
func (p *Profile) Validate() error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if p.SchemaVersion != SchemaVersion {
		bad("schema_version is %d, this build understands %d", p.SchemaVersion, SchemaVersion)
	}
	if !reID.MatchString(p.ProfileID) {
		bad("profile_id %q is not a valid id", p.ProfileID)
	}
	if !reVersion.MatchString(p.Version) {
		bad("version %q is not major.minor.patch", p.Version)
	}
	if !safeText(p.Vendor, 64) {
		bad("vendor %q is empty or not plain text", p.Vendor)
	}
	for _, c := range p.CompatiblePatterns {
		if !rePattern.MatchString(c) || strings.Count(c, "*") > 1 {
			bad("compatible pattern %q is not valid", c)
		}
	}
	for _, m := range p.Maintainers {
		if !safeText(m, 64) {
			bad("maintainer %q is not plain text", m)
		}
	}
	for _, l := range p.KnownLimitations {
		if !reID.MatchString(l) {
			bad("known limitation %q must be a key like \"memory_512mb\", not text", l)
		}
	}
	for _, l := range p.DocumentationLinks {
		if !reRef.MatchString(l) {
			bad("documentation link %q must be a path under docs/ or a swingstate/oarfish URL", l)
		}
	}
	for _, c := range p.Capabilities {
		if !knownCapability(c) {
			bad("capability %q is not a known capability id", c)
		}
	}
	for _, b := range p.SensorBindings {
		if !KnownSensors[b.Sensor] {
			bad("sensor %q is not a known sensor", b.Sensor)
		}
		if !KnownHandlers[b.Handler] {
			bad("handler %q is not a known core handler", b.Handler)
		}
	}
	if p.KernelConstraints != nil && p.KernelConstraints.MinRelease != "" && !reKernelMin.MatchString(p.KernelConstraints.MinRelease) {
		bad("kernel_constraints.min_release %q is not major.minor", p.KernelConstraints.MinRelease)
	}
	if p.StorageHints != nil {
		for _, m := range p.StorageHints.BootMedia {
			if !slices.Contains([]string{"mmc", "nvme", "usb"}, m) {
				bad("storage_hints.boot_media %q is not mmc, nvme or usb", m)
			}
		}
	}

	switch p.Kind {
	case KindSoCFamily:
		errs = append(errs, p.validateSoC()...)
	case KindBoard:
		errs = append(errs, p.validateBoard()...)
	case KindGeneric:
		errs = append(errs, p.validateGeneric()...)
	default:
		bad("kind %q is not soc_family, board or generic", p.Kind)
	}
	errs = append(errs, p.validateTier()...)
	return errors.Join(errs...)
}

func (p *Profile) validateSoC() []error {
	var errs []error
	if !safeText(p.SoCFamily, 32) {
		errs = append(errs, errors.New("soc_family profile needs soc_family"))
	}
	if len(p.CompatiblePatterns) == 0 {
		errs = append(errs, errors.New("soc_family profile needs compatible_patterns"))
	}
	if len(p.Maintainers) == 0 {
		errs = append(errs, errors.New("soc_family profile needs maintainers"))
	}
	if p.Model != "" || p.SupportTier != "" || len(p.TestedCombinations) > 0 || p.OSConstraints != nil || p.KernelConstraints != nil {
		errs = append(errs, errors.New("soc_family profile must not set model, support_tier, tested_combinations, os_constraints or kernel_constraints"))
	}
	return errs
}

func (p *Profile) validateBoard() []error {
	var errs []error
	if !safeText(p.Model, 64) {
		errs = append(errs, errors.New("board profile needs model"))
	}
	if !safeText(p.SoCFamily, 32) {
		errs = append(errs, errors.New("board profile needs soc_family"))
	}
	if len(p.CompatiblePatterns) == 0 {
		errs = append(errs, errors.New("board profile needs compatible_patterns"))
	}
	if len(p.Maintainers) == 0 {
		errs = append(errs, errors.New("board profile needs maintainers"))
	}
	if p.OSConstraints == nil || len(p.OSConstraints.Adapters) == 0 {
		errs = append(errs, errors.New("board profile needs os_constraints.adapters"))
	} else {
		for _, a := range p.OSConstraints.Adapters {
			if !reID.MatchString(a) {
				errs = append(errs, fmt.Errorf("os adapter %q is not a valid id", a))
			}
		}
	}
	return errs
}

func (p *Profile) validateGeneric() []error {
	var errs []error
	if p.ProfileID != GenericProfileID {
		errs = append(errs, fmt.Errorf("the generic profile must have profile_id %q", GenericProfileID))
	}
	if len(p.CompatiblePatterns) != 0 || p.SoCFamily != "" || len(p.TestedCombinations) != 0 || p.OSConstraints != nil {
		errs = append(errs, errors.New("generic profile must not set compatible_patterns, soc_family, tested_combinations or os_constraints"))
	}
	if p.SupportTier != TierGeneric {
		errs = append(errs, errors.New("generic profile must have support_tier \"generic\""))
	}
	return errs
}

// validateTier enforces the tier rules, in particular the Verified rule of
// Pflichtenheft 12.4: no Verified without physical evidence for all core functions.
func (p *Profile) validateTier() []error {
	var errs []error
	if p.Kind == KindSoCFamily {
		return nil
	}
	switch p.SupportTier {
	case TierVerified:
		if !slices.ContainsFunc(p.TestedCombinations, func(tc TestedCombination) bool {
			return hasAll(tc.FunctionsTested, CoreFunctions) && tc.EvidenceRef != ""
		}) {
			errs = append(errs, errors.New(`support_tier "verified" needs a tested combination that lists every core function and an evidence reference`))
		}
	case TierCompatible, TierCommunity, TierExperimental:
	case TierGeneric:
		if p.Kind == KindBoard {
			errs = append(errs, errors.New(`a board profile cannot have support_tier "generic"`))
		}
	default:
		errs = append(errs, fmt.Errorf("support_tier %q is not verified, compatible, community, experimental or generic", p.SupportTier))
	}
	for i, tc := range p.TestedCombinations {
		if !reID.MatchString(tc.OSAdapter) {
			errs = append(errs, fmt.Errorf("tested_combinations[%d].os_adapter %q is not valid", i, tc.OSAdapter))
		}
		if tc.OSVersionID != "" && !safeText(tc.OSVersionID, 32) {
			errs = append(errs, fmt.Errorf("tested_combinations[%d].os_version_id is not plain text", i))
		}
		if !reOarfish.MatchString(tc.OarfishVersion) {
			errs = append(errs, fmt.Errorf("tested_combinations[%d].oarfish_version %q is not a version", i, tc.OarfishVersion))
		}
		if !reRef.MatchString(tc.EvidenceRef) {
			errs = append(errs, fmt.Errorf("tested_combinations[%d] needs an evidence_ref (a path under docs/ or a swingstate/oarfish URL)", i))
		}
		for _, f := range tc.FunctionsTested {
			if !slices.Contains(CoreFunctions, f) {
				errs = append(errs, fmt.Errorf("tested_combinations[%d].functions_tested has unknown function %q", i, f))
			}
		}
	}
	return errs
}

func hasAll(have, want []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

// safeText accepts non-empty printable text without shell metacharacters.
func safeText(s string, max int) bool {
	if s == "" || len(s) > max || strings.ContainsAny(s, shellMeta) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r > 0x7e {
			return false
		}
	}
	return strings.TrimSpace(s) == s
}

func knownCapability(id string) bool { return slices.Contains(capabilities.All, id) }

// parseKernelMinor returns major and minor of a "major.minor[.x...]" release.
func parseKernelMinor(release string) (major, minor int, ok bool) {
	parts := strings.SplitN(release, ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, err1 := strconv.Atoi(parts[0])
	minorDigits := parts[1]
	if i := strings.IndexFunc(minorDigits, func(r rune) bool { return r < '0' || r > '9' }); i >= 0 {
		minorDigits = minorDigits[:i]
	}
	minor, err2 := strconv.Atoi(minorDigits)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return major, minor, true
}
