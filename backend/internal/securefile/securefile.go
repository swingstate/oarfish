// Package securefile creates, checks and atomically replaces the files the
// service keeps under its state directory (account, setup token, audit). Every
// file is 0600 and every directory 0700 (ORB-SEC-005). A file or directory that
// is more open than that is tightened when the service user owns it; when
// another user owns it, the caller refuses to start, because the service could
// not keep its secrets to itself.
package securefile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// The only modes this package creates.
const (
	DirMode  fs.FileMode = 0o700
	FileMode fs.FileMode = 0o600
)

// owner returns the uid that owns fi.
func owner(fi fs.FileInfo) (uint32, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Uid, true
}

// check validates one existing path of the wanted kind and tightens its mode
// if the process owns it.
func check(path string, fi fs.FileInfo, wantDir bool, want fs.FileMode) error {
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("%s is a symbolic link; Oarfish only uses real files and directories here", path)
	case wantDir && !fi.IsDir():
		return fmt.Errorf("%s is not a directory", path)
	case !wantDir && !fi.Mode().IsRegular():
		return fmt.Errorf("%s is not a regular file", path)
	}
	uid, ok := owner(fi)
	if !ok {
		return fmt.Errorf("%s: cannot read the owner", path)
	}
	if uid != uint32(os.Getuid()) {
		return fmt.Errorf("%s belongs to uid %d, not to the service user (uid %d); fix the owner (chown) or remove it", path, uid, os.Getuid())
	}
	if fi.Mode().Perm() != want {
		if err := os.Chmod(path, want); err != nil {
			return fmt.Errorf("%s has mode %04o and could not be set to %04o: %w", path, fi.Mode().Perm(), want, err)
		}
	}
	return nil
}

// EnsureDir creates path as a 0700 directory, or checks (and tightens) an
// existing one. The parent must exist.
func EnsureDir(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(path, DirMode); err != nil {
			return fmt.Errorf("create %s: %w", path, err)
		}
		// Mkdir applies the umask; set the mode explicitly.
		return os.Chmod(path, DirMode)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return check(path, fi, true, DirMode)
}

// CheckFile checks (and tightens) an existing file. It reports whether the
// file exists; a missing file is not an error.
func CheckFile(path string) (bool, error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	return true, check(path, fi, false, FileMode)
}

// WriteAtomic replaces path with data: a temporary 0600 file in the same
// directory is written and synced, renamed over path, and the directory is
// synced, so an interruption leaves either the old or the new content
// (ORB-NFR-013).
func WriteAtomic(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err = tmp.Chmod(FileMode); err != nil {
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	return SyncDir(dir)
}

// SyncDir makes a rename or a removal in dir durable.
func SyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// CheckWritableDir verifies that dir exists, is a directory and that the
// process may write in it. It asks the kernel (access(2), which also sees a
// read-only mount) instead of writing a probe file, so a start costs no write.
func CheckWritableDir(dir string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("the state directory %s does not exist: %w", dir, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("the state directory %s is not a directory", dir)
	}
	const wOK, xOK = 0x2, 0x1
	if err := syscall.Access(dir, wOK|xOK); err != nil {
		return fmt.Errorf("the state directory %s is not writable: %w", dir, err)
	}
	return nil
}
