package auth

import (
	_ "embed"
	"strings"
	"unicode/utf8"
)

// Account rules (ADR 0009, E4).
const (
	MinPasswordLen = 12  // Unicode code points
	MaxPasswordLen = 128 // Unicode code points
	MaxUsernameLen = 32
)

// PolicyError says which rule a user name or password breaks. Code is a stable
// API error code; the UI turns it into a sentence.
type PolicyError struct{ Code string }

func (e *PolicyError) Error() string { return "policy: " + e.Code }

// The policy error codes.
const (
	CodeUsernameInvalid  = "username_invalid"
	CodePasswordTooShort = "password_too_short"
	CodePasswordTooLong  = "password_too_long"
	CodePasswordCommon   = "password_too_common"
	CodePasswordInvalid  = "password_invalid"
)

// ValidateUsername accepts 1 to 32 characters from [a-z0-9._-].
func ValidateUsername(name string) error {
	if name == "" || len(name) > MaxUsernameLen {
		return &PolicyError{CodeUsernameInvalid}
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return &PolicyError{CodeUsernameInvalid}
		}
	}
	return nil
}

// commonPasswords is a short list of passwords that are long enough for the
// length rule but too predictable: keyboard rows, digit runs, "password" in
// English and German, and default words of devices and distributions with
// common suffixes. It was written for this project (docs/AUTH.md); every entry
// is lower case and 12 to 128 characters long.
//
//go:embed common_passwords.txt
var commonPasswordsText string

var commonPasswords = func() map[string]struct{} {
	m := map[string]struct{}{}
	for _, line := range strings.Split(commonPasswordsText, "\n") {
		if line != "" {
			m[line] = struct{}{}
		}
	}
	return m
}()

// ValidatePassword checks the length in code points (12 to 128, no composition
// rules) and refuses the common passwords, compared without case.
func ValidatePassword(password string) error {
	if !utf8.ValidString(password) || strings.ContainsRune(password, 0) {
		return &PolicyError{CodePasswordInvalid}
	}
	n := utf8.RuneCountInString(password)
	switch {
	case n < MinPasswordLen:
		return &PolicyError{CodePasswordTooShort}
	case n > MaxPasswordLen:
		return &PolicyError{CodePasswordTooLong}
	}
	if _, common := commonPasswords[strings.ToLower(password)]; common {
		return &PolicyError{CodePasswordCommon}
	}
	return nil
}
