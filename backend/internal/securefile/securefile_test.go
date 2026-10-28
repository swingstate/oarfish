package securefile

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// foreignInfo is a FileInfo whose owner is another uid; creating a real one
// needs root.
type foreignInfo struct {
	fs.FileInfo
	uid uint32
}

func (f foreignInfo) Sys() any {
	st := *f.FileInfo.Sys().(*syscall.Stat_t)
	st.Uid = f.uid
	return &st
}

// ORB-SEC-005: a file or directory of another user is never used, whatever
// its mode, and the error says what to do.
func TestForeignOwnerIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Lstat(path)
	err := check(path, foreignInfo{fi, uint32(os.Getuid()) + 1}, false, FileMode)
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "chown") {
		t.Fatalf("err = %v", err)
	}
}

// ORB-SEC-005: too open modes of the own user are tightened, for files and
// directories, and a new directory is 0700 whatever the umask.
func TestModesAreTightenedAndCreatedRestrictive(t *testing.T) {
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	root := t.TempDir()
	dir := filepath.Join(root, "d")
	if err := EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	if m := perm(t, dir); m != 0o700 {
		t.Fatalf("new dir %o", m)
	}
	_ = os.Chmod(dir, 0o777)
	if err := EnsureDir(dir); err != nil || perm(t, dir) != 0o700 {
		t.Fatalf("dir not tightened: %v %o", err, perm(t, dir))
	}
	f := filepath.Join(dir, "f")
	_ = os.WriteFile(f, nil, 0o666)
	if exists, err := CheckFile(f); !exists || err != nil || perm(t, f) != 0o600 {
		t.Fatalf("file not tightened: %v %v %o", exists, err, perm(t, f))
	}
	if exists, err := CheckFile(filepath.Join(dir, "absent")); exists || err != nil {
		t.Fatalf("missing file: %v %v", exists, err)
	}
}

func TestSymlinksAreRefused(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	_ = os.WriteFile(target, nil, 0o600)
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	if _, err := CheckFile(link); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("file symlink: %v", err)
	}
	dirLink := filepath.Join(root, "dirlink")
	_ = os.Symlink(root, dirLink)
	if err := EnsureDir(dirLink); err == nil {
		t.Fatal("a directory symlink was accepted")
	}
}

// ORB-NFR-013: an atomic write replaces the file completely and leaves no
// temporary file; a failed write keeps the old content.
func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "account.json")
	for _, content := range []string{"first\n", "second\n"} {
		if err := WriteAtomic(path, []byte(content)); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(path); string(b) != content || perm(t, path) != 0o600 {
			t.Fatalf("content %q mode %o", b, perm(t, path))
		}
	}
	// The rename cannot replace a non-empty directory: the write fails, the
	// target stays as it was and the temporary file is gone.
	blocked := filepath.Join(dir, "blocked")
	_ = os.MkdirAll(filepath.Join(blocked, "x"), 0o700)
	if err := WriteAtomic(blocked, []byte("new")); err == nil {
		t.Fatal("the write over a directory succeeded")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file left: %s", e.Name())
		}
	}
}

func TestCheckWritableDir(t *testing.T) {
	dir := t.TempDir()
	if err := CheckWritableDir(dir); err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	if fi, _ := os.Stat(dir); fi.ModTime().After(before) {
		t.Error("the check wrote into the directory")
	}
	f := filepath.Join(dir, "file")
	_ = os.WriteFile(f, nil, 0o600)
	if err := CheckWritableDir(f); err == nil {
		t.Error("a file was accepted as state directory")
	}
}

func perm(t *testing.T, p string) fs.FileMode {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}
