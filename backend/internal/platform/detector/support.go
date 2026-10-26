package detector

import (
	"slices"

	"github.com/swingstate/oarfish/backend/internal/platform/capabilities"
	"github.com/swingstate/oarfish/backend/internal/platform/hardware"
	"github.com/swingstate/oarfish/backend/internal/platform/operating_system"
	"github.com/swingstate/oarfish/backend/internal/platform/profiles"
)

// Support tiers (Pflichtenheft 3.6). Verified is only ever produced for a
// combination that a profile lists with evidence for all core functions
// (Pflichtenheft 12.4); no shipped profile does.
const (
	TierVerified     = "verified"
	TierCompatible   = "compatible"
	TierCommunity    = "community"
	TierExperimental = "experimental"
	TierGeneric      = "generic"
	TierUnsupported  = "unsupported"
)

// Reason codes. Together with the tier they select the user-facing
// explanation: key = "support." + tier + "." + reason.
const (
	ReasonNotLinux            = "not_linux"
	ReasonNotARM64            = "not_arm64"
	ReasonMissingMinimum      = "missing_minimum_capabilities"
	ReasonNoBoardProfile      = "no_board_profile"
	ReasonOSNotCovered        = "os_not_covered_by_profile"
	ReasonKernelTooOld        = "kernel_too_old"
	ReasonProfileNotVerified  = "profile_not_verified"
	ReasonCombinationUntested = "verified_combination_not_matched"
	ReasonCommunity           = "profile_community"
	ReasonExperimental        = "profile_experimental"
	ReasonVerified            = "verified_combination"
)

// Limitation keys that the detector itself adds (profiles add their own).
const (
	LimitGenericMode        = "generic_mode"
	LimitRaspberryPiNoBoard = "raspberry_pi_no_board_profile"
	LimitOSNotQualified     = "os_adapter_not_qualified"
	LimitOSUnrecognised     = "os_unrecognised"
	LimitExpectedCapMissing = "expected_capability_missing"
	limitMissingCapPrefix   = "capability_missing_"
)

// Support is the support statement for this system.
type Support struct {
	Tier           string
	Reason         string
	ExplanationKey string
	Limitations    []string
	// BoardActionsAllowed is true only when a board profile matched and the
	// system is not in Generic Mode or Unsupported. Board-specific actions must
	// check it (ORB-HW-007, ORB-NFR-015). G4 has no such actions.
	BoardActionsAllowed bool
}

func newSupport(tier, reason string, limitations []string, boardActions bool) Support {
	if limitations == nil {
		limitations = []string{}
	}
	return Support{
		Tier:                tier,
		Reason:              reason,
		ExplanationKey:      "support." + tier + "." + reason,
		Limitations:         limitations,
		BoardActionsAllowed: boardActions,
	}
}

func computeSupport(hw hardware.Info, osInfo operating_system.Info, caps capabilities.Result, match profiles.Match) Support {
	if hw.GOOS != "linux" {
		return newSupport(TierUnsupported, ReasonNotLinux, nil, false)
	}
	if hw.Arch != "arm64" {
		return newSupport(TierUnsupported, ReasonNotARM64, nil, false)
	}
	var missing []string
	for _, id := range capabilities.Minimum {
		if !caps.Available(id) {
			missing = append(missing, limitMissingCapPrefix+id)
		}
	}
	if len(missing) > 0 {
		return newSupport(TierUnsupported, ReasonMissingMinimum, missing, false)
	}

	var limits []string
	switch osInfo.AdapterStatus {
	case operating_system.StatusNotYetQualified:
		limits = append(limits, LimitOSNotQualified)
	case operating_system.StatusUnrecognised:
		limits = append(limits, LimitOSUnrecognised)
	}

	if !match.IsBoard() {
		limits = append(limits, LimitGenericMode)
		if hw.Class == hardware.ClassRaspberryPi {
			limits = append(limits, LimitRaspberryPiNoBoard)
		}
		return newSupport(TierGeneric, ReasonNoBoardProfile, limits, false)
	}

	p := match.Profile
	limits = append(limits, p.KnownLimitations...)
	if p.OSConstraints == nil || !slices.Contains(p.OSConstraints.Adapters, osInfo.Adapter) {
		return newSupport(TierGeneric, ReasonOSNotCovered, append(limits, LimitGenericMode), false)
	}
	if p.KernelConstraints != nil && !profiles.KernelAtLeast(osInfo.Kernel, p.KernelConstraints.MinRelease) {
		return newSupport(TierGeneric, ReasonKernelTooOld, append(limits, LimitGenericMode), false)
	}
	for _, id := range p.Capabilities {
		if !caps.Available(id) {
			limits = append(limits, LimitExpectedCapMissing)
			break
		}
	}

	switch p.SupportTier {
	case profiles.TierVerified:
		if testedHere(p, osInfo) {
			return newSupport(TierVerified, ReasonVerified, limits, true)
		}
		return newSupport(TierCompatible, ReasonCombinationUntested, limits, true)
	case profiles.TierCommunity:
		return newSupport(TierCommunity, ReasonCommunity, limits, true)
	case profiles.TierExperimental:
		return newSupport(TierExperimental, ReasonExperimental, limits, true)
	default:
		return newSupport(TierCompatible, ReasonProfileNotVerified, limits, true)
	}
}

// testedHere reports whether the profile lists a complete physical test of this
// OS adapter (and OS version, if the entry names one).
func testedHere(p *profiles.Profile, osInfo operating_system.Info) bool {
	return slices.ContainsFunc(p.TestedCombinations, func(tc profiles.TestedCombination) bool {
		complete := true
		for _, f := range profiles.CoreFunctions {
			complete = complete && slices.Contains(tc.FunctionsTested, f)
		}
		return complete && tc.EvidenceRef != "" && tc.OSAdapter == osInfo.Adapter &&
			(tc.OSVersionID == "" || tc.OSVersionID == osInfo.VersionID)
	})
}

// TierReasons lists every (tier, reason) pair the detector can produce. The
// GUI translation files must have an explanation for each; a test checks this.
var TierReasons = [][2]string{
	{TierUnsupported, ReasonNotLinux},
	{TierUnsupported, ReasonNotARM64},
	{TierUnsupported, ReasonMissingMinimum},
	{TierGeneric, ReasonNoBoardProfile},
	{TierGeneric, ReasonOSNotCovered},
	{TierGeneric, ReasonKernelTooOld},
	{TierCompatible, ReasonProfileNotVerified},
	{TierCompatible, ReasonCombinationUntested},
	{TierCommunity, ReasonCommunity},
	{TierExperimental, ReasonExperimental},
	{TierVerified, ReasonVerified},
}

// ExplanationKeys returns the translation key of every possible explanation.
func ExplanationKeys() []string {
	keys := make([]string, len(TierReasons))
	for i, tr := range TierReasons {
		keys[i] = "support." + tr[0] + "." + tr[1]
	}
	return keys
}

// LimitationKeys returns every limitation key the detector can add itself.
func LimitationKeys() []string {
	keys := []string{LimitGenericMode, LimitRaspberryPiNoBoard, LimitOSNotQualified, LimitOSUnrecognised, LimitExpectedCapMissing}
	for _, id := range capabilities.Minimum {
		keys = append(keys, limitMissingCapPrefix+id)
	}
	return keys
}
