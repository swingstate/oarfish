package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
)

// The component transaction of ADR 0010 U15 rule 4 (owner decisions D1 to D6 of
// A15 and E1 to E4 of A16). A staged release whose four helper files differ from
// the installed ones is applied in two parts with one transaction id:
//
//	the helper that received `activate` (the old one)
//	  c_switching   intent, with everything the next process needs (E3)
//	                components/active -> the new set
//	  c_switched    daemon-reload, then the process ends with exit code 75
//	the helper that systemd starts next (the new one), before it serves a request
//	  self-check    D1, rule 5, rule 6; a failure switches active back
//	  c_verified    components/known-good -> the new set, daemon-reload (D2)
//	  c_committed   the components' own commit point
//	  switching ... the release phases of U5 and U6, by the new helper
//
// The old helper never executes the new binary and never restarts itself or the
// socket. A release that fails after c_committed is rolled back alone.
//
// Every phase is written and fsynced before its action. Every write goes through
// fsops, and the only program is systemctl with its compiled-in shapes.

const (
	// exitHandOver is the exit status that makes systemd start the helper again
	// without running the recover unit (SuccessExitStatus=75 and
	// RestartForceExitStatus=75 of the helper service, A1).
	exitHandOver = 75

	linkActive    = "active"
	linkKnownGood = "known-good"
	// componentTempPrefix is the temporary name of a component link, below
	// components/ (U2, U5: "components/.new.<id>").
	componentTempPrefix = ".new."

	// selfExePath is the executable image of the running process (D1).
	selfExePath = "/proc/self/exe"

	// monitorUnitVar is the environment variable that systemd sets for a unit
	// it starts through OnFailure=: the name of the unit that failed.
	// Assumption (systemd 251 and later, recalled, not observed on a device).
	monitorUnitVar = "MONITOR_UNIT"
	helperUnit     = "oarfish-update-helper.service"
)

// componentPath is the directory of a component set. The id has been validated
// (it is a version, U5), so it holds no separator.
func componentPath(id string) string { return "components/" + id }

// linkID is the set that a component link names. The only accepted link text
// is the bare id the installer and the helper write (a relative link to a
// directory beside it); anything else, and a missing link, gives "".
func (e *env) linkID(link string) string {
	target, err := e.opt.Readlink("components/" + link)
	if err != nil || !validVersion(target) {
		return ""
	}
	return target
}

// setDigests reads the SHA-256 of the four files of a component set, in the
// fixed order of U2. dir is components/<id> or a staging directory.
func (e *env) setDigests(dir string) ([4][sha256.Size]byte, error) {
	var d [4][sha256.Size]byte
	for i := range d {
		sum, err := fileDigest(e.opt, dir+"/"+componentNames[i])
		if err != nil {
			return d, err
		}
		d[i] = sum
	}
	return d, nil
}

// setDigest is the one hash of a component set (E2): the SHA-256 over the four
// file digests, each 32 bytes, concatenated in the fixed order of U2 (the
// helper, the socket unit, the helper service unit, the recover unit), as 64
// lowercase hexadecimal digits.
func setDigest(d [4][sha256.Size]byte) string {
	h := sha256.New()
	for i := range d {
		h.Write(d[i][:])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// switchComponentLink makes components/<link> name the set id with one atomic
// replacement: a link under a temporary name, rename over the old link, fsync
// of the directory (U5).
func (e *env) switchComponentLink(link, id string) error {
	if !validVersion(id) || (link != linkActive && link != linkKnownGood) {
		return errors.New("invalid component link")
	}
	nid, err := e.h.newID()
	if err != nil {
		return err
	}
	tmp := "components/" + componentTempPrefix + nid
	if err := e.fs.Symlink(treeOpt, id, tmp); err != nil {
		return err
	}
	if err := e.fs.Rename(treeOpt, tmp, "components/"+link); err != nil {
		e.fs.RemoveAll(treeOpt, tmp)
		return err
	}
	return e.fs.SyncDir(treeOpt, "components")
}

// knownGood compares the set that known-good names with the reference hash of
// E2. The record applies only to the set it was computed from: when known-good
// names another set (the installer moved it last) or the state holds no hash
// (never recorded, or rebuilt), there is nothing to compare with. Then, with
// record, the hash is taken from the disk now and written into st (changed),
// which is the residual risk that A16 states: damage before the first recording
// is not detected. ok false means the set is unreadable or differs.
func (e *env) knownGood(st *state, record bool) (ok, changed bool) {
	k := e.linkID(linkKnownGood)
	if k == "" {
		return false, false
	}
	d, err := e.setDigests(componentPath(k))
	if err != nil {
		return false, false
	}
	sum := setDigest(d)
	if st.Txn.KnownGoodID == k && st.Txn.KnownGoodSHA256 != "" {
		return sum == st.Txn.KnownGoodSHA256, false
	}
	if !record {
		return true, false
	}
	st.Txn.KnownGoodSHA256, st.Txn.KnownGoodID = sum, k
	return true, true
}

// componentKeep is the retention of U5 for component sets: the sets that the
// two links name, the set the last commit replaced, and the sets a transaction
// in the state names. ok false: a link is unreadable, so nothing may be removed.
func (e *env) componentKeep(st *state) (map[string]bool, bool) {
	a, k := e.linkID(linkActive), e.linkID(linkKnownGood)
	if a == "" || k == "" {
		return nil, false
	}
	return map[string]bool{a: true, k: true, st.ComponentsPrevious: true, st.Txn.ComponentsFrom: true, st.Txn.ComponentsTo: true}, true
}

// componentLeftovers lists what sweepComponents would remove.
func (e *env) componentLeftovers(st *state) []string {
	keep, ok := e.componentKeep(st)
	if !ok {
		return nil
	}
	d, err := e.opt.Open("components")
	if err != nil {
		return nil
	}
	entries, err := d.ReadDir(-1)
	d.Close()
	if err != nil {
		return nil
	}
	var out []string
	for _, ent := range entries {
		n := ent.Name()
		if id, isTemp := strings.CutPrefix(n, componentTempPrefix); isTemp && isHex(16)(id) {
			out = append(out, n)
		} else if ent.IsDir() && validVersion(n) && !keep[n] {
			out = append(out, n)
		}
	}
	return out
}

// sweepComponents removes the component directories that neither link nor the
// state names (U15 rule 4 rows c1 and c9, and the retention of U5 after a
// commit), each by a rename to releases/.trash.<id> and then the deletion, and
// the temporary links of an interrupted switch. Names that are not a version
// are never touched.
func (e *env) sweepComponents(st *state) error {
	for _, n := range e.componentLeftovers(st) {
		if strings.HasPrefix(n, componentTempPrefix) {
			if err := e.fs.RemoveAll(treeOpt, "components/"+n); err != nil {
				return err
			}
			continue
		}
		if err := e.trash(componentPath(n)); err != nil {
			return err
		}
	}
	return nil
}

// removeStagedSet removes the component set of a staged release that ends
// without being activated. A set that a link names, or the previous one, is
// never touched.
func (e *env) removeStagedSet(st *state) error {
	id := st.Txn.ComponentsTo
	if id == "" || id == st.ComponentsPrevious || id == e.linkID(linkActive) || id == e.linkID(linkKnownGood) {
		return nil
	}
	return e.trash(componentPath(id))
}

// ---- the process's own image (D1) ---------------------------------------------

// ownImageHash is the SHA-256 of the executable image of this process, read
// once, streamed with the 32 KiB buffer. Tests replace h.selfImage.
func (h *handler) ownImageHash() (string, error) {
	h.ownOnce.Do(func() {
		open := h.selfImage
		if open == nil {
			open = func() (io.ReadCloser, error) { return os.Open(selfExePath) }
		}
		f, err := open()
		if err != nil {
			h.ownErr = err
			return
		}
		defer f.Close()
		sum := sha256.New()
		n, err := io.CopyBuffer(sum, io.LimitReader(f, maxCompareBytes+1), make([]byte, chunk))
		if err != nil || n > maxCompareBytes {
			h.ownErr = errNotRegular
			return
		}
		h.ownSum = hex.EncodeToString(sum.Sum(nil))
	})
	return h.ownSum, h.ownErr
}

// handOver ends the process with exit code 75 once every connection has been
// answered and every transaction has released the lock: it stops the accept
// loop, and serve returns the status.
func (h *handler) handOver() {
	h.exit.Store(exitHandOver)
	if h.stop != nil {
		h.stop()
	}
}

// selfCheck is the proof of the new helper at its first start (U15 rule 4 step
// 4, D1, rules 5 and 6). It returns "" or the reason of the rollback. That
// state.json is readable is part of the proof by construction: without it the
// process does not know the transaction (rebuild then gives state_unreadable).
func (r *txnRun) selfCheck() string {
	h, e, t := r.h, r.e, r.st.Txn
	if t.ComponentsTo == "" || t.ComponentsHelperSHA256 == "" || e.linkID(linkActive) != t.ComponentsTo {
		return reasonHelperMismatch
	}
	onDisk, err := fileDigest(e.opt, "components/"+linkActive+"/"+componentNames[0])
	if err != nil || hex.EncodeToString(onDisk[:]) != t.ComponentsHelperSHA256 {
		return reasonHelperMismatch
	}
	if own, err := h.ownImageHash(); err != nil || own != t.ComponentsHelperSHA256 {
		return reasonHelperMismatch
	}
	if h.keyID == "" || h.keyID != t.ComponentsKeyID {
		return reasonKeyChanged
	}
	if t.ComponentsProtocolMin < 1 || int64(h.protoMax) < t.ComponentsProtocolMin || t.ComponentsProtocolMax < int64(h.protoMin) {
		return reasonProtocolMismatch
	}
	return ""
}

// ---- the helper that hands over -----------------------------------------------

// runComponents is the transaction after "accepted" when a component set is
// staged: the state says c_switching.
func (r *txnRun) runComponents() {
	defer r.release()
	t := r.st.Txn
	if err := r.e.switchComponentLink(linkActive, t.ComponentsTo); err != nil {
		r.rollbackComponents(reasonSwitchFailed)
		return
	}
	r.st.Txn.Phase = phaseCSwitched
	if err := r.e.saveState(r.st); err != nil {
		r.log(phaseCSwitched, string(codeIOError))
		r.st.Txn.Phase = phaseCSwitching
		r.rollbackComponents(reasonRecordFailed)
		return
	}
	r.log(phaseCSwitched, "ok")
	if _, err := r.h.systemctl(sysDaemonReload...); err != nil {
		r.rollbackComponents(reasonReloadFailed)
		return
	}
	// The next process is the new helper. This one executes nothing of it.
	r.h.handOver()
}

// ---- the new helper ------------------------------------------------------------

// verifyComponents runs after a passed self-check: c_verified, the switch of
// known-good, the commit. It returns "" when c_committed is durable.
func (r *txnRun) verifyComponents() errorCode {
	if r.st.Txn.Phase != phaseCVerified {
		r.st.Txn.Phase = phaseCVerified
		if err := r.e.saveState(r.st); err != nil {
			r.log(phaseCVerified, string(codeIOError))
			return codeIOError
		}
		r.log(phaseCVerified, "ok")
	}
	if to := r.st.Txn.ComponentsTo; r.e.linkID(linkKnownGood) != to {
		if err := r.e.switchComponentLink(linkKnownGood, to); err != nil {
			return codeIOError
		}
	}
	return r.commitComponents()
}

// commitComponents is the commit point of the components (row c5): both links
// name the new set. daemon-reload makes systemd read the recover unit of the
// new set (D2); when it fails the loaded text stays until the next reload or
// boot, and the commit is still right, because the disk is the truth.
func (r *txnRun) commitComponents() errorCode {
	h, e := r.h, r.e
	t := &r.st.Txn
	h.systemctl(sysDaemonReload...)
	d, err := e.setDigests(componentPath(t.ComponentsTo))
	if err != nil {
		return codeIOError
	}
	t.KnownGoodSHA256, t.KnownGoodID = setDigest(d), t.ComponentsTo
	t.Phase, t.ComponentsUpdated = phaseCCommitted, true
	if t.ComponentsFrom != t.ComponentsTo {
		r.st.ComponentsPrevious = t.ComponentsFrom
	}
	if t.SocketTextChanged {
		now, _ := h.uptime()
		t.SocketRestartPending, t.SocketRestartSetAtUptimeS = true, max(now, 1)
	}
	if err := e.saveState(r.st); err != nil {
		r.log(phaseCCommitted, string(codeIOError))
		return codeIOError
	}
	r.log(phaseCCommitted, "ok")
	// Retention (U15 rule 4 step 7). What a failure leaves is removed by the
	// next recovery (row c9).
	e.sweepComponents(r.st)
	return ""
}

// releasePhases continues the same transaction with the release phases (U15
// rule 4 step 5). The new helper reads the process that runs now itself (E3).
func (r *txnRun) releasePhases() {
	// The release was staged by another process, some time ago: look at the
	// binary that will run before current may name it. When it is gone (an
	// abort that removed it was interrupted before it was recorded), the
	// release is aborted and the components stay.
	if err := r.e.requireRegular(releasePath(r.st.Txn.To) + "/oarfish"); err != nil {
		r.abortRelease(installedVersion(r.h.root))
		return
	}
	r.prevPID, r.baseRestarts = -1, -1
	if p, err := r.h.show(); err == nil {
		r.prevPID, r.baseRestarts = p.MainPID, p.NRestarts
	}
	r.st.Txn.Phase = phaseSwitching
	if err := r.e.saveState(r.st); err != nil {
		r.log(phaseSwitching, string(codeIOError))
		return // the state says c_committed: the next start continues or aborts
	}
	r.log(phaseSwitching, "ok")
	r.runRelease()
}

// ---- rollback and recovery -----------------------------------------------------

// rollbackComponents is the rollback of the components (U15 rule 4, failure
// handling and row c8): write the intent, compare known-good with the reference
// hash, make active name what known-good names, daemon-reload, and end the
// transaction with components_rolled_back. A damaged rollback target is never
// switched to (E5): when the set differs or cannot be read, the transaction
// ends in rollback_failed and nothing else is touched, not even daemon-reload.
// The release was never activated, so its directory and the new set are
// removed. In a serve process the helper then hands back when it is not the
// binary that active names.
func (r *txnRun) rollbackComponents(reason string) errorCode {
	h, e := r.h, r.e
	if r.st.Txn.Phase == phaseCRollingBack {
		if r.st.Txn.RollbackReason != "" {
			reason = r.st.Txn.RollbackReason
		}
	} else {
		r.st.Txn.Phase, r.st.Txn.RollbackReason = phaseCRollingBack, reason
		if err := e.saveState(r.st); err != nil {
			// No action without a durable intent.
			r.log(phaseCRollingBack, string(codeIOError))
			return codeIOError
		}
		r.log(phaseCRollingBack, "ok")
	}
	t := r.st.Txn
	// E5: the target is compared before active may name it (P1: only when the
	// reference names the set that known-good names; an unreadable link or set
	// counts as damaged).
	k := e.linkID(linkKnownGood)
	if ok, _ := e.knownGood(r.st, false); k == "" || !ok {
		r.rollbackFailed(reasonKnownGoodDamaged)
		return ""
	}
	if e.linkID(linkActive) != k {
		if err := e.switchComponentLink(linkActive, k); err != nil {
			return codeIOError // the intent stays; the next start repeats this
		}
	}
	h.systemctl(sysDaemonReload...)
	if h.onFailure {
		h.systemctl(sysResetFailedHelper...)
	}
	// The release was never activated and its set failed: both go. Nothing else
	// is pruned after a rollback (U5).
	if err := e.removeRelease(t.To, installedVersion(h.root), r.st.Previous); err != nil {
		return codeIOError
	}
	if err := e.removeStagedSet(r.st); err != nil {
		return codeIOError
	}
	r.st.Last = r.result(outcomeComponentsRolledBack, reason)
	r.setCooldown() // E4: the version for 15 minutes, and the 60 s marker
	if now, err := h.uptime(); err == nil {
		r.st.Cool.LastActivateUptimeS, r.st.Cool.LastActivateSetAtUptimeS = max(now, 1), max(now, 1)
	}
	r.st.resetTxn()
	if err := e.saveState(r.st); err != nil {
		r.log(outcomeComponentsRolledBack, string(codeIOError))
		return codeIOError
	}
	h.log(outcomeComponentsRolledBack, t.To, t.From, t.CorrelationID, reason)
	r.handBack()
	return ""
}

// handBack ends a serve process that is not the binary active names, so that
// systemd starts that one (exit code 75). An image that cannot be read counts
// as another binary. The recover subcommand is a oneshot and hands nothing over.
func (r *txnRun) handBack() {
	if r.mode != modeLive {
		return
	}
	own, err := r.h.ownImageHash()
	if err == nil {
		if d, derr := fileDigest(r.e.opt, "components/"+linkActive+"/"+componentNames[0]); derr == nil && hex.EncodeToString(d[:]) == own {
			return
		}
	}
	r.h.handOver()
}

// recoverComponents applies the table of U15 rule 4 (rows c2 to c8) to a
// component phase that no process owns. Only a serve process that passes the
// self-check moves an unproven set forward (rows c3 to c5); the recover
// subcommand, which runs the known-good binary, and a serve that fails the
// check roll back.
func (r *txnRun) recoverComponents(installed string) errorCode {
	e := r.e
	t := r.st.Txn
	a, k := e.linkID(linkActive), e.linkID(linkKnownGood)
	if a == "" || k == "" || t.ComponentsTo == "" {
		return r.markAttention() // the links say nothing: nothing is touched
	}
	serve := r.mode == modeLive
	switch t.Phase {
	case phaseCSwitching, phaseCSwitched:
		if a != t.ComponentsTo {
			return r.clearComponentIntent(installed) // row c2, and c3 with the old set
		}
		if !serve || t.Phase != phaseCSwitched {
			return r.rollbackComponents(reasonInterrupted) // rows c3 and c4
		}
		if reason := r.selfCheck(); reason != "" {
			return r.rollbackComponents(reason)
		}
		if code := r.verifyComponents(); code != "" {
			return code
		}
	case phaseCVerified:
		switch {
		case a == t.ComponentsTo && k == t.ComponentsTo:
			// Row c5: the disk is the truth.
			if code := r.commitComponents(); code != "" {
				return code
			}
		case a == t.ComponentsTo && serve:
			if reason := r.selfCheck(); reason != "" {
				return r.rollbackComponents(reason)
			}
			if code := r.verifyComponents(); code != "" {
				return code
			}
		default:
			return r.rollbackComponents(reasonInterrupted)
		}
	case phaseCCommitted:
	default: // c_rolling_back, row c8
		return r.rollbackComponents(reasonInterrupted)
	}
	// c_committed (row c6): a serve process continues with the release phases;
	// the recover subcommand aborts the release and the components stay.
	if serve {
		r.releasePhases()
		return ""
	}
	return r.abortRelease(installed)
}

// clearComponentIntent is row c2: the link was not switched, so the intent is
// cleared and the release is staged again; it then follows U7 row 4.
func (r *txnRun) clearComponentIntent(installed string) errorCode {
	r.st.Txn.Phase = phaseStaged
	if err := r.e.saveState(r.st); err != nil {
		return codeIOError
	}
	r.log(phaseStaged, reasonInterrupted)
	return r.recoverStaged(installed)
}

// abortRelease is row c6 at boot: the components committed, the release phases
// did not start. The staged release is removed and the result is aborted.
func (r *txnRun) abortRelease(installed string) errorCode {
	t := r.st.Txn
	if err := r.e.removeRelease(t.To, installed, r.st.Previous); err != nil {
		return codeIOError
	}
	r.st.Last = r.result(outcomeAborted, reasonInterrupted)
	r.st.resetTxn()
	if err := r.e.saveState(r.st); err != nil {
		return codeIOError
	}
	r.h.log(phaseIdle, t.To, t.From, t.CorrelationID, reasonInterrupted)
	return ""
}

// rollbackUnprovenSet is the part of rebuild (state.json unreadable) that
// concerns the components: when active and known-good name different sets, an
// unproven set is active and nothing says why, so active goes back to
// known-good (U7: the disk is the truth). It returns the set that was active.
// The reference hash was in the state that is lost, so only a known-good set
// that cannot be read stops the switch (E5): then damaged is true and nothing
// is touched, not even daemon-reload.
func (r *txnRun) rollbackUnprovenSet() (unproven string, damaged bool, code errorCode) {
	e := r.e
	a, k := e.linkID(linkActive), e.linkID(linkKnownGood)
	if a == "" || k == "" || a == k {
		return "", false, ""
	}
	if _, err := e.setDigests(componentPath(k)); err != nil {
		return a, true, ""
	}
	if err := e.switchComponentLink(linkActive, k); err != nil {
		return "", false, codeIOError
	}
	r.h.systemctl(sysDaemonReload...)
	if r.h.onFailure {
		r.h.systemctl(sysResetFailedHelper...)
	}
	return a, false, ""
}
