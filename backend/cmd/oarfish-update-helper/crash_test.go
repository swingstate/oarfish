package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The crash-sweep harness (ADR 0010, "Evidence and test plan", item 4; reused
// by G6.6). A scenario is run once to count its mutating file system calls N;
// then, for every k from 1 to N, once with "error at k" (that one call fails,
// everything else runs) and once with "stop at k" (the file system executes
// nothing from the k-th call on, which simulates power loss). After each run
// the invariants of U7 are checked, then the staging recovery runs and the
// system must be clean and usable again.

type crashScenario struct {
	name string
	// setup prepares the environment (the installed release is there); it
	// returns the version the scenario's request works on.
	setup func(t *testing.T, v *tenv) string
	// request is the request under test.
	request func(v *tenv, version string) []byte
	// stale: a directory releases/<version> exists before the request (a
	// leftover that is not ours); it may still be there at the end when the
	// request failed before it was moved aside.
	stale bool
	// check runs on the disk as the request left it and again after the
	// recovery (recovered).
	check func(t *testing.T, v *tenv, recovered bool)
	// components: the release brings a component set (helper bytes of that version).
	components bool
}

// preserved is the part of the tree no scenario of this gate may change:
// everything except the staging areas and the directories of versions other
// than the installed one (the closed write-target table of U2).
func preserved(rel string) bool {
	if rel == "releases" || strings.HasPrefix(rel, "releases/.stage.") || strings.HasPrefix(rel, "releases/.trash.") {
		return false
	}
	// G6.10: a staged component set, under the version of the release.
	if rel == "components" || rel == "components/1.1.0" || strings.HasPrefix(rel, "components/1.1.0/") {
		return false
	}
	if rest, ok := strings.CutPrefix(rel, "releases/"); ok {
		first, _, _ := strings.Cut(rest, "/")
		return first == "1.0.0"
	}
	return true
}

func sweepCrashes(t *testing.T, sc crashScenario) {
	// Count the calls of a complete run.
	var total int
	{
		v := newTenv(t)
		ver := sc.setup(t, v)
		log := collectOps(v)
		sc.request(v, ver)
		total = len(log.take())
	}
	if total < 5 {
		t.Fatalf("%s: only %d mutating calls", sc.name, total)
	}
	t.Logf("%s: %d mutating calls, %d runs", sc.name, total, 2*total)

	for k := 1; k <= total; k++ {
		for _, mode := range []string{"error", "stop"} {
			t.Run(fmt.Sprintf("%s/%s at %d", sc.name, mode, k), func(t *testing.T) {
				v := newTenv(t)
				ver := sc.setup(t, v)
				before := snapshot(t, v.root)
				stateBefore, hadState := v.readState()
				var tfs *testFS
				v.h.wrapFS = func(inner fsops) fsops {
					tfs = newTestFS(noSyncFS{inner})
					if mode == "error" {
						tfs.failAt = k
					} else {
						tfs.stopAt = k
					}
					v.fs = tfs
					return tfs
				}
				sc.request(v, ver)
				// Nothing outside the write-target table was touched.
				for _, op := range tfs.Ops() {
					if !allowedTarget(op.Tree, op.Path) || (op.Path2 != "" && !allowedTarget(op.Tree, op.Path2)) {
						t.Fatalf("%s is outside the write-target table", op)
					}
				}
				after := snapshot(t, v.root)
				for _, d := range diffSnapshots(before, after) {
					rel := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(d, "removed "), "added "), "changed ")
					if preserved(rel) {
						t.Fatalf("the tree changed outside the staging areas: %s", d)
					}
				}
				// current names the installed release.
				if target, _ := os.Readlink(filepath.Join(v.root, "current")); target != "releases/1.0.0" {
					t.Fatalf("current -> %q", target)
				}
				if _, err := os.Stat(filepath.Join(v.root, "releases", "1.0.0", "oarfish.service")); err != nil {
					t.Fatal("the installed release is damaged")
				}
				// releases/<version> is absent or the fully verified tree.
				checkReleaseDir(t, v, ver)
				if sc.check != nil {
					sc.check(t, v, false)
				}
				// state.json parses and is the old or a new state.
				st, ok := v.readState()
				switch {
				case !ok && hadState:
					t.Fatal("the state file disappeared")
				case ok:
					switch st.Txn.Phase {
					case phaseIdle, phaseStaging, phaseStaged:
					default:
						t.Fatalf("phase %s", st.Txn.Phase)
					}
					if hadState && st.Txn.Phase == stateBefore.Txn.Phase && st.Txn.To != stateBefore.Txn.To && st.Txn.Phase != phaseStaged && st.Txn.Phase != phaseStaging {
						t.Fatalf("state %+v", st.Txn)
					}
				}
				// An idle state means nothing is left to recover.
				if ok && st.Txn.Phase == phaseIdle {
					for _, n := range v.releasesEntries() {
						if strings.HasPrefix(n, ".stage.") || strings.HasPrefix(n, ".trash.") {
							t.Fatalf("state idle but %s is left", n)
						}
					}
				}
				if !lockFree(t, v.stateDir) {
					t.Fatal("lock held")
				}

				// The staging recovery (U7 rows 2 to 4), with a working file system.
				v.h.wrapFS = func(inner fsops) fsops { return noSyncFS{inner} }
				settle(t, v)
				for _, n := range v.releasesEntries() {
					if n != "1.0.0" && !(sc.stale && n == ver) {
						t.Fatalf("releases/ holds %v after the recovery", v.releasesEntries())
					}
				}
				if v.phase() != phaseIdle {
					t.Fatalf("phase %s after the recovery", v.phase())
				}
				if sc.check != nil {
					sc.check(t, v, true)
				}
				// And the helper is usable again.
				if sc.components {
					v.place(ver, v.componentRelease(ver, unitRecover).build())
				} else {
					v.stageable(ver)
				}
				wantOK(t, v.stage(ver))
			})
		}
	}
}

// settle runs the staging recovery when the state is not idle.
func settle(t *testing.T, v *tenv) {
	t.Helper()
	st, ok := v.readState()
	if !ok || (st.Txn.Phase == phaseIdle) {
		// Remove a leftover of the scenario setup (a stale newer release) so that wantClean can hold.
		return
	}
	e, code := v.h.open(true)
	if code != "" {
		t.Fatal(code)
	}
	defer e.close()
	if c := e.recoverStaging(st, v.installed); c != "" {
		t.Fatalf("recovery: %s", c)
	}
}

// checkReleaseDir requires releases/<version> to be absent or exactly the four
// release files of the signed release, with their modes.
func checkReleaseDir(t *testing.T, v *tenv, version string) {
	t.Helper()
	dir := filepath.Join(v.root, "releases", version)
	ents, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) == 0 {
		return // an empty stale directory of the scenario setup
	}
	if len(ents) == 1 && ents[0].Name() == "stale-marker" {
		return // the stale release of the setup, not yet replaced
	}
	want := map[string]tf{}
	for _, f := range testFiles(version, true) {
		switch f.name {
		case "oarfish", "oarfish.service", "LICENSE", "THIRD_PARTY_NOTICES.txt":
			want[f.name] = f
		}
	}
	if len(ents) != len(want) {
		t.Fatalf("releases/%s holds %d entries, want the %d release files", version, len(ents), len(want))
	}
	for _, e := range ents {
		w, ok := want[e.Name()]
		if !ok {
			t.Fatalf("unexpected %s", e.Name())
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		fi, _ := os.Stat(filepath.Join(dir, e.Name()))
		if err != nil || string(data) != string(w.data) || fi.Mode().Perm() != os.FileMode(w.mode) {
			t.Fatalf("releases/%s/%s is not the verified file", version, e.Name())
		}
	}
}

func TestCrashSweepStage(t *testing.T) {
	sweepCrashes(t, crashScenario{
		name: "stage",
		setup: func(t *testing.T, v *tenv) string {
			v.stageable("1.1.0")
			return "1.1.0"
		},
		request: func(v *tenv, ver string) []byte { return v.stage(ver) },
	})
}

// The same with a stale newer release in place (the move-aside path).
func TestCrashSweepStageReplacingAStaleRelease(t *testing.T) {
	sweepCrashes(t, crashScenario{
		name:  "stage over a stale release",
		stale: true,
		setup: func(t *testing.T, v *tenv) string {
			os.MkdirAll(filepath.Join(v.root, "releases", "1.1.0"), 0o755)
			os.WriteFile(filepath.Join(v.root, "releases", "1.1.0", "stale-marker"), []byte("old"), 0o644)
			v.stageable("1.1.0")
			return "1.1.0"
		},
		request: func(v *tenv, ver string) []byte { return v.stage(ver) },
	})
}

// The same for a stage that first has to clean an expired staged release.
func TestCrashSweepStageAfterExpiry(t *testing.T) {
	sweepCrashes(t, crashScenario{
		name: "stage after expiry",
		setup: func(t *testing.T, v *tenv) string {
			v.stageable("1.1.0")
			wantOK(t, v.stage("1.1.0"))
			v.uptime.Store(1000 + stageLifetimeS)
			v.stageable("1.2.0")
			return "1.2.0"
		},
		request: func(v *tenv, ver string) []byte { return v.stage(ver) },
	})
}

func TestCrashSweepDiscard(t *testing.T) {
	sweepCrashes(t, crashScenario{
		name: "discard",
		setup: func(t *testing.T, v *tenv) string {
			v.stageable("1.1.0")
			wantOK(t, v.stage("1.1.0"))
			return "1.1.0"
		},
		request: func(v *tenv, ver string) []byte { return v.request(discardLine(ver)) },
	})
}

func TestCrashSweepExpiryByStatus(t *testing.T) {
	sweepCrashes(t, crashScenario{
		name: "expiry",
		setup: func(t *testing.T, v *tenv) string {
			v.stageable("1.1.0")
			wantOK(t, v.stage("1.1.0"))
			v.uptime.Store(1000 + stageLifetimeS)
			return "1.1.0"
		},
		request: func(v *tenv, ver string) []byte { return v.request([]byte(okStatus)) },
	})
}
