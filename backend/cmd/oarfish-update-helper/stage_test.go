package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestStageSuccess(t *testing.T) {
	v := newTenv(t)
	rel, _ := v.stageable("1.1.0")
	r := wantOK(t, v.stage("1.1.0"))
	if r.Version != "1.1.0" || r.Commit != testCommit || r.ComponentsDiffer {
		t.Fatalf("answer %+v", r)
	}
	// The release is in place with the four release files, exactly as signed.
	dir := filepath.Join(v.root, "releases", "1.1.0")
	got, err := os.ReadDir(dir)
	if err != nil || len(got) != 4 {
		t.Fatalf("release dir: %v %v", got, err)
	}
	for _, f := range rel.files {
		if f.name == "oarfish" || f.name == "LICENSE" || f.name == "THIRD_PARTY_NOTICES.txt" || f.name == "oarfish.service" {
			data, err := os.ReadFile(filepath.Join(dir, f.name))
			if err != nil || string(data) != string(f.data) {
				t.Fatalf("%s: %v", f.name, err)
			}
			fi, _ := os.Stat(filepath.Join(dir, f.name))
			if fi.Mode().Perm() != os.FileMode(f.mode) {
				t.Fatalf("%s mode %v, want %o", f.name, fi.Mode().Perm(), f.mode)
			}
		}
	}
	if names := v.releasesEntries(); strings.Join(names, ",") != "1.0.0,1.1.0" {
		t.Fatalf("releases/: %v", names)
	}
	st, ok := v.readState()
	if !ok || st.Txn.Phase != phaseStaged || st.Txn.To != "1.1.0" || st.Txn.From != "1.0.0" ||
		st.Txn.StagedAtUptimeS != 1000 || st.Txn.CorrelationID != testCID {
		t.Fatalf("state %+v", st)
	}
	if target, _ := os.Readlink(filepath.Join(v.root, "current")); target != "releases/1.0.0" {
		t.Fatalf("current -> %s", target)
	}
	_ = syscall.Getpid
	t.Log(v.journal.String())
}

// ---- one end-to-end test per relverify code, passed through unchanged -----

func TestStageErrorCodes(t *testing.T) {
	type tc struct {
		name    string
		want    errorCode
		version string // the version requested; 1.1.0 when empty
		setup   func(v *tenv)
	}
	elfOther := func(label string) []byte { b := elfStub(label); b[18] = 0x3e; return b } // x86-64
	tests := []tc{
		{"signature_invalid: signed by another key", codeSignatureInvalid, "", func(v *tenv) {
			r := v.release("1.1.0")
			r.key = newTestKey(t, "another key")
			v.place("1.1.0", r.build())
		}},
		{"signature_invalid: checksums.txt changed after signing", codeSignatureInvalid, "", func(v *tenv) {
			b := v.release("1.1.0").build()
			b.Checksums = append(b.Checksums, '\n')
			v.place("1.1.0", b)
		}},
		{"checksum_mismatch: one byte of the bundle", codeChecksumMismatch, "", func(v *tenv) {
			b := v.release("1.1.0").build()
			b.Bundle = append([]byte(nil), b.Bundle...)
			b.Bundle[len(b.Bundle)/2] ^= 1
			v.place("1.1.0", b)
		}},
		{"checksum_mismatch: mix and match, the bundle of another version", codeChecksumMismatch, "", func(v *tenv) {
			a := v.release("1.1.0").build()
			other := v.release("1.1.0")
			other.setFile("LICENSE", []byte("a different licence\n"))
			a.Bundle = other.build().Bundle
			v.place("1.1.0", a)
		}},
		{"manifest_invalid: manifest names another version", codeManifestInvalid, "", func(v *tenv) {
			r := v.release("1.1.0")
			r.man["version"] = "1.1.1"
			v.place("1.1.0", r.build())
		}},
		{"manifest_invalid: a symbolic link in the bundle", codeManifestInvalid, "", func(v *tenv) {
			r := v.release("1.1.0")
			ents := defaultEnts(r.root, r.files)
			ents[3] = tarEnt{name: ents[3].name, typ: 0x32, link: "/etc/passwd", mode: 0o644} // symlink in place of a file
			r.setBundle(ents)
			v.place("1.1.0", r.build())
		}},
		{"manifest_invalid: a path with ..", codeManifestInvalid, "", func(v *tenv) {
			r := v.release("1.1.0")
			ents := defaultEnts(r.root, r.files)
			ents = append(ents, tarEnt{name: r.root + "/../escape", body: []byte("x"), mode: 0o644})
			r.setBundle(ents)
			v.place("1.1.0", r.build())
		}},
		{"manifest_invalid: an extra file", codeManifestInvalid, "", func(v *tenv) {
			r := v.release("1.1.0")
			ents := defaultEnts(r.root, r.files)
			ents = append(ents, tarEnt{name: r.root + "/extra", body: []byte("x"), mode: 0o644})
			r.setBundle(ents)
			v.place("1.1.0", r.build())
		}},
		{"manifest_invalid: not a gzip stream", codeManifestInvalid, "", func(v *tenv) {
			r := v.release("1.1.0")
			r.bundle = []byte("this is not a bundle at all, it is just text\n")
			r.man = r.defaultManifest()
			v.place("1.1.0", r.build())
		}},
		{"checksum_mismatch: an entry larger than the manifest says", codeChecksumMismatch, "", func(v *tenv) {
			r := v.release("1.1.0")
			ents := defaultEnts(r.root, r.files)
			ents[1].body = append(append([]byte(nil), ents[1].body...), "extra bytes"...)
			r.bundle = gz(makeTar(ents))
			r.man = r.defaultManifest() // sizes from the original files, bundle hash of the hostile one
			v.place("1.1.0", r.build())
		}},
		{"downgrade_refused", codeDowngradeRefused, "0.9.0", func(v *tenv) { v.place("0.9.0", v.release("0.9.0").build()) }},
		{"already_installed", codeAlreadyInstalled, "1.0.0", func(v *tenv) { v.place("1.0.0", v.release("1.0.0").build()) }},
		{"requirements_not_met: a program that is not an arm64 ELF executable", codeRequirementsNotMet, "", func(v *tenv) {
			r := v.release("1.1.0")
			r.setFile("oarfish", elfOther("oarfish"))
			v.place("1.1.0", r.build())
		}},
		{"requirements_not_met: a legacy-shape release", codeRequirementsNotMet, "", func(v *tenv) {
			v.place("1.1.0", newRelease(t, v.key, "1.1.0", false).build())
		}},
		{"requirements_not_met: min_installed_version above the installed", codeRequirementsNotMet, "", func(v *tenv) {
			r := v.release("1.1.0")
			r.man["requirements"].(map[string]any)["min_installed_version"] = "1.0.1"
			v.place("1.1.0", r.build())
		}},
		{"helper_too_old", codeHelperTooOld, "", func(v *tenv) {
			r := v.release("1.1.0")
			r.man["requirements"].(map[string]any)["helper_protocol"] = 2
			v.place("1.1.0", r.build())
		}},
		{"installer_required: the helper differs", codeInstallerRequired, "", func(v *tenv) {
			r := v.release("1.1.0")
			r.setFile("oarfish-update-helper", elfStub("a newer helper"))
			v.place("1.1.0", r.build())
		}},
		{"components_damaged: the active set is incomplete", codeComponentsDamaged, "", func(v *tenv) {
			v.stageable("1.1.0")
			os.Remove(filepath.Join(v.root, "components", "1.0.0", "oarfish-update-recover.service"))
		}},
		{"current_unhealthy", codeCurrentUnhealthy, "", func(v *tenv) { v.stageable("1.1.0"); os.Remove(filepath.Join(v.root, "current")) }},
		{"no_space", codeNoSpace, "", func(v *tenv) {
			v.stageable("1.1.0")
			v.h.freeBytes = func(*os.File) (uint64, error) { return 64<<20 + 10, nil }
		}},
		{"not_found", codeNotFound, "", func(v *tenv) {}},
		{"io_error: the sink cannot write", codeIOError, "", func(v *tenv) {
			v.stageable("1.1.0")
			v.h.wrapFS = func(inner fsops) fsops {
				f := newTestFS(inner)
				f.before = func(op fsOp) error {
					if op.Kind == "write" && strings.Contains(op.Path, "/release/") {
						return errInjected
					}
					return nil
				}
				return f
			}
		}},
	}
	seen := map[errorCode]bool{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newTenv(t)
			tt.setup(v)
			ver := tt.version
			if ver == "" {
				ver = "1.1.0"
			}
			r := wantErrCode(t, v.stage(ver), tt.want)
			if (tt.want == codeInstallerRequired) != (len(r.Differs) > 0) {
				t.Fatalf("differs %v", r.Differs)
			}
			seen[tt.want] = true
			v.wantClean()
			if st, ok := v.readState(); ok && (st.Txn.Phase != phaseIdle || st.Last != nil) {
				t.Fatalf("state after a synchronous error: %+v (no last result, idle)", st)
			}
			if tt.want == codeNotFound || tt.want == codeCurrentUnhealthy || tt.want == codeNoSpace {
				if _, ok := v.readState(); ok {
					t.Fatal("state written before the failing check")
				}
			}
		})
	}
	for _, c := range []errorCode{codeSignatureInvalid, codeChecksumMismatch, codeManifestInvalid, codeDowngradeRefused,
		codeAlreadyInstalled, codeRequirementsNotMet, codeHelperTooOld, codeIOError} {
		if !seen[c] {
			t.Errorf("no end-to-end test for %s", c)
		}
	}
}

func TestStageCorrelationIdAndVersionInTheState(t *testing.T) {
	v := newTenv(t)
	v.stageable("1.1.0")
	wantOK(t, v.request(stageLine("1.1.0", "feedfacefeedface")))
	st, _ := v.readState()
	if st.Txn.CorrelationID != "feedfacefeedface" || st.Txn.ID == "" || st.HelperVersion != "test-1" {
		t.Fatalf("state %+v", st)
	}
	if got := v.status(); got.Phase != phaseStaged || got.StagedVersion != "1.1.0" {
		t.Fatalf("status %+v", got)
	}
}

// The state says staging before anything is created in releases/ (write-ahead).
func TestStageWritesIntentFirst(t *testing.T) {
	v := newTenv(t)
	v.stageable("1.1.0")
	wantOK(t, v.stage("1.1.0"))
	ops := v.fs.Ops()
	firstStateRename, firstRelease := -1, -1
	for i, op := range ops {
		if op.Tree == treeState && op.Kind == "rename" && firstStateRename < 0 {
			firstStateRename = i
		}
		if op.Tree == treeOpt && firstRelease < 0 {
			firstRelease = i
		}
	}
	if firstStateRename < 0 || firstRelease < 0 || firstStateRename > firstRelease {
		t.Fatalf("the staging state is written at call %d, the first release call is %d", firstStateRename, firstRelease)
	}
}

func TestStageDeadline(t *testing.T) {
	for _, at := range []string{"before the first chunk", "after the small files", "during the extraction"} {
		t.Run(at, func(t *testing.T) {
			v := newTenv(t)
			v.stageable("1.1.0")
			switch at {
			case "before the first chunk":
				v.h.onChunk = func(string, int64) { v.mono.Store(int64(stageDeadline)) }
			case "after the small files":
				v.h.onChunk = func(file string, copied int64) {
					if strings.HasSuffix(file, ".tar.gz") {
						v.mono.Store(int64(stageDeadline))
					}
				}
			case "during the extraction":
				v.h.wrapFS = func(inner fsops) fsops {
					f := newTestFS(inner)
					f.before = func(op fsOp) error {
						if op.Kind == "create" && strings.HasSuffix(op.Path, "/release/oarfish") {
							v.mono.Store(int64(stageDeadline))
						}
						return nil
					}
					return f
				}
			}
			wantErrCode(t, v.stage("1.1.0"), codeIOError)
			v.wantClean()
			if v.phase() != phaseIdle {
				t.Fatalf("phase %s", v.phase())
			}
			if !lockFree(t, v.stateDir) {
				t.Fatal("lock held after the deadline")
			}
		})
	}
	t.Run("just under the deadline", func(t *testing.T) {
		v := newTenv(t)
		v.stageable("1.1.0")
		v.h.onChunk = func(string, int64) { v.mono.Store(int64(stageDeadline) - 1) }
		wantOK(t, v.stage("1.1.0"))
	})
}

// A releases/<version> of a newer version, left by an installer rollback, is
// moved aside, replaced, and deleted (G2). The installed directory is never
// touched.
func TestStageReplacesAStaleNewerRelease(t *testing.T) {
	v := newTenv(t)
	stale := filepath.Join(v.root, "releases", "1.1.0")
	os.MkdirAll(stale, 0o755)
	os.WriteFile(filepath.Join(stale, "stale-marker"), []byte("old"), 0o644)
	installedMarker := filepath.Join(v.root, "releases", "1.0.0", "marker")
	os.WriteFile(installedMarker, []byte("keep"), 0o644)
	v.stageable("1.1.0")
	wantOK(t, v.stage("1.1.0"))
	if _, err := os.Stat(filepath.Join(stale, "stale-marker")); err == nil {
		t.Fatal("the stale release is still there")
	}
	if _, err := os.Stat(filepath.Join(stale, "oarfish")); err != nil {
		t.Fatal("the staged release is not in place")
	}
	assertNoStageLeft(t, v)
	if b, _ := os.ReadFile(installedMarker); string(b) != "keep" {
		t.Fatal("the installed release was touched")
	}
	// The mutation log: the stale directory went to releases/.trash.<id>.
	var trashed bool
	for _, op := range v.fs.Ops() {
		if op.Kind == "rename" && op.Path == "releases/1.1.0" && strings.HasPrefix(op.Path2, "releases/.trash.") {
			trashed = true
		}
		if strings.HasPrefix(op.Path, "releases/1.0.0") || strings.HasPrefix(op.Path2, "releases/1.0.0") {
			t.Fatalf("touched the installed release: %v", op)
		}
	}
	if !trashed {
		t.Fatal("the stale release was not moved to .trash")
	}
}

// If the stage fails after the stale release was moved aside, the request ends
// clean as well.
func TestStageFailureAfterMovingAside(t *testing.T) {
	v := newTenv(t)
	os.MkdirAll(filepath.Join(v.root, "releases", "1.1.0"), 0o755)
	v.stageable("1.1.0")
	v.h.wrapFS = func(inner fsops) fsops {
		f := newTestFS(inner)
		f.before = func(op fsOp) error {
			if op.Kind == "rename" && strings.HasSuffix(op.Path, "/release") {
				return errInjected
			}
			return nil
		}
		return f
	}
	wantErrCode(t, v.stage("1.1.0"), codeIOError)
	assertNoStageLeft(t, v)
	if v.phase() != phaseIdle {
		t.Fatalf("phase %s", v.phase())
	}
}

// Never the installed or the recorded previous version: a stage whose version
// equals the previous one is refused when a directory of that name exists.
func TestStageNeverTouchesThePreviousVersion(t *testing.T) {
	v := newTenv(t)
	st := freshState("x")
	st.Previous = "1.1.0"
	v.writeState(st)
	prev := filepath.Join(v.root, "releases", "1.1.0")
	os.MkdirAll(prev, 0o755)
	os.WriteFile(filepath.Join(prev, "marker"), []byte("keep"), 0o644)
	v.stageable("1.1.0")
	wantErrCode(t, v.stage("1.1.0"), codeIOError)
	if b, _ := os.ReadFile(filepath.Join(prev, "marker")); string(b) != "keep" {
		t.Fatal("the previous release was touched")
	}
	assertNoStageLeft(t, v)
}

// A second stage after a successful one is busy until the staged release
// expires; after the expiry the next stage cleans it first.
func TestStageAfterStaged(t *testing.T) {
	v := newTenv(t)
	v.stageable("1.1.0")
	wantOK(t, v.stage("1.1.0"))
	v.stageable("1.2.0")
	wantErrCode(t, v.stage("1.2.0"), codeBusy)
	v.uptime.Store(1000 + stageLifetimeS - 1)
	wantErrCode(t, v.stage("1.2.0"), codeBusy)
	v.uptime.Store(1000 + stageLifetimeS)
	wantOK(t, v.stage("1.2.0"))
	if names := v.releasesEntries(); strings.Join(names, ",") != "1.0.0,1.2.0" {
		t.Fatalf("releases/: %v", names)
	}
	st, _ := v.readState()
	if st.Txn.To != "1.2.0" || st.Last == nil || st.Last.ReasonCode != reasonStageExpired || st.Last.To != "1.1.0" {
		t.Fatalf("state %+v last %+v", st.Txn, st.Last)
	}
}

// Other phases (a later gate's) answer busy and change nothing.
func TestStageInOtherPhasesIsBusy(t *testing.T) {
	for _, phase := range []string{"switching", "verifying", "committed", "rolling_back", "c_switching", "c_switched"} {
		v := newTenv(t)
		v.stageable("1.1.0")
		st := freshState("x")
		st.Txn = txn{Phase: phase, To: "1.1.0", From: "1.0.0"}
		v.writeState(st)
		wantErrCode(t, v.stage("1.1.0"), codeBusy)
		v.wantClean()
		if got, _ := v.readState(); got.Txn.Phase != phase {
			t.Fatalf("phase %s changed to %s", phase, got.Txn.Phase)
		}
		if len(v.fs.Ops()) != 0 {
			t.Fatalf("mutated in phase %s", phase)
		}
	}
}

// The free-space check has two stages: a lower bound before anything is
// written and the exact bound after the manifest is known.
func TestStageSpaceChecks(t *testing.T) {
	v := newTenv(t)
	r, b := v.stageable("1.1.0")
	total := int64(len(b.Checksums) + len(b.Sig) + len(b.Manifest) + len(b.Bundle))
	var contents int64
	for _, f := range r.files {
		contents += int64(len(f.data))
	}
	lower := uint64(total) + spaceReserve
	exact := uint64(total) + uint64(contents) + spaceReserve

	v.h.freeBytes = func(*os.File) (uint64, error) { return lower - 1, nil }
	wantErrCode(t, v.stage("1.1.0"), codeNoSpace)
	if len(v.fs.Ops()) != 0 {
		t.Fatal("the lower bound failed late")
	}

	v.h.freeBytes = func(*os.File) (uint64, error) { return exact - 1, nil }
	wantErrCode(t, v.stage("1.1.0"), codeNoSpace)
	v.wantClean()
	if v.phase() != phaseIdle {
		t.Fatalf("phase %s", v.phase())
	}

	v.h.freeBytes = func(*os.File) (uint64, error) { return exact, nil }
	wantOK(t, v.stage("1.1.0"))

	v2 := newTenv(t)
	v2.stageable("1.1.0")
	v2.h.freeBytes = func(*os.File) (uint64, error) { return 0, os.ErrInvalid }
	wantErrCode(t, v2.stage("1.1.0"), codeIOError)
}

// A prerelease device installed version and prerelease candidates.
func TestStagePrereleaseVersions(t *testing.T) {
	v := newTenv(t)
	os.Rename(filepath.Join(v.root, "releases", "1.0.0"), filepath.Join(v.root, "releases", "1.0.0-alpha.2"))
	os.Remove(filepath.Join(v.root, "current"))
	os.Symlink("releases/1.0.0-alpha.2", filepath.Join(v.root, "current"))
	v.installed = "1.0.0-alpha.2"
	v.stageable("1.0.0-alpha.3")
	wantOK(t, v.stage("1.0.0-alpha.3"))
}
