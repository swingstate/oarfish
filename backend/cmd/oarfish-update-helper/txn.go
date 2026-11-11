package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"slices"
	"strings"
	"time"
)

// The release transaction of ADR 0010 U5, U6 and U7: switching, verifying,
// committed, rolling_back and the results rolled_back and rollback_failed. The
// component phases that may sit in front of it are in components.go.
//
// Every phase is written to state.json (atomic replace, fsync) before the
// action it announces, so that a crash always leaves a state that names what
// might be half done. The lock on the state directory is held for the whole
// transaction, by the goroutine that runs it; the helper does not exit on its
// idle timer while a transaction runs (h.running).

const (
	// cooldownSameS: after a rollback or a failure the same version is refused
	// for 15 minutes (U6, R1).
	cooldownSameS = 15 * 60
	// cooldownAnyS: any activate within 60 s of the previous one is refused.
	cooldownAnyS = 60
)

type recoverMode int

const (
	// modeLive: the helper is running, so the service may be running too. A
	// rollback is the full procedure of U6: switch, reset-failed, restart,
	// health check.
	modeLive recoverMode = iota
	// modeBoot: the recover subcommand runs before oarfish.service starts. A
	// rollback is only the link switch (U7).
	modeBoot
)

// txnRun is one transaction (or one recovery) while it holds the lock.
type txnRun struct {
	h    *handler
	e    *env
	lk   *dirLock
	st   *state
	mode recoverMode

	// From the pre-check: the process that runs now, and its restart counter.
	prevPID, baseRestarts int64
	// activated: the new version may have been started. It decides between the
	// results rolled_back and aborted.
	activated bool
	// deferred: the recover unit started by OnFailure= left a release phase to
	// the next serve start (E6).
	deferred bool
}

func (r *txnRun) log(phase, result string) {
	t := r.st.Txn
	r.h.log(phase, t.To, t.From, t.CorrelationID, result)
}

// release ends the use of the lock and the trees.
func (r *txnRun) release() {
	r.e.close()
	r.lk.release()
}

// ---- activate ---------------------------------------------------------------

// activate answers the request `activate` (U2): it checks everything that can
// be refused synchronously, writes the intent (`switching`, or `c_switching`
// when a component set is staged) and answers "accepted"; the transaction then
// continues in the helper without the caller.
func (h *handler) activate(req request) []byte {
	lk, code := h.lockState()
	if code != "" {
		return errorBody(code)
	}
	e, code := h.open(true)
	if code != "" {
		lk.release()
		return errorBody(code)
	}
	r := &txnRun{h: h, e: e, lk: lk, mode: modeLive}
	handed := false
	defer func() {
		if !handed {
			r.release()
		}
	}()
	if code := r.admit(req); code != "" {
		return errorBody(code)
	}
	handed = true
	id := r.st.Txn.ID // the transaction may be over before launch returns
	if r.st.Txn.Phase == phaseCSwitching {
		h.launch(r.runComponents)
	} else {
		h.launch(r.run)
	}
	return encode(activateResponse{V: protocolMax, OK: true, Status: "accepted", TransactionID: id})
}

// admit runs the checks of U5 and U6 and writes the intent. On success the
// state says switching and the pre-check values are in r.
func (r *txnRun) admit(req request) errorCode {
	h, e := r.h, r.e
	st, err := e.loadState()
	if err != nil {
		return codeIOError
	}
	r.st = st
	switch st.Txn.Phase {
	case phaseStaged:
	case phaseIdle:
		return codeNotFound
	default:
		return codeBusy
	}
	if st.Txn.To != req.Version {
		return codeNotFound
	}
	installed := installedVersion(h.root)
	if installed == "" {
		return codeCurrentUnhealthy
	}
	now, err := h.uptime()
	if err != nil {
		return codeIOError
	}
	if stageExpired(st.Txn.StagedAtUptimeS, now) || st.Txn.From != installed {
		// A staged release that was compared with another installed version is
		// as good as expired.
		if code := e.expireStaged(st, installed); code != "" {
			return code
		}
		return codeStageExpired
	}
	// components_confirmed (U2, D6): the second confirmation is required for
	// any difference, a component set or oarfish.service alone. A true flag
	// when nothing differs is a confused client.
	differs := len(st.Txn.Differs) > 0 || st.Txn.ComponentsTo != ""
	switch {
	case differs && !req.Confirmed:
		return codeConfirmationRequired
	case !differs && req.Confirmed:
		return codeBadRequest
	}
	if st.Cool.RefuseVersion == req.Version && st.Cool.refuse().active(now) {
		return codeCooldown
	}
	if st.Cool.LastActivateSetAtUptimeS > 0 &&
		(cooldownMarker{SetAtUptimeS: st.Cool.LastActivateSetAtUptimeS, UntilUptimeS: st.Cool.LastActivateUptimeS + cooldownAnyS}).active(now) {
		return codeCooldown
	}
	// The unit lint again, and the staged files against what stage recorded.
	socketChanged, code := r.checkStaged(installed)
	if code != "" {
		return code
	}
	if st.Txn.ComponentsTo != "" {
		// E2: the known-good set must be the one that was recorded.
		if ok, _ := e.knownGood(st, true); !ok {
			return codeComponentsDamaged
		}
	}
	// The pre-check: the installed version must be healthy now (U6).
	res, props := h.waitHealthy(healthParams{version: installed, prevPID: -1, baseRestarts: -1,
		deadline: precheckLimit, polls: precheckPolls})
	if res != healthOK {
		return codeCurrentUnhealthy
	}
	r.prevPID, r.baseRestarts = props.MainPID, props.NRestarts
	// The hash of the binary that is running, recorded before the switch, and a
	// look at the binary that will run.
	sum, err := e.binaryHash(installed)
	if err != nil {
		return codeIOError
	}
	if err := e.requireRegular(releasePath(req.Version) + "/oarfish"); err != nil {
		return codeIOError
	}
	id := st.Txn.ID
	if !isHex(16)(id) || id == "" {
		if id, err = h.newID(); err != nil {
			return codeIOError
		}
	}
	// The intent. What stage recorded stays; with a component set the phase is
	// c_switching and the state gets everything the next process needs (E3):
	// nothing is carried across the hand-over in memory.
	t := &st.Txn
	t.ID, t.Phase, t.From, t.FromBinarySHA256, t.CorrelationID = id, phaseSwitching, installed, sum, req.CorrelationID
	if t.ComponentsTo != "" {
		t.Phase = phaseCSwitching
		t.ComponentsKeyID = h.keyID
		t.ComponentsProtocolMin, t.ComponentsProtocolMax = int64(h.protoMin), int64(h.protoMax)
		t.SocketTextChanged = socketChanged
	}
	st.Cool.LastActivateUptimeS, st.Cool.LastActivateSetAtUptimeS = max(now, 1), max(now, 1)
	if err := e.saveState(st); err != nil {
		return codeIOError
	}
	r.log(t.Phase, "ok")
	return ""
}

// checkStaged lints the staged unit files again (U15 rule 3: at stage and
// again at activate) and compares the staged files with the hashes that stage
// recorded. Each unit file is read once, and the same bytes are linted and
// hashed. A rejected or changed file ends the staged release. It reports
// whether the normalised text of the socket unit changes (D4).
func (r *txnRun) checkStaged(installed string) (socketChanged bool, code errorCode) {
	e, t := r.e, r.st.Txn
	end := func(reason string, code errorCode) (bool, errorCode) {
		if c := e.endStaged(r.st, installed, reason); c != "" {
			return false, c
		}
		return false, code
	}
	lint := func(name, path string) ([]byte, [sha256.Size]byte, errorCode) {
		text, tooLarge, err := readUnit(e.opt, path)
		if err != nil {
			return nil, [sha256.Size]byte{}, codeIOError
		}
		res := rejected(lintTooLarge)
		if !tooLarge {
			res = lintUnit(name, text)
		}
		if !res.Accepted {
			r.h.logLint(t.To, installed, t.CorrelationID, name, res.Reason)
			return nil, [sha256.Size]byte{}, codeUnitRejected
		}
		return text, sha256.Sum256(text), ""
	}
	_, sum, code := lint(unitService, releasePath(t.To)+"/"+unitService)
	switch {
	case code == codeUnitRejected:
		return end(reasonUnitRejected, code)
	case code != "":
		return false, code
	case t.ServiceUnitSHA256 != "" && hex.EncodeToString(sum[:]) != t.ServiceUnitSHA256:
		return end(reasonStagedChanged, codeChecksumMismatch)
	}
	if t.ComponentsTo == "" {
		return false, ""
	}
	if e.linkID(linkActive) != t.ComponentsFrom {
		// Staged against another active set: as good as expired.
		return end(reasonStageExpired, codeStageExpired)
	}
	dir := componentPath(t.ComponentsTo)
	var d [4][sha256.Size]byte
	var socketText []byte
	var err error
	if d[0], err = fileDigest(e.opt, dir+"/"+componentNames[0]); err != nil {
		return false, codeIOError
	}
	for i := 1; i < len(d); i++ {
		text, sum, code := lint(componentNames[i], dir+"/"+componentNames[i])
		switch {
		case code == codeUnitRejected:
			return end(reasonUnitRejected, code)
		case code != "":
			return false, code
		}
		d[i] = sum
		if componentNames[i] == unitHelperSocket {
			socketText = text
		}
	}
	if setDigest(d) != t.ComponentsSetSHA256 || hex.EncodeToString(d[0][:]) != t.ComponentsHelperSHA256 {
		return end(reasonStagedChanged, codeChecksumMismatch)
	}
	old, tooLarge, err := readUnit(e.opt, "components/"+linkActive+"/"+unitHelperSocket)
	return err != nil || tooLarge || socketTextDiffers(old, socketText), ""
}

// socketTextDiffers compares the normalised texts of two socket units, the
// texts the lint compares with the accepted forms (D4): comments, blank lines
// and the values of Description= and Documentation= do not count. A text that
// can not be parsed counts as different.
func socketTextDiffers(a, b []byte) bool {
	kind := unitKinds[unitHelperSocket]
	la, ra := parseUnit(kind, a)
	lb, rb := parseUnit(kind, b)
	return ra != "" || rb != "" || normalisedUnit(la) != normalisedUnit(lb)
}

// serviceDiffers: the oarfish.service of the new release differs from the
// installed one, so systemd must read it again after each switch of current (U6).
func (r *txnRun) serviceDiffers() bool { return slices.Contains(r.st.Txn.Differs, unitService) }

// run is the transaction after "accepted" when no component set is staged.
func (r *txnRun) run() {
	defer r.release()
	r.runRelease()
}

// runRelease is the release part of a transaction: the state says switching.
func (r *txnRun) runRelease() {
	h, t := r.h, r.st.Txn
	if err := r.e.switchLink(releasePath(t.To)); err != nil {
		r.rollback(reasonSwitchFailed)
		return
	}
	r.st.Txn.Phase = phaseVerifying
	if err := r.e.saveState(r.st); err != nil {
		// The link has moved but nothing was restarted and the intent is not
		// durable. The state still says switching, which the recovery of the
		// next start rolls back.
		r.log(phaseVerifying, string(codeIOError))
		return
	}
	r.log(phaseVerifying, "ok")
	if r.serviceDiffers() {
		// The unit is a link into current: systemd reads the new text (U6).
		if _, err := h.systemctl(sysDaemonReload...); err != nil {
			r.rollback(reasonReloadFailed)
			return
		}
	}
	r.activated = true // the restart may take effect even when it reports an error
	if _, err := h.systemctl(sysRestart...); err != nil {
		r.rollback(reasonRestartFailed)
		return
	}
	res, _ := h.waitHealthy(healthParams{version: t.To, prevPID: r.prevPID, baseRestarts: r.baseRestarts,
		deadline: healthDeadline, polls: healthPolls, window: healthStability})
	switch res {
	case healthOK:
		r.commit()
	case healthFailFast:
		r.rollback(reasonStartFailed)
	default:
		r.rollback(reasonHealthTimeout)
	}
}

// commit records the success (U7 row 9) and prunes.
func (r *txnRun) commit() {
	t := r.st.Txn
	r.st.Previous = t.From
	r.st.Last = r.result(outcomeSuccess, "")
	r.st.Txn.Phase = phaseCommitted
	if err := r.e.saveState(r.st); err != nil {
		// Not durable: the state still says verifying and the next start rolls
		// back (U7 row 8, the accepted cost).
		r.log(phaseCommitted, string(codeIOError))
		return
	}
	r.log(phaseCommitted, "ok")
	r.finishCommitted()
}

// finishCommitted prunes and ends the transaction. A failure leaves the phase
// committed, and the recovery finishes the pruning (U7 row 9).
func (r *txnRun) finishCommitted() errorCode {
	t := r.st.Txn
	installed := installedVersion(r.h.root)
	if installed == "" || installed != t.To {
		// current does not name the committed version: nothing is pruned.
		r.st.NeedsAttention = true
	} else if err := r.e.prune(installed, r.st.Previous); err != nil {
		return codeIOError
	}
	r.st.resetTxn()
	if err := r.e.saveState(r.st); err != nil {
		return codeIOError
	}
	r.h.log(phaseIdle, t.To, t.From, t.CorrelationID, "ok")
	return ""
}

func (r *txnRun) result(outcome, reason string) *lastResult {
	t := r.st.Txn
	return &lastResult{TransactionID: t.ID, To: t.To, From: t.From, Outcome: outcome, ReasonCode: reason,
		CorrelationID: t.CorrelationID, FinishedAt: r.h.wall().UTC().Format(time.RFC3339), ComponentsUpdated: t.ComponentsUpdated}
}

// setCooldown refuses the same version for 15 minutes (U6, R1).
func (r *txnRun) setCooldown() {
	now, err := r.h.uptime()
	if err != nil {
		return
	}
	r.st.Cool.RefuseVersion = r.st.Txn.To
	r.st.Cool.RefuseSetAtUptimeS = max(now, 1)
	r.st.Cool.RefuseUntilUptimeS = max(now, 1) + cooldownSameS
}

// rollback is the automatic rollback (ORB-UPD-006, U6): write the intent,
// check the hash of the old binary against the one recorded before the switch,
// switch current back, and in the live mode reset the failure counters,
// restart and apply the health criteria to the old version. A damaged rollback
// target is never switched to.
func (r *txnRun) rollback(reason string) {
	h, e := r.h, r.e
	t := r.st.Txn
	r.st.Txn.Phase = phaseRollingBack
	if err := e.saveState(r.st); err != nil {
		// No action without a durable intent: the state still names the earlier
		// phase and the recovery of the next start decides.
		r.log(phaseRollingBack, string(codeIOError))
		return
	}
	r.log(phaseRollingBack, "ok")
	sum, err := e.binaryHash(t.From)
	if err != nil || t.FromBinarySHA256 == "" || sum != t.FromBinarySHA256 {
		r.rollbackFailed(reasonTargetDamaged)
		return
	}
	if err := e.switchLink(releasePath(t.From)); err != nil {
		r.rollbackFailed(reasonSwitchFailed)
		return
	}
	if r.serviceDiffers() {
		// Again after the switch back (U6), also at boot: the unit that systemd
		// loaded is the one current named. A failure leaves the loaded text.
		h.systemctl(sysDaemonReload...)
	}
	if r.activated && r.mode == modeLive {
		h.systemctl(sysResetFailed...) // a failure here does not stop the restart
		pid, base := int64(-1), int64(-1)
		if p, err := h.show(); err == nil {
			pid, base = p.MainPID, p.NRestarts
		}
		if _, err := h.systemctl(sysRestart...); err != nil {
			r.rollbackFailed(reasonRestartFailed)
			return
		}
		res, _ := h.waitHealthy(healthParams{version: t.From, prevPID: pid, baseRestarts: base,
			deadline: healthDeadline, polls: healthPolls, window: healthStability})
		if res != healthOK {
			r.rollbackFailed(reasonRollbackUnheal)
			return
		}
	}
	r.finishRollback(reason)
}

// finishRollback records rolled_back, or aborted when the new version never
// ran, sets the cooldown and ends the transaction. Nothing is pruned.
func (r *txnRun) finishRollback(reason string) {
	t := r.st.Txn
	outcome := outcomeRolledBack
	if !r.activated {
		outcome = outcomeAborted
		if err := r.e.removeRelease(t.To, t.From, r.st.Previous); err != nil {
			return // the intent stays; the recovery repeats this
		}
	}
	r.st.Last = r.result(outcome, reason)
	r.setCooldown()
	r.st.resetTxn()
	if err := r.e.saveState(r.st); err != nil {
		r.log(outcome, string(codeIOError))
		return
	}
	r.h.log(outcome, t.To, t.From, t.CorrelationID, reason)
}

// rollbackFailed records rollback_failed with needs_attention. The helper
// stops; the administrator needs a shell (U6).
func (r *txnRun) rollbackFailed(reason string) {
	t := r.st.Txn
	r.st.Txn.Phase = phaseRollbackFailed
	r.st.NeedsAttention = true
	r.st.Last = r.result(outcomeRollbackFailed, reason)
	r.setCooldown()
	if err := r.e.saveState(r.st); err != nil {
		r.log(phaseRollbackFailed, string(codeIOError))
		return
	}
	r.h.log(phaseRollbackFailed, t.To, t.From, t.CorrelationID, reason)
}

// ---- the file system side ---------------------------------------------------

// binaryHash is the SHA-256 of releases/<version>/oarfish, read from one
// descriptor in 32 KiB chunks.
func (e *env) binaryHash(version string) (string, error) {
	if !validVersion(version) {
		return "", errors.New("invalid version")
	}
	f, _, err := openRegular(e.opt, releasePath(version)+"/oarfish")
	if err != nil {
		return "", err
	}
	defer f.Close()
	sum := sha256.New()
	buf := make([]byte, chunk)
	for {
		n, err := f.Read(buf)
		sum.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func (e *env) requireRegular(name string) error {
	f, _, err := openRegular(e.opt, name)
	if err != nil {
		return err
	}
	return f.Close()
}

// switchLink makes current name target (a releases/<version> path) with one
// atomic replacement: a link under a temporary name, rename over current,
// fsync of the directory (U5). The temporary name has its own random id.
func (e *env) switchLink(target string) error {
	id, err := e.h.newID()
	if err != nil {
		return err
	}
	tmp := tempLinkPrefix + id
	if err := e.fs.Symlink(treeOpt, target, tmp); err != nil {
		return err
	}
	if err := e.fs.Rename(treeOpt, tmp, "current"); err != nil {
		e.fs.RemoveAll(treeOpt, tmp)
		return err
	}
	return e.fs.SyncDir(treeOpt, ".")
}

const tempLinkPrefix = ".current.new."

// prune is the retention of U5, run after a commit: keep the active release and
// the previous healthy one, remove every other directory whose name matches the
// version pattern (rename to .trash.<id>, then delete), and the stale .stage.*,
// .trash.* and .current.new.* entries. Names that do not match are never
// touched, and neither is anything that is not a directory.
func (e *env) prune(installed, previous string) error {
	if installed == "" {
		return nil
	}
	d, err := e.opt.Open("releases")
	if err != nil {
		return err
	}
	entries, err := d.ReadDir(-1)
	d.Close()
	if err != nil {
		return err
	}
	for _, ent := range entries {
		n := ent.Name()
		if !ent.IsDir() || !validVersion(n) || n == installed || n == previous {
			continue
		}
		if err := e.trash(releasePath(n)); err != nil {
			return err
		}
	}
	return e.sweep()
}

// launch runs the transaction in its own goroutine, counted so that the helper
// does not exit while it runs. Tests set syncTxn to run it inline.
func (h *handler) launch(f func()) {
	if h.syncTxn {
		f()
		return
	}
	h.running.Add(1)
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		defer h.running.Add(-1)
		f()
	}()
}

// wait blocks until every transaction has ended (the tests, and serve before it
// returns).
func (h *handler) wait() { h.wg.Wait() }

// isLeftover reports whether a name below the opt tree or releases/ is a
// temporary entry of this helper.
func isLeftover(name string) bool {
	return strings.HasPrefix(name, ".stage.") || strings.HasPrefix(name, ".trash.") || strings.HasPrefix(name, tempLinkPrefix)
}
