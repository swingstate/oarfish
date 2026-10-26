// Package platformtest helps tests of the detection packages: it opens the
// synthetic fixture trees under internal/platform/testdata as file sources and
// records which files a detector reads. It is only imported by tests.
package platformtest

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"testing/fstest"
)

// Dir returns the absolute path of the testdata directory.
func Dir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "testdata")
}

// Fixture returns the named fixture as a read-only file source.
func Fixture(t testing.TB, name string) fs.FS {
	t.Helper()
	dir := filepath.Join(Dir(), name)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("fixture %q not found at %s", name, dir)
	}
	return os.DirFS(dir)
}

// Clone copies the named fixture into memory so a test can change it.
func Clone(t testing.TB, name string) fstest.MapFS {
	t.Helper()
	src := Fixture(t, name)
	m := fstest.MapFS{}
	err := fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(src, path)
		if err != nil {
			return err
		}
		m[path] = &fstest.MapFile{Data: data}
		return nil
	})
	if err != nil {
		t.Fatalf("clone fixture %q: %v", name, err)
	}
	return m
}

// RecordingFS wraps a file source and remembers every name that was opened,
// listed or read as a link.
type RecordingFS struct {
	inner fs.FS
	mu    sync.Mutex
	seen  []string
}

// Recording wraps fsys.
func Recording(fsys fs.FS) *RecordingFS { return &RecordingFS{inner: fsys} }

func (r *RecordingFS) note(name string) {
	r.mu.Lock()
	r.seen = append(r.seen, name)
	r.mu.Unlock()
}

// Open implements fs.FS.
func (r *RecordingFS) Open(name string) (fs.File, error) {
	r.note(name)
	return r.inner.Open(name)
}

// ReadLink implements fs.ReadLinkFS.
func (r *RecordingFS) ReadLink(name string) (string, error) {
	r.note(name)
	return fs.ReadLink(r.inner, name)
}

// Lstat implements fs.ReadLinkFS.
func (r *RecordingFS) Lstat(name string) (fs.FileInfo, error) {
	r.note(name)
	return fs.Lstat(r.inner, name)
}

// Names returns every name accessed so far, in order, with duplicates.
func (r *RecordingFS) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...)
}
