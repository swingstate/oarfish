package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The crash sweep of the component transaction, across the process boundary
// (ADR 0010, "Evidence and test plan", item 4; U15 rule 4 rows c1 to c9). A
// scenario runs once to count its numbered calls: every write, sync, directory
// sync, link, rename and removal of the file system and every acting systemctl
// call (daemon-reload, restart, reset-failed) of every process of the helper
// that takes part. Then, for every k from 1 to N, once with "error at k" (that
// call fails, the programs go on) and once with "stop at k" (that call and
// every later one do nothing: power loss). After each run the invariant of U7
// is checked on the disk as it is: current names a complete release, the two
// component links name complete sets, and no release or component directory
// under a version name is incomplete. Then the next start recovers (a reboot
// after "stop": the recover unit and then serve; the next start of serve after
// "error") and must reach a final state of the table: both links on one set,
// nothing left behind, and a second recovery that changes nothing.

type componentSweep struct {
	name  string
	setup func(t *testing.T) (*tenv, *supervisor)
	run   func(v *tenv, s *supervisor)
}

func activateAndSettle(v *tenv, s *supervisor) {
	v.confirm(newVersion)
	s.settle()
}

func sweepComponentRun(t *testing.T, sc componentSweep) {
	total := 0
	{
		v, s := sc.setup(t)
		g := &faultGate{}
		installGate(v, g)
		sc.run(v, s)
		total = g.count()
		checkSettled(t, v, false)
	}
	if total < 5 {
		t.Fatalf("%s: only %d calls", sc.name, total)
	}
	t.Logf("%s: %d numbered calls, %d runs", sc.name, total, 2*total)
	for k := 1; k <= total; k++ {
		for _, mode := range []string{"error", "stop"} {
			t.Run(fmt.Sprintf("%s/%s at %d", sc.name, mode, k), func(t *testing.T) {
				// Every run has its own environment, so the runs of one scenario
				// go in parallel; the sweep is the longest part of the package.
				t.Parallel()
				v, s := sc.setup(t)
				g := &faultGate{}
				if mode == "error" {
					g.failAt = k
				} else {
					g.stopAt = k
				}
				installGate(v, g)
				sc.run(v, s)
				checkComponentDisk(t, v)

				// The next start. The fault is over: a file system that works.
				v.h.wrapFS = withoutSync
				v.sd.gate = nil
				next := s.nextStart
				if mode == "stop" {
					next = func() {
						if code := s.reboot(); code != 0 {
							t.Fatalf("recover exit %d", code)
						}
					}
				}
				next()
				checkSettled(t, v, mode == "error")
				before, _ := os.ReadFile(filepath.Join(v.stateDir, stateFile))
				next()
				after, _ := os.ReadFile(filepath.Join(v.stateDir, stateFile))
				if string(before) != string(after) {
					t.Fatalf("recovery is not idempotent:\n%s\n%s", before, after)
				}
				checkSettled(t, v, mode == "error")
			})
		}
	}
}

// The whole transaction: the old helper, exit 75, the new helper, the release
// phases (rows c2 to c7 and c9, and the rows of U7 behind them).
func TestCrashSweepComponentTransaction(t *testing.T) {
	sweepComponentRun(t, componentSweep{
		name: "component transaction",
		setup: func(t *testing.T) (*tenv, *supervisor) {
			v, s := newComponentEnv(t, unitService)
			v.makeSet("0.8.0") // something to prune
			return v, s
		},
		run: activateAndSettle,
	})
}

// The new helper fails its self-check and rolls back (row c8).
func TestCrashSweepComponentRollback(t *testing.T) {
	sweepComponentRun(t, componentSweep{
		name: "component rollback",
		setup: func(t *testing.T) (*tenv, *supervisor) {
			v, s := newComponentEnv(t)
			s.mutate = func(h *handler, image string) {
				if isNewImage(image) {
					h.keyID = otherKeyID
				}
			}
			return v, s
		},
		run: activateAndSettle,
	})
}

// The components commit and the release then fails (F31).
func TestCrashSweepComponentsThenReleaseFails(t *testing.T) {
	sweepComponentRun(t, componentSweep{
		name: "components, then a failing release",
		setup: func(t *testing.T) (*tenv, *supervisor) {
			v, s := newComponentEnv(t)
			v.sd.beh[newVersion] = bCrash
			return v, s
		},
		run: activateAndSettle,
	})
}

// The new helper does not start; the recover unit rolls back (F27).
func TestCrashSweepNewHelperDoesNotStart(t *testing.T) {
	sweepComponentRun(t, componentSweep{
		name: "new helper does not start",
		setup: func(t *testing.T) (*tenv, *supervisor) {
			v, s := newComponentEnv(t)
			s.crash = map[string]bool{string(elfStub("helper " + newVersion)): true}
			return v, s
		},
		run: activateAndSettle,
	})
}

// The recover unit at boot, from each component phase that a crash can leave.
func TestCrashSweepRecoverComponentPhases(t *testing.T) {
	for _, c := range []struct{ phase, active, knownGood string }{
		{phaseCSwitching, "1.0.0", "1.0.0"},
		{phaseCSwitching, newVersion, "1.0.0"},
		{phaseCSwitched, newVersion, "1.0.0"},
		{phaseCVerified, newVersion, "1.0.0"},
		{phaseCVerified, newVersion, newVersion},
		{phaseCRollingBack, newVersion, "1.0.0"},
	} {
		sweepComponentRun(t, componentSweep{
			name: fmt.Sprintf("recover %s active %s known-good %s", c.phase, c.active, c.knownGood),
			setup: func(t *testing.T) (*tenv, *supervisor) {
				v, s := atPhase(t, c.phase, c.active, c.knownGood)
				v.sd.calls = nil
				return v, s
			},
			run: func(v *tenv, s *supervisor) { s.recover(false) },
		})
	}
}

// stage with a component set: the set and the release are moved into place
// (row c1, and U7 rows 2 and 3).
func TestCrashSweepStageWithAComponentSet(t *testing.T) {
	sweepCrashes(t, crashScenario{
		name: "stage with a component set",
		setup: func(t *testing.T, v *tenv) string {
			v.place(newVersion, v.componentRelease(newVersion, unitRecover).build())
			return newVersion
		},
		request: func(v *tenv, ver string) []byte { return v.stage(ver) },
		check: func(t *testing.T, v *tenv, recovered bool) {
			for _, l := range []string{linkActive, linkKnownGood} {
				if v.link(l) != "1.0.0" {
					t.Fatalf("stage moved components/%s", l)
				}
			}
			if v.exists("components/"+newVersion) && !setComplete(v, newVersion) {
				t.Fatal("components/1.1.0 is incomplete")
			}
			if recovered && v.exists("components/"+newVersion) {
				t.Fatal("the unreferenced set stays after the recovery")
			}
		},
		components: true,
	})
}
