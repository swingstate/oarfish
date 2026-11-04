package main

import (
	"errors"
	"io/fs"
	"strings"
	"time"
)

// Cleanup, staging recovery, expiry and discard. All of these run while the
// lock is held and change the disk only through fsops.

// releasePath is the directory of a version below the tree. The version has
// been validated (request decoder or state decoder), so it holds no separator.
func releasePath(version string) string { return "releases/" + version }

// trash removes a path in two steps (U5 retention): an atomic rename to
// releases/.trash.<id>, then the deletion, so an interrupted deletion never
// leaves a half-removed directory under a version name. A missing path is
// success.
func (e *env) trash(path string) error {
	if _, err := e.opt.Lstat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	id, err := e.h.newID()
	if err != nil {
		return err
	}
	tmp := "releases/.trash." + id
	if err := e.fs.Rename(treeOpt, path, tmp); err != nil {
		return err
	}
	return e.fs.RemoveAll(treeOpt, tmp)
}

// removeRelease removes releases/<version> unless it is the installed or the
// recorded previous version: those directories are never touched.
func (e *env) removeRelease(version, installed, previous string) error {
	if version == "" || version == installed || version == previous {
		return nil
	}
	return e.trash(releasePath(version))
}

// sweep removes every releases/.stage.* and releases/.trash.* directory.
func (e *env) sweep() error {
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
		if n := ent.Name(); strings.HasPrefix(n, ".stage.") || strings.HasPrefix(n, ".trash.") {
			if err := e.fs.RemoveAll(treeOpt, "releases/"+n); err != nil {
				return err
			}
		}
	}
	return nil
}

// abortedResult builds the last result of a transaction that ended without
// being applied.
func (e *env) abortedResult(t txn, reason string) *lastResult {
	return &lastResult{
		TransactionID: t.ID, To: t.To, From: t.From, Outcome: outcomeAborted, ReasonCode: reason,
		CorrelationID: t.CorrelationID, FinishedAt: e.h.wall().UTC().Format(time.RFC3339),
	}
}

// recoverStaging is the staging recovery of ADR 0010 U7 rows 2 to 4, run while
// the lock is held and the state says staging (nobody owns it: a crash). It
// removes every .stage.* directory (and .trash.*, which only the stage
// sequence creates), removes releases/<to> unless it is the installed or the
// recorded previous version, and sets idle with the last result aborted,
// reason interrupted. The boot-time recover subcommand is G6.6.
func (e *env) recoverStaging(st *state, installed string) errorCode {
	if err := e.sweep(); err != nil {
		return codeIOError
	}
	if err := e.removeRelease(st.Txn.To, installed, st.Previous); err != nil {
		return codeIOError
	}
	old := st.Txn
	st.Last = e.abortedResult(old, reasonInterrupted)
	st.resetTxn()
	if err := e.saveState(st); err != nil {
		return codeIOError
	}
	e.h.log(phaseIdle, old.To, old.From, old.CorrelationID, reasonInterrupted)
	return ""
}

// expireStaged removes a staged release that has expired and sets idle, last
// result aborted, reason stage_expired (U5, U7 row 4).
func (e *env) expireStaged(st *state, installed string) errorCode {
	if err := e.removeRelease(st.Txn.To, installed, st.Previous); err != nil {
		return codeIOError
	}
	old := st.Txn
	st.Last = e.abortedResult(old, reasonStageExpired)
	st.resetTxn()
	if err := e.saveState(st); err != nil {
		return codeIOError
	}
	e.h.log(phaseIdle, old.To, old.From, old.CorrelationID, reasonStageExpired)
	return ""
}

// discard removes the staged release of exactly that version and sets idle. No
// last result is written. It answers not_found when nothing is staged for the
// version, and never touches the installed or the recorded previous version.
func (h *handler) discard(req request) []byte {
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
	if st.Txn.Phase != phaseStaged || st.Txn.To != req.Version {
		return errorBody(codeNotFound)
	}
	installed := installedVersion(h.root)
	if installed == "" {
		return errorBody(codeCurrentUnhealthy)
	}
	if req.Version == installed || req.Version == st.Previous {
		return errorBody(codeNotFound)
	}
	if err := e.removeRelease(req.Version, installed, st.Previous); err != nil {
		return errorBody(codeIOError)
	}
	old := st.Txn
	st.resetTxn()
	if err := e.saveState(st); err != nil {
		return errorBody(codeIOError)
	}
	h.log(phaseIdle, old.To, old.From, old.CorrelationID, "discarded")
	return encode(okResponse{V: protocolMax, OK: true})
}
