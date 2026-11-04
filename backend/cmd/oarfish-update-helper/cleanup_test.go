package main

import (
	"archive/tar"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
)

// snapshot describes every entry below dir: directories, files with their
// content hash and mode, links with their target.
func snapshot(t testing.TB, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		fi, err := os.Lstat(path)
		if err != nil {
			return err
		}
		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			target, _ := os.Readlink(path)
			out[rel] = "l:" + target
		case fi.IsDir():
			out[rel] = fmt.Sprintf("d:%o", fi.Mode().Perm())
		case fi.Mode().IsRegular():
			data, _ := os.ReadFile(path)
			out[rel] = fmt.Sprintf("f:%o:%x", fi.Mode().Perm(), sha256.Sum256(data))
		default:
			out[rel] = fmt.Sprintf("special:%v", fi.Mode())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func diffSnapshots(a, b map[string]string) []string {
	var d []string
	for k, v := range a {
		if w, ok := b[k]; !ok {
			d = append(d, "removed "+k)
		} else if w != v {
			d = append(d, "changed "+k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			d = append(d, "added "+k)
		}
	}
	sort.Strings(d)
	return d
}

// stagingDir finds the one .stage.<id> directory below releases/ and fails when
// there is none.
func (v *tenv) stagingDir() string {
	v.t.Helper()
	for _, n := range v.releasesEntries() {
		if strings.HasPrefix(n, ".stage.") {
			return filepath.Join(v.root, "releases", n)
		}
	}
	v.t.Fatal("no staging directory")
	return ""
}

// atFirstExtraction runs f just before the second Inspect (the one with the
// sink) starts reading, after the first one verified the private bundle copy;
// the copy can be changed there. (Changing it later would mix the bytes the
// reader has already buffered with the new ones.)
func (v *tenv) atFirstExtraction(f func(bundlePath string)) {
	var done bool
	v.h.wrapFS = func(inner fsops) fsops {
		t := newTestFS(inner)
		t.before = func(op fsOp) error {
			if !done && op.Kind == "mkdir" && strings.HasSuffix(op.Path, "/components") {
				done = true
				ents, _ := os.ReadDir(filepath.Join(v.root, "releases"))
				for _, e := range ents {
					if strings.HasPrefix(e.Name(), ".stage.") {
						dir := filepath.Join(v.root, "releases", e.Name())
						files, _ := os.ReadDir(dir)
						for _, fl := range files {
							if strings.HasSuffix(fl.Name(), ".tar.gz") {
								f(filepath.Join(dir, fl.Name()))
							}
						}
					}
				}
			}
			return nil
		}
		v.fs = t
		return t
	}
}

// wantAborted checks that a failed stage left nothing: the tree is as it was,
// the state is idle without a last result, and the lock is free.
func wantNothingLeft(t *testing.T, v *tenv, before map[string]string) {
	t.Helper()
	if d := diffSnapshots(before, snapshot(t, v.root)); len(d) != 0 {
		t.Fatalf("the release tree changed: %v", d)
	}
	st, ok := v.readState()
	if !ok || st.Txn.Phase != phaseIdle || st.Last != nil || st.Txn.To != "" {
		t.Fatalf("state %+v", st)
	}
	if !lockFree(t, v.stateDir) {
		t.Fatal("lock held")
	}
}

func TestCleanupAfterFailuresDuringExtraction(t *testing.T) {
	// A bundle with a big file, so that failures can happen in the middle of it.
	bigBundle := func(v *tenv) (*release, built) {
		r := v.release("1.1.0")
		r.setFile("oarfish", append(elfStub("big"), incompressible(200<<10)...))
		return r, r.build()
	}
	tests := []struct {
		name  string
		want  errorCode
		setup func(v *tenv)
	}{
		{"Inspect fails after the first file was finished (second run)", codeChecksumMismatch, func(v *tenv) {
			r, _ := bigBundle(v)
			v.place("1.1.0", r.build())
			v.atFirstExtraction(func(bundle string) {
				// A later file differs: the files before it are written and finished.
				ents := defaultEnts(r.root, r.files)
				ents[len(ents)-1].body = []byte("[Service]\nExecStart=/changed\n")
				os.WriteFile(bundle, gz(makeTar(ents)), 0o644)
			})
		}},
		{"Inspect fails in the middle of a file (the stream ends)", codeManifestInvalid, func(v *tenv) {
			_, b := bigBundle(v)
			v.place("1.1.0", b)
			v.atFirstExtraction(func(bundle string) { os.Truncate(bundle, int64(len(b.Bundle)*3/5)) })
		}},
		{"the hash of the last file differs in the very last byte (second run)", codeChecksumMismatch, func(v *tenv) {
			r, b := bigBundle(v)
			v.place("1.1.0", b)
			v.atFirstExtraction(func(bundle string) {
				ents := defaultEnts(r.root, r.files)
				last := &ents[len(ents)-1]
				body := append([]byte(nil), last.body...)
				body[len(body)-1] ^= 0x01
				last.body = body
				os.WriteFile(bundle, gz(makeTar(ents)), 0o644)
			})
		}},
		{"the hash of the last file differs in the very last byte (first run)", codeChecksumMismatch, func(v *tenv) {
			r, _ := bigBundle(v)
			ents := defaultEnts(r.root, r.files)
			last := &ents[len(ents)-1]
			body := append([]byte(nil), last.body...)
			body[len(body)-1] ^= 0x01
			last.body = body
			r.bundle = gz(makeTar(ents))
			man := r.defaultManifest() // sizes and hashes of the original files
			r.man = man
			v.place("1.1.0", r.build())
		}},
		{"the ELF check fails (first run)", codeRequirementsNotMet, func(v *tenv) {
			r := v.release("1.1.0")
			bad := elfStub("x")
			bad[0] = 'X'
			r.setFile("oarfish", bad)
			v.place("1.1.0", r.build())
		}},
		{"the ELF check fails (second run, after files were finished)", codeRequirementsNotMet, func(v *tenv) {
			r, b := bigBundle(v)
			v.place("1.1.0", b)
			v.atFirstExtraction(func(bundle string) {
				ents := defaultEnts(r.root, r.files)
				for i := range ents {
					if strings.HasSuffix(ents[i].name, "/oarfish") {
						bad := append([]byte(nil), ents[i].body...)
						bad[1] = 'X'
						ents[i].body = bad
					}
				}
				os.WriteFile(bundle, gz(makeTar(ents)), 0o644)
			})
		}},
		{"the sink Write fails in the middle of a file (disk full)", codeIOError, func(v *tenv) {
			_, b := bigBundle(v)
			v.place("1.1.0", b)
			writes := 0
			v.h.wrapFS = func(inner fsops) fsops {
				f := newTestFS(inner)
				f.before = func(op fsOp) error {
					if op.Kind == "write" && strings.HasSuffix(op.Path, "/release/oarfish") {
						if writes++; writes == 3 {
							return syscall.ENOSPC
						}
					}
					return nil
				}
				v.fs = f
				return f
			}
		}},
		{"the sink fsync fails", codeIOError, func(v *tenv) {
			v.stageable("1.1.0")
			v.h.wrapFS = func(inner fsops) fsops {
				f := newTestFS(inner)
				f.before = func(op fsOp) error {
					if op.Kind == "sync" && strings.HasSuffix(op.Path, "/release/oarfish") {
						return syscall.EIO
					}
					return nil
				}
				v.fs = f
				return f
			}
		}},
		{"the gzip stream is truncated (first run)", codeManifestInvalid, func(v *tenv) {
			r, b := bigBundle(v)
			r.bundle = b.Bundle[:len(b.Bundle)/2]
			r.man = r.defaultManifest()
			v.place("1.1.0", r.build())
		}},
		{"the tar has a forbidden entry after good ones (first run)", codeManifestInvalid, func(v *tenv) {
			r, _ := bigBundle(v)
			ents := defaultEnts(r.root, r.files)
			ents = append(ents, tarEnt{name: r.root + "/link", typ: tar.TypeSymlink, link: "/etc/shadow"})
			r.setBundle(ents)
			v.place("1.1.0", r.build())
		}},
		{"the tar has a forbidden entry after good ones (second run)", codeManifestInvalid, func(v *tenv) {
			r, b := bigBundle(v)
			v.place("1.1.0", b)
			v.atFirstExtraction(func(bundle string) {
				ents := defaultEnts(r.root, r.files)
				ents = append(ents, tarEnt{name: r.root + "/link", typ: tar.TypeSymlink, link: "/etc/shadow"})
				os.WriteFile(bundle, gz(makeTar(ents)), 0o644)
			})
		}},
		{"the private copy is a different bundle between the two runs", codeManifestInvalid, func(v *tenv) {
			_, b := bigBundle(v)
			v.place("1.1.0", b)
			v.atFirstExtraction(func(bundle string) { os.WriteFile(bundle, []byte("not a bundle"), 0o644) })
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newTenv(t)
			tt.setup(v)
			before := snapshot(t, v.root)
			wantErrCode(t, v.stage("1.1.0"), tt.want)
			wantNothingLeft(t, v, before)
			// A failure of the first run (nil sink) is found before the sink wrote a file.
			if strings.Contains(tt.name, "(first run)") {
				for _, op := range v.fs.Ops() {
					if op.Kind == "create" && strings.Contains(op.Path, "/release/") {
						t.Fatalf("the sink was used although the verifying run fails: %v", op)
					}
				}
			}
			if got := v.journal.String(); !strings.Contains(got, "phase=staging") || !strings.Contains(got, "phase=idle to=1.1.0 from=1.0.0 cid="+testCID+" result="+string(tt.want)) {
				t.Fatalf("journal:\n%s", got)
			}
		})
	}
}

// If the cleanup itself fails, the answer is io_error and the state stays at
// staging with the field "to"; the staging recovery of the next request then
// finishes the job.
func TestCleanupFailureLeavesStagingAndRecoveryFinishesIt(t *testing.T) {
	for _, step := range []string{"removing the staging directory", "removing the renamed release", "writing idle"} {
		t.Run(step, func(t *testing.T) {
			v := newTenv(t)
			v.stageable("1.1.0")
			before := snapshot(t, v.root)
			failedOnce := false
			var cause func(op fsOp) bool
			switch step {
			case "removing the staging directory":
				cause = func(op fsOp) bool { return op.Kind == "removeall" && strings.HasPrefix(op.Path, "releases/.stage.") }
			case "removing the renamed release":
				cause = func(op fsOp) bool { return op.Kind == "rename" && strings.HasPrefix(op.Path2, "releases/.trash.") }
			case "writing idle":
				cause = func(op fsOp) bool { return op.Kind == "create" && op.Tree == treeState && v.phase() == phaseStaging }
			}
			trigger := false
			v.h.wrapFS = func(inner fsops) fsops {
				f := newTestFS(inner)
				f.before = func(op fsOp) error {
					// The request fails at the state write of "staged" (call n), which is after
					// the rename; the cleanup then meets the failure under test.
					if op.Kind == "create" && op.Tree == treeState && v.phase() == phaseStaging && strings.Count(v.journal.String(), "\n") == 1 && !trigger {
						trigger = true
						return errInjected
					}
					if trigger && !failedOnce && cause(op) {
						failedOnce = true
						return errInjected
					}
					return nil
				}
				v.fs = f
				return f
			}
			wantErrCode(t, v.stage("1.1.0"), codeIOError)
			if !failedOnce {
				t.Fatal("the failure under test did not happen")
			}
			if st, _ := v.readState(); st == nil || st.Txn.Phase != phaseStaging || st.Txn.To != "1.1.0" {
				t.Fatalf("state %+v: want staging with to", st)
			}
			if !lockFree(t, v.stateDir) {
				t.Fatal("lock held")
			}

			// The next request runs the staging recovery first. Same version again.
			v.h.wrapFS = nil
			out := wantOK(t, v.stage("1.1.0"))
			_ = out
			if got := v.journal.String(); !strings.Contains(got, "result=interrupted") {
				t.Fatalf("the recovery did not run:\n%s", got)
			}
			st, _ := v.readState()
			if st.Txn.Phase != phaseStaged || st.Last == nil || st.Last.Outcome != outcomeAborted || st.Last.ReasonCode != reasonInterrupted {
				t.Fatalf("state %+v last %+v", st.Txn, st.Last)
			}
			assertNoStageLeft(t, v)
			_ = before
		})
	}
}

func TestStagingRecovery(t *testing.T) {
	v := newTenv(t)
	// A crash left a staging directory, a trash directory, a renamed release
	// and the state staging.
	os.MkdirAll(filepath.Join(v.root, "releases", ".stage.00000000000000aa", "release"), 0o700)
	os.WriteFile(filepath.Join(v.root, "releases", ".stage.00000000000000aa", "release", "oarfish"), []byte("partial"), 0o755)
	os.MkdirAll(filepath.Join(v.root, "releases", ".stage.00000000000000bb"), 0o700)
	os.MkdirAll(filepath.Join(v.root, "releases", ".trash.00000000000000cc"), 0o700)
	os.MkdirAll(filepath.Join(v.root, "releases", "1.1.0"), 0o755)
	os.WriteFile(filepath.Join(v.root, "releases", "1.0.0", "marker"), []byte("keep"), 0o644)
	st := freshState("x")
	st.Txn = txn{ID: "00000000000000aa", Phase: phaseStaging, To: "1.1.0", From: "1.0.0", CorrelationID: testCID}
	v.writeState(st)

	e, _ := v.h.open(true)
	defer e.close()
	if code := e.recoverStaging(st, "1.0.0"); code != "" {
		t.Fatal(code)
	}
	v.wantClean()
	got, _ := v.readState()
	if got.Txn.Phase != phaseIdle || got.Last == nil || got.Last.Outcome != outcomeAborted || got.Last.ReasonCode != reasonInterrupted ||
		got.Last.To != "1.1.0" || got.Last.From != "1.0.0" || got.Last.CorrelationID != testCID || got.Last.TransactionID != "00000000000000aa" ||
		got.Last.Acknowledged || got.Last.FinishedAt == "" {
		t.Fatalf("state %+v last %+v", got.Txn, got.Last)
	}
	if b, _ := os.ReadFile(filepath.Join(v.root, "releases", "1.0.0", "marker")); string(b) != "keep" {
		t.Fatal("the installed release was touched")
	}
	if !strings.Contains(v.journal.String(), "phase=idle to=1.1.0 from=1.0.0 cid="+testCID+" result=interrupted") {
		t.Fatalf("journal %q", v.journal.String())
	}
}

// The recovery never removes the installed or the recorded previous version,
// even when the state names it as "to".
func TestStagingRecoveryGuards(t *testing.T) {
	for _, name := range []string{"installed", "previous"} {
		t.Run(name, func(t *testing.T) {
			v := newTenv(t)
			to := "1.0.0"
			st := freshState("x")
			if name == "previous" {
				to = "0.9.0"
				st.Previous = to
				os.MkdirAll(filepath.Join(v.root, "releases", to), 0o755)
				os.WriteFile(filepath.Join(v.root, "releases", to, "marker"), []byte("keep"), 0o644)
			}
			st.Txn = txn{ID: "00000000000000aa", Phase: phaseStaging, To: to, From: "1.0.0"}
			v.writeState(st)
			e, _ := v.h.open(true)
			defer e.close()
			if code := e.recoverStaging(st, "1.0.0"); code != "" {
				t.Fatal(code)
			}
			if _, err := os.Stat(filepath.Join(v.root, "releases", to)); err != nil {
				t.Fatalf("%s release removed: %v", name, err)
			}
			if v.phase() != phaseIdle {
				t.Fatalf("phase %s", v.phase())
			}
			for _, op := range v.fs.Ops() {
				if strings.HasPrefix(op.Path, "releases/"+to) || strings.HasPrefix(op.Path2, "releases/"+to) {
					t.Fatalf("touched %s: %v", name, op)
				}
			}
		})
	}
}

// A stage with the state at staging (a crash, nobody holds the lock) runs the
// recovery first; with the lock held by somebody else it is busy and touches
// nothing.
func TestStageFindsStagingWhileTheLockIsHeldElsewhere(t *testing.T) {
	v := newTenv(t)
	v.stageable("1.1.0")
	os.MkdirAll(filepath.Join(v.root, "releases", ".stage.00000000000000aa"), 0o700)
	st := freshState("x")
	st.Txn = txn{ID: "00000000000000aa", Phase: phaseStaging, To: "1.1.0", From: "1.0.0"}
	v.writeState(st)
	held, _ := lockDir(v.stateDir)
	wantErrCode(t, v.stage("1.1.0"), codeBusy)
	held.release()
	if _, err := os.Stat(filepath.Join(v.root, "releases", ".stage.00000000000000aa")); err != nil {
		t.Fatal("the recovery ran while the lock was held")
	}
	wantOK(t, v.stage("1.1.0"))
	assertNoStageLeft(t, v)
}
