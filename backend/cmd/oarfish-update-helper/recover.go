package main

import (
	"fmt"
	"io"
	"strings"
)

// Recovery (ADR 0010 U7): one rule for every unfinished transaction, "an update
// that has not reached committed is rolled back". The recover subcommand runs
// it at boot, before oarfish.service starts (modeBoot, only a link switch);
// serve runs it at start when it finds a transaction that nobody owns (modeLive,
// the full procedure of U6). Both hold the lock on the state directory.

// recoverBoot is the recover subcommand. Exit status: 0 when the disk is
// consistent after it (also after a rollback), 1 when it could not finish or
// left a rollback_failed. Messages are fixed; no path and no operating-system
// text. The unit is ordered Before=oarfish.service and not required by it, so a
// failure never keeps Oarfish from starting (A9).
func (h *handler) recoverBoot(stderr io.Writer) int {
	lk, code := h.lockState()
	if code != "" {
		fmt.Fprintln(stderr, "oarfish-update-helper: recovery did not run: "+errorTexts[code])
		return 1
	}
	e, code := h.open(true)
	if code != "" {
		lk.release()
		fmt.Fprintln(stderr, "oarfish-update-helper: recovery did not run: "+errorTexts[code])
		return 1
	}
	r := &txnRun{h: h, e: e, lk: lk, mode: modeBoot}
	defer r.release()
	if code := r.recoverAll(); code != "" {
		fmt.Fprintln(stderr, "oarfish-update-helper: recovery did not finish: "+errorTexts[code])
		return 1
	}
	if r.st != nil && r.st.Txn.Phase == phaseRollbackFailed {
		fmt.Fprintln(stderr, "oarfish-update-helper: a rollback failed; the installed version needs attention")
		return 1
	}
	return 0
}

// startupRecovery runs when serve starts, before the first connection is
// accepted. The common case (nothing to recover) is decided under the lock in
// a moment and costs no write; real work continues in the background while
// requests get `busy`.
func (h *handler) startupRecovery() {
	lk, code := h.lockState()
	if code != "" {
		return // the installer or another helper holds it
	}
	e, code := h.open(true)
	if code != "" {
		lk.release()
		return
	}
	r := &txnRun{h: h, e: e, lk: lk, mode: modeLive}
	if !r.needsRecovery() {
		r.release()
		return
	}
	h.launch(func() {
		defer r.release()
		r.recoverAll()
	})
}

// needsRecovery: the state file is not valid, the phase is not one a running
// helper may find (idle, staged), or a leftover entry exists.
func (r *txnRun) needsRecovery() bool {
	st, err := r.e.loadState()
	if err != nil {
		return true
	}
	if st.Txn.Phase != phaseIdle && st.Txn.Phase != phaseStaged {
		return true
	}
	return r.e.hasLeftover()
}

// hasLeftover looks for .stage.*, .trash.* and .current.new.* entries and for
// temporary state files.
func (e *env) hasLeftover() bool {
	if e.state != nil {
		if d, err := e.state.Open("."); err == nil {
			entries, _ := d.ReadDir(-1)
			d.Close()
			for _, ent := range entries {
				if strings.HasPrefix(ent.Name(), stateTempPrefix) {
					return true
				}
			}
		}
	}
	for _, dir := range []string{".", "releases"} {
		d, err := e.opt.Open(dir)
		if err != nil {
			continue
		}
		entries, err := d.ReadDir(-1)
		d.Close()
		if err != nil {
			continue
		}
		for _, ent := range entries {
			if isLeftover(ent.Name()) {
				return true
			}
		}
	}
	return false
}

// recoverAll applies the rule of U7 to whatever phase the state file names and
// then removes what a crash left behind (.stage.*, .trash.*, .current.new.*,
// temporary state files). It returns an error code only when it could not
// finish.
func (r *txnRun) recoverAll() errorCode {
	if code := r.recoverPhase(); code != "" {
		return code
	}
	if err := r.e.sweep(); err != nil {
		return codeIOError
	}
	return ""
}

func (r *txnRun) recoverPhase() errorCode {
	e := r.e
	st, err := e.loadState()
	if err != nil {
		return r.rebuild()
	}
	r.st = st
	installed := installedVersion(r.h.root)
	t := st.Txn
	switch t.Phase {
	case phaseIdle:
		if err := e.sweep(); err != nil {
			return codeIOError
		}
		return ""
	case phaseStaging:
		if installed == "" {
			return r.markAttention()
		}
		return e.recoverStaging(st, installed)
	case phaseStaged:
		now, err := r.h.uptime()
		if err != nil {
			return codeIOError
		}
		// At boot the staged release is gone with the reboot (U7 row 4); a
		// running helper only removes one that has expired.
		if r.mode == modeBoot || stageExpired(t.StagedAtUptimeS, now) {
			if installed == "" {
				return r.markAttention()
			}
			return e.expireStaged(st, installed)
		}
		return ""
	case phaseSwitching, phaseVerifying:
		return r.recoverSwitch(installed)
	case phaseCommitted:
		if code := r.finishCommitted(); code != "" {
			return code
		}
		return ""
	case phaseRollingBack:
		return r.recoverRollingBack(installed)
	case phaseRollbackFailed:
		return r.recoverRollbackFailed(installed)
	case phaseRolledBack:
		st.resetTxn()
		if err := e.saveState(st); err != nil {
			return codeIOError
		}
		return ""
	}
	// The component phases (G6.10) are not implemented: nothing is touched.
	return r.markAttention()
}

// markAttention sets needs_attention and touches nothing else.
func (r *txnRun) markAttention() errorCode {
	if r.st.NeedsAttention {
		return ""
	}
	r.st.NeedsAttention = true
	if err := r.e.saveState(r.st); err != nil {
		return codeIOError
	}
	return ""
}

// recoverSwitch: U7 rows 5 to 8. current names from: the switch never took
// effect. current names to: roll back.
func (r *txnRun) recoverSwitch(installed string) errorCode {
	t := r.st.Txn
	switch installed {
	case t.From:
		if t.Phase == phaseSwitching {
			// Row 5: clear the intent and remove releases/<to>.
			if err := r.e.removeRelease(t.To, installed, r.st.Previous); err != nil {
				return codeIOError
			}
			r.st.Last = r.e.abortedResult(t, reasonInterrupted)
		} else {
			r.st.Last = r.result(outcomeRolledBack, reasonInterrupted)
			r.setCooldown()
		}
		r.st.resetTxn()
		if err := r.e.saveState(r.st); err != nil {
			return codeIOError
		}
		r.h.log(phaseIdle, t.To, t.From, t.CorrelationID, reasonInterrupted)
		return ""
	case t.To:
		r.activated = true
		r.rollback(reasonInterrupted)
		return r.afterRollback()
	}
	r.rollbackFailed(reasonCurrentUnknown)
	return ""
}

// recoverRollingBack: U7 row 10. Ensure current names from, compare the hash,
// and finish.
func (r *txnRun) recoverRollingBack(installed string) errorCode {
	t := r.st.Txn
	if installed != t.From && installed != t.To {
		r.rollbackFailed(reasonCurrentUnknown)
		return ""
	}
	r.activated = true
	r.rollback(reasonInterrupted)
	return r.afterRollback()
}

// afterRollback reports whether the rollback reached a final state.
func (r *txnRun) afterRollback() errorCode {
	switch r.st.Txn.Phase {
	case phaseIdle, phaseRollbackFailed:
		return ""
	}
	return codeIOError
}

// recoverRollbackFailed: a rollback that failed because a call failed (the
// switch or the restart) is tried again at the next start, in either mode: an
// update that has not reached committed is rolled back (U7), and a transient
// error must not leave the failed version as current. Any other failed rollback
// needs a person. At boot it is looked at again: when current names from and
// the old binary still has the recorded hash, the transaction ends (the
// attention flag stays until the result is acknowledged); a running helper
// leaves it alone.
func (r *txnRun) recoverRollbackFailed(installed string) errorCode {
	t := r.st.Txn
	if r.st.Last != nil && (r.st.Last.ReasonCode == reasonSwitchFailed || r.st.Last.ReasonCode == reasonRestartFailed) &&
		(installed == t.From || installed == t.To) {
		reason := r.st.Last.ReasonCode
		r.st.NeedsAttention = false // set by the failure that is retried now
		r.activated = true
		r.rollback(reason)
		return r.afterRollback()
	}
	if r.mode != modeBoot || installed != t.From {
		return ""
	}
	sum, err := r.e.binaryHash(t.From)
	if err != nil || t.FromBinarySHA256 == "" || sum != t.FromBinarySHA256 {
		return ""
	}
	r.st.NeedsAttention = true
	r.st.resetTxn()
	if err := r.e.saveState(r.st); err != nil {
		return codeIOError
	}
	r.h.log(phaseIdle, t.To, t.From, t.CorrelationID, outcomeRollbackFailed)
	return ""
}

// rebuild is the recovery when state.json is missing or unreadable (U7): the
// installed version is the target of current, there is no previous version and
// no transaction. It removes .stage.*, .trash.* and .current.new.* and reports
// needs_attention once (until the result is acknowledged).
func (r *txnRun) rebuild() errorCode {
	e := r.e
	if err := e.sweep(); err != nil {
		return codeIOError
	}
	st := freshState(r.h.helperVersion)
	id, err := r.h.newID()
	if err != nil {
		return codeIOError
	}
	st.NeedsAttention = true
	st.Last = &lastResult{TransactionID: id, Outcome: outcomeAborted, ReasonCode: reasonStateRebuilt,
		FinishedAt: r.h.wall().UTC().Format("2006-01-02T15:04:05Z07:00")}
	r.st = st
	if err := e.saveState(st); err != nil {
		return codeIOError
	}
	r.h.log(phaseIdle, "", installedVersion(r.h.root), "", reasonStateRebuilt)
	return ""
}

// ---- ack ----------------------------------------------------------------------

// ack marks the last result as seen by the service (U8). It answers not_found
// when the transaction id is not the one of the last result.
func (h *handler) ack(req request) []byte {
	lk, code := h.lockState()
	if code != "" {
		return errorBody(code)
	}
	defer lk.release()
	e, code := h.open(true)
	if code != "" {
		return errorBody(code)
	}
	defer e.close()
	st, err := e.loadState()
	if err != nil {
		return errorBody(codeIOError)
	}
	if st.Last == nil || st.Last.TransactionID != req.TransactionID {
		return errorBody(codeNotFound)
	}
	if !st.Last.Acknowledged {
		st.Last.Acknowledged = true
		if st.Txn.Phase == phaseIdle {
			st.NeedsAttention = false
		}
		if err := e.saveState(st); err != nil {
			return errorBody(codeIOError)
		}
	}
	return encode(okResponse{V: protocolMax, OK: true})
}
