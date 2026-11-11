package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The harness of the component transaction (ADR 0010 U15 rule 4). The
// transaction crosses a process boundary: the helper that receives `activate`
// ends with exit code 75 and systemd starts the binary that components/active
// names. `supervisor` plays that part of systemd with the fake root: it starts
// a new handler whose own executable image is the helper file that the link
// names at that instant, runs the recover unit from known-good when a start
// fails, and counts the restarts against the start limit of the helper service
// (StartLimitBurst=3 in 10 s, A1). What it proves is the order of the helper's
// writes and calls, not the behaviour of systemd.

const newVersion = "1.1.0"

// componentRelease is a release whose helper binary differs from the installed
// one, so it brings a component set; alsoChanged names units that get another
// comment line (a difference the unit lint accepts).
func (v *tenv) componentRelease(version string, alsoChanged ...string) *release {
	r := v.release(version)
	r.setFile("oarfish-update-helper", elfStub("helper "+version))
	for _, n := range alsoChanged {
		r.setFile(n, changedUnit(n))
	}
	return r
}

// newComponentEnv returns an environment with the installed binary, the fakes
// and a staged 1.1.0 that brings a component set.
func newComponentEnv(t *testing.T, alsoChanged ...string) (*tenv, *supervisor) {
	t.Helper()
	v := newTenv(t)
	v.installOarfish()
	v.fakeSystemd()
	// The flushes are skipped: their order is checked by TestDurabilityOrder and
	// TestComponentWriteAheadOrder, and these tests need the effect of the
	// other calls only. A test that records calls installs its own wrapper.
	v.h.wrapFS = withoutSync
	v.place(newVersion, v.componentRelease(newVersion, alsoChanged...).build())
	if r := wantOK(t, v.stage(newVersion)); !r.ComponentsDiffer {
		t.Fatal("the release brings no component difference")
	}
	return v, &supervisor{v: v}
}

// makeSet writes a complete component set components/<id>.
func (v *tenv) makeSet(id string) {
	v.t.Helper()
	dir := filepath.Join(v.root, "components", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		v.t.Fatal(err)
	}
	for n, d := range baseComponents {
		if n == "oarfish-update-helper" {
			d = elfStub("helper " + id)
		}
		if err := os.WriteFile(filepath.Join(dir, n), d, 0o755); err != nil {
			v.t.Fatal(err)
		}
	}
}

func withoutSync(inner fsops) fsops { return noSyncFS{inner} }

// link is the text of a component link.
func (v *tenv) link(name string) string {
	target, _ := os.Readlink(filepath.Join(v.root, "components", name))
	return target
}

// confirm sends activate with components_confirmed.
func (v *tenv) confirm(version string) []byte { return v.request(activateLine(version, testCID, true)) }

func (v *tenv) exists(rel string) bool {
	_, err := os.Lstat(filepath.Join(v.root, rel))
	return err == nil
}

type supervisor struct {
	v *tenv
	// restartAt is the uptime of every restart of the helper service.
	restartAt []int64
	// recovers counts the runs of the recover unit.
	recovers int
	// crash: a helper image (its bytes) that exits with status 1 at once.
	crash map[string]bool
	// mutate changes a serve process before it starts; image is its own image.
	mutate func(h *handler, image string)
	// async: the processes run their transactions in goroutines.
	async bool
}

// process is a new process of the helper with the image that link names. The
// fakes, the fault gate and the file system wrapper are those of the
// environment, so a fault outlives the process that hit it.
func (s *supervisor) process(link string) *handler {
	old := s.v.h
	h := s.v.newHandler(link)
	h.wrapFS, h.sysrun, h.healthGet, h.sleep, h.syncTxn = old.wrapFS, old.sysrun, old.healthGet, old.sleep, !s.async
	return h
}

func (s *supervisor) activeImage() string {
	b, _ := os.ReadFile(filepath.Join(s.v.root, "components", linkActive, "oarfish-update-helper"))
	return string(b)
}

// serveStart is one start of the helper service: the binary that active names.
// A binary that fails at once makes systemd run the recover unit (OnFailure=,
// at the first failed exit, A1) and then start the service again
// (Restart=on-failure). When the recovery did not switch back, the same
// happens again, until the start limit ends it: then nothing serves (false)
// until the next boot.
func (s *supervisor) serveStart() bool {
	s.v.t.Helper()
	for i := 0; s.crash[s.activeImage()]; i++ {
		if i == 2 {
			return false // start-limit-hit
		}
		s.recover(true)
		s.restartAt = append(s.restartAt, s.v.uptime.Load())
	}
	h := s.process(linkActive)
	if s.mutate != nil {
		s.mutate(h, s.activeImage())
	}
	s.v.h = h
	h.startupRecovery()
	if !s.async {
		h.wait()
	}
	return true
}

// settle does what systemd does while the last process ended with exit code
// 75: start the service again (RestartForceExitStatus=75, no recover unit).
func (s *supervisor) settle() {
	s.v.t.Helper()
	for i := 0; s.v.h.exit.Load() == exitHandOver; i++ {
		if i > 5 {
			s.v.t.Fatal("the helper keeps handing over")
		}
		s.countRestart()
		if !s.serveStart() {
			return
		}
	}
}

// countRestart fails the test at the third restart within 10 s: one
// transaction may restart the helper at most twice in that window (A1).
func (s *supervisor) countRestart() {
	s.v.t.Helper()
	now := s.v.uptime.Load()
	s.restartAt = append(s.restartAt, now)
	n := 0
	for _, at := range s.restartAt {
		if now-at < 10 {
			n++
		}
	}
	if n > 2 {
		s.v.t.Fatalf("restart number %d within 10 s: the start limit of the helper service would be hit", n)
	}
}

// recover runs the recover unit: the helper binary of known-good.
func (s *supervisor) recover(onFailure bool) int {
	h := s.process(linkKnownGood)
	h.onFailure = onFailure
	s.recovers++
	return h.recoverBoot(io.Discard)
}

// reboot is the next boot: the recover unit, then the first start of serve.
func (s *supervisor) reboot() int {
	s.v.t.Helper()
	s.restartAt = nil
	code := s.recover(false)
	s.serveStart()
	s.settle()
	return code
}

// nextStart is the next socket activation after the process went idle.
func (s *supervisor) nextStart() {
	s.v.t.Helper()
	s.restartAt = nil
	s.serveStart()
	s.settle()
}

// setComplete: components/<id> holds the four files, the helper with the bytes
// of that set and units that the lint accepts.
func setComplete(v *tenv, id string) bool {
	want := elfStub("helper " + id)
	if id == v.installed {
		want = baseComponents["oarfish-update-helper"]
	}
	dir := filepath.Join(v.root, "components", id)
	if got, err := os.ReadFile(filepath.Join(dir, "oarfish-update-helper")); err != nil || string(got) != string(want) {
		return false
	}
	for _, n := range componentNames[1:4] {
		text, err := os.ReadFile(filepath.Join(dir, n))
		// The units of a staged set passed the lint; installed ones are not linted.
		if err != nil || (id != v.installed && !lintUnit(n, text).Accepted) {
			return false
		}
	}
	return true
}

// checkComponentDisk is the invariant of U7 with the component part: current
// names a complete release, the two component links name complete sets, and no
// release or component directory under a version name is incomplete.
func checkComponentDisk(t *testing.T, v *tenv) {
	t.Helper()
	checkDisk(t, v)
	for _, l := range []string{linkActive, linkKnownGood} {
		id := v.link(l)
		if !validVersion(id) || !setComplete(v, id) {
			t.Fatalf("components/%s names %q, which is not a complete set", l, id)
		}
	}
	ents, _ := os.ReadDir(filepath.Join(v.root, "components"))
	for _, e := range ents {
		if validVersion(e.Name()) && !setComplete(v, e.Name()) {
			t.Fatalf("components/%s is incomplete", e.Name())
		}
	}
}

// diskSetDigest is the set digest of components/<id> as the files are.
func diskSetDigest(t *testing.T, v *tenv, id string) string {
	t.Helper()
	d, err := (&env{opt: mustRoot(t, v.root)}).setDigests(componentPath(id))
	if err != nil {
		t.Fatal(err)
	}
	return setDigest(d)
}

// checkSettled is what must hold when a transaction and the recovery of its
// interruption are over.
func checkSettled(t *testing.T, v *tenv, stagedAllowed bool) *state {
	t.Helper()
	checkComponentDisk(t, v)
	st, ok := v.readState()
	if !ok {
		t.Fatal("no state")
	}
	staged := st.Txn.Phase == phaseStaged
	if st.Txn.Phase != phaseIdle && !(staged && stagedAllowed) {
		t.Fatalf("phase %s, last %+v", st.Txn.Phase, st.Last)
	}
	a, k, cur := v.link(linkActive), v.link(linkKnownGood), v.current()
	if a != k {
		t.Fatalf("active names %s and known-good %s", a, k)
	}
	if cur == newVersion && (st.Last == nil || st.Last.Outcome != outcomeSuccess || a != newVersion) {
		t.Fatalf("current names %s without a committed update (components %s): %+v", cur, a, st.Last)
	}
	if a == newVersion {
		// The components committed: the reference hash is the new set's, and the
		// result says that the components were updated.
		if st.Txn.KnownGoodID != newVersion || st.Txn.KnownGoodSHA256 != diskSetDigest(t, v, newVersion) {
			t.Fatalf("known-good record %s %s", st.Txn.KnownGoodID, st.Txn.KnownGoodSHA256)
		}
		if st.Last == nil || !st.Last.ComponentsUpdated || st.ComponentsPrevious != v.installed {
			t.Fatalf("committed components without a result that says so: %+v, previous %q", st.Last, st.ComponentsPrevious)
		}
	} else if !staged && (v.exists("components/"+newVersion) || v.exists("releases/"+newVersion)) {
		t.Fatalf("the components were not updated, but the new set or release is still there (last %+v)", st.Last)
	}
	v.wantNoLeftovers()
	ents, _ := os.ReadDir(filepath.Join(v.root, "components"))
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), componentTempPrefix) {
			t.Fatalf("components/ holds %s", e.Name())
		}
	}
	if !lockFree(t, v.stateDir) {
		t.Fatal("lock held")
	}
	return st
}
