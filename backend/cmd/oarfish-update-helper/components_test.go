package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The component transaction (ADR 0010 U15 rule 4, A15 D1 to D6, A16 E1 to E4)
// with the fake root and the fake systemd. Class C evidence for the rows F24
// and F26 to F32 of U13 and for the rows c1 to c9 of the component table; none
// of it ran on a device.

const otherKeyID = "00000000000000aa"

func isNewImage(image string) bool { return image == string(elfStub("helper "+newVersion)) }

// acting returns the systemctl calls without the read-only `show`.
func (f *fakeSystemd) acting() []string {
	var out []string
	for _, c := range f.Calls() {
		if c[0] != "show" {
			out = append(out, strings.Join(c, " "))
		}
	}
	return out
}

func journalLines(v *tenv) []string {
	return strings.Split(strings.TrimSuffix(v.journal.String(), "\n"), "\n")
}

// F24 (class C): a release with another helper binary and another unit text is
// applied as one transaction, in the order of U15 rule 4.
func TestComponentTransactionSuccess(t *testing.T) {
	v, s := newComponentEnv(t, unitRecover, unitService)
	staged, _ := v.readState()
	if staged.Txn.KnownGoodID != "1.0.0" || staged.Txn.KnownGoodSHA256 != diskSetDigest(t, v, "1.0.0") {
		t.Fatalf("stage did not record the known-good hash (E2): %+v", staged.Txn)
	}
	old := v.h

	// The helper that receives activate: c_switching, the link, c_switched,
	// daemon-reload, exit 75. It restarts nothing and runs nothing of the new set.
	r := wantOK(t, v.confirm(newVersion))
	if r.Status != "accepted" || r.TransactionID != staged.Txn.ID {
		t.Fatalf("answer %+v", r)
	}
	if got := v.h.exit.Load(); got != exitHandOver {
		t.Fatalf("exit status %d, want 75", got)
	}
	st, _ := v.readState()
	tx := st.Txn
	if tx.Phase != phaseCSwitched || v.link(linkActive) != newVersion || v.link(linkKnownGood) != "1.0.0" || v.current() != "1.0.0" {
		t.Fatalf("after the hand-over: phase %s active %s known-good %s current %s", tx.Phase, v.link(linkActive), v.link(linkKnownGood), v.current())
	}
	// Everything the next process needs is in the state (E3).
	if tx.ComponentsFrom != "1.0.0" || tx.ComponentsTo != newVersion || tx.ComponentsKeyID != testKeyID ||
		tx.ComponentsProtocolMin != protocolMin || tx.ComponentsProtocolMax != protocolMax ||
		tx.ComponentsHelperSHA256 != sumHex(elfStub("helper "+newVersion)) ||
		tx.FromBinarySHA256 != fromHash(v) || tx.From != "1.0.0" || tx.To != newVersion || tx.CorrelationID != testCID ||
		strings.Join(tx.Differs, ",") != "oarfish-update-helper,oarfish-update-recover.service,oarfish.service" {
		t.Fatalf("c_switched state %+v", tx)
	}
	if got := v.sd.acting(); !slices.Equal(got, []string{"daemon-reload"}) {
		t.Fatalf("the old helper called %v", got)
	}
	if s := v.status(); s.Phase != phaseCSwitched {
		t.Fatalf("status phase %s", s.Phase)
	}

	// systemd starts the new binary; it proves itself and finishes the transaction.
	s.settle()
	if v.h == old || len(s.restartAt) != 1 || s.recovers != 0 {
		t.Fatalf("restarts %v, recover runs %d", s.restartAt, s.recovers)
	}
	fin := checkSettled(t, v, false)
	if v.current() != newVersion || v.link(linkActive) != newVersion || v.link(linkKnownGood) != newVersion {
		t.Fatalf("current %s active %s known-good %s", v.current(), v.link(linkActive), v.link(linkKnownGood))
	}
	if fin.Last.Outcome != outcomeSuccess || fin.Last.TransactionID != staged.Txn.ID || fin.Previous != "1.0.0" || fin.Txn.ComponentsTo != "" {
		t.Fatalf("result %+v, state %+v", fin.Last, fin)
	}
	// The order of the calls: reload (old helper), reload after the known-good
	// switch (D2), reload after the switch of current because oarfish.service
	// differs (U6), then the one restart of the service.
	if got, want := v.sd.acting(), []string{"daemon-reload", "daemon-reload", "daemon-reload", "restart oarfish.service"}; !slices.Equal(got, want) {
		t.Fatalf("systemctl calls %v, want %v", got, want)
	}
	// Retention: the sets the links name and the one they replaced.
	if !v.exists("components/1.0.0") || !v.exists("components/"+newVersion) {
		t.Fatal("retention removed a kept set")
	}
	want := []string{
		"phase=staging to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok",
		"phase=staged to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok",
		"phase=c_switching to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok",
		"phase=c_switched to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok",
		"phase=c_verified to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok",
		"phase=c_committed to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok",
		"phase=switching to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok",
		"phase=verifying to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok",
		"phase=committed to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok",
		"phase=idle to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok",
	}
	if got := journalLines(v); !slices.Equal(got, want) {
		t.Fatalf("journal:\n%s", strings.Join(got, "\n"))
	}
	sr := v.status()
	if sr.Phase != phaseIdle || sr.InstalledVersion != newVersion || sr.LastResult == nil || !sr.LastResult.ComponentsUpdated || sr.SocketRestartPending {
		t.Fatalf("status %+v %+v", sr, sr.LastResult)
	}
}

func sumHex(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// The write-ahead order: every phase is durable before its action, and the
// link switches are a temporary link, a rename and a directory fsync. Every
// mutating call of stage, of the transaction and of a rollback is in the closed
// write-target table of U2.
func TestComponentWriteAheadOrder(t *testing.T) {
	record := func(t *testing.T, v *tenv, ops *[]string) {
		v.h.wrapFS = func(inner fsops) fsops {
			f := newTestFS(inner)
			f.before = func(op fsOp) error {
				if op.Tree == treeOpt && (op.Kind == "symlink" || op.Kind == "rename" || op.Kind == "syncdir") {
					*ops = append(*ops, op.Kind+" "+op.Path+" "+op.Path2+" @"+v.phase())
				}
				if !allowedTarget(op.Tree, op.Path) || (op.Path2 != "" && op.Kind == "rename" && !allowedTarget(op.Tree, op.Path2)) {
					t.Errorf("%s is outside the write-target table", op)
				}
				return nil
			}
			return f
		}
	}
	setup := func(t *testing.T, ops *[]string) (*tenv, *supervisor) {
		v := newTenv(t)
		v.installOarfish()
		v.fakeSystemd()
		// The set that an earlier commit replaced: the retention removes it at the
		// next commit and keeps it after a rollback.
		v.makeSet("0.8.0")
		st := freshState(helperID)
		st.ComponentsPrevious = "0.8.0"
		v.writeState(st)
		record(t, v, ops)
		v.place(newVersion, v.componentRelease(newVersion).build())
		wantOK(t, v.stage(newVersion))
		return v, &supervisor{v: v}
	}
	t.Run("success", func(t *testing.T) {
		var ops []string
		v, s := setup(t, &ops)
		wantOK(t, v.confirm(newVersion))
		s.settle()
		checkSettled(t, v, false)
		idx := func(prefix string) int {
			for i, o := range ops {
				if strings.HasPrefix(o, prefix) {
					return i
				}
			}
			t.Fatalf("no %q in\n%s", prefix, strings.Join(ops, "\n"))
			return -1
		}
		// stage: the set is moved before the release, while the state says staging (row c1).
		set, rel := idx("rename releases/.stage."), idx("rename releases/.stage.")
		for i, o := range ops {
			if strings.Contains(o, " components/"+newVersion+" @staging") {
				set = i
			}
			if strings.Contains(o, " releases/"+newVersion+" @staging") {
				rel = i
			}
		}
		if set >= rel || !strings.HasPrefix(ops[set+1], "syncdir components ") {
			t.Fatalf("stage moved the set at %d and the release at %d:\n%s", set, rel, strings.Join(ops, "\n"))
		}
		// active: link created and renamed while the state says c_switching.
		a := idx("rename components/.new.")
		if !strings.HasSuffix(ops[a], " components/active @c_switching") || !strings.HasPrefix(ops[a-1], "symlink components/.new.") ||
			!strings.HasPrefix(ops[a+1], "syncdir components ") {
			t.Fatalf("switch of active: %v", ops[a-1:a+2])
		}
		// known-good: after c_verified is durable.
		k := -1
		for i, o := range ops {
			if strings.Contains(o, " components/known-good ") {
				k = i
			}
		}
		if k < 0 || !strings.HasSuffix(ops[k], "@c_verified") || !strings.HasPrefix(ops[k+1], "syncdir components ") {
			t.Fatalf("switch of known-good: %v", ops)
		}
		// The retention runs after c_committed is durable.
		p := idx("rename components/0.8.0 releases/.trash.")
		if p < k || !strings.HasSuffix(ops[p], "@c_committed") {
			t.Fatalf("retention: %s", ops[p])
		}
		// current: after c_committed, while the state says switching.
		c := idx("rename .current.new.")
		if c < p || !strings.HasSuffix(ops[c], " current @switching") {
			t.Fatalf("switch of current: %s", ops[c])
		}
	})
	t.Run("rollback", func(t *testing.T) {
		var ops []string
		v, s := setup(t, &ops)
		s.mutate = func(h *handler, image string) {
			if isNewImage(image) {
				h.keyID = otherKeyID
			}
		}
		wantOK(t, v.confirm(newVersion))
		s.settle()
		checkSettled(t, v, false)
		// The switch back happens while the state says c_rolling_back, and nothing
		// is pruned after a rollback: the older set stays.
		back := -1
		for i, o := range ops {
			if strings.HasSuffix(o, " components/active @c_rolling_back") {
				back = i
			}
		}
		st, _ := v.readState()
		if back < 0 || !v.exists("components/0.8.0") || st.ComponentsPrevious != "0.8.0" {
			t.Fatalf("switch back at %d, previous set kept %v:\n%s", back, v.exists("components/0.8.0"), strings.Join(ops, "\n"))
		}
	})
}

// Retention of component sets (U15 rule 4 step 7): after a commit the sets that
// the links name and the set they replaced stay, others go through .trash.<id>.
func TestComponentRetention(t *testing.T) {
	v, s := newComponentEnv(t)
	os.MkdirAll(filepath.Join(v.root, "components", "0.8.0"), 0o755)
	os.MkdirAll(filepath.Join(v.root, "components", "not-a-version"), 0o755)
	var trashed []string
	v.h.wrapFS = func(inner fsops) fsops {
		f := newTestFS(inner)
		f.before = func(op fsOp) error {
			if op.Kind == "rename" && strings.HasPrefix(op.Path, "components/") && strings.HasPrefix(op.Path2, "releases/.trash.") {
				trashed = append(trashed, op.Path)
			}
			return nil
		}
		return f
	}
	wantOK(t, v.confirm(newVersion))
	s.settle()
	st := checkSettled(t, v, false)
	if st.ComponentsPrevious != "1.0.0" || !slices.Equal(trashed, []string{"components/0.8.0"}) || !v.exists("components/not-a-version") {
		t.Fatalf("previous %q, trashed %v", st.ComponentsPrevious, trashed)
	}
	// The next component update replaces 1.1.0: 1.0.0 goes, 1.1.0 stays as the previous set.
	v.uptime.Add(120)
	v.place("1.2.0", v.componentRelease("1.2.0").build())
	wantOK(t, v.stage("1.2.0"))
	wantOK(t, v.confirm("1.2.0"))
	s.restartAt = nil
	s.settle()
	st, _ = v.readState()
	if v.link(linkActive) != "1.2.0" || v.link(linkKnownGood) != "1.2.0" || st.ComponentsPrevious != newVersion ||
		v.exists("components/1.0.0") || !v.exists("components/"+newVersion) || !slices.Equal(trashed, []string{"components/0.8.0", "components/1.0.0"}) {
		t.Fatalf("links %s %s previous %q trashed %v", v.link(linkActive), v.link(linkKnownGood), st.ComponentsPrevious, trashed)
	}
}

// F32: without components_confirmed nothing happens, for a component set and
// for a difference of oarfish.service alone (D6).
func TestMissingComponentConfirmation(t *testing.T) {
	t.Run("a component set", func(t *testing.T) {
		v, _ := newComponentEnv(t)
		before := snapshot(t, v.root)
		wantErrCode(t, v.activate(newVersion), codeConfirmationRequired)
		if d := diffSnapshots(before, snapshot(t, v.root)); len(d) != 0 || v.phase() != phaseStaged || len(v.sd.Calls()) != 0 || v.h.exit.Load() != 0 {
			t.Fatalf("acted without the confirmation: %v", d)
		}
	})
	t.Run("oarfish.service alone", func(t *testing.T) {
		v := newTenv(t)
		v.installOarfish()
		v.fakeSystemd()
		r := v.release(newVersion)
		r.setFile(unitService, changedUnit(unitService))
		v.place(newVersion, r.build())
		wantOK(t, v.stage(newVersion))
		wantErrCode(t, v.activate(newVersion), codeConfirmationRequired)
		if v.phase() != phaseStaged || len(v.sd.Calls()) != 0 {
			t.Fatal("acted without the confirmation")
		}
	})
}

// D6 and U6: a difference of oarfish.service alone needs the confirmation but
// no component phase; systemd reads the unit again after the switch of current
// and before the restart, and again after a switch back.
func TestServiceUnitDifferenceAlone(t *testing.T) {
	setup := func(t *testing.T) *tenv {
		v := newTenv(t)
		v.installOarfish()
		v.fakeSystemd()
		r := v.release(newVersion)
		r.setFile(unitService, changedUnit(unitService))
		v.place(newVersion, r.build())
		if got := wantOK(t, v.stage(newVersion)); !got.ComponentsDiffer || !slices.Equal(got.Differs, []string{unitService}) {
			t.Fatalf("stage answer %+v", got)
		}
		if st, _ := v.readState(); st.Txn.ComponentsTo != "" || v.exists("components/"+newVersion) {
			t.Fatal("a component set was staged for a difference of oarfish.service alone")
		}
		return v
	}
	t.Run("success", func(t *testing.T) {
		v := setup(t)
		wantOK(t, v.confirm(newVersion))
		if got, want := v.sd.acting(), []string{"daemon-reload", "restart oarfish.service"}; !slices.Equal(got, want) {
			t.Fatalf("systemctl calls %v, want %v", got, want)
		}
		if v.h.exit.Load() != 0 || v.current() != newVersion || strings.Contains(v.journal.String(), "phase=c_") {
			t.Fatalf("exit %d current %s\n%s", v.h.exit.Load(), v.current(), v.journal.String())
		}
		if l := v.lastResult(); l.Outcome != outcomeSuccess || l.ComponentsUpdated {
			t.Fatalf("result %+v", l)
		}
	})
	t.Run("rollback", func(t *testing.T) {
		v := setup(t)
		v.sd.beh[newVersion] = bCrash
		wantOK(t, v.confirm(newVersion))
		want := []string{"daemon-reload", "restart oarfish.service", "daemon-reload", "reset-failed oarfish.service", "restart oarfish.service"}
		if got := v.sd.acting(); !slices.Equal(got, want) {
			t.Fatalf("systemctl calls %v, want %v", got, want)
		}
		v.wantIdleWith(outcomeRolledBack, reasonStartFailed)
	})
	t.Run("a failed reload rolls back before the restart", func(t *testing.T) {
		v := setup(t)
		v.sd.reloadFail[1] = true
		wantOK(t, v.confirm(newVersion))
		if v.sd.count("restart") != 0 || v.current() != "1.0.0" {
			t.Fatalf("restarts %d, current %s", v.sd.count("restart"), v.current())
		}
		v.wantIdleWith(outcomeAborted, reasonReloadFailed)
	})
	t.Run("at boot the unit is read again after the switch back", func(t *testing.T) {
		v := setup(t)
		st, _ := v.readState()
		st.Txn.Phase, st.Txn.FromBinarySHA256 = phaseVerifying, fromHash(v)
		v.writeState(st)
		v.pointCurrent(newVersion)
		if code := v.recoverBoot(); code != 0 {
			t.Fatalf("exit %d", code)
		}
		if got := v.sd.acting(); !slices.Equal(got, []string{"daemon-reload"}) || v.current() != "1.0.0" {
			t.Fatalf("systemctl calls %v, current %s", got, v.current())
		}
	})
}

// WP1: stage with a component set, and what discard and the expiry remove.
func TestStageComponentSet(t *testing.T) {
	t.Run("the set is moved into place with the modes of a set", func(t *testing.T) {
		v, _ := newComponentEnv(t, unitHelperSocket)
		if !setComplete(v, newVersion) {
			t.Fatal("components/1.1.0 is not complete")
		}
		for n, want := range map[string]os.FileMode{"": 0o755 | os.ModeDir, "/oarfish-update-helper": 0o755, "/oarfish-update-helper.socket": 0o644} {
			fi, err := os.Lstat(filepath.Join(v.root, "components", newVersion) + n)
			if err != nil || fi.Mode() != want {
				t.Fatalf("components/1.1.0%s: %v %v", n, fi.Mode(), err)
			}
		}
		if v.link(linkActive) != "1.0.0" || v.link(linkKnownGood) != "1.0.0" || v.exists("releases/.stage") {
			t.Fatal("stage moved a link")
		}
		v.wantNoLeftovers()
	})
	t.Run("discard removes the staged set", func(t *testing.T) {
		v, _ := newComponentEnv(t)
		wantOK(t, v.request(discardLine(newVersion)))
		if v.exists("components/"+newVersion) || v.exists("releases/"+newVersion) || v.phase() != phaseIdle {
			t.Fatal("discard left the staged set or release")
		}
		v.wantNoLeftovers()
	})
	t.Run("the expiry removes the staged set", func(t *testing.T) {
		v, _ := newComponentEnv(t)
		v.uptime.Add(stageLifetimeS)
		if s := v.status(); s.Phase != phaseIdle || len(s.StagedDiffers) != 0 {
			t.Fatalf("status %+v", s)
		}
		if v.exists("components/"+newVersion) || v.exists("releases/"+newVersion) {
			t.Fatal("the expiry left the staged set or release")
		}
		v.wantIdleWith(outcomeAborted, reasonStageExpired)
	})
	t.Run("an expired set is replaced by the next stage", func(t *testing.T) {
		v, _ := newComponentEnv(t)
		v.uptime.Add(stageLifetimeS)
		v.place("1.2.0", v.componentRelease("1.2.0").build())
		wantOK(t, v.stage("1.2.0"))
		if v.exists("components/"+newVersion) || !setComplete(v, "1.2.0") {
			t.Fatal("the expired set stays, or the new one is missing")
		}
	})
	t.Run("a stale components/<id> that no link names is replaced", func(t *testing.T) {
		v := newTenv(t)
		os.MkdirAll(filepath.Join(v.root, "components", newVersion), 0o755)
		os.WriteFile(filepath.Join(v.root, "components", newVersion, "stale-marker"), []byte("old"), 0o644)
		v.place(newVersion, v.componentRelease(newVersion).build())
		wantOK(t, v.stage(newVersion))
		if !setComplete(v, newVersion) || v.exists("components/"+newVersion+"/stale-marker") {
			t.Fatal("the stale directory was not replaced")
		}
		v.wantNoLeftovers()
	})
	t.Run("a components/<id> that a link names is never touched", func(t *testing.T) {
		v := newTenv(t)
		// known-good names a set with the id of the release: the bytes differ
		// from the active set, and the directory must stay as it is.
		dir := filepath.Join(v.root, "components", newVersion)
		os.MkdirAll(dir, 0o755)
		for n, d := range baseComponents {
			os.WriteFile(filepath.Join(dir, n), d, 0o755)
		}
		os.Remove(filepath.Join(v.root, "components", linkKnownGood))
		os.Symlink(newVersion, filepath.Join(v.root, "components", linkKnownGood))
		before := snapshot(t, filepath.Join(v.root, "components"))
		v.place(newVersion, v.componentRelease(newVersion).build())
		wantErrCode(t, v.stage(newVersion), codeIOError)
		if d := diffSnapshots(before, snapshot(t, filepath.Join(v.root, "components"))); len(d) != 0 {
			t.Fatalf("components/ changed: %v", d)
		}
		v.wantClean()
	})
	t.Run("a failure after the set was moved removes it", func(t *testing.T) {
		v := newTenv(t)
		v.place(newVersion, v.componentRelease(newVersion).build())
		v.h.wrapFS = func(inner fsops) fsops {
			f := newTestFS(inner)
			f.before = func(op fsOp) error {
				if op.Kind == "rename" && op.Path2 == "releases/"+newVersion {
					return errInjected
				}
				return nil
			}
			return f
		}
		wantErrCode(t, v.stage(newVersion), codeIOError)
		if v.exists("components/"+newVersion) || v.phase() != phaseIdle {
			t.Fatal("the set stays after a failed stage")
		}
		v.wantClean()
	})
}

// AC2: the refusals of stage for a component set come before any change, and
// installer_required is not among them (E1).
func TestStageComponentRefusals(t *testing.T) {
	for name, tt := range map[string]struct {
		mutate func(v *tenv, r *release)
		want   errorCode
	}{
		"key_changed: the manifest declares another key id (rule 5, F26)": {func(v *tenv, r *release) {
			r.man["components"].(map[string]any)["key_id"] = otherKeyID
		}, codeKeyChanged},
		"requirements_not_met: components.protocol is P+2 (D5)": {func(v *tenv, r *release) {
			r.man["components"].(map[string]any)["protocol"] = protocolMax + 2
		}, codeRequirementsNotMet},
		"unit_rejected: a helper unit differs in a directive": {func(v *tenv, r *release) {
			r.setFile(unitHelperSocket, bytes.Replace(baseComponents[unitHelperSocket], []byte("Backlog=8"), []byte("Backlog=9"), 1))
		}, codeUnitRejected},
	} {
		t.Run(name, func(t *testing.T) {
			v := newTenv(t)
			r := v.componentRelease(newVersion)
			tt.mutate(v, r)
			v.place(newVersion, r.build())
			before := snapshot(t, v.root)
			wantErrCode(t, v.stage(newVersion), tt.want)
			wantNothingLeft(t, v, before)
			if st, ok := v.readState(); ok && (st.Txn.Phase != phaseIdle || st.Last != nil) {
				t.Fatalf("state %+v", st)
			}
		})
	}
	t.Run("components.protocol P+1 is staged (D5)", func(t *testing.T) {
		v := newTenv(t)
		r := v.componentRelease(newVersion)
		r.man["components"].(map[string]any)["protocol"] = protocolMax + 1
		v.place(newVersion, r.build())
		wantOK(t, v.stage(newVersion))
	})
	t.Run("the key and protocol rules apply to a component set only", func(t *testing.T) {
		// Without a difference of the helper files nothing of the delivered
		// helper is installed, so its declaration is not a reason to refuse.
		v := newTenv(t)
		r := v.release(newVersion)
		r.man["components"].(map[string]any)["key_id"] = otherKeyID
		v.place(newVersion, r.build())
		if wantOK(t, v.stage(newVersion)).ComponentsDiffer {
			t.Fatal("components_differ")
		}
	})
	t.Run("no difference answers installer_required", func(t *testing.T) {
		for _, name := range componentNames {
			v := newTenv(t)
			r := v.release(newVersion)
			if name == "oarfish-update-helper" {
				r.setFile(name, elfStub("another"))
			} else {
				r.setFile(name, changedUnit(name))
			}
			v.place(newVersion, r.build())
			if raw := v.stage(newVersion); strings.Contains(string(raw), string(codeInstallerRequired)) {
				t.Fatalf("%s: %s", name, raw)
			}
		}
	})
}

// U15 rule 3: the staged units are linted again at activate, from the bytes
// that are also hashed, and a staged file that changed is refused.
func TestActivateChecksTheStagedFilesAgain(t *testing.T) {
	bad := bytes.Replace(baseServiceUnit, []byte("User=oarfish"), []byte("User=root"), 1)
	for name, tt := range map[string]struct {
		path   string
		data   []byte
		want   errorCode
		reason string
	}{
		"oarfish.service fails the lint":      {"releases/1.1.0/oarfish.service", bad, codeUnitRejected, reasonUnitRejected},
		"oarfish.service has other bytes":     {"releases/1.1.0/oarfish.service", changedUnit(unitService), codeChecksumMismatch, reasonStagedChanged},
		"a helper unit fails the lint":        {"components/1.1.0/" + unitRecover, []byte("[Unit]\nDescription=x\n"), codeUnitRejected, reasonUnitRejected},
		"a helper unit has other bytes":       {"components/1.1.0/" + unitHelperService, changedUnit(unitHelperService), codeChecksumMismatch, reasonStagedChanged},
		"the helper binary has other bytes":   {"components/1.1.0/oarfish-update-helper", elfStub("swapped"), codeChecksumMismatch, reasonStagedChanged},
		"a helper unit is larger than 16 KiB": {"components/1.1.0/" + unitHelperSocket, bytes.Repeat([]byte("# x\n"), 5000), codeUnitRejected, reasonUnitRejected},
	} {
		t.Run(name, func(t *testing.T) {
			v, _ := newComponentEnv(t)
			if err := os.WriteFile(filepath.Join(v.root, tt.path), tt.data, 0o644); err != nil {
				t.Fatal(err)
			}
			wantErrCode(t, v.confirm(newVersion), tt.want)
			v.wantIdleWith(outcomeAborted, tt.reason)
			if v.exists("components/"+newVersion) || v.exists("releases/"+newVersion) || v.link(linkActive) != "1.0.0" || len(v.sd.acting()) != 0 {
				t.Fatal("a refused activate left the staged files or acted")
			}
			if tt.want == codeUnitRejected && !strings.Contains(v.journal.String(), "result=unit_rejected unit=") {
				t.Fatalf("no lint line in the journal:\n%s", v.journal.String())
			}
		})
	}
	t.Run("the unit is read once: the bytes that are linted are the bytes that are hashed", func(t *testing.T) {
		v, _ := newComponentEnv(t)
		// Swap the unit between the Lstat and the open: whatever is read is
		// linted and hashed as one, so the other bytes are refused by the hash.
		reads := 0
		hookAfterLstat = func(root *os.Root, name string) {
			if name == "components/1.1.0/"+unitRecover {
				reads++
				os.WriteFile(filepath.Join(v.root, name), changedUnit(unitRecover), 0o644)
			}
		}
		defer func() { hookAfterLstat = nil }()
		wantErrCode(t, v.confirm(newVersion), codeChecksumMismatch)
		if reads != 1 {
			t.Fatalf("the unit was opened %d times", reads)
		}
	})
	t.Run("staged against another active set is expired", func(t *testing.T) {
		v, _ := newComponentEnv(t)
		dir := filepath.Join(v.root, "components", "0.9.0")
		os.MkdirAll(dir, 0o755)
		for n, d := range baseComponents {
			os.WriteFile(filepath.Join(dir, n), d, 0o755)
		}
		os.Remove(filepath.Join(v.root, "components", linkActive))
		os.Symlink("0.9.0", filepath.Join(v.root, "components", linkActive))
		wantErrCode(t, v.confirm(newVersion), codeStageExpired)
		v.wantIdleWith(outcomeAborted, reasonStageExpired)
	})
}

// E2: the encoding of known_good_sha256.
func TestKnownGoodDigestEncoding(t *testing.T) {
	v := newTenv(t)
	// SHA-256 over the four file digests (32 bytes each) in the order of U2:
	// helper, socket unit, helper service unit, recover unit.
	var cat []byte
	for _, n := range []string{"oarfish-update-helper", "oarfish-update-helper.socket", "oarfish-update-helper.service", "oarfish-update-recover.service"} {
		d := sha256.Sum256(baseComponents[n])
		cat = append(cat, d[:]...)
	}
	want := sha256.Sum256(cat)
	if got := diskSetDigest(t, v, "1.0.0"); got != hex.EncodeToString(want[:]) || !isHex(64)(got) {
		t.Fatalf("set digest %s, want %x", got, want)
	}
	// One changed byte in any of the four files changes it; another file does not.
	for _, n := range componentNames[:4] {
		p := filepath.Join(v.root, "components", "1.0.0", n)
		orig, _ := os.ReadFile(p)
		os.WriteFile(p, append(append([]byte(nil), orig...), '\n'), 0o755)
		if diskSetDigest(t, v, "1.0.0") == hex.EncodeToString(want[:]) {
			t.Fatalf("a change of %s does not change the digest", n)
		}
		os.WriteFile(p, orig, 0o755)
	}
	os.WriteFile(filepath.Join(v.root, "components", "1.0.0", "extra"), []byte("x"), 0o644)
	if diskSetDigest(t, v, "1.0.0") != hex.EncodeToString(want[:]) {
		t.Fatal("a file outside the set changes the digest")
	}
}

// E2: when the reference hash is recorded, and for which set it counts.
func TestKnownGoodRecord(t *testing.T) {
	t.Run("stage records it at the first component difference only", func(t *testing.T) {
		v := newTenv(t)
		v.stageable(newVersion)
		wantOK(t, v.stage(newVersion))
		if st, _ := v.readState(); st.Txn.KnownGoodSHA256 != "" {
			t.Fatal("recorded without a component difference")
		}
		wantOK(t, v.request(discardLine(newVersion)))
		v.place(newVersion, v.componentRelease(newVersion).build())
		wantOK(t, v.stage(newVersion))
		st, _ := v.readState()
		if st.Txn.KnownGoodID != "1.0.0" || st.Txn.KnownGoodSHA256 != diskSetDigest(t, v, "1.0.0") {
			t.Fatalf("record %+v", st.Txn)
		}
		// The record outlives the transaction.
		wantOK(t, v.request(discardLine(newVersion)))
		if st, _ := v.readState(); st.Txn.KnownGoodSHA256 == "" || st.Txn.KnownGoodID != "1.0.0" {
			t.Fatal("the record was dropped with the transaction")
		}
	})
	t.Run("a record for another set does not count (the installer moved known-good last)", func(t *testing.T) {
		v := newTenv(t)
		st := freshState(helperID)
		st.Txn.KnownGoodID, st.Txn.KnownGoodSHA256 = "0.9.0", strings.Repeat("ab", 32)
		v.writeState(st)
		v.place(newVersion, v.componentRelease(newVersion).build())
		wantOK(t, v.stage(newVersion))
		got, _ := v.readState()
		if got.Txn.KnownGoodID != "1.0.0" || got.Txn.KnownGoodSHA256 != diskSetDigest(t, v, "1.0.0") {
			t.Fatalf("record %+v", got.Txn)
		}
	})
}

// F30: a damaged known-good set is components_damaged before c_switching, and
// rollback_failed with needs_attention at recovery.
func TestDamagedKnownGoodSet(t *testing.T) {
	damage := func(v *tenv) {
		p := filepath.Join(v.root, "components", "1.0.0", unitRecover)
		orig, _ := os.ReadFile(p)
		if err := os.WriteFile(p, append(orig, "# damaged\n"...), 0o644); err != nil {
			v.t.Fatal(err)
		}
	}
	t.Run("activate answers components_damaged and starts no component transaction", func(t *testing.T) {
		v, _ := newComponentEnv(t)
		damage(v)
		before := snapshot(t, v.root)
		wantErrCode(t, v.confirm(newVersion), codeComponentsDamaged)
		if d := diffSnapshots(before, snapshot(t, v.root)); len(d) != 0 || v.phase() != phaseStaged || len(v.sd.acting()) != 0 || v.h.exit.Load() != 0 {
			t.Fatalf("a refused activate changed something: %v", d)
		}
	})
	t.Run("stage answers components_damaged when a record exists", func(t *testing.T) {
		v, _ := newComponentEnv(t)
		wantOK(t, v.request(discardLine(newVersion)))
		damage(v)
		// The damaged file is also a file of the active set, so the staged
		// release differs in it; the reference hash refuses before any change.
		v.place(newVersion, v.componentRelease(newVersion).build())
		wantErrCode(t, v.stage(newVersion), codeComponentsDamaged)
		v.wantClean()
	})
	t.Run("at recovery: rollback_failed and needs_attention (row c8)", func(t *testing.T) {
		v, s := newComponentEnv(t)
		wantOK(t, v.confirm(newVersion))
		damage(v) // after c_switching, before the rollback compares
		if code := s.recover(false); code != 1 {
			t.Fatalf("exit %d, want 1", code)
		}
		st, _ := v.readState()
		if st.Txn.Phase != phaseRollbackFailed || !st.NeedsAttention || st.Last.Outcome != outcomeRollbackFailed || st.Last.ReasonCode != reasonKnownGoodDamaged {
			t.Fatalf("state %+v last %+v", st.Txn, st.Last)
		}
		if v.link(linkActive) != "1.0.0" || v.current() != "1.0.0" {
			t.Fatalf("active %s current %s", v.link(linkActive), v.current())
		}
		if s := v.status(); !s.NeedsAttention {
			t.Fatal("status does not report needs_attention")
		}
		// The next boot ends the transaction; the attention flag stays.
		if code := s.recover(false); code != 0 {
			t.Fatalf("second recovery: exit %d", code)
		}
		st, _ = v.readState()
		if st.Txn.Phase != phaseIdle || !st.NeedsAttention {
			t.Fatalf("after the next boot: %+v", st)
		}
	})
}

// The self-check of the new helper (D1, rules 5 and 6): each failed condition
// and its reason from the closed set.
func TestSelfCheck(t *testing.T) {
	for name, tt := range map[string]struct {
		mutate func(v *tenv, h *handler)
		want   string
	}{
		"pass": {func(*tenv, *handler) {}, ""},
		"the own image has other bytes": {func(v *tenv, h *handler) {
			h.selfImage = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("another binary")), nil }
		}, reasonHelperMismatch},
		"the own image cannot be read": {func(v *tenv, h *handler) {
			h.selfImage = func() (io.ReadCloser, error) { return nil, os.ErrPermission }
		}, reasonHelperMismatch},
		"the active helper file has other bytes": {func(v *tenv, h *handler) {
			os.WriteFile(filepath.Join(v.root, "components", newVersion, "oarfish-update-helper"), elfStub("swapped"), 0o755)
		}, reasonHelperMismatch},
		"active does not name components_to": {func(v *tenv, h *handler) {
			os.Remove(filepath.Join(v.root, "components", linkActive))
			os.Symlink("1.0.0", filepath.Join(v.root, "components", linkActive))
		}, reasonHelperMismatch},
		"another embedded key id (rule 5)":                 {func(v *tenv, h *handler) { h.keyID = otherKeyID }, reasonKeyChanged},
		"a protocol range above the old helper's (rule 6)": {func(v *tenv, h *handler) { h.protoMin, h.protoMax = 3, 4 }, reasonProtocolMismatch},
		"a protocol range that overlaps":                   {func(v *tenv, h *handler) { h.protoMin, h.protoMax = 1, 2 }, ""},
	} {
		t.Run(name, func(t *testing.T) {
			v, s := newComponentEnv(t)
			wantOK(t, v.confirm(newVersion))
			h := s.process(linkActive)
			tt.mutate(v, h)
			e, code := h.open(true)
			if code != "" {
				t.Fatal(code)
			}
			defer e.close()
			st, _ := v.readState()
			if got := (&txnRun{h: h, e: e, st: st, mode: modeLive}).selfCheck(); got != tt.want {
				t.Fatalf("reason %q, want %q", got, tt.want)
			}
		})
	}
	// The reasons are a closed set, and each is a valid reason code.
	if len(componentRollbackReasons) != 8 {
		t.Fatalf("%d reasons", len(componentRollbackReasons))
	}
	for _, r := range componentRollbackReasons {
		if !reasonPattern.MatchString(r) {
			t.Errorf("reason %q", r)
		}
	}
}

// failedFirstStart runs a transaction whose new helper fails its self-check
// and checks the rollback: components_rolled_back with the reason, the cooldown
// of E4, two restarts of the helper and no third (A1).
func failedFirstStart(t *testing.T, mutate func(h *handler), reason string) {
	t.Helper()
	v, s := newComponentEnv(t)
	old := v.h
	s.mutate = func(h *handler, image string) {
		if isNewImage(image) {
			mutate(h)
		}
	}
	wantOK(t, v.confirm(newVersion))
	s.settle()
	st := checkSettled(t, v, false)
	if st.Last == nil || st.Last.Outcome != outcomeComponentsRolledBack || st.Last.ReasonCode != reason || st.Last.ComponentsUpdated {
		t.Fatalf("result %+v, want components_rolled_back %s", st.Last, reason)
	}
	if v.link(linkActive) != "1.0.0" || v.link(linkKnownGood) != "1.0.0" || v.current() != "1.0.0" {
		t.Fatalf("active %s known-good %s current %s", v.link(linkActive), v.link(linkKnownGood), v.current())
	}
	// The hand-over and the hand-back, and nothing else: the process that
	// serves now is the old binary again, a new process.
	if len(s.restartAt) != 2 || s.restartAt[1]-s.restartAt[0] >= 10 || s.recovers != 0 || v.h == old || v.h.exit.Load() != 0 {
		t.Fatalf("restarts %v, recover runs %d, exit %d", s.restartAt, s.recovers, v.h.exit.Load())
	}
	if own, _ := v.h.ownImageHash(); own != sumHex(baseComponents["oarfish-update-helper"]) {
		t.Fatal("the serving process is not the old helper")
	}
	// The old helper reloaded once, the new one once after its switch back; the
	// service was never restarted.
	if got, want := v.sd.acting(), []string{"daemon-reload", "daemon-reload"}; !slices.Equal(got, want) {
		t.Fatalf("systemctl calls %v, want %v", got, want)
	}
	// E4: the version for 15 minutes and the 60 s marker.
	if st.Cool.RefuseVersion != newVersion || st.Cool.RefuseUntilUptimeS-st.Cool.RefuseSetAtUptimeS != cooldownSameS || st.Cool.LastActivateSetAtUptimeS != v.uptime.Load() {
		t.Fatalf("cooldown %+v", st.Cool)
	}
	if sr := v.status(); sr.Phase != phaseIdle || sr.CooldownVersion != newVersion || sr.LastResult.Outcome != outcomeComponentsRolledBack {
		t.Fatalf("status %+v", sr)
	}
	v.place(newVersion, v.componentRelease(newVersion).build())
	wantOK(t, v.stage(newVersion))
	wantErrCode(t, v.confirm(newVersion), codeCooldown)
	want := []string{
		"phase=c_switching to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok",
		"phase=c_switched to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok",
		"phase=c_rolling_back to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok",
		"phase=components_rolled_back to=1.1.0 from=1.0.0 cid=0123456789abcdef result=" + reason,
	}
	if got := journalLines(v); !slices.Equal(got[2:6], want) {
		t.Fatalf("journal:\n%s", strings.Join(got, "\n"))
	}
}

// F26: the key rule. The pre-check is at stage (TestStageComponentRefusals);
// the post-check is the new helper's own, against the key id the old helper
// recorded.
func TestKeyRule(t *testing.T) {
	t.Run("pre-check: the manifest declares another key id", func(t *testing.T) {
		v := newTenv(t)
		r := v.componentRelease(newVersion)
		r.man["components"].(map[string]any)["key_id"] = otherKeyID
		v.place(newVersion, r.build())
		wantErrCode(t, v.stage(newVersion), codeKeyChanged)
		v.wantClean()
	})
	t.Run("post-check: the new helper embeds another key", func(t *testing.T) {
		failedFirstStart(t, func(h *handler) { h.keyID = otherKeyID }, reasonKeyChanged)
	})
}

// F28: a new helper whose protocol range does not overlap the old helper's
// rolls the components back.
func TestNewHelperWithAWrongProtocol(t *testing.T) {
	failedFirstStart(t, func(h *handler) { h.protoMin, h.protoMax = protocolMax+2, protocolMax+2 }, reasonProtocolMismatch)
}

// D1: a new helper that is not the bytes the old helper recorded, or cannot
// read its own image, rolls the components back.
func TestNewHelperWithOtherBytes(t *testing.T) {
	t.Run("another image", func(t *testing.T) {
		failedFirstStart(t, func(h *handler) {
			h.selfImage = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("not the staged helper")), nil }
		}, reasonHelperMismatch)
	})
	t.Run("an image that cannot be read", func(t *testing.T) {
		failedFirstStart(t, func(h *handler) {
			h.selfImage = func() (io.ReadCloser, error) { return nil, errors.New("no access") }
		}, reasonHelperMismatch)
	})
}

// A1: one transaction restarts the helper at most twice within 10 seconds.
// The supervisor fails every test at a third restart; this one shows the two
// cases by name.
func TestHelperRestartsAtMostTwice(t *testing.T) {
	t.Run("success: one restart", func(t *testing.T) {
		v, s := newComponentEnv(t)
		wantOK(t, v.confirm(newVersion))
		s.settle()
		if len(s.restartAt) != 1 {
			t.Fatalf("restarts %v", s.restartAt)
		}
	})
	t.Run("failed self-check: the hand-over and the hand-back", func(t *testing.T) {
		v, s := newComponentEnv(t)
		s.mutate = func(h *handler, image string) {
			if isNewImage(image) {
				h.keyID = otherKeyID
			}
		}
		wantOK(t, v.confirm(newVersion))
		s.settle()
		if len(s.restartAt) != 2 || v.h.exit.Load() != 0 {
			t.Fatalf("restarts %v, exit %d", s.restartAt, v.h.exit.Load())
		}
	})
	t.Run("a third restart is caught by the fake", func(t *testing.T) {
		s := &supervisor{v: newTenv(t)}
		s.countRestart()
		s.countRestart()
		if n := len(s.restartAt); n != 2 {
			t.Fatal(n)
		}
		s.v.uptime.Add(10) // outside the window: allowed again
		s.countRestart()
	})
}

// F27: a new helper that does not start. systemd runs the recover unit at the
// first failed exit; it runs the known-good binary, rolls the components back
// and resets the failure of the helper service; the old helper answers again.
func TestNewHelperDoesNotStart(t *testing.T) {
	v, s := newComponentEnv(t)
	s.crash = map[string]bool{string(elfStub("helper " + newVersion)): true}
	wantOK(t, v.confirm(newVersion))
	s.settle()
	st := checkSettled(t, v, false)
	if st.Last.Outcome != outcomeComponentsRolledBack || st.Last.ReasonCode != reasonInterrupted {
		t.Fatalf("result %+v", st.Last)
	}
	if s.recovers != 1 || len(s.restartAt) != 2 {
		t.Fatalf("recover runs %d, restarts %v", s.recovers, s.restartAt)
	}
	// The recovery: the switch back, daemon-reload, reset-failed of the helper service.
	if got, want := v.sd.acting(), []string{"daemon-reload", "daemon-reload", "reset-failed oarfish-update-helper.service"}; !slices.Equal(got, want) {
		t.Fatalf("systemctl calls %v, want %v", got, want)
	}
	if sr := v.status(); !sr.OK || sr.Phase != phaseIdle || sr.LastResult.Outcome != outcomeComponentsRolledBack {
		t.Fatalf("the old helper does not answer: %+v", sr)
	}
	if own, _ := v.h.ownImageHash(); own != sumHex(baseComponents["oarfish-update-helper"]) {
		t.Fatal("the serving process is not the old helper")
	}
}

// The recover subcommand resets the failure of the helper service only when
// OnFailure= started it, not at boot.
func TestResetFailedOnlyWhenStartedByOnFailure(t *testing.T) {
	for _, onFailure := range []bool{false, true} {
		v, s := newComponentEnv(t)
		wantOK(t, v.confirm(newVersion))
		if code := s.recover(onFailure); code != 0 {
			t.Fatalf("exit %d", code)
		}
		got := slices.Contains(v.sd.acting(), "reset-failed oarfish-update-helper.service")
		if got != onFailure {
			t.Fatalf("started by OnFailure=%v: reset-failed called %v", onFailure, got)
		}
		v.wantIdleWith(outcomeComponentsRolledBack, reasonInterrupted)
	}
	// The process knows it from the environment variable that systemd sets.
	v := newTenv(t)
	o := testOptions(v.root)
	o.stateDir = v.stateDir
	seen := ""
	o.getenv = func(name string) string { seen = name; return helperUnit }
	if code := recoverMain(io.Discard, o); code != 0 || seen != "MONITOR_UNIT" {
		t.Fatalf("exit %d, read %q", code, seen)
	}
}

// F31: the components committed, the release then fails its health check. Only
// the release is rolled back; the new helper stays and the result says so.
func TestReleaseFailsAfterTheComponentsCommitted(t *testing.T) {
	v, s := newComponentEnv(t)
	v.sd.beh[newVersion] = bCrash
	wantOK(t, v.confirm(newVersion))
	s.settle()
	st := checkSettled(t, v, false)
	if st.Last.Outcome != outcomeRolledBack || st.Last.ReasonCode != reasonStartFailed || !st.Last.ComponentsUpdated {
		t.Fatalf("result %+v", st.Last)
	}
	if v.current() != "1.0.0" || v.link(linkActive) != newVersion || v.link(linkKnownGood) != newVersion || v.sd.running != "1.0.0" {
		t.Fatalf("current %s active %s known-good %s running %s", v.current(), v.link(linkActive), v.link(linkKnownGood), v.sd.running)
	}
	if len(s.restartAt) != 1 || s.recovers != 0 {
		t.Fatalf("restarts %v", s.restartAt)
	}
	sr := v.status()
	if sr.LastResult == nil || sr.LastResult.Outcome != outcomeRolledBack || !sr.LastResult.ComponentsUpdated || sr.CooldownVersion != newVersion {
		t.Fatalf("status %+v %+v", sr, sr.LastResult)
	}
	// The old release works with the new helper: the next update needs no
	// component transaction, because the helper files are installed now.
	v.uptime.Add(cooldownSameS + 1)
	v.sd.beh[newVersion] = bOK
	v.place(newVersion, v.componentRelease(newVersion).build())
	if r := wantOK(t, v.stage(newVersion)); r.ComponentsDiffer {
		t.Fatalf("the committed components differ again: %v", r.Differs)
	}
	wantOK(t, v.activate(newVersion))
	if v.current() != newVersion || v.h.exit.Load() != 0 {
		t.Fatal("the release was not applied by the new helper")
	}
}

// D4: socket_restart_pending is set when the normalised socket text changes,
// stored with the uptime, reported by status and void after a reboot (row c7).
func TestSocketRestartPending(t *testing.T) {
	t.Run("a changed directive sets the flag", func(t *testing.T) {
		v := newTenv(t)
		v.installOarfish()
		v.fakeSystemd()
		// The installed socket unit is an older text; installed units are not linted.
		p := filepath.Join(v.root, "components", "1.0.0", unitHelperSocket)
		os.WriteFile(p, bytes.Replace(baseComponents[unitHelperSocket], []byte("Backlog=8"), []byte("Backlog=4"), 1), 0o644)
		s := &supervisor{v: v}
		v.place(newVersion, v.componentRelease(newVersion).build())
		wantOK(t, v.stage(newVersion))
		wantOK(t, v.confirm(newVersion))
		if st, _ := v.readState(); !st.Txn.SocketTextChanged || st.Txn.SocketRestartPending {
			t.Fatalf("c_switched state %+v", st.Txn)
		}
		if v.status().SocketRestartPending {
			t.Fatal("pending before the components committed")
		}
		at := v.uptime.Load()
		s.settle()
		st := checkSettled(t, v, false)
		if !st.Txn.SocketRestartPending || st.Txn.SocketRestartSetAtUptimeS != at {
			t.Fatalf("flag %v set at %d, want %d", st.Txn.SocketRestartPending, st.Txn.SocketRestartSetAtUptimeS, at)
		}
		if !v.status().SocketRestartPending {
			t.Fatal("status does not report the flag")
		}
		// A reboot: the uptime is smaller than the one at which the flag was set.
		v.uptime.Store(40)
		if v.status().SocketRestartPending {
			t.Fatal("the flag is not void after a reboot")
		}
	})
	t.Run("comments, Description and Documentation do not", func(t *testing.T) {
		v := newTenv(t)
		v.installOarfish()
		v.fakeSystemd()
		p := filepath.Join(v.root, "components", "1.0.0", unitHelperSocket)
		os.WriteFile(p, bytes.Replace(baseComponents[unitHelperSocket], []byte("Description="), []byte("Description=an older text"), 1), 0o644)
		s := &supervisor{v: v}
		v.place(newVersion, v.componentRelease(newVersion, unitHelperSocket).build())
		wantOK(t, v.stage(newVersion))
		wantOK(t, v.confirm(newVersion))
		s.settle()
		if st := checkSettled(t, v, false); st.Txn.SocketRestartPending || v.status().SocketRestartPending {
			t.Fatal("pending for a change that is not in the normalised text")
		}
	})
	t.Run("a rollback of the components does not set it", func(t *testing.T) {
		v := newTenv(t)
		v.installOarfish()
		v.fakeSystemd()
		p := filepath.Join(v.root, "components", "1.0.0", unitHelperSocket)
		os.WriteFile(p, bytes.Replace(baseComponents[unitHelperSocket], []byte("Backlog=8"), []byte("Backlog=4"), 1), 0o644)
		s := &supervisor{v: v, mutate: func(h *handler, image string) {
			if isNewImage(image) {
				h.keyID = otherKeyID
			}
		}}
		v.place(newVersion, v.componentRelease(newVersion).build())
		wantOK(t, v.stage(newVersion))
		wantOK(t, v.confirm(newVersion))
		s.settle()
		if st, _ := v.readState(); st.Txn.SocketRestartPending || st.Last.Outcome != outcomeComponentsRolledBack {
			t.Fatalf("state %+v", st.Txn)
		}
	})
}

// F29 (the logic, class C): power is lost in each component phase that U13
// names: at c_switching, after the link switch, in the new helper's first
// start, and around the known-good switch. After the next boot the state is
// the one the table of U15 rule 4 gives, and no link names an incomplete set.
func TestPowerLossInEachComponentPhase(t *testing.T) {
	for _, c := range []struct {
		name string
		// at: power is lost at this call, before it runs (or right after it).
		at             func(op fsOp, phase string) bool
		after          bool
		outcome, cause string
		set            string
		updated        bool
	}{
		{"c_switching, before the link switch (row c2)",
			func(op fsOp, phase string) bool { return op.Kind == "symlink" && phase == phaseCSwitching }, false,
			outcomeAborted, reasonStageExpired, "1.0.0", false},
		{"at the link switch, the rename not done (row c3, old)",
			func(op fsOp, phase string) bool { return op.Kind == "rename" && op.Path2 == "components/active" }, false,
			outcomeAborted, reasonStageExpired, "1.0.0", false},
		{"after the link switch (row c3, new)",
			func(op fsOp, phase string) bool { return op.Kind == "rename" && op.Path2 == "components/active" }, true,
			outcomeComponentsRolledBack, reasonInterrupted, "1.0.0", false},
		{"the new helper's first start (row c4)",
			func(op fsOp, phase string) bool { return op.Tree == treeState && phase == phaseCSwitched }, false,
			outcomeComponentsRolledBack, reasonInterrupted, "1.0.0", false},
		{"before the known-good switch (row c5, not persisted)",
			func(op fsOp, phase string) bool { return op.Kind == "rename" && op.Path2 == "components/known-good" }, false,
			outcomeComponentsRolledBack, reasonInterrupted, "1.0.0", false},
		{"after the known-good switch (rows c5 and c6)",
			func(op fsOp, phase string) bool { return op.Kind == "rename" && op.Path2 == "components/known-good" }, true,
			outcomeAborted, reasonInterrupted, newVersion, true},
		{"after c_committed, before the release phases (row c6)",
			func(op fsOp, phase string) bool { return op.Tree == treeState && phase == phaseCCommitted }, false,
			outcomeAborted, reasonInterrupted, newVersion, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			v, s := newComponentEnv(t)
			g := &faultGate{}
			v.sd.gate = g
			hit := false
			v.h.wrapFS = func(inner fsops) fsops {
				f := newTestFS(noSyncFS{inner})
				f.before = func(op fsOp) error {
					if err := g.step(); err != nil {
						return err
					}
					if !hit && c.at(op, v.phase()) {
						hit = true
						g.mu.Lock()
						g.stopped = true
						g.mu.Unlock()
						if !c.after {
							return errStopped
						}
					}
					return nil
				}
				return f
			}
			v.confirm(newVersion)
			s.settle()
			if !hit {
				t.Fatal("the phase was not reached")
			}
			checkComponentDisk(t, v)
			v.h.wrapFS, v.sd.gate = nil, nil
			if code := s.reboot(); code != 0 {
				t.Fatalf("recover exit %d", code)
			}
			st := checkSettled(t, v, false)
			if st.Last == nil || st.Last.Outcome != c.outcome || st.Last.ReasonCode != c.cause || st.Last.ComponentsUpdated != c.updated {
				t.Fatalf("result %+v, want %s %s", st.Last, c.outcome, c.cause)
			}
			if v.link(linkActive) != c.set || v.link(linkKnownGood) != c.set || v.current() != "1.0.0" {
				t.Fatalf("active %s known-good %s current %s", v.link(linkActive), v.link(linkKnownGood), v.current())
			}
		})
	}
}

// ---- the rows of the component table (U15 rule 4) -----------------------------

// atPhase runs the transaction up to the hand-over and then sets the state and
// the links to what a crash in the given phase leaves.
func atPhase(t *testing.T, phase string, active, knownGood string) (*tenv, *supervisor) {
	t.Helper()
	v, s := newComponentEnv(t)
	wantOK(t, v.confirm(newVersion))
	v.h.exit.Store(0)
	st, _ := v.readState()
	st.Txn.Phase = phase
	v.writeState(st)
	for link, id := range map[string]string{linkActive: active, linkKnownGood: knownGood} {
		p := filepath.Join(v.root, "components", link)
		os.Remove(p)
		if err := os.Symlink(id, p); err != nil {
			t.Fatal(err)
		}
	}
	v.journal.b.Reset()
	return v, s
}

// Row c1: a component set in place that neither link nor the state names.
func TestComponentRowC1UnreferencedSet(t *testing.T) {
	t.Run("a crash of stage after the set was moved", func(t *testing.T) {
		v := newTenv(t)
		v.installOarfish()
		v.place(newVersion, v.componentRelease(newVersion).build())
		v.h.wrapFS = func(inner fsops) fsops {
			f := newTestFS(inner)
			f.before = func(op fsOp) error {
				if op.Kind == "rename" && op.Path2 == "components/"+newVersion {
					f.mu.Lock()
					f.stopAt = f.count + 1 // the rename runs; power is lost right after it
					f.mu.Unlock()
				}
				return nil
			}
			return f
		}
		v.stage(newVersion)
		if !setComplete(v, newVersion) || v.phase() != phaseStaging {
			t.Fatalf("not the state of row c1: phase %s", v.phase())
		}
		v.h.wrapFS = nil
		if code := v.recoverBoot(); code != 0 {
			t.Fatalf("exit %d", code)
		}
		if v.exists("components/" + newVersion) {
			t.Fatal("the unreferenced set stays")
		}
		v.wantIdleWith(outcomeAborted, reasonInterrupted)
		v.wantNoLeftovers()
		checkComponentDisk(t, v)
	})
	t.Run("an unreferenced set with an idle state, at boot and when serve starts", func(t *testing.T) {
		for _, mode := range []string{"boot", "live"} {
			v := newTenv(t)
			v.installOarfish()
			for _, id := range []string{"0.7.0", "0.8.0"} {
				os.MkdirAll(filepath.Join(v.root, "components", id), 0o755)
			}
			st := freshState(helperID)
			st.ComponentsPrevious = "0.8.0" // the set the last commit replaced stays
			v.writeState(st)
			doRecover(t, v, mode)
			if v.exists("components/0.7.0") || !v.exists("components/0.8.0") || !v.exists("components/1.0.0") {
				t.Fatalf("%s: wrong sets removed", mode)
			}
		}
	})
	t.Run("nothing is removed when a link cannot be read", func(t *testing.T) {
		v := newTenv(t)
		os.MkdirAll(filepath.Join(v.root, "components", "0.7.0"), 0o755)
		os.Remove(filepath.Join(v.root, "components", linkKnownGood))
		v.writeState(freshState(helperID))
		v.recoverBoot()
		if !v.exists("components/0.7.0") {
			t.Fatal("removed a set without knowing what known-good names")
		}
	})
}

// Row c2: c_switching, the link not switched. The intent is cleared; the staged
// release follows U7 row 4.
func TestComponentRowC2IntentBeforeTheSwitch(t *testing.T) {
	t.Run("at boot the staged release is removed", func(t *testing.T) {
		v, s := atPhase(t, phaseCSwitching, "1.0.0", "1.0.0")
		if code := s.recover(false); code != 0 {
			t.Fatalf("exit %d", code)
		}
		v.wantIdleWith(outcomeAborted, reasonStageExpired)
		if v.exists("components/"+newVersion) || v.exists("releases/"+newVersion) || len(v.sd.acting()) != 1 {
			t.Fatalf("leftovers, or calls %v", v.sd.acting())
		}
	})
	t.Run("a running helper keeps it staged", func(t *testing.T) {
		v, s := atPhase(t, phaseCSwitching, "1.0.0", "1.0.0")
		s.nextStart()
		st := checkSettled(t, v, true)
		if st.Txn.Phase != phaseStaged || st.Last != nil || !setComplete(v, newVersion) || len(s.restartAt) != 0 {
			t.Fatalf("phase %s last %+v restarts %v", st.Txn.Phase, st.Last, s.restartAt)
		}
		// It can be activated again after the 60 s marker.
		v.uptime.Add(cooldownAnyS)
		wantOK(t, v.confirm(newVersion))
		s.settle()
		if checkSettled(t, v, false).Last.Outcome != outcomeSuccess {
			t.Fatal("the second attempt did not succeed")
		}
	})
}

// Rows c3 and c4: active names the new set and nothing is committed. The
// recover unit switches back; so does a serve process that is not the new
// helper; the new helper's own first start moves forward (that is the success
// path, TestComponentTransactionSuccess).
func TestComponentRowsC3C4ActiveSwitched(t *testing.T) {
	for _, phase := range []string{phaseCSwitching, phaseCSwitched} {
		t.Run(phase+" at boot", func(t *testing.T) {
			v, s := atPhase(t, phase, newVersion, "1.0.0")
			if code := s.recover(false); code != 0 {
				t.Fatalf("exit %d", code)
			}
			st := checkSettled(t, v, false)
			if st.Last.Outcome != outcomeComponentsRolledBack || st.Last.ReasonCode != reasonInterrupted || v.link(linkActive) != "1.0.0" {
				t.Fatalf("result %+v", st.Last)
			}
			// The reload after the switch, before the socket starts (row c4).
			if got := v.sd.acting(); !slices.Equal(got[len(got)-1:], []string{"daemon-reload"}) {
				t.Fatalf("calls %v", got)
			}
			want := []string{
				"phase=c_rolling_back to=1.1.0 from=1.0.0 cid=0123456789abcdef result=ok",
				"phase=components_rolled_back to=1.1.0 from=1.0.0 cid=0123456789abcdef result=interrupted",
			}
			if got := journalLines(v); !slices.Equal(got, want) {
				t.Fatalf("journal:\n%s", strings.Join(got, "\n"))
			}
		})
	}
	t.Run("c_switching with the new set active: a serve start rolls back, also as the new binary", func(t *testing.T) {
		// c_switched is not durable, so no process may take this for a first start.
		v, s := atPhase(t, phaseCSwitching, newVersion, "1.0.0")
		s.nextStart()
		st := checkSettled(t, v, false)
		if st.Last.Outcome != outcomeComponentsRolledBack || st.Last.ReasonCode != reasonInterrupted || len(s.restartAt) != 1 {
			t.Fatalf("result %+v restarts %v", st.Last, s.restartAt)
		}
	})
	t.Run("c_switched: a serve process that fails the self-check rolls back", func(t *testing.T) {
		v, s := atPhase(t, phaseCSwitched, newVersion, "1.0.0")
		s.mutate = func(h *handler, image string) {
			h.selfImage = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("another process")), nil }
			s.mutate = nil
		}
		s.nextStart()
		if st := checkSettled(t, v, false); st.Last.ReasonCode != reasonHelperMismatch || v.link(linkActive) != "1.0.0" {
			t.Fatalf("result %+v", st.Last)
		}
	})
}

// Row c5: the new helper verified, known-good switched, c_committed not yet
// durable. Both links name the new set: any recovery treats it as committed.
// If the known-good switch did not persist, it is row c3.
func TestComponentRowC5VerifiedNotCommitted(t *testing.T) {
	t.Run("both links new, at boot: committed, then row c6", func(t *testing.T) {
		v, s := atPhase(t, phaseCVerified, newVersion, newVersion)
		if code := s.recover(false); code != 0 {
			t.Fatalf("exit %d", code)
		}
		st := checkSettled(t, v, false)
		if st.Last.Outcome != outcomeAborted || st.Last.ReasonCode != reasonInterrupted || !st.Last.ComponentsUpdated || v.current() != "1.0.0" {
			t.Fatalf("result %+v", st.Last)
		}
		if got := journalLines(v); !strings.HasPrefix(got[0], "phase=c_committed ") {
			t.Fatalf("journal:\n%s", strings.Join(got, "\n"))
		}
		if got := v.sd.acting(); !slices.Equal(got[len(got)-1:], []string{"daemon-reload"}) {
			t.Fatalf("calls %v", got)
		}
	})
	t.Run("both links new, a serve start: committed and continued", func(t *testing.T) {
		v, s := atPhase(t, phaseCVerified, newVersion, newVersion)
		s.nextStart()
		if st := checkSettled(t, v, false); st.Last.Outcome != outcomeSuccess || v.current() != newVersion {
			t.Fatalf("result %+v", st.Last)
		}
	})
	t.Run("known-good not switched, at boot: rolled back", func(t *testing.T) {
		v, s := atPhase(t, phaseCVerified, newVersion, "1.0.0")
		s.recover(false)
		if st := checkSettled(t, v, false); st.Last.Outcome != outcomeComponentsRolledBack || st.Last.ReasonCode != reasonInterrupted {
			t.Fatalf("result %+v", st.Last)
		}
	})
	t.Run("known-good not switched, the new helper starts again: forward", func(t *testing.T) {
		v, s := atPhase(t, phaseCVerified, newVersion, "1.0.0")
		s.nextStart()
		if st := checkSettled(t, v, false); st.Last.Outcome != outcomeSuccess {
			t.Fatalf("result %+v", st.Last)
		}
	})
}

// Row c6: c_committed, the release phases not started. At boot the release is
// aborted and the components stay; a running helper continues.
func TestComponentRowC6CommittedBeforeTheRelease(t *testing.T) {
	commit := func(t *testing.T) (*tenv, *supervisor) {
		v, s := atPhase(t, phaseCVerified, newVersion, newVersion)
		// Let the recovery of row c5 write c_committed, then stop before the release.
		h := s.process(linkKnownGood)
		e, _ := h.open(true)
		st, _ := v.readState()
		if code := (&txnRun{h: h, e: e, st: st, mode: modeBoot}).commitComponents(); code != "" {
			t.Fatal(code)
		}
		e.close()
		if v.phase() != phaseCCommitted {
			t.Fatal(v.phase())
		}
		v.journal.b.Reset()
		return v, s
	}
	t.Run("at boot", func(t *testing.T) {
		v, s := commit(t)
		calls := len(v.sd.acting())
		if code := s.recover(false); code != 0 {
			t.Fatalf("exit %d", code)
		}
		st := checkSettled(t, v, false)
		if st.Last.Outcome != outcomeAborted || !st.Last.ComponentsUpdated || v.exists("releases/"+newVersion) || len(v.sd.acting()) != calls {
			t.Fatalf("result %+v", st.Last)
		}
		// The administrator can apply the update again; it is a plain release now.
		v.uptime.Add(cooldownAnyS)
		v.place(newVersion, v.componentRelease(newVersion).build())
		if r := wantOK(t, v.stage(newVersion)); r.ComponentsDiffer {
			t.Fatal("components differ after they committed")
		}
	})
	t.Run("a serve start continues with the release phases", func(t *testing.T) {
		v, s := commit(t)
		s.nextStart()
		if st := checkSettled(t, v, false); st.Last.Outcome != outcomeSuccess || !st.Last.ComponentsUpdated || len(s.restartAt) != 0 {
			t.Fatalf("result %+v", st.Last)
		}
		if got := journalLines(v); !strings.HasPrefix(got[0], "phase=switching ") {
			t.Fatalf("journal:\n%s", strings.Join(got, "\n"))
		}
	})
}

// Row c8: an interrupted rollback of the components is finished, with the
// reason that started it.
func TestComponentRowC8RollingBack(t *testing.T) {
	for _, active := range []string{newVersion, "1.0.0"} {
		t.Run("active names "+active, func(t *testing.T) {
			v, s := atPhase(t, phaseCRollingBack, active, "1.0.0")
			st, _ := v.readState()
			st.Txn.RollbackReason = reasonKeyChanged
			v.writeState(st)
			if code := s.recover(false); code != 0 {
				t.Fatalf("exit %d", code)
			}
			fin := checkSettled(t, v, false)
			if fin.Last.Outcome != outcomeComponentsRolledBack || fin.Last.ReasonCode != reasonKeyChanged || v.link(linkActive) != "1.0.0" {
				t.Fatalf("result %+v", fin.Last)
			}
		})
	}
	// The damaged case is TestDamagedKnownGoodSet.
}

// Row c9: what an interrupted pruning or link switch leaves is removed.
func TestComponentRowC9Leftovers(t *testing.T) {
	v := newTenv(t)
	v.installOarfish()
	os.Mkdir(filepath.Join(v.root, "releases", ".trash.00000000000000ee"), 0o755)
	os.Symlink("9.9.9", filepath.Join(v.root, "components", ".new.00000000000000ef"))
	os.Symlink("9.9.9", filepath.Join(v.root, "components", ".new.not-ours"))
	v.writeState(freshState(helperID))
	if code := v.recoverBoot(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if v.exists("releases/.trash.00000000000000ee") || v.exists("components/.new.00000000000000ef") || !v.exists("components/.new.not-ours") {
		t.Fatal("leftovers not removed, or a foreign name removed")
	}
	// serve finds them as well.
	os.Symlink("9.9.9", filepath.Join(v.root, "components", ".new.00000000000000f0"))
	v.recoverLive()
	if v.exists("components/.new.00000000000000f0") {
		t.Fatal("serve left the temporary link")
	}
}

// state.json unreadable while an unproven set is active: active goes back to
// known-good and the result carries the reason state_unreadable.
func TestStateUnreadableWithAnUnprovenSet(t *testing.T) {
	v, s := atPhase(t, phaseCSwitched, newVersion, "1.0.0")
	os.WriteFile(filepath.Join(v.stateDir, stateFile), []byte("{ not json"), 0o600)
	s.nextStart()
	st, _ := v.readState()
	if st.Txn.Phase != phaseIdle || !st.NeedsAttention || st.Last.Outcome != outcomeComponentsRolledBack || st.Last.ReasonCode != reasonStateUnreadable ||
		st.Last.To != newVersion || st.Cool.RefuseVersion != newVersion {
		t.Fatalf("state %+v last %+v", st, st.Last)
	}
	if v.link(linkActive) != "1.0.0" || len(s.restartAt) != 1 || v.exists("components/"+newVersion) {
		t.Fatalf("active %s restarts %v", v.link(linkActive), s.restartAt)
	}
	checkComponentDisk(t, v)
}

// A failing call in the helper that hands over ends in a rollback by that
// helper, without a restart.
func TestHandOverFailures(t *testing.T) {
	for name, tt := range map[string]struct {
		setup  func(v *tenv)
		reason string
	}{
		"the link switch fails": {func(v *tenv) {
			v.h.wrapFS = func(inner fsops) fsops {
				f := newTestFS(inner)
				f.before = func(op fsOp) error {
					if op.Kind == "rename" && op.Path2 == "components/active" && v.phase() == phaseCSwitching {
						return errInjected
					}
					return nil
				}
				return f
			}
		}, reasonSwitchFailed},
		"daemon-reload fails": {func(v *tenv) { v.sd.reloadFail[1] = true }, reasonReloadFailed},
	} {
		t.Run(name, func(t *testing.T) {
			v, s := newComponentEnv(t)
			tt.setup(v)
			wantOK(t, v.confirm(newVersion))
			if v.h.exit.Load() != 0 {
				t.Fatal("the old helper handed over after a failure")
			}
			s.settle()
			st := checkSettled(t, v, false)
			if st.Last.Outcome != outcomeComponentsRolledBack || st.Last.ReasonCode != tt.reason || len(s.restartAt) != 0 {
				t.Fatalf("result %+v restarts %v", st.Last, s.restartAt)
			}
		})
	}
}

// ---- the hand-over on a real socket --------------------------------------------

// The old helper ends with exit code 75 only after the answer of activate has
// been written and the connection is finished; a connection that arrives during
// the hand-over waits and is answered by the next process.
func TestHandOverExitsAfterTheAnswer(t *testing.T) {
	v, s := newComponentEnv(t)
	s.async = true
	v.h.syncTxn = false
	o := testOptions(v.root)
	path := filepath.Join(shortDir(t), "s")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false) // as activatedListener does: the socket is systemd's
	kept, err := ln.File()     // systemd keeps the socket open
	if err != nil {
		t.Fatal(err)
	}
	defer kept.Close()

	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	old := v.h
	go func() {
		code, err := serveLoop(old, newServer(o, testUID, old), ln)
		done <- result{code, err}
	}()
	dial := func() *net.UnixConn {
		c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		c.SetDeadline(time.Now().Add(20 * time.Second))
		return c
	}
	c := dial()
	if _, err := c.Write(activateLine(newVersion, testCID, true)); err != nil {
		t.Fatal(err)
	}
	// The client is slow: it reads only after the process has ended.
	res := <-done
	if res.code != exitHandOver || res.err != nil {
		t.Fatalf("serve returned %d, %v; want 75", res.code, res.err)
	}
	raw, err := io.ReadAll(c)
	if err != nil {
		t.Fatalf("the answer was lost: %v", err)
	}
	c.Close()
	if r := wantOK(t, raw); r.Status != "accepted" {
		t.Fatalf("answer %s", raw)
	}
	if !lockFree(t, v.stateDir) || v.phase() != phaseCSwitched {
		t.Fatalf("after the exit: phase %s", v.phase())
	}

	// A request during the hand-over: nobody accepts yet.
	c2 := dial()
	if _, err := c2.Write([]byte(okStatus)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	// systemd starts the new binary on the same socket.
	fl, err := net.FileListener(kept)
	if err != nil {
		t.Fatal(err)
	}
	ln2 := fl.(*net.UnixListener)
	ln2.SetUnlinkOnClose(false)
	h := s.process(linkActive)
	v.h = h
	done2 := make(chan result, 1)
	go func() {
		code, err := serveLoop(h, newServer(o, testUID, h), ln2)
		done2 <- result{code, err}
	}()
	raw2, err := io.ReadAll(c2)
	c2.Close()
	if err != nil {
		t.Fatalf("the waiting request was not answered: %v", err)
	}
	if r := wantOK(t, raw2); !knownPhases[r.Phase] {
		t.Fatalf("status %s", raw2)
	}
	// The new helper finishes the transaction without a client request.
	deadline := time.Now().Add(20 * time.Second)
	for v.phase() != phaseIdle {
		if time.Now().After(deadline) {
			t.Fatalf("the transaction did not finish: phase %s", v.phase())
		}
		time.Sleep(5 * time.Millisecond)
	}
	ln2.Close()
	if res := <-done2; res.code != 0 {
		t.Fatalf("the new helper exited with %d", res.code)
	}
	if st := checkSettled(t, v, false); st.Last.Outcome != outcomeSuccess {
		t.Fatalf("result %+v", st.Last)
	}
}

// While the new helper proves itself and continues, requests answer busy and
// status answers with the phase; the helper does not exit on its idle timer.
func TestFirstStartHoldsTheLock(t *testing.T) {
	v, s := newComponentEnv(t)
	wantOK(t, v.confirm(newVersion))
	s.async = true
	h := s.process(linkActive)
	v.h = h
	release := make(chan struct{})
	reached := make(chan struct{}, 1)
	v.sd.onRestart = func() { reached <- struct{}{} }
	v.sd.restartHook = release
	h.startupRecovery()
	<-reached
	if h.running.Load() != 1 {
		t.Fatal("the transaction is not counted as running")
	}
	wantErrCode(t, v.stage("1.2.0"), codeBusy)
	wantErrCode(t, v.confirm(newVersion), codeBusy)
	wantErrCode(t, v.request(discardLine(newVersion)), codeBusy)
	if sr := v.status(); sr.Phase != phaseVerifying {
		t.Fatalf("status phase %s", sr.Phase)
	}
	close(release)
	h.wait()
	if st := checkSettled(t, v, false); st.Last.Outcome != outcomeSuccess {
		t.Fatalf("result %+v", st.Last)
	}
}

// When the lock is held while serve starts (the recover unit or the installer
// runs), the start-up recovery is tried again with the next request.
func TestStartupRecoveryIsRetriedWhenTheLockWasHeld(t *testing.T) {
	v, s := newComponentEnv(t)
	wantOK(t, v.confirm(newVersion))
	h := s.process(linkActive)
	v.h = h
	lk, err := lockDir(v.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	h.startupRecovery()
	if !h.recoveryPending.Load() || v.phase() != phaseCSwitched {
		t.Fatalf("pending %v phase %s", h.recoveryPending.Load(), v.phase())
	}
	if sr := v.status(); sr.Phase != phaseCSwitched {
		t.Fatalf("status %+v", sr)
	}
	lk.release()
	v.status()
	h.wait()
	if h.recoveryPending.Load() {
		t.Fatal("still pending")
	}
	if st := checkSettled(t, v, false); st.Last.Outcome != outcomeSuccess {
		t.Fatalf("result %+v", st.Last)
	}
}
