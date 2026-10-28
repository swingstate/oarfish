package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/swingstate/oarfish/backend/internal/securefile"
)

// setupTokenBytes is 128 bits (ADR 0009, E5).
const setupTokenBytes = 16

// SetupToken is the one-time secret that proves access to the device during
// first start. It exists only while there is no account. Its value is in this
// struct and in the 0600 file; it is never logged.
type SetupToken struct {
	path  string
	value string // lower-case hex
}

// SetupTokenPath is the token file below a state directory.
func SetupTokenPath(stateDir string) string { return filepath.Join(stateDir, SetupTokenName) }

// NewSetupToken creates a fresh token and writes it to the token file,
// replacing an old one. It is called at every start while no account exists.
func NewSetupToken(stateDir string) (*SetupToken, error) {
	path := SetupTokenPath(stateDir)
	if _, err := securefile.CheckFile(path); err != nil {
		return nil, err
	}
	removeStaleTemps(stateDir, SetupTokenName)
	raw := make([]byte, setupTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	t := &SetupToken{path: path, value: hex.EncodeToString(raw)}
	if err := securefile.WriteAtomic(path, []byte(t.value+"\n")); err != nil {
		return nil, err
	}
	return t, nil
}

// RemoveSetupToken deletes a stale token file (an account exists).
func RemoveSetupToken(stateDir string) error {
	path := SetupTokenPath(stateDir)
	if _, err := securefile.CheckFile(path); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Matches compares a candidate in constant time. Surrounding white space and
// upper-case hex digits are accepted, since the value is copied by hand.
func (t *SetupToken) Matches(candidate string) bool {
	c := strings.ToLower(strings.TrimSpace(candidate))
	return subtle.ConstantTimeCompare([]byte(c), []byte(t.value)) == 1
}

// Remove deletes the token file after a successful setup.
func (t *SetupToken) Remove() error {
	if err := os.Remove(t.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return securefile.SyncDir(filepath.Dir(t.path))
}
