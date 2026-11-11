package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The recovery tests set the disk and the state to what a crash leaves (one
// test per row of the U7 table, and the rows of U13 that the logic covers) and
// run the recover subcommand (boot) or the start-up recovery of serve (live).

// makeRelease writes a complete release directory.
func (v *tenv) makeRelease(version string) {
	v.t.Helper()
	dir := filepath.Join(v.root, "releases", version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		v.t.Fatal(err)
	}
	for n, d := range map[string][]byte{"oarfish": elfStub("oarfish " + version), "oarfish.service": baseServiceUnit} {
		if err := os.WriteFile(filepath.Join(dir, n), d, 0o755); err != nil {
			v.t.Fatal(err)
		}
	}
}

func (v *tenv) pointCurrent(version string) {
	v.t.Helper()
	tmp := filepath.Join(v.root, "tmp-link")
	os.Remove(tmp)
	if err := os.Symlink("releases/"+version, tmp); err != nil {
		v.t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(v.root, "current")); err != nil {
		v.t.Fatal(err)
	}
}

func fromHash(v *tenv) string {
	sum, err := (&env{opt: mustRoot(v.t, v.root)}).binaryHash(v.installed)
	if err != nil {
		v.t.Fatal(err)
	}
	return sum
}

// crashedAt writes a state as a crash leaves it: a transaction 1.0.0 -> 1.1.0
// in the given phase.
func (v *tenv) crashedAt(phase string) {
	v.t.Helper()
	st := freshState("test-1")
	st.Txn = txn{ID: "00000000000000c1", Phase: phase, To: "1.1.0", From: "1.0.0", FromBinarySHA256: fromHash(v),
		CorrelationID: testCID, StagedAtUptimeS: 900}
	v.writeState(st)
}

func (v *tenv) recoverBoot() int {
	var b bytes.Buffer
	return v.h.recoverBoot(&b)
}

// recoverLive runs the start-up recovery of serve, inline.
func (v *tenv) recoverLive() {
	v.h.syncTxn = true
	v.h.startupRecovery()
	v.h.wait()
}

func (v *tenv) wantIdleWith(outcome, reason string) {
	v.t.Helper()
	st, ok := v.readState()
	if !ok {
		v.t.Fatal("no state")
	}
	if st.Txn.Phase != phaseIdle || st.Last == nil || st.Last.Outcome != outcome || st.Last.ReasonCode != reason {
		v.t.Fatalf("phase %s last %+v", st.Txn.Phase, st.Last)
	}
}

func (v *tenv) wantNoLeftovers() {
	v.t.Helper()
	for _, dir := range []string{v.root, filepath.Join(v.root, "releases"), v.stateDir} {
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			if isLeftover(e.Name()) || strings.HasPrefix(e.Name(), stateTempPrefix) {
				v.t.Fatalf("%s holds %s", dir, e.Name())
			}
		}
	}
}

// U7 row 2.
func TestRecoverRow2Staging(t *testing.T) {
	for _, mode := range []string{"boot", "live"} {
		t.Run(mode, func(t *testing.T) {
			v := newActiveEnv(t)
			os.RemoveAll(filepath.Join(v.root, "releases", "1.1.0"))
			os.MkdirAll(filepath.Join(v.root, "releases", ".stage.00000000000000aa", "release"), 0o700)
			os.WriteFile(filepath.Join(v.root, "releases", ".stage.00000000000000aa", "release", "oarfish"), []byte("half"), 0o600)
			v.crashedAt(phaseStaging)
			doRecover(t, v, mode)
			v.wantIdleWith(outcomeAborted, reasonInterrupted)
			v.wantClean()
			if v.current() != "1.0.0" {
				t.Fatal("current moved")
			}
		})
	}
}

func doRecover(t *testing.T, v *tenv, mode string) {
	t.Helper()
	if mode == "boot" {
		if code := v.recoverBoot(); code != 0 {
			t.Fatalf("recover exit %d", code)
		}
		return
	}
	v.recoverLive()
}

// U7 row 3: the rename happened, staged is not durable.
func TestRecoverRow3RenamedButNotStaged(t *testing.T) {
	for _, mode := range []string{"boot", "live"} {
		t.Run(mode, func(t *testing.T) {
			v := newActiveEnv(t) // 1.1.0 is in releases/
			v.crashedAt(phaseStaging)
			doRecover(t, v, mode)
			v.wantIdleWith(outcomeAborted, reasonInterrupted)
			v.wantClean()
		})
	}
	t.Run("the installed and the previous version are never removed", func(t *testing.T) {
		v := newActiveEnv(t)
		v.makeRelease("0.9.0")
		v.crashedAt(phaseStaging)
		st, _ := v.readState()
		st.Txn.To = "0.9.0"
		st.Previous = "0.9.0"
		v.writeState(st)
		doRecover(t, v, "boot")
		if !releaseComplete(v.root, "0.9.0") || !releaseComplete(v.root, "1.0.0") {
			t.Fatal("removed a protected release")
		}
	})
}

// U7 row 4: a staged release does not outlive a reboot; a running helper
// removes one that has expired and leaves a valid one alone.
func TestRecoverRow4Staged(t *testing.T) {
	t.Run("boot removes it", func(t *testing.T) {
		v := newActiveEnv(t)
		doRecover(t, v, "boot")
		v.wantIdleWith(outcomeAborted, reasonStageExpired)
		v.wantClean()
	})
	t.Run("live, still valid", func(t *testing.T) {
		v := newActiveEnv(t)
		v.recoverLive()
		if v.phase() != phaseStaged || !releaseComplete(v.root, "1.1.0") {
			t.Fatal("removed a valid staged release")
		}
	})
	t.Run("live, expired", func(t *testing.T) {
		v := newActiveEnv(t)
		v.uptime.Add(stageLifetimeS)
		st, _ := v.readState()
		v.writeState(st)
		// needsRecovery does not look at the age; the next request or status does.
		wantErrCode(t, v.activate("1.1.0"), codeStageExpired)
	})
}

// U7 row 5.
func TestRecoverRow5SwitchingBeforeTheRename(t *testing.T) {
	for _, mode := range []string{"boot", "live"} {
		t.Run(mode, func(t *testing.T) {
			v := newActiveEnv(t)
			v.crashedAt(phaseSwitching)
			doRecover(t, v, mode)
			v.wantIdleWith(outcomeAborted, reasonInterrupted)
			v.wantClean()
			if v.current() != "1.0.0" || v.sd.count("restart") != 0 {
				t.Fatal("touched the running version")
			}
		})
	}
}

// U7 rows 6 to 8: current names to; the update has not reached committed.
func TestRecoverRows6To8RollBack(t *testing.T) {
	for _, phase := range []string{phaseSwitching, phaseVerifying, phaseRollingBack} {
		for _, mode := range []string{"boot", "live"} {
			t.Run(phase+"/"+mode, func(t *testing.T) {
				v := newActiveEnv(t)
				v.sd.running = "1.1.0"
				v.crashedAt(phase)
				v.pointCurrent("1.1.0")
				doRecover(t, v, mode)
				v.wantIdleWith(outcomeRolledBack, reasonInterrupted)
				if v.current() != "1.0.0" {
					t.Fatalf("current %q", v.current())
				}
				if !releaseComplete(v.root, "1.1.0") {
					t.Fatal("pruned after a rollback")
				}
				// At boot the service is not running, so a rollback is a link
				// switch; later it is the full procedure of U6.
				restarts := v.sd.count("restart")
				if mode == "boot" && restarts != 0 || mode == "live" && restarts != 1 {
					t.Fatalf("%s: %d restarts", mode, restarts)
				}
				st, _ := v.readState()
				if st.Cool.RefuseVersion != "1.1.0" {
					t.Fatal("no cooldown after an interrupted update")
				}
			})
		}
	}
}

// Row 7 with current already on from: the rollback switch had happened.
func TestRecoverVerifyingWithCurrentOnFrom(t *testing.T) {
	v := newActiveEnv(t)
	v.crashedAt(phaseVerifying)
	doRecover(t, v, "boot")
	v.wantIdleWith(outcomeRolledBack, reasonInterrupted)
}

// U7 row 9.
func TestRecoverRow9FinishesPruning(t *testing.T) {
	v := newActiveEnv(t)
	v.makeRelease("0.8.0")
	v.makeRelease("0.9.0")
	v.makeRelease("1.1.0")
	os.MkdirAll(filepath.Join(v.root, "releases", ".trash.00000000000000ee", "x"), 0o755)
	os.Mkdir(filepath.Join(v.root, "releases", "keepme"), 0o755)
	v.pointCurrent("1.1.0")
	st := freshState("test-1")
	st.Txn = txn{ID: "00000000000000c1", Phase: phaseCommitted, To: "1.1.0", From: "1.0.0", CorrelationID: testCID}
	st.Previous = "1.0.0"
	st.Last = &lastResult{TransactionID: "00000000000000c1", To: "1.1.0", From: "1.0.0", Outcome: outcomeSuccess}
	v.writeState(st)
	doRecover(t, v, "boot")
	if got := v.releasesEntries(); strings.Join(got, ",") != "1.0.0,1.1.0,keepme" {
		t.Fatalf("releases/ holds %v", got)
	}
	got, _ := v.readState()
	if got.Txn.Phase != phaseIdle || got.Last.Outcome != outcomeSuccess || got.Previous != "1.0.0" {
		t.Fatalf("state %+v", got)
	}
}

// U7 row 10 and U13 F14.
func TestRecoverRow10RollbackHash(t *testing.T) {
	t.Run("hash equal", func(t *testing.T) {
		v := newActiveEnv(t)
		v.crashedAt(phaseRollingBack)
		doRecover(t, v, "boot")
		v.wantIdleWith(outcomeRolledBack, reasonInterrupted)
	})
	t.Run("hash different", func(t *testing.T) {
		v := newActiveEnv(t)
		v.crashedAt(phaseRollingBack)
		v.pointCurrent("1.1.0")
		os.WriteFile(filepath.Join(v.root, "releases", "1.0.0", "oarfish"), elfStub("damaged"), 0o755)
		if code := v.recoverBoot(); code != 1 {
			t.Fatalf("exit %d: a failed rollback must show as a failed unit", code)
		}
		st, _ := v.readState()
		if st.Txn.Phase != phaseRollbackFailed || !st.NeedsAttention || st.Last.ReasonCode != reasonTargetDamaged || v.current() != "1.1.0" {
			t.Fatalf("state %+v current %q", st.Txn, v.current())
		}
	})
	t.Run("current names neither", func(t *testing.T) {
		v := newActiveEnv(t)
		v.makeRelease("2.0.0")
		v.crashedAt(phaseRollingBack)
		v.pointCurrent("2.0.0")
		v.recoverBoot()
		if st, _ := v.readState(); st.Txn.Phase != phaseRollbackFailed || st.Last.ReasonCode != reasonCurrentUnknown || v.current() != "2.0.0" {
			t.Fatalf("state %+v", st.Txn)
		}
	})
}

// A failed rollback is looked at again at boot; a running helper leaves it.
func TestRecoverRollbackFailed(t *testing.T) {
	setup := func(t *testing.T, repaired bool) *tenv {
		v := newActiveEnv(t)
		v.crashedAt(phaseRollbackFailed)
		st, _ := v.readState()
		st.NeedsAttention = true
		v.writeState(st)
		if !repaired {
			os.WriteFile(filepath.Join(v.root, "releases", "1.0.0", "oarfish"), elfStub("damaged"), 0o755)
		}
		return v
	}
	t.Run("boot, target fine", func(t *testing.T) {
		v := setup(t, true)
		if code := v.recoverBoot(); code != 0 {
			t.Fatalf("exit %d", code)
		}
		st, _ := v.readState()
		if st.Txn.Phase != phaseIdle || !st.NeedsAttention {
			t.Fatalf("state %+v", st)
		}
	})
	t.Run("boot, target still damaged", func(t *testing.T) {
		v := setup(t, false)
		if code := v.recoverBoot(); code != 1 {
			t.Fatalf("exit %d", code)
		}
		if v.phase() != phaseRollbackFailed {
			t.Fatal("cleared a failed rollback")
		}
	})
	t.Run("live", func(t *testing.T) {
		v := setup(t, true)
		v.recoverLive()
		if v.phase() != phaseRollbackFailed {
			t.Fatal("a running helper cleared a failed rollback")
		}
	})
}

// E6: the recover unit started by OnFailure= (MONITOR_UNIT) leaves every
// release phase as it is and exits 0, because oarfish.service may be running
// and a switch of current without a restart would leave it running a binary
// that current no longer names. The next serve start rolls back with a
// restart; at boot (no MONITOR_UNIT) the link-only rule is unchanged.
func TestRecoverStartedByOnFailureLeavesReleasePhases(t *testing.T) {
	type tc struct {
		phase   string
		current string
		last    *lastResult
		// after the next serve start
		wantPhase, wantOutcome, wantCurrent string
		wantRestarts                        int
	}
	failed := &lastResult{TransactionID: "00000000000000c1", To: "1.1.0", From: "1.0.0", Outcome: outcomeRollbackFailed, ReasonCode: reasonSwitchFailed}
	success := &lastResult{TransactionID: "00000000000000c1", To: "1.1.0", From: "1.0.0", Outcome: outcomeSuccess}
	for _, c := range []tc{
		{phaseSwitching, "1.1.0", nil, phaseIdle, outcomeRolledBack, "1.0.0", 1},
		{phaseVerifying, "1.1.0", nil, phaseIdle, outcomeRolledBack, "1.0.0", 1},
		{phaseRollingBack, "1.1.0", nil, phaseIdle, outcomeRolledBack, "1.0.0", 1},
		{phaseRollbackFailed, "1.1.0", failed, phaseIdle, outcomeRolledBack, "1.0.0", 1},
		{phaseCommitted, "1.1.0", success, phaseIdle, outcomeSuccess, "1.1.0", 0},
		{phaseRolledBack, "1.0.0", nil, phaseIdle, "", "1.0.0", 0},
	} {
		t.Run(c.phase, func(t *testing.T) {
			v := newActiveEnv(t)
			v.sd.running = c.current
			v.crashedAt(c.phase)
			if c.last != nil {
				st, _ := v.readState()
				st.Last = c.last
				v.writeState(st)
			}
			v.pointCurrent(c.current)
			v.sd.calls = nil
			beforeTree, beforeState := snapshot(t, v.root), snapshot(t, v.stateDir)

			v.h.onFailure = true
			if code := v.recoverBoot(); code != 0 {
				t.Fatalf("exit %d, want 0", code)
			}
			if d := diffSnapshots(beforeTree, snapshot(t, v.root)); len(d) != 0 {
				t.Fatalf("the tree changed: %v", d)
			}
			if d := diffSnapshots(beforeState, snapshot(t, v.stateDir)); len(d) != 0 {
				t.Fatalf("the state changed: %v", d)
			}
			if len(v.sd.Calls()) != 0 || v.current() != c.current {
				t.Fatalf("systemctl %v current %s", v.sd.Calls(), v.current())
			}

			// The next serve start: the full procedure of U6.
			old := v.h
			v.h = v.newHandler(linkActive)
			v.h.sysrun, v.h.healthGet, v.h.sleep = old.sysrun, old.healthGet, old.sleep
			v.recoverLive()
			st, _ := v.readState()
			if st.Txn.Phase != c.wantPhase || v.current() != c.wantCurrent || v.sd.count("restart") != c.wantRestarts {
				t.Fatalf("after serve: phase %s current %s restarts %d", st.Txn.Phase, v.current(), v.sd.count("restart"))
			}
			if c.wantOutcome != "" && (st.Last == nil || st.Last.Outcome != c.wantOutcome) {
				t.Fatalf("after serve: last %+v", st.Last)
			}
		})
	}
	t.Run("a component phase is handled as before", func(t *testing.T) {
		v := newActiveEnv(t)
		v.crashedAt(phaseCRollingBack)
		st, _ := v.readState()
		st.Txn.ComponentsTo, st.Txn.ComponentsFrom = "1.0.0", "1.0.0"
		v.writeState(st)
		v.h.onFailure = true
		if code := v.recoverBoot(); code != 0 {
			t.Fatalf("exit %d", code)
		}
		v.wantIdleWith(outcomeComponentsRolledBack, reasonInterrupted)
	})
	t.Run("without MONITOR_UNIT the boot rule is unchanged", func(t *testing.T) {
		v := newActiveEnv(t)
		v.sd.running = "1.1.0"
		v.crashedAt(phaseVerifying)
		v.pointCurrent("1.1.0")
		v.sd.calls = nil
		if code := v.recoverBoot(); code != 0 {
			t.Fatalf("exit %d", code)
		}
		v.wantIdleWith(outcomeRolledBack, reasonInterrupted)
		if v.current() != "1.0.0" || v.sd.count("restart") != 0 {
			t.Fatalf("current %s restarts %d", v.current(), v.sd.count("restart"))
		}
	})
	t.Run("recoverMain reads MONITOR_UNIT", func(t *testing.T) {
		v := newActiveEnv(t)
		v.crashedAt(phaseVerifying)
		v.pointCurrent("1.1.0")
		before := snapshot(t, v.stateDir)
		o := testOptions(v.root)
		o.stateDir, o.cacheDir, o.journal = v.stateDir, v.cacheDir, v.journal
		o.getenv = func(k string) string {
			if k == monitorUnitVar {
				return helperUnit
			}
			return ""
		}
		if code := recoverMain(io.Discard, o); code != 0 || v.current() != "1.1.0" {
			t.Fatalf("exit %d current %s", code, v.current())
		}
		if d := diffSnapshots(before, snapshot(t, v.stateDir)); len(d) != 0 {
			t.Fatalf("the state changed: %v", d)
		}
	})
}

// U7 row 11 and 12.
func TestRecoverStateFile(t *testing.T) {
	t.Run("temporary state files are deleted", func(t *testing.T) {
		v := newActiveEnv(t)
		os.WriteFile(filepath.Join(v.stateDir, stateTempPrefix+"00000000000000dd"), []byte("{"), 0o600)
		before, _ := os.ReadFile(filepath.Join(v.stateDir, stateFile))
		doRecover(t, v, "boot")
		v.wantNoLeftovers()
		// the staged state itself was the previous phase; boot removes the staged release
		_ = before
	})
	for _, mode := range []string{"boot", "live"} {
		for name, content := range map[string]string{"garbage": "not json", "empty": "", "wrong schema": `{"schema_version":2}`, "unknown phase": `{"schema_version":1,"transaction":{"phase":"nonsense"}}`} {
			t.Run("unreadable "+name+"/"+mode, func(t *testing.T) {
				v := newActiveEnv(t)
				os.WriteFile(filepath.Join(v.stateDir, stateFile), []byte(content), 0o600)
				os.MkdirAll(filepath.Join(v.root, "releases", ".stage.00000000000000aa"), 0o700)
				doRecover(t, v, mode)
				st, ok := v.readState()
				if !ok || !st.NeedsAttention || st.Txn.Phase != phaseIdle || st.Last == nil || st.Last.ReasonCode != reasonStateRebuilt || st.Previous != "" {
					t.Fatalf("state %+v", st)
				}
				v.wantNoLeftovers()
				if v.current() != "1.0.0" {
					t.Fatal("current moved")
				}
				if !v.status().NeedsAttention {
					t.Fatal("status does not report it")
				}
				// reported once: the ack clears it
				wantOK(t, v.request(ackLine(st.Last.TransactionID)))
				if v.status().NeedsAttention {
					t.Fatal("still reported after the ack")
				}
			})
		}
	}
	t.Run("missing is a fresh state, not an alarm", func(t *testing.T) {
		v := newTenv(t)
		v.fakeSystemd()
		if code := v.recoverBoot(); code != 0 {
			t.Fatalf("exit %d", code)
		}
		if v.status().NeedsAttention {
			t.Fatal("a fresh install needs no attention")
		}
	})
}

// A component phase whose state names no component set can not be matched with
// the table of U15 rule 4: nothing is touched and a person has to look.
func TestRecoverLeavesAComponentPhaseWithoutASetAlone(t *testing.T) {
	v := newActiveEnv(t)
	v.crashedAt(phaseCSwitched)
	if code := v.recoverBoot(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	st, _ := v.readState()
	if st.Txn.Phase != phaseCSwitched || !st.NeedsAttention || !releaseComplete(v.root, "1.1.0") {
		t.Fatalf("state %+v", st)
	}
}

func TestRecoverInstalledUnknown(t *testing.T) {
	for _, phase := range []string{phaseStaging, phaseStaged} {
		v := newActiveEnv(t)
		if phase == phaseStaging {
			v.crashedAt(phase)
		}
		os.Remove(filepath.Join(v.root, "current"))
		if code := v.recoverBoot(); code != 0 {
			t.Fatalf("%s: exit %d", phase, code)
		}
		if !releaseComplete(v.root, "1.1.0") || !releaseComplete(v.root, "1.0.0") {
			t.Fatalf("%s: removed a release with an unknown current", phase)
		}
		if st, _ := v.readState(); !st.NeedsAttention {
			t.Fatalf("%s: no needs_attention", phase)
		}
	}
	t.Run("switching with current unknown", func(t *testing.T) {
		v := newActiveEnv(t)
		v.crashedAt(phaseSwitching)
		os.Remove(filepath.Join(v.root, "current"))
		v.recoverBoot()
		if st, _ := v.readState(); st.Txn.Phase != phaseRollbackFailed || st.Last.ReasonCode != reasonCurrentUnknown {
			t.Fatalf("state %+v", st.Txn)
		}
	})
}

// The recovery holds the lock and answers busy while it runs; when the lock is
// taken it does nothing.
func TestRecoverNeedsTheLock(t *testing.T) {
	v := newActiveEnv(t)
	v.crashedAt(phaseVerifying)
	v.pointCurrent("1.1.0")
	lk, err := lockDir(v.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if code := v.h.recoverBoot(&b); code != 1 || b.String() != "oarfish-update-helper: recovery did not run: The helper is busy with another operation.\n" {
		t.Fatalf("exit %d %q", code, b.String())
	}
	v.recoverLive()
	if v.phase() != phaseVerifying || v.current() != "1.1.0" {
		t.Fatal("recovered without the lock")
	}
	lk.release()
	v.recoverBoot()
	v.wantIdleWith(outcomeRolledBack, reasonInterrupted)
}

// serve starts a transaction it finds in the state under the lock; requests
// meanwhile are busy, and the helper does not exit.
func TestStartupRecoveryRunsInTheBackground(t *testing.T) {
	v := newActiveEnv(t)
	v.h.syncTxn = false
	v.sd.running = "1.1.0"
	v.crashedAt(phaseVerifying)
	v.pointCurrent("1.1.0")
	release := make(chan struct{})
	v.sd.restartHook = release
	v.h.startupRecovery()
	if v.h.running.Load() != 1 {
		t.Fatal("no transaction running")
	}
	wantErrCode(t, v.stage("1.2.0"), codeBusy)
	wantErrCode(t, v.activate("1.1.0"), codeBusy)
	close(release)
	v.h.wait()
	if v.h.running.Load() != 0 {
		t.Fatal("still running")
	}
	v.wantIdleWith(outcomeRolledBack, reasonInterrupted)
}

// activate answers at once; the transaction continues without the caller and
// holds the lock for its whole run.
func TestActivateReturnsAtOnceAndHoldsTheLock(t *testing.T) {
	v := newActiveEnv(t)
	v.h.syncTxn = false
	release := make(chan struct{})
	reached := make(chan struct{}, 1)
	v.sd.onRestart = func() { reached <- struct{}{} }
	v.sd.restartHook = release
	r := wantOK(t, v.activate("1.1.0"))
	if r.Status != "accepted" || r.TransactionID == "" {
		t.Fatalf("answer %+v", r)
	}
	<-reached
	if v.h.running.Load() != 1 {
		t.Fatal("not counted as running")
	}
	wantErrCode(t, v.stage("1.2.0"), codeBusy)
	wantErrCode(t, v.activate("1.1.0"), codeBusy)
	wantErrCode(t, v.request(discardLine("1.1.0")), codeBusy)
	// status needs no lock and shows the phase.
	if s := v.status(); s.Phase != phaseVerifying {
		t.Fatalf("status phase %s", s.Phase)
	}
	close(release)
	v.h.wait()
	if v.current() != "1.1.0" || v.h.running.Load() != 0 {
		t.Fatal("did not finish")
	}
	// The lock is free again.
	lk, err := lockDir(v.stateDir)
	if err != nil {
		t.Fatalf("lock still held: %v", err)
	}
	lk.release()
}
