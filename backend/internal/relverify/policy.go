package relverify

// CheckMinInstalled applies requirements.min_installed_version: when the
// manifest has it, the installed version must be at least that. A failure is
// CodeRequirementsNotMet.
func CheckMinInstalled(installed Version, m *Manifest) error {
	if m == nil {
		return fail(CodeInternal, "no manifest")
	}
	if m.Requirements.MinInstalledVersion == "" {
		return nil
	}
	min, err := ParseVersion(m.Requirements.MinInstalledVersion)
	if err != nil {
		return fail(CodeManifestInvalid, "requirements.min_installed_version is not a valid version")
	}
	if Compare(installed, min) < 0 {
		return fail(CodeRequirementsNotMet, "the installed version is older than requirements.min_installed_version")
	}
	return nil
}

// CheckHelperProtocol applies requirements.helper_protocol: the release needs a
// helper that implements at least that protocol, and the helper that exists
// after the transaction is the one the bundle delivers (components.protocol) or,
// for a bundle without components, the installed one (installedProtocol, the
// highest protocol it implements). A release that needs more gives
// CodeHelperTooOld.
func CheckHelperProtocol(m *Manifest, installedProtocol int) error {
	if m == nil {
		return fail(CodeInternal, "no manifest")
	}
	need := m.Requirements.HelperProtocol
	if need == 0 {
		return nil
	}
	have := installedProtocol
	if m.Components != nil {
		have = m.Components.Protocol
	}
	if need > have {
		return fail(CodeHelperTooOld, "the release needs a newer helper protocol than the helper will have")
	}
	return nil
}

// CheckUpgrade applies the version rules to a verified manifest, in this order:
// the candidate (the manifest's version) is strictly newer than the installed
// version (CheckNewer); the installed version meets min_installed_version; the
// helper after the transaction implements the protocol the release needs. The
// prerelease eligibility of Q7 (Eligible) is a rule for choosing what to offer
// and is not repeated here: a candidate the administrator was offered and
// requested explicitly is checked on its own merits, and the manifest's
// prerelease field was already tied to the version.
func CheckUpgrade(installed Version, m *Manifest, installedHelperProtocol int) error {
	if m == nil {
		return fail(CodeInternal, "no manifest")
	}
	cand, err := m.Semver()
	if err != nil {
		return fail(CodeManifestInvalid, "the manifest version is not valid")
	}
	if err := CheckNewer(installed, cand); err != nil {
		return err
	}
	if err := CheckMinInstalled(installed, m); err != nil {
		return err
	}
	return CheckHelperProtocol(m, installedHelperProtocol)
}
