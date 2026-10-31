package auth

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func policyCode(err error) string {
	var p *PolicyError
	if errors.As(err, &p) {
		return p.Code
	}
	if err != nil {
		return "other: " + err.Error()
	}
	return ""
}

// E4: user names are 1 to 32 characters from [a-z0-9._-].
func TestUsernamePolicy(t *testing.T) {
	for _, ok := range []string{"a", "admin", "admin-test", "a.b_c-d", "0", strings.Repeat("a", 32)} {
		if err := ValidateUsername(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", strings.Repeat("a", 33), "Admin", "ad min", "admin!", "ädmin", "admin\n", "admin@host", "../x", "a/b"} {
		if code := policyCode(ValidateUsername(bad)); code != CodeUsernameInvalid {
			t.Errorf("%q: code %q", bad, code)
		}
	}
}

// E4: passwords are 12 to 128 Unicode code points, with no composition rules,
// and a short list of common passwords is refused regardless of case.
func TestPasswordPolicy(t *testing.T) {
	cases := map[string]string{
		"riemenfisch":                     CodePasswordTooShort, // 11
		"riemenfischxy":                   "",                   // 13, letters only is fine
		strings.Repeat("x", 128):          "",
		strings.Repeat("x", 129):          CodePasswordTooLong,
		"äöüßäöüßäöüß":                    "",                   // 12 code points, 24 bytes
		strings.Repeat("🐟", 11):           CodePasswordTooShort, // 44 bytes, 11 code points
		strings.Repeat("🐟", 128):          "",                   // 512 bytes
		"password1234":                    CodePasswordCommon,
		"PASSWORD1234":                    CodePasswordCommon,
		"Raspberrypi123":                  CodePasswordCommon,
		"qwertzuiopasdf":                  CodePasswordCommon,
		"123456789012":                    CodePasswordCommon,
		sentinelPassword:                  "",
		"valid but \x00 has a NUL":        CodePasswordInvalid,
		"invalid utf-8 \xff\xfe 12345678": CodePasswordInvalid,
	}
	for pw, want := range cases {
		if got := policyCode(ValidatePassword(pw)); got != want {
			t.Errorf("%q (%d code points): code %q, want %q", pw, utf8.RuneCountInString(pw), got, want)
		}
	}
}

// The embedded list is small, lower case, without duplicates, and every
// entry would pass the length rule (shorter ones are refused anyway).
func TestCommonPasswordListIsSmallAndWellFormed(t *testing.T) {
	lines := strings.Split(strings.TrimSuffix(commonPasswordsText, "\n"), "\n")
	if len(lines) < 100 || len(lines) > 1000 {
		t.Fatalf("%d entries; keep the list at a few hundred", len(lines))
	}
	seen := map[string]bool{}
	for _, l := range lines {
		n := utf8.RuneCountInString(l)
		if l != strings.ToLower(l) || n < MinPasswordLen || n > MaxPasswordLen || seen[l] || strings.TrimSpace(l) != l {
			t.Errorf("bad entry %q", l)
		}
		seen[l] = true
	}
	if len(commonPasswords) != len(lines) {
		t.Errorf("map has %d entries, file %d", len(commonPasswords), len(lines))
	}
}
