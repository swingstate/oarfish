package auth

import (
	"context"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swingstate/oarfish/backend/internal/clock/clocktest"
)

func mode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func openManager(t *testing.T, dir string, h *Hasher) *Manager {
	t.Helper()
	m, err := Open(Config{StateDir: dir, Clock: clocktest.New(), Hasher: h})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func readToken(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(SetupTokenPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

// ORB-SEC-005, E4: auth/ is 0700, account.json 0600, written atomically; the
// file holds the user name, the PHC hash and the creation time, never the
// password.
func TestAccountFileModesAndContent(t *testing.T) {
	dir := t.TempDir()
	m := openManager(t, dir, NewHasherWithParams(cheap))
	if mode(t, filepath.Join(dir, AuthDirName)) != 0o700 {
		t.Errorf("auth dir mode %o", mode(t, filepath.Join(dir, AuthDirName)))
	}
	if _, _, err := m.Setup(context.Background(), readToken(t, dir), "admin-test", sentinelPassword); err != nil {
		t.Fatal(err)
	}
	if mode(t, AccountPath(dir)) != 0o600 {
		t.Errorf("account.json mode %o", mode(t, AccountPath(dir)))
	}
	data, _ := os.ReadFile(AccountPath(dir))
	text := string(data)
	for _, want := range []string{`"username": "admin-test"`, `"password_hash": "$argon2id$v=19$`, `"created_at": "2026-10-28T12:00:00Z"`, `"schema_version": 1`} {
		if !strings.Contains(text, want) {
			t.Errorf("account.json lacks %s:\n%s", want, text)
		}
	}
	if strings.Contains(text, sentinelPassword) {
		t.Fatal("account.json contains the password")
	}
	entries, _ := os.ReadDir(filepath.Join(dir, AuthDirName))
	if len(entries) != 1 {
		t.Errorf("auth/ holds %d entries, want only account.json (no temporary file left)", len(entries))
	}
}

// ORB-SEC-005: a directory or file of the service user that is too open is
// tightened at start. (A file of another user makes Open fail; that needs a
// second uid and is tested in package securefile with a stub.)
func TestStartTightensTooOpenModes(t *testing.T) {
	dir := t.TempDir()
	m := openManager(t, dir, NewHasherWithParams(cheap))
	if _, _, err := m.Setup(context.Background(), readToken(t, dir), "admin-test", sentinelPassword); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(filepath.Join(dir, AuthDirName), 0o755)
	_ = os.Chmod(AccountPath(dir), 0o644)
	openManager(t, dir, NewHasherWithParams(cheap))
	if mode(t, filepath.Join(dir, AuthDirName)) != 0o700 || mode(t, AccountPath(dir)) != 0o600 {
		t.Errorf("modes after start: dir %o, file %o", mode(t, filepath.Join(dir, AuthDirName)), mode(t, AccountPath(dir)))
	}
}

// E4: an account file that exists but cannot be used stops the start with an
// error naming the file and the recovery. It never falls back to setup mode.
func TestCorruptAccountFileRefusesToStart(t *testing.T) {
	valid := `{"schema_version":1,"username":"admin-test","password_hash":"$argon2id$v=19$m=19456,t=2,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","created_at":"2026-10-28T12:00:00Z"}`
	cases := map[string]string{
		"empty":           "",
		"not JSON":        "this is not json",
		"unknown field":   strings.Replace(valid, `"schema_version":1`, `"schema_version":1,"role":"x"`, 1),
		"trailing data":   valid + `{}`,
		"schema 2":        strings.Replace(valid, `"schema_version":1`, `"schema_version":2`, 1),
		"bad username":    strings.Replace(valid, "admin-test", "Admin Test", 1),
		"bad hash":        strings.Replace(valid, "m=19456", "m=999999", 1),
		"no created_at":   strings.Replace(valid, `,"created_at":"2026-10-28T12:00:00Z"`, "", 1),
		"too large":       valid + strings.Repeat(" ", 5000),
		"truncated write": valid[:40],
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			_ = os.Mkdir(filepath.Join(dir, AuthDirName), 0o700)
			if err := os.WriteFile(AccountPath(dir), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Open(Config{StateDir: dir, Hasher: NewHasherWithParams(cheap)})
			if err == nil {
				t.Fatal("started with a broken account file")
			}
			for _, want := range []string{AccountPath(dir), "remove", "docs/AUTH.md"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
			if strings.Contains(err.Error(), "AAAAAAAAAAAAAAAA") {
				t.Errorf("the error repeats file content: %q", err)
			}
			if _, statErr := os.Stat(SetupTokenPath(dir)); statErr == nil {
				t.Error("a setup token was created although an account file exists")
			}
		})
	}
	t.Run("not a regular file", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.MkdirAll(AccountPath(dir), 0o700)
		if _, err := Open(Config{StateDir: dir}); err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("err = %v", err)
		}
	})
}

// The state directory must exist and be writable; there is no fallback.
func TestMissingOrReadOnlyStateDirectoryRefusesToStart(t *testing.T) {
	if _, err := Open(Config{StateDir: ""}); err == nil || !strings.Contains(err.Error(), "STATE_DIRECTORY") {
		t.Errorf("no state dir: %v", err)
	}
	missing := filepath.Join(t.TempDir(), "absent")
	if _, err := Open(Config{StateDir: missing}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("missing state dir: %v", err)
	}
	if os.Getuid() == 0 {
		t.Skip("root can write everywhere")
	}
	ro := t.TempDir()
	_ = os.Chmod(ro, 0o500)
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
	if _, err := Open(Config{StateDir: ro}); err == nil || !strings.Contains(err.Error(), "not writable") {
		t.Errorf("read-only state dir: %v", err)
	}
}

// ORB-ONB-002, E5: while no account exists the service writes a fresh 128-bit
// token (0600) at every start; after setup the file is gone, and a stale file
// is removed at the next start.
func TestSetupTokenLifecycle(t *testing.T) {
	dir := t.TempDir()
	m := openManager(t, dir, NewHasherWithParams(cheap))
	if !m.SetupRequired() {
		t.Fatal("a fresh state directory must be in setup mode")
	}
	first := readToken(t, dir)
	if raw, err := hex.DecodeString(first); err != nil || len(raw) != 16 {
		t.Fatalf("token %q is not 128 bits in hex", first)
	}
	if mode(t, SetupTokenPath(dir)) != 0o600 {
		t.Errorf("token file mode %o", mode(t, SetupTokenPath(dir)))
	}

	m = openManager(t, dir, NewHasherWithParams(cheap)) // restart, still no account
	second := readToken(t, dir)
	if second == first {
		t.Fatal("the token was not regenerated at the restart")
	}
	if _, _, err := m.Setup(context.Background(), first, "admin-test", sentinelPassword); err != ErrInvalidSetupToken {
		t.Fatalf("the old token still works: %v", err)
	}
	if _, _, err := m.Setup(context.Background(), " "+strings.ToUpper(second)+"\n", "admin-test", sentinelPassword); err != nil {
		t.Fatalf("setup with the token as copied by hand: %v", err)
	}
	if _, err := os.Stat(SetupTokenPath(dir)); !os.IsNotExist(err) {
		t.Fatal("the token file survived the setup")
	}
	if _, _, err := m.Setup(context.Background(), second, "admin-test", sentinelPassword); err != ErrSetupDone {
		t.Fatalf("a second setup: %v", err)
	}

	_ = os.WriteFile(SetupTokenPath(dir), []byte("stale\n"), 0o600)
	m = openManager(t, dir, NewHasherWithParams(cheap))
	if m.SetupRequired() {
		t.Fatal("setup mode with an account")
	}
	if _, err := os.Stat(SetupTokenPath(dir)); !os.IsNotExist(err) {
		t.Fatal("a stale token file was kept although an account exists")
	}
}
