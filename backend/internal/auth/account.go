package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/swingstate/oarfish/backend/internal/securefile"
)

// File layout under the state directory (ADR 0009, E4, E5; D-C47).
const (
	AuthDirName       = "auth"
	AccountFileName   = "account.json"
	SetupTokenName    = "setup-token"
	accountSchema     = 1
	maxAccountFileLen = 4 << 10
)

// Account is the one local administrator.
type Account struct {
	SchemaVersion int       `json:"schema_version"`
	Username      string    `json:"username"`
	PasswordHash  string    `json:"password_hash"`
	CreatedAt     time.Time `json:"created_at"`
}

// AccountStore reads and writes auth/account.json.
type AccountStore struct {
	path string
}

// AccountPath is the account file below a state directory.
func AccountPath(stateDir string) string {
	return filepath.Join(stateDir, AuthDirName, AccountFileName)
}

// RecoveryHint is the documented way back from a lost password or a broken
// account file. It names no secret.
func RecoveryHint(stateDir string) string {
	return fmt.Sprintf("to set up a new administrator: stop the service, remove %s as root, start the service (docs/AUTH.md, \"Lost password\")", AccountPath(stateDir))
}

// OpenAccountStore prepares auth/ (0700) and loads the account if there is
// one. A missing file means setup mode. A file that exists but cannot be read
// or does not validate is an error: the service must not fall back to setup
// mode by itself, because that would let whoever reads the new setup token
// take over the device.
func OpenAccountStore(stateDir string) (*AccountStore, *Account, error) {
	dir := filepath.Join(stateDir, AuthDirName)
	if err := securefile.EnsureDir(dir); err != nil {
		return nil, nil, err
	}
	s := &AccountStore{path: filepath.Join(dir, AccountFileName)}
	removeStaleTemps(dir, AccountFileName)
	exists, err := securefile.CheckFile(s.path)
	if err != nil {
		return nil, nil, err
	}
	if !exists {
		return s, nil, nil
	}
	acc, err := readAccount(s.path)
	if err != nil {
		return nil, nil, fmt.Errorf("the account file %s is unreadable or corrupt (%v); Oarfish does not start in this state; %s", s.path, err, RecoveryHint(stateDir))
	}
	return s, acc, nil
}

func readAccount(path string) (*Account, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxAccountFileLen+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxAccountFileLen {
		return nil, errors.New("file too large")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var acc Account
	if err := dec.Decode(&acc); err != nil {
		return nil, errors.New("not valid JSON")
	}
	if dec.More() {
		return nil, errors.New("trailing data")
	}
	if acc.SchemaVersion != accountSchema {
		return nil, fmt.Errorf("unknown schema_version %d", acc.SchemaVersion)
	}
	if ValidateUsername(acc.Username) != nil {
		return nil, errors.New("invalid username")
	}
	if _, err := parsePHC(acc.PasswordHash); err != nil {
		return nil, errors.New("invalid password hash")
	}
	if acc.CreatedAt.IsZero() {
		return nil, errors.New("missing created_at")
	}
	return &acc, nil
}

// Save replaces the account file atomically.
func (s *AccountStore) Save(acc Account) error {
	acc.SchemaVersion = accountSchema
	data, err := json.MarshalIndent(acc, "", "  ")
	if err != nil {
		return err
	}
	return securefile.WriteAtomic(s.path, append(data, '\n'))
}

// removeStaleTemps deletes temporary files an interrupted atomic write left.
func removeStaleTemps(dir, base string) {
	matches, _ := filepath.Glob(filepath.Join(dir, "."+base+".*.tmp"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}
