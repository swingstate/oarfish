package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestActivateSuccess(t *testing.T) {
	v := newActiveEnv(t)
	for _, old := range []string{"0.8.0", "0.9.0", "0.7.0-rc.1"} {
		os.MkdirAll(filepath.Join(v.root, "releases", old), 0o755)
	}
	os.WriteFile(filepath.Join(v.root, "releases", "notes"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(v.root, "releases", "backup.d"), 0o755)
	st, _ := v.readState()
	st.Previous = "0.9.0"
	v.writeState(st)

	r := wantOK(t, v.activate("1.1.0"))
	if r.Status != "accepted" || r.TransactionID != "00000000000a0001" {
		t.Fatalf("answer %+v", r)
	}
	if v.current() != "1.1.0" || !releaseComplete(v.root, "1.1.0") {
		t.Fatalf("current %q", v.current())
	}
	got, _ := v.readState()
	if got.Txn.Phase != phaseIdle || got.Previous != "1.0.0" || got.Last == nil || got.Last.Outcome != outcomeSuccess ||
		got.Last.To != "1.1.0" || got.Last.From != "1.0.0" || got.Last.TransactionID != r.TransactionID || got.Last.Acknowledged ||
		got.Last.CorrelationID != testCID {
		t.Fatalf("state %+v last %+v", got, got.Last)
	}
	// Retention: the active and the previous healthy release stay; every other
	// directory with a version name goes; other names are never touched.
	want := []string{"1.0.0", "1.1.0", "backup.d", "notes"}
	if ents := v.releasesEntries(); !reflect.DeepEqual(ents, want) {
		t.Fatalf("releases/ holds %v, want %v", ents, want)
	}
	// The journal has one line per phase.
	log := v.journal.String()
	for _, want := range []string{
		"phase=switching to=1.1.0 from=1.0.0 cid=" + testCID + " result=ok",
		"phase=verifying to=1.1.0 from=1.0.0 cid=" + testCID + " result=ok",
		"phase=committed to=1.1.0 from=1.0.0 cid=" + testCID + " result=ok",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("journal lacks %q:\n%s", want, log)
		}
	}
	if v.sd.count("restart") != 1 || v.sd.count("reset-failed") != 0 {
		t.Fatalf("calls %v", v.sd.Calls())
	}
}

// The state names switching, with the hash of the running binary, before the
// link moves; verifying before the restart.
func TestActivateWritesTheIntentBeforeEachAction(t *testing.T) {
	v := newActiveEnv(t)
	var seen []string
	v.h.wrapFS = func(inner fsops) fsops {
		f := newTestFS(inner)
		f.before = func(op fsOp) error {
			if op.Kind == "rename" && op.Path2 == "current" {
				st, _ := v.readState()
				seen = append(seen, "link:"+st.Txn.Phase+":"+st.Txn.FromBinarySHA256[:8])
			}
			return nil
		}
		return f
	}
	v.sd.onRestart = func() {
		st, _ := v.readState()
		seen = append(seen, "restart:"+st.Txn.Phase)
	}
	wantOK(t, v.activate("1.1.0"))
	sum, _ := (&env{opt: mustRoot(t, v.root)}).binaryHash("1.0.0")
	if len(seen) != 2 || seen[0] != "link:switching:"+sum[:8] || seen[1] != "restart:verifying" {
		t.Fatalf("seen %v", seen)
	}
}

func mustRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestRollbackAfterAStartFailure(t *testing.T) {
	v := newActiveEnv(t)
	v.sd.beh["1.1.0"] = bCrash
	wantOK(t, v.activate("1.1.0"))
	if v.current() != "1.0.0" {
		t.Fatalf("current %q", v.current())
	}
	st, _ := v.readState()
	l := st.Last
	if st.Txn.Phase != phaseIdle || l == nil || l.Outcome != outcomeRolledBack || l.ReasonCode != reasonStartFailed || st.Previous != "" {
		t.Fatalf("state %+v last %+v", st.Txn, l)
	}
	// Fail fast: well under the 45 s deadline.
	if v.mono.Load() > int64(20*time.Second) { // 1 s to see the failure, then the old version proves itself for 11 s
		t.Fatalf("took %v", time.Duration(v.mono.Load()))
	}
	// Nothing is pruned after a rollback; the tried release stays.
	if !releaseComplete(v.root, "1.1.0") {
		t.Fatal("the tried release was removed")
	}
	if v.sd.count("reset-failed") != 1 || v.sd.count("restart") != 2 {
		t.Fatalf("calls %v", v.sd.Calls())
	}
	if !strings.Contains(v.journal.String(), "phase=rolled_back to=1.1.0 from=1.0.0 cid="+testCID+" result=start_failed") {
		t.Fatalf("journal:\n%s", v.journal.String())
	}
	if v.status().NeedsAttention {
		t.Fatal("a rolled back update needs no attention")
	}
}

func TestRollbackAfterTheDeadline(t *testing.T) {
	for _, b := range []behavior{bHang, bWrongVersion, bNoVersion, bNeverUp} {
		v := newActiveEnv(t)
		v.sd.beh["1.1.0"] = b
		wantOK(t, v.activate("1.1.0"))
		l := v.lastResult()
		if v.current() != "1.0.0" || l == nil || l.Outcome != outcomeRolledBack || l.ReasonCode != reasonHealthTimeout {
			t.Fatalf("behavior %d: current %q last %+v", b, v.current(), l)
		}
		if d := time.Duration(v.mono.Load()); d < healthDeadline || d > healthDeadline+30*time.Second {
			t.Fatalf("behavior %d: took %v", b, d)
		}
	}
}

func TestRollbackAfterARestartLoop(t *testing.T) {
	v := newActiveEnv(t)
	v.sd.beh["1.1.0"] = bLoop
	wantOK(t, v.activate("1.1.0"))
	l := v.lastResult()
	if v.current() != "1.0.0" || l == nil || l.ReasonCode != reasonStartFailed {
		t.Fatalf("current %q last %+v", v.current(), l)
	}
}

func TestRollbackAfterARestartError(t *testing.T) {
	v := newActiveEnv(t)
	v.sd.restartFail[1] = true
	wantOK(t, v.activate("1.1.0"))
	l := v.lastResult()
	if v.current() != "1.0.0" || l == nil || l.Outcome != outcomeRolledBack || l.ReasonCode != reasonRestartFailed {
		t.Fatalf("current %q last %+v", v.current(), l)
	}
}

// U13 F14: a damaged rollback target is never switched to.
func TestDamagedRollbackTarget(t *testing.T) {
	v := newActiveEnv(t)
	v.sd.beh["1.1.0"] = bCrash
	v.sd.onRestart = func() {
		os.WriteFile(filepath.Join(v.root, "releases", "1.0.0", "oarfish"), elfStub("damaged"), 0o755)
	}
	wantOK(t, v.activate("1.1.0"))
	st, _ := v.readState()
	if v.current() != "1.1.0" {
		t.Fatalf("switched to the damaged tree: current %q", v.current())
	}
	if st.Txn.Phase != phaseRollbackFailed || !st.NeedsAttention || st.Last == nil || st.Last.Outcome != outcomeRollbackFailed ||
		st.Last.ReasonCode != reasonTargetDamaged {
		t.Fatalf("state %+v last %+v", st.Txn, st.Last)
	}
	if !v.status().NeedsAttention || v.status().Phase != phaseRollbackFailed {
		t.Fatal("status does not show needs_attention")
	}
	wantErrCode(t, v.stage("1.2.0"), codeBusy)
	if v.sd.count("restart") != 1 {
		t.Fatalf("restarted after a damaged target: %v", v.sd.Calls())
	}
}

func TestRollbackFailsWhenTheOldVersionIsUnhealthy(t *testing.T) {
	v := newActiveEnv(t)
	v.sd.beh["1.1.0"] = bCrash
	v.sd.onRestart = func() {
		if v.sd.restarts == 1 { // the restart for the rollback
			v.sd.beh["1.0.0"] = bHang
		}
	}
	wantOK(t, v.activate("1.1.0"))
	st, _ := v.readState()
	if v.current() != "1.0.0" {
		t.Fatalf("current %q: rollback_failed leaves current on from", v.current())
	}
	if st.Txn.Phase != phaseRollbackFailed || !st.NeedsAttention || st.Last.ReasonCode != reasonRollbackUnheal {
		t.Fatalf("state %+v last %+v", st.Txn, st.Last)
	}
}

func TestPrecheckCurrentUnhealthy(t *testing.T) {
	for _, b := range []behavior{bHang, bCrash, bWrongVersion, bNoVersion, bNeverUp} {
		v := newActiveEnv(t)
		v.sd.beh[installedVersionString] = b
		if b == bCrash {
			v.sd.active, v.sd.sub = "failed", "failed"
		}
		if b == bNeverUp {
			v.sd.active, v.sd.sub = "activating", "start"
		}
		before, _ := os.ReadFile(filepath.Join(v.stateDir, stateFile))
		wantErrCode(t, v.activate("1.1.0"), codeCurrentUnhealthy)
		after, _ := os.ReadFile(filepath.Join(v.stateDir, stateFile))
		if string(before) != string(after) || v.current() != "1.0.0" || v.sd.count("restart") != 0 {
			t.Fatalf("behavior %d: the refused activate changed something", b)
		}
		if d := time.Duration(v.mono.Load()); d < precheckLimit || d > precheckLimit+2*time.Second {
			if b != bCrash {
				t.Fatalf("behavior %d: pre-check took %v", b, d)
			}
		}
	}
}

func TestPrecheckUnreadableCurrent(t *testing.T) {
	v := newActiveEnv(t)
	os.Remove(filepath.Join(v.root, "current"))
	wantErrCode(t, v.activate("1.1.0"), codeCurrentUnhealthy)
	if !releaseComplete(v.root, "1.1.0") {
		t.Fatal("removed something")
	}
}

func TestActivateRefusals(t *testing.T) {
	t.Run("nothing staged", func(t *testing.T) {
		v := newTenv(t)
		v.installOarfish()
		v.fakeSystemd()
		wantErrCode(t, v.activate("1.1.0"), codeNotFound)
	})
	t.Run("another version staged", func(t *testing.T) {
		v := newActiveEnv(t)
		wantErrCode(t, v.activate("1.2.0"), codeNotFound)
		if v.phase() != phaseStaged {
			t.Fatal("changed the staged state")
		}
	})
	t.Run("installed version", func(t *testing.T) {
		v := newActiveEnv(t)
		wantErrCode(t, v.activate("1.0.0"), codeNotFound)
	})
	for _, phase := range []string{phaseStaging, phaseSwitching, phaseVerifying, phaseCommitted, phaseRollingBack, phaseRollbackFailed} {
		t.Run("phase "+phase, func(t *testing.T) {
			v := newActiveEnv(t)
			st, _ := v.readState()
			st.Txn.Phase = phase
			v.writeState(st)
			wantErrCode(t, v.activate("1.1.0"), codeBusy)
			if v.sd.count("restart") != 0 || v.current() != "1.0.0" {
				t.Fatal("acted on a busy state")
			}
		})
	}
	t.Run("lock held", func(t *testing.T) {
		v := newActiveEnv(t)
		lk, err := lockDir(v.stateDir)
		if err != nil {
			t.Fatal(err)
		}
		defer lk.release()
		wantErrCode(t, v.activate("1.1.0"), codeBusy)
		if len(v.sd.Calls()) != 0 {
			t.Fatal("called systemctl without the lock")
		}
	})
}

func TestActivateStageExpired(t *testing.T) {
	for name, adjust := range map[string]func(v *tenv){
		"exactly 30 minutes": func(v *tenv) { v.uptime.Add(stageLifetimeS) },
		"a day":              func(v *tenv) { v.uptime.Add(86400) },
		"reboot":             func(v *tenv) { v.uptime.Store(3) },
	} {
		t.Run(name, func(t *testing.T) {
			v := newActiveEnv(t)
			adjust(v)
			wantErrCode(t, v.activate("1.1.0"), codeStageExpired)
			l := v.lastResult()
			if v.phase() != phaseIdle || l == nil || l.Outcome != outcomeAborted || l.ReasonCode != reasonStageExpired {
				t.Fatalf("state %s last %+v", v.phase(), l)
			}
			v.wantClean()
			if v.sd.count("restart") != 0 {
				t.Fatal("restarted")
			}
		})
	}
	t.Run("one second before", func(t *testing.T) {
		v := newActiveEnv(t)
		v.uptime.Add(stageLifetimeS - 1)
		wantOK(t, v.activate("1.1.0"))
		if v.current() != "1.1.0" {
			t.Fatal("not activated")
		}
	})
	t.Run("installed version changed after the stage", func(t *testing.T) {
		v := newActiveEnv(t)
		st, _ := v.readState()
		st.Txn.From = "0.9.0"
		v.writeState(st)
		wantErrCode(t, v.activate("1.1.0"), codeStageExpired)
		if releaseComplete(v.root, "1.1.0") {
			t.Fatal("the stale staged release stays")
		}
	})
}

// The gap decision for components_confirmed.
func TestComponentsConfirmedRule(t *testing.T) {
	v := newActiveEnv(t)
	wantErrCode(t, v.request(activateLine("1.1.0", testCID, true)), codeBadRequest)
	if v.phase() != phaseStaged || v.current() != "1.0.0" {
		t.Fatal("a refused flag changed something")
	}
	// A staged component difference (G6.10) needs the flag.
	st, _ := v.readState()
	st.Txn.ComponentsTo = "1.1.0"
	v.writeState(st)
	wantErrCode(t, v.activate("1.1.0"), codeConfirmationRequired)
	if v.phase() != phaseStaged || len(v.sd.Calls()) != 0 {
		t.Fatal("acted without the confirmation")
	}
	wantOK(t, v.request(activateLine("1.1.0", testCID, true)))
}

func TestCooldown(t *testing.T) {
	v := newActiveEnv(t)
	v.sd.beh["1.1.0"] = bCrash
	wantOK(t, v.activate("1.1.0"))
	st, _ := v.readState()
	if st.Cool.RefuseVersion != "1.1.0" || st.Cool.RefuseUntilUptimeS-st.Cool.RefuseSetAtUptimeS != 900 {
		t.Fatalf("cooldown %+v", st.Cool)
	}
	// The helper process is new each time: everything is in the state.
	v.stagedRelease("1.1.0")
	wantErrCode(t, v.activate("1.1.0"), codeCooldown)
	if s := v.status(); s.CooldownVersion != "1.1.0" || s.CooldownRemainingS <= 0 || s.CooldownRemainingS > 900 {
		t.Fatalf("status cooldown %q %d", s.CooldownVersion, s.CooldownRemainingS)
	}
	// Another version is refused within 60 s of the previous activate, and
	// accepted after.
	wantOK(t, v.request(discardLine("1.1.0")))
	v.stagedRelease("1.2.0")
	last := st.Cool.LastActivateUptimeS
	v.uptime.Store(last + 59)
	wantErrCode(t, v.activate("1.2.0"), codeCooldown)
	v.uptime.Store(last + 60)
	v.sd.beh["1.2.0"] = bOK
	// The staged 1.2.0 may have aged; it was staged at about last+6.
	wantOK(t, v.activate("1.2.0"))
	if v.current() != "1.2.0" {
		t.Fatalf("current %q", v.current())
	}
}

func TestCooldownSameVersionBoundary(t *testing.T) {
	v := newActiveEnv(t)
	v.sd.beh["1.1.0"] = bCrash
	wantOK(t, v.activate("1.1.0"))
	st, _ := v.readState()
	until := st.Cool.RefuseUntilUptimeS
	v.stagedRelease("1.1.0")
	v.sd.beh["1.1.0"] = bOK
	v.uptime.Store(until - 1)
	// The newly staged copy must be fresh at that time.
	st, _ = v.readState()
	st.Txn.StagedAtUptimeS = until - 1
	v.writeState(st)
	wantErrCode(t, v.activate("1.1.0"), codeCooldown)
	v.uptime.Store(until)
	st.Txn.StagedAtUptimeS = until
	v.writeState(st)
	wantOK(t, v.activate("1.1.0"))
}

// U6 / U13 F21: a marker is void when the uptime is smaller than the uptime at
// which it was set.
func TestCooldownVoidAfterAReboot(t *testing.T) {
	v := newActiveEnv(t)
	v.sd.beh["1.1.0"] = bCrash
	wantOK(t, v.activate("1.1.0"))
	v.stagedRelease("1.1.0")
	v.sd.beh["1.1.0"] = bOK
	v.uptime.Store(5) // rebooted
	st, _ := v.readState()
	st.Txn.StagedAtUptimeS = 5
	v.writeState(st)
	if s := v.status(); s.CooldownRemainingS != 0 || s.CooldownVersion != "" {
		t.Fatalf("status shows a void marker: %q %d", s.CooldownVersion, s.CooldownRemainingS)
	}
	wantOK(t, v.activate("1.1.0"))
}

func TestAckClearsAttention(t *testing.T) {
	v := newActiveEnv(t)
	r := wantOK(t, v.activate("1.1.0"))
	wantErrCode(t, v.request(ackLine("ffffffffffffffff")), codeNotFound)
	if st, _ := v.readState(); st.Last.Acknowledged {
		t.Fatal("acknowledged by a wrong id")
	}
	wantOK(t, v.request(ackLine(r.TransactionID)))
	wantOK(t, v.request(ackLine(r.TransactionID))) // idempotent
	if st, _ := v.readState(); !st.Last.Acknowledged {
		t.Fatal("not acknowledged")
	}
	// needs_attention from a rebuilt state is cleared by the ack.
	st, _ := v.readState()
	st.NeedsAttention = true
	st.Last.Acknowledged = false
	v.writeState(st)
	if !v.status().NeedsAttention {
		t.Fatal("status")
	}
	wantOK(t, v.request(ackLine(r.TransactionID)))
	if v.status().NeedsAttention {
		t.Fatal("ack did not clear needs_attention")
	}
	// A failed rollback is not cleared by an ack.
	st, _ = v.readState()
	st.Txn.Phase = phaseRollbackFailed
	st.NeedsAttention = true
	st.Last.Acknowledged = false
	v.writeState(st)
	wantOK(t, v.request(ackLine(r.TransactionID)))
	if got, _ := v.readState(); !got.NeedsAttention || !got.Last.Acknowledged {
		t.Fatalf("ack of a failed rollback: needs_attention %v, acknowledged %v", got.NeedsAttention, got.Last.Acknowledged)
	}
}

func TestAckWithoutAnyResult(t *testing.T) {
	v := newTenv(t)
	wantErrCode(t, v.request(ackLine("0123456789abcdef")), codeNotFound)
}

func TestStatusShowsThePreviousVersionAndTheLastResult(t *testing.T) {
	v := newActiveEnv(t)
	r := wantOK(t, v.activate("1.1.0"))
	s := v.status()
	if s.InstalledVersion != "1.1.0" || s.PreviousVersion != "1.0.0" || s.Phase != phaseIdle || s.NeedsAttention ||
		s.LastResult == nil || s.LastResult.Outcome != "success" || s.LastResult.TransactionID != r.TransactionID || s.LastResult.Acknowledged {
		t.Fatalf("status %+v last %+v", s, s.LastResult)
	}
}

// No action without a durable intent: when the switching state can not be
// written, nothing moves.
func TestActivateStopsWhenTheIntentCannotBeWritten(t *testing.T) {
	v := newActiveEnv(t)
	v.h.wrapFS = func(inner fsops) fsops { f := newTestFS(inner); f.failAt = 1; v.fs = f; return f }
	wantErrCode(t, v.activate("1.1.0"), codeIOError)
	if v.current() != "1.0.0" || v.phase() != phaseStaged || v.sd.count("restart") != 0 {
		t.Fatalf("current %q phase %s", v.current(), v.phase())
	}
	if len(v.fs.Ops()) != 1 {
		t.Fatalf("ops after the failed write: %v", v.fs.Ops())
	}
}

// When the verifying state can not be written, the service is not restarted;
// the state still says switching and the next start rolls the link back.
func TestNoRestartWithoutTheVerifyingIntent(t *testing.T) {
	v := newActiveEnv(t)
	v.h.wrapFS = func(inner fsops) fsops {
		f := newTestFS(inner)
		f.before = func(op fsOp) error {
			if op.Kind == "create" && op.Tree == treeState {
				st, _ := v.readState()
				if st.Txn.Phase == phaseSwitching && v.current() == "1.1.0" {
					return errInjected
				}
			}
			return nil
		}
		return f
	}
	wantOK(t, v.activate("1.1.0"))
	if v.sd.count("restart") != 0 || v.phase() != phaseSwitching || v.current() != "1.1.0" {
		t.Fatalf("restarts %d phase %s current %s", v.sd.count("restart"), v.phase(), v.current())
	}
	v.h.wrapFS = nil
	v.recoverLive()
	v.wantIdleWith(outcomeRolledBack, reasonInterrupted)
	if v.current() != "1.0.0" {
		t.Fatal("not rolled back")
	}
}

// A switch that fails before the restart ends as aborted, not rolled_back, and
// removes the release that never ran (U7 row 5).
func TestSwitchFailureIsAborted(t *testing.T) {
	v := newActiveEnv(t)
	v.h.wrapFS = func(inner fsops) fsops {
		f := newTestFS(inner)
		f.before = func(op fsOp) error {
			if op.Kind == "symlink" && op.Path2 == "releases/1.1.0" {
				return errInjected
			}
			return nil
		}
		return f
	}
	wantOK(t, v.activate("1.1.0"))
	v.wantIdleWith(outcomeAborted, reasonSwitchFailed)
	if v.current() != "1.0.0" || v.sd.count("restart") != 0 {
		t.Fatal("moved")
	}
	v.wantClean()
	if st, _ := v.readState(); st.Cool.RefuseVersion != "1.1.0" {
		t.Fatal("no cooldown after a failure")
	}
}

// failStateCreate fails the n-th creation of a state file (the n-th write of
// state.json) and nothing else.
func failStateCreate(v *tenv, n int) {
	count := 0
	v.h.wrapFS = func(inner fsops) fsops {
		f := newTestFS(inner)
		f.before = func(op fsOp) error {
			if op.Kind == "create" && op.Tree == treeState {
				count++
				if count == n {
					return errInjected
				}
			}
			return nil
		}
		return f
	}
}

// The commit is not final until it is durable: when it can not be written,
// nothing is pruned and the next start rolls back (U7 row 8, the accepted cost).
func TestCommitThatCanNotBeWrittenPrunesNothing(t *testing.T) {
	v := newActiveEnv(t)
	v.makeRelease("0.8.0")
	failStateCreate(v, 3) // switching, verifying, committed
	wantOK(t, v.activate("1.1.0"))
	st, _ := v.readState()
	if st.Txn.Phase != phaseVerifying || st.Previous != "" || (st.Last != nil && st.Last.Outcome == outcomeSuccess) {
		t.Fatalf("state %+v", st)
	}
	if !releaseComplete(v.root, "0.8.0") {
		t.Fatal("pruned before the commit was durable")
	}
	v.h.wrapFS = nil
	v.recoverBoot()
	v.wantIdleWith(outcomeRolledBack, reasonInterrupted)
	if v.current() != "1.0.0" {
		t.Fatal("not rolled back")
	}
}

// No rollback action without a durable rolling_back.
func TestRollbackThatCanNotBeWrittenActsOnNothing(t *testing.T) {
	v := newActiveEnv(t)
	v.sd.beh["1.1.0"] = bCrash
	failStateCreate(v, 3) // switching, verifying, rolling_back
	wantOK(t, v.activate("1.1.0"))
	if v.current() != "1.1.0" || v.sd.count("restart") != 1 || v.phase() != phaseVerifying {
		t.Fatalf("current %s restarts %d phase %s", v.current(), v.sd.count("restart"), v.phase())
	}
	v.h.wrapFS = nil
	v.recoverBoot()
	v.wantIdleWith(outcomeRolledBack, reasonInterrupted)
}

// Retention removes directories only: a file or a link with a version name is
// not touched, and neither is a name that is not a version.
func TestPruneTouchesOnlyDirectoriesWithVersionNames(t *testing.T) {
	v := newActiveEnv(t)
	v.makeRelease("0.8.0")
	os.WriteFile(filepath.Join(v.root, "releases", "0.7.5"), []byte("a file"), 0o644)
	os.Symlink("0.8.0", filepath.Join(v.root, "releases", "0.7.6"))
	os.Mkdir(filepath.Join(v.root, "releases", "0.7"), 0o755)
	os.Mkdir(filepath.Join(v.root, "releases", "v0.7.7"), 0o755)
	wantOK(t, v.activate("1.1.0"))
	want := []string{"0.7", "0.7.5", "0.7.6", "1.0.0", "1.1.0", "v0.7.7"}
	if got := v.releasesEntries(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("releases/ holds %v, want %v", got, want)
	}
}

// The switch of current is flushed before anything else happens (U5).
func TestSwitchIsFlushedBeforeTheNextStep(t *testing.T) {
	v := newActiveEnv(t)
	wantOK(t, v.activate("1.1.0"))
	ops := v.fs.Ops()
	for i, op := range ops {
		if op.Kind == "rename" && op.Path2 == "current" {
			if i+1 >= len(ops) || ops[i+1].Kind != "syncdir" || ops[i+1].Tree != treeOpt || ops[i+1].Path != "." {
				t.Fatalf("after the rename of current: %v", ops[i+1:min(i+3, len(ops))])
			}
			if i < 1 || ops[i-1].Kind != "symlink" || ops[i-1].Path2 != "releases/1.1.0" {
				t.Fatalf("before the rename of current: %v", ops[i-1])
			}
			return
		}
	}
	t.Fatal("current was not renamed")
}
