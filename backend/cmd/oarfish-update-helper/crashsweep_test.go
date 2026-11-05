package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The crash sweep of G6.6 (ADR 0010, "Evidence and test plan", item 4). A
// scenario runs once to count its calls: every write, sync, directory sync,
// link, rename and removal of the file system and every systemctl call is one
// numbered call. Then, for every k from 1 to N, once with "error at k" (that
// call fails and does nothing, the program goes on) and once with "stop at k"
// (that call and every later one do nothing: power loss). After each run the
// invariant of U7 is checked on the disk as it is, then the next start
// recovers (a reboot after "stop", the start-up recovery of serve after
// "error") and the invariant is checked again, together with a clean tree.

type sweepScenario struct {
	name  string
	setup func(t *testing.T) *tenv
	// run is the code under test; it uses v.h with the gate installed.
	run func(v *tenv)
	// ok: a state after recovery that may name this current.
	finalCurrent map[string]bool
}

// installGate numbers every mutating call of the helper under test.
func installGate(v *tenv, g *faultGate) {
	v.h.wrapFS = func(inner fsops) fsops {
		f := newTestFS(noSyncFS{inner})
		f.before = func(fsOp) error { return g.step() }
		return f
	}
	v.sd.gate = g
}

// checkDisk is the invariant of U7 on the disk as a crash leaves it: current
// names a complete release with the bytes of that version, no directory under a
// version name is incomplete, and state.json (if there is one) parses.
func checkDisk(t *testing.T, v *tenv) string {
	t.Helper()
	cur := v.current()
	if cur == "" {
		t.Fatalf("current is not a link to releases/<version>")
	}
	if !releaseComplete(v.root, cur) {
		t.Fatalf("current names %s, which is not complete", cur)
	}
	if got, _ := os.ReadFile(filepath.Join(v.root, "releases", cur, "oarfish")); string(got) != string(elfStub("oarfish "+cur)) {
		t.Fatalf("current names %s with the wrong binary", cur)
	}
	ents, _ := os.ReadDir(filepath.Join(v.root, "releases"))
	for _, e := range ents {
		if validVersion(e.Name()) && !releaseComplete(v.root, e.Name()) {
			t.Fatalf("releases/%s is incomplete", e.Name())
		}
	}
	if data, err := os.ReadFile(filepath.Join(v.stateDir, stateFile)); err == nil {
		if _, err := decodeState(data); err != nil {
			t.Fatalf("state.json: %v", err)
		}
	}
	return cur
}

func sweepRun(t *testing.T, sc sweepScenario) {
	total := 0
	{
		v := sc.setup(t)
		g := &faultGate{}
		installGate(v, g)
		sc.run(v)
		total = g.count()
		checkDisk(t, v)
	}
	if total < 5 {
		t.Fatalf("%s: only %d calls", sc.name, total)
	}
	t.Logf("%s: %d numbered calls, %d runs", sc.name, total, 2*total)
	for k := 1; k <= total; k++ {
		for _, mode := range []string{"error", "stop"} {
			t.Run(fmt.Sprintf("%s/%s at %d", sc.name, mode, k), func(t *testing.T) {
				v := sc.setup(t)
				g := &faultGate{}
				if mode == "error" {
					g.failAt = k
				} else {
					g.stopAt = k
				}
				installGate(v, g)
				sc.run(v)
				checkDisk(t, v)

				// The next start. The fault is over: a plain file system.
				v.h.wrapFS = nil
				v.sd.gate = nil
				if mode == "stop" {
					if code := v.h.recoverBoot(io.Discard); code != 0 && v.phase() != phaseRollbackFailed {
						t.Fatalf("recover exit %d", code)
					}
				} else {
					v.recoverLive()
				}
				cur := checkDisk(t, v)
				if !sc.finalCurrent[cur] {
					t.Fatalf("after recovery current names %s", cur)
				}
				v.wantNoLeftovers()
				st, ok := v.readState()
				if !ok {
					t.Fatal("no state after recovery")
				}
				switch st.Txn.Phase {
				case phaseIdle, phaseStaged:
				default:
					t.Fatalf("phase %s after recovery", st.Txn.Phase)
				}
				// current names the new version only if the update committed.
				if cur == "1.1.0" && (st.Last == nil || st.Last.Outcome != outcomeSuccess) && st.Txn.Phase != phaseStaged {
					t.Fatalf("current names 1.1.0 without a committed update: %+v", st.Last)
				}
				// A second recovery changes nothing.
				before, _ := os.ReadFile(filepath.Join(v.stateDir, stateFile))
				if mode == "stop" {
					v.recoverBoot()
				} else {
					v.recoverLive()
				}
				after, _ := os.ReadFile(filepath.Join(v.stateDir, stateFile))
				if string(before) != string(after) {
					t.Fatalf("recovery is not idempotent:\n%s\n%s", before, after)
				}
			})
		}
	}
}

func withOldReleases(v *tenv) {
	v.makeRelease("0.8.0")
	v.makeRelease("0.9.0")
}

func TestCrashSweepActivateSuccess(t *testing.T) {
	sweepRun(t, sweepScenario{
		name: "activate",
		setup: func(t *testing.T) *tenv {
			v := newActiveEnv(t)
			withOldReleases(v)
			v.h.syncTxn = true
			return v
		},
		run: func(v *tenv) { v.activate("1.1.0") },
		// An interrupted update rolls back; a committed one stays.
		finalCurrent: map[string]bool{"1.0.0": true, "1.1.0": true},
	})
}

func TestCrashSweepActivateRollback(t *testing.T) {
	sweepRun(t, sweepScenario{
		name: "rollback",
		setup: func(t *testing.T) *tenv {
			v := newActiveEnv(t)
			v.sd.beh["1.1.0"] = bCrash
			v.h.syncTxn = true
			return v
		},
		run:          func(v *tenv) { v.activate("1.1.0") },
		finalCurrent: map[string]bool{"1.0.0": true},
	})
}

func TestCrashSweepRecoverFromVerifying(t *testing.T) {
	sweepRun(t, sweepScenario{
		name: "recover verifying",
		setup: func(t *testing.T) *tenv {
			v := newActiveEnv(t)
			v.crashedAt(phaseVerifying)
			v.pointCurrent("1.1.0")
			os.WriteFile(filepath.Join(v.stateDir, stateTempPrefix+"00000000000000dd"), []byte("{"), 0o600)
			os.Mkdir(filepath.Join(v.root, "releases", ".trash.00000000000000ee"), 0o755)
			return v
		},
		run:          func(v *tenv) { v.h.recoverBoot(io.Discard) },
		finalCurrent: map[string]bool{"1.0.0": true},
	})
}

func TestCrashSweepRecoverFromSwitching(t *testing.T) {
	sweepRun(t, sweepScenario{
		name: "recover switching",
		setup: func(t *testing.T) *tenv {
			v := newActiveEnv(t)
			v.crashedAt(phaseSwitching)
			return v
		},
		run:          func(v *tenv) { v.h.recoverBoot(io.Discard) },
		finalCurrent: map[string]bool{"1.0.0": true},
	})
}

func TestCrashSweepRecoverFromRollingBack(t *testing.T) {
	sweepRun(t, sweepScenario{
		name: "recover rolling_back",
		setup: func(t *testing.T) *tenv {
			v := newActiveEnv(t)
			v.crashedAt(phaseRollingBack)
			v.pointCurrent("1.1.0")
			return v
		},
		run:          func(v *tenv) { v.h.recoverBoot(io.Discard) },
		finalCurrent: map[string]bool{"1.0.0": true},
	})
}

func TestCrashSweepRecoverLive(t *testing.T) {
	sweepRun(t, sweepScenario{
		name: "recover live",
		setup: func(t *testing.T) *tenv {
			v := newActiveEnv(t)
			v.sd.running = "1.1.0"
			v.crashedAt(phaseVerifying)
			v.pointCurrent("1.1.0")
			v.h.syncTxn = true
			return v
		},
		run:          func(v *tenv) { v.recoverLive() },
		finalCurrent: map[string]bool{"1.0.0": true},
	})
}

func TestCrashSweepPruning(t *testing.T) {
	sweepRun(t, sweepScenario{
		name: "pruning",
		setup: func(t *testing.T) *tenv {
			v := newActiveEnv(t)
			withOldReleases(v)
			v.makeRelease("1.1.0")
			v.pointCurrent("1.1.0")
			st := freshState("test-1")
			st.Txn = txn{ID: "00000000000000c1", Phase: phaseCommitted, To: "1.1.0", From: "1.0.0", CorrelationID: testCID}
			st.Previous = "1.0.0"
			st.Last = &lastResult{TransactionID: "00000000000000c1", To: "1.1.0", From: "1.0.0", Outcome: outcomeSuccess}
			v.writeState(st)
			return v
		},
		run:          func(v *tenv) { v.h.recoverBoot(io.Discard) },
		finalCurrent: map[string]bool{"1.1.0": true},
	})
}

func TestCrashSweepStagedExpiry(t *testing.T) {
	sweepRun(t, sweepScenario{
		name: "staged at boot",
		setup: func(t *testing.T) *tenv {
			return newActiveEnv(t)
		},
		run:          func(v *tenv) { v.h.recoverBoot(io.Discard) },
		finalCurrent: map[string]bool{"1.0.0": true},
	})
}

// After a sweep run that stopped the update halfway, the previous healthy
// version and the installed one are both still complete (U5): the sweep above
// checks that for current; this checks the version pair.
func TestSweepNeverLeavesTheOnlyHealthyCopyIncomplete(t *testing.T) {
	v := newActiveEnv(t)
	g := &faultGate{}
	installGate(v, g)
	v.activate("1.1.0")
	if !releaseComplete(v.root, "1.0.0") || !releaseComplete(v.root, "1.1.0") {
		t.Fatal("a release is incomplete")
	}
	if strings.Count(v.journal.String(), "phase=") < 4 {
		t.Fatal("no journal")
	}
}
