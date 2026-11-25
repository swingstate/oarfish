// Package unitname is rule 1 of ADR 0011 B3, the syntax of a systemd service
// name, for the service process: the read path (G7.4), the broker client, the
// action route and the audit target pattern (ADR 0009 A7). The broker has its
// own copy in backend/cmd/oarfish-broker/unit.go, because it shares no code
// with the service (ADR 0011 B1); a test of this package reads that file and
// fails when the two patterns differ.
package unitname

import (
	"regexp"
	"strings"
)

// MaxLength is the longest name rule 1 allows, in bytes.
const MaxLength = 255

// Pattern is the pattern of rule 1 exactly as the broker builds it (ADR 0011
// A4.4 item 1): the instance of a template may be empty, so that the broker
// answers unit_template for `name@.service`. A backslash is checked by
// validEscapes.
const Pattern = `^[A-Za-z0-9][A-Za-z0-9:_.-]*(@[A-Za-z0-9:_.\\-]*)?\.service$`

var pattern = regexp.MustCompile(Pattern)

// Valid is rule 1 of B3: ASCII only, 1 to 255 bytes, the pattern, no `..`, and
// a backslash only as `\x` followed by two lowercase hexadecimal digits. The
// pattern excludes `/`, a space, a leading `-` and every type but `.service`.
func Valid(s string) bool {
	if len(s) < 1 || len(s) > MaxLength {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return !strings.Contains(s, "..") && pattern.MatchString(s) && validEscapes(s)
}

func validEscapes(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			continue
		}
		if i+3 >= len(s) || s[i+1] != 'x' || !lowerHex(s[i+2]) || !lowerHex(s[i+3]) {
			return false
		}
		i += 3
	}
	return true
}

func lowerHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' }

// IsTemplate reports whether a valid name is a template without an instance,
// `name@.service`. Such a name is never a row of the list and never offered.
func IsTemplate(s string) bool { return strings.HasSuffix(s, "@.service") }

// Template returns the template of an instance (`getty@tty1.service` gives
// `getty@.service`) and false for a name that is not an instance.
func Template(s string) (string, bool) {
	at := strings.IndexByte(s, '@')
	if at < 0 || IsTemplate(s) {
		return "", false
	}
	return s[:at+1] + ".service", true
}
