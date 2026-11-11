package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// testFS wraps an fsops, records every mutating call, and can fail or stop at
// the k-th one. It is the recording implementation and the failing
// implementation of ADR 0010's crash tests, and the harness G6.6 reuses.
//
//   - failAt k: the k-th call returns errInjected and does nothing; every other
//     call runs (an error at one point).
//   - stopAt k: the k-th call and every later one return errStopped and do
//     nothing: the file system executes nothing further, which is what a power
//     loss looks like to the program.
//
// Counted calls: Mkdir, Chmod, Create, Rename, RemoveAll, SyncDir, and Write
// and Sync of a file.
type testFS struct {
	inner fsops

	mu      sync.Mutex
	ops     []fsOp
	count   int
	failAt  int
	stopAt  int
	stopped bool
	// before runs before each counted call that would otherwise run, outside
	// the lock; tests use it to pause a request at a chosen point or to fail one
	// call by predicate (a non-nil result fails the call and does nothing).
	before func(op fsOp) error
}

type fsOp struct {
	Kind  string
	Tree  tree
	Path  string
	Path2 string // the target of a rename
}

func (o fsOp) String() string { return fmt.Sprintf("%s %s:%s %s", o.Kind, o.Tree, o.Path, o.Path2) }

var (
	errInjected = errors.New("injected failure")
	errStopped  = errors.New("file system stopped")
)

func newTestFS(inner fsops) *testFS { return &testFS{inner: inner} }

// step records the call and says whether it runs.
func (f *testFS) step(op fsOp) error {
	f.mu.Lock()
	f.count++
	f.ops = append(f.ops, op)
	var err error
	switch {
	case f.stopped, f.stopAt > 0 && f.count >= f.stopAt:
		f.stopped = true
		err = errStopped
	case f.failAt > 0 && f.count == f.failAt:
		err = errInjected
	}
	hook := f.before
	f.mu.Unlock()
	if err == nil && hook != nil {
		err = hook(op)
	}
	return err
}

func (f *testFS) Ops() []fsOp {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fsOp(nil), f.ops...)
}

func (f *testFS) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.count
}

func (f *testFS) Mkdir(t tree, name string, perm os.FileMode) error {
	if err := f.step(fsOp{Kind: "mkdir", Tree: t, Path: name}); err != nil {
		return err
	}
	return f.inner.Mkdir(t, name, perm)
}

func (f *testFS) Chmod(t tree, name string, perm os.FileMode) error {
	if err := f.step(fsOp{Kind: "chmod", Tree: t, Path: name}); err != nil {
		return err
	}
	return f.inner.Chmod(t, name, perm)
}

func (f *testFS) Create(t tree, name string, perm os.FileMode) (wfile, error) {
	if err := f.step(fsOp{Kind: "create", Tree: t, Path: name}); err != nil {
		return nil, err
	}
	w, err := f.inner.Create(t, name, perm)
	if err != nil {
		return nil, err
	}
	return &testFile{fs: f, w: w, t: t, name: name}, nil
}

func (f *testFS) Symlink(t tree, target, name string) error {
	if err := f.step(fsOp{Kind: "symlink", Tree: t, Path: name, Path2: target}); err != nil {
		return err
	}
	return f.inner.Symlink(t, target, name)
}

func (f *testFS) Rename(t tree, oldname, newname string) error {
	if err := f.step(fsOp{Kind: "rename", Tree: t, Path: oldname, Path2: newname}); err != nil {
		return err
	}
	return f.inner.Rename(t, oldname, newname)
}

func (f *testFS) RemoveAll(t tree, name string) error {
	if err := f.step(fsOp{Kind: "removeall", Tree: t, Path: name}); err != nil {
		return err
	}
	return f.inner.RemoveAll(t, name)
}

func (f *testFS) SyncDir(t tree, name string) error {
	if err := f.step(fsOp{Kind: "syncdir", Tree: t, Path: name}); err != nil {
		return err
	}
	return f.inner.SyncDir(t, name)
}

type testFile struct {
	fs   *testFS
	w    wfile
	t    tree
	name string
}

func (f *testFile) Write(p []byte) (int, error) {
	if err := f.fs.step(fsOp{Kind: "write", Tree: f.t, Path: f.name}); err != nil {
		return 0, err
	}
	return f.w.Write(p)
}

func (f *testFile) Sync() error {
	if err := f.fs.step(fsOp{Kind: "sync", Tree: f.t, Path: f.name}); err != nil {
		return err
	}
	return f.w.Sync()
}

// Close always runs: a stopped file system leaks no descriptor in the test.
func (f *testFile) Close() error { return f.w.Close() }

// ---- the closed write-target table of ADR 0010 U2 -------------------------

// allowedTarget reports whether a mutated path is in the closed table of U2:
// below /opt/oarfish releases/<version>, releases/.stage.<id>,
// releases/.trash.<id>, current and .current.new.<id>, components/<id>,
// components/active, components/known-good and components/.new.<id>, and the
// state directory.
func allowedTarget(t tree, p string) bool {
	if t == treeState {
		return p == "." || p == stateFile || strings.HasPrefix(p, stateTempPrefix)
	}
	if p == "releases" || p == "." || p == "current" || p == "components" {
		return true // fsync of a directory; the current link
	}
	if rest, ok := strings.CutPrefix(p, "components/"); ok {
		// G6.10: a component set, the two links and their temporaries.
		if id, isTemp := strings.CutPrefix(rest, componentTempPrefix); isTemp {
			return isHex(16)(id)
		}
		return rest == linkActive || rest == linkKnownGood || validVersion(rest)
	}
	if id, ok := strings.CutPrefix(p, tempLinkPrefix); ok {
		return isHex(16)(id)
	}
	rest, ok := strings.CutPrefix(p, "releases/")
	if !ok {
		return false
	}
	first, _, _ := strings.Cut(rest, "/")
	switch {
	case strings.HasPrefix(first, ".stage."):
		return isHex(16)(strings.TrimPrefix(first, ".stage."))
	case strings.HasPrefix(first, ".trash."):
		return isHex(16)(strings.TrimPrefix(first, ".trash."))
	}
	return validVersion(first) && first == rest
}

// noSyncFS forwards everything except the flushes, which it skips. The crash
// sweep runs hundreds of requests; the order of the flushes is checked by
// TestDurabilityOrder and the sweep only needs the effect of the other calls.
type noSyncFS struct{ fsops }

func (n noSyncFS) SyncDir(tree, string) error { return nil }

func (n noSyncFS) Create(t tree, name string, perm os.FileMode) (wfile, error) {
	w, err := n.fsops.Create(t, name, perm)
	if err != nil {
		return nil, err
	}
	return noSyncFile{w}, nil
}

type noSyncFile struct{ wfile }

func (noSyncFile) Sync() error { return nil }
