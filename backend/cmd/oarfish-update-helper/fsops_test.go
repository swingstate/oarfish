package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// The production implementation of fsops, on its own.
func TestRealFS(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	dir := shortDir(t)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f := realFS{roots: map[tree]*os.Root{treeOpt: root}}

	if err := f.Mkdir(treeOpt, "d", 0o755); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "d")); fi.Mode().Perm() != 0o700 {
		t.Fatalf("mkdir under umask 0077: %o (the caller must chmod)", fi.Mode().Perm())
	}
	if err := f.Chmod(treeOpt, "d", 0o755); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "d")); fi.Mode().Perm() != 0o755 {
		t.Fatalf("chmod: %o", fi.Mode().Perm())
	}

	for _, mode := range []os.FileMode{0o644, 0o755, 0o600} {
		name := "d/f" + mode.String()
		w, err := f.Create(treeOpt, name, mode)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte("x"))
		if err := w.Sync(); err != nil {
			t.Fatal(err)
		}
		w.Close()
		if fi, _ := os.Stat(filepath.Join(dir, name)); fi.Mode().Perm() != mode {
			t.Fatalf("create with %o under umask 0077: %o", mode, fi.Mode().Perm())
		}
	}
	// O_EXCL: an existing file is never opened for writing, and a link is never followed.
	if _, err := f.Create(treeOpt, "d/f"+os.FileMode(0o644).String(), 0o644); err == nil {
		t.Fatal("Create replaced an existing file")
	}
	os.Symlink("target", filepath.Join(dir, "d", "link"))
	if _, err := f.Create(treeOpt, "d/link", 0o644); err == nil {
		t.Fatal("Create followed a link")
	}
	if _, err := os.Stat(filepath.Join(dir, "d", "target")); err == nil {
		t.Fatal("Create wrote through a dangling link")
	}
	// A name can not leave the tree.
	for _, name := range []string{"../x", "d/../../x", "/etc/x"} {
		if _, err := f.Create(treeOpt, name, 0o644); err == nil {
			t.Fatalf("created %q", name)
		}
		if err := f.Mkdir(treeOpt, name, 0o755); err == nil {
			t.Fatalf("mkdir %q", name)
		}
		if err := f.RemoveAll(treeOpt, name); err == nil && name != "/etc/x" {
			t.Fatalf("removeall %q", name)
		}
	}
	if err := f.Rename(treeOpt, "d", "e"); err != nil {
		t.Fatal(err)
	}
	if err := f.SyncDir(treeOpt, "e"); err != nil {
		t.Fatal(err)
	}
	if err := f.SyncDir(treeOpt, "."); err != nil {
		t.Fatal(err)
	}
	if err := f.RemoveAll(treeOpt, "e"); err != nil {
		t.Fatal(err)
	}
	if err := f.RemoveAll(treeOpt, "does-not-exist"); err != nil {
		t.Fatal("removing a missing path is success")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("left %d entries", len(entries))
	}
}
