package relverify

import "strings"

const (
	// maxVersionLen is the bound of the helper protocol's version argument
	// (ADR 0010 U2).
	maxVersionLen = 64
	// maxCandidates bounds the list Latest looks at.
	maxCandidates = 1024
)

// Version is a parsed version. The syntax is the one of valid_version in
// install/install.sh and of the version pattern of scripts/bundlegen:
//
//	^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$
//
// plus the two bounds the protocol puts on the argument (ADR 0010 U2): at most
// 64 characters and no "..". Build metadata ("+") is not part of the syntax.
// This is slightly wider than SemVer 2.0 in the prerelease part (the pattern
// allows empty identifiers and leading zeros there, which SemVer forbids);
// widening or narrowing it is a change of the installer and of bundlegen first,
// and version_test.go pins the equality with the installer.
//
// The zero Version is not valid; use ParseVersion.
type Version struct {
	s    string
	core [3]string
	pre  []string
}

// ParseVersion parses s. A failure is a *Error with CodeBadRequest.
func ParseVersion(s string) (Version, error) {
	bad := func() (Version, error) { return Version{}, fail(CodeBadRequest, "the version is not valid") }
	if s == "" || len(s) > maxVersionLen || strings.Contains(s, "..") {
		return bad()
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '.' || c == '-') {
			return bad()
		}
	}
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return bad()
	}
	v := Version{s: s}
	for i, p := range parts {
		if !isCanonicalNumber(p) {
			return bad()
		}
		v.core[i] = p
	}
	if hasPre {
		if pre == "" {
			return bad()
		}
		v.pre = strings.Split(pre, ".")
	}
	return v, nil
}

// String returns the version as parsed.
func (v Version) String() string { return v.s }

// Prerelease reports whether the version has a prerelease part.
func (v Version) Prerelease() bool { return v.pre != nil }

// Compare orders two versions by SemVer 2.0 precedence and returns -1, 0 or 1.
// The core parts compare as numbers (as digit strings, so nothing overflows).
// A release is newer than its own prereleases. Prerelease identifiers compare
// one by one: numeric ones as numbers and below alphanumeric ones, alphanumeric
// ones in ASCII order, and a shorter list below a longer one with the same
// start. Where the grammar goes beyond SemVer (leading zeros and empty
// identifiers in the prerelease part) the order is still total: numeric
// identifiers with the same value compare by length. Compare is zero exactly
// when the two version strings are equal.
func Compare(a, b Version) int {
	for i := range a.core {
		if c := compareNumbers(a.core[i], b.core[i]); c != 0 {
			return c
		}
	}
	switch {
	case a.pre == nil && b.pre == nil:
		return 0
	case a.pre == nil:
		return 1
	case b.pre == nil:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		if c := compareIdentifiers(a.pre[i], b.pre[i]); c != 0 {
			return c
		}
	}
	return compareInts(len(a.pre), len(b.pre))
}

// CheckNewer applies the strictly-newer rule of ADR 0010 U4: the candidate must
// be strictly newer than the installed version. Equal gives CodeAlreadyInstalled,
// older gives CodeDowngradeRefused.
func CheckNewer(installed, candidate Version) error {
	switch c := Compare(candidate, installed); {
	case c == 0:
		return fail(CodeAlreadyInstalled, "the candidate equals the installed version")
	case c < 0:
		return fail(CodeDowngradeRefused, "the candidate is older than the installed version")
	}
	return nil
}

// Eligible applies owner decision Q7 (ADR 0010 U4): a device whose installed
// version has a prerelease part is offered prereleases and stable releases; a
// device on a stable version is offered stable releases only. It says nothing
// about whether the candidate is newer.
func Eligible(installed, candidate Version) bool {
	return installed.Prerelease() || !candidate.Prerelease()
}

// Latest returns the highest candidate that is eligible (Eligible) and strictly
// newer than the installed version. It looks at no more than 1024 candidates and
// reports false for a longer list, because ignoring part of it would silently
// hide releases.
func Latest(installed Version, candidates []Version) (Version, bool) {
	if len(candidates) > maxCandidates {
		return Version{}, false
	}
	var best Version
	found := false
	for _, c := range candidates {
		if !Eligible(installed, c) || Compare(c, installed) <= 0 {
			continue
		}
		if !found || Compare(c, best) > 0 {
			best, found = c, true
		}
	}
	return best, found
}

func isCanonicalNumber(s string) bool {
	if s == "" || (len(s) > 1 && s[0] == '0') {
		return false
	}
	return isDigits(s)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// compareNumbers compares two digit strings by value, then by length (they
// differ only in leading zeros, which the core part cannot have).
func compareNumbers(a, b string) int {
	ta, tb := strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if c := compareInts(len(ta), len(tb)); c != 0 {
		return c
	}
	if c := strings.Compare(ta, tb); c != 0 {
		return c
	}
	return compareInts(len(a), len(b))
}

func compareIdentifiers(a, b string) int {
	an, bn := isDigits(a), isDigits(b)
	switch {
	case an && bn:
		return compareNumbers(a, b)
	case an:
		return -1
	case bn:
		return 1
	}
	return strings.Compare(a, b)
}

func compareInts(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
