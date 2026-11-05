package main

import (
	"os"
	"syscall"
)

// fsops is the one place where the helper changes the file system (ADR 0010
// U2, closed write targets). Every mutating call of stage, discard, the state
// file and the cleanup goes through this interface; no other non-test file of
// the helper calls a mutating function of package os (TestNoMutationOutsideFsops
// scans the sources). The tests replace it with a recording and a failing
// implementation to prove the write-target table and the crash behaviour.
//
// Names are relative to one of two trees, never absolute: treeOpt is
// /opt/oarfish (releases, the current link and, from G6.10, the component sets) and
// treeState is /var/lib/oarfish-update. The production implementation uses
// os.Root for both, so a name can not leave its tree.
type fsops interface {
	// Mkdir creates a directory; the mode is the umask-reduced permission, so
	// callers follow it with Chmod where the mode matters (UMask=0077).
	Mkdir(t tree, name string, perm os.FileMode) error
	// Chmod sets the mode of a file or directory through an open descriptor
	// (fchmod), not through the name.
	Chmod(t tree, name string, perm os.FileMode) error
	// Create creates a new file (O_CREATE|O_EXCL|O_WRONLY), made 0600 and then
	// set to perm with fchmod, whatever the umask. O_EXCL refuses an existing
	// name, a link included.
	Create(t tree, name string, perm os.FileMode) (wfile, error)
	// Symlink creates a link whose text is target (a relative path). The
	// switch of `current` is a Symlink to a temporary name followed by a Rename
	// over the old link.
	Symlink(t tree, target, name string) error
	Rename(t tree, oldname, newname string) error
	RemoveAll(t tree, name string) error
	// SyncDir flushes a directory ("." is the tree itself).
	SyncDir(t tree, name string) error
}

// wfile is a file being written.
type wfile interface {
	Write(p []byte) (int, error)
	Sync() error
	Close() error
}

type tree string

const (
	treeOpt   tree = "opt"
	treeState tree = "state"
)

// realFS is the production fsops. The roots are opened by the caller and
// closed by the caller.
type realFS struct{ roots map[tree]*os.Root }

func (r realFS) Mkdir(t tree, name string, perm os.FileMode) error {
	return r.roots[t].Mkdir(name, perm)
}

// Chmod opens the name and calls fchmod on the descriptor. os.Root.Chmod would
// do the same by name, but the standard library documents a race there (the
// target replaced by a link while the call runs); the descriptor has no such
// window. O_NOFOLLOW is passed but os.Root gives no no-follow on the last path
// element (a link that stays inside the root is followed, A10.1); the trees are
// root-owned, so no unprivileged process can place a link there.
func (r realFS) Chmod(t tree, name string, perm os.FileMode) error {
	f, err := r.roots[t].OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Chmod(perm)
}

func (r realFS) Create(t tree, name string, perm os.FileMode) (wfile, error) {
	f, err := r.roots[t].OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func (r realFS) Symlink(t tree, target, name string) error {
	return r.roots[t].Symlink(target, name)
}

func (r realFS) Rename(t tree, oldname, newname string) error {
	return r.roots[t].Rename(oldname, newname)
}

func (r realFS) RemoveAll(t tree, name string) error { return r.roots[t].RemoveAll(name) }

func (r realFS) SyncDir(t tree, name string) error {
	f, err := r.roots[t].Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
