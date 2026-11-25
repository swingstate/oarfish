package updater

import "context"

// Activity is what a reboot or shutdown needs to know about the update (ADR
// 0011 B6, step G7.5), for the list of waiting actions and for the service's
// own refusal update_running. The broker refuses by the helper's lock and
// state itself; this is the convenience that gives the person the message.
type Activity struct {
	// Staged: a prepared update would be discarded by a reboot (ADR 0010 T19):
	// the apply job waits for the second confirmation, or the helper reports
	// the phase staged.
	Staged bool
	// Running: an update runs now: the apply job is past its start and does not
	// wait for the confirmation, the helper reports a phase inside a
	// transaction, or the helper cannot be reached during the hand-over of a
	// component update (the rule of F4, handingOverLocked).
	Running bool
}

// finishedPhases are the helper phases outside a transaction (the set the
// broker uses, ADR 0011 A4.4 item 8). Every other phase, component phases
// included, is a running update.
var finishedPhases = map[string]bool{"idle": true, "staged": true, "committed": true, "rolled_back": true, "rollback_failed": true}

// Activity reads the in-memory job and the helper's status, the same read GET
// /api/v1/update makes, bounded by ctx. Unlike State it writes no audit entry,
// acknowledges nothing and changes no field of the Updater. A helper that
// cannot be reached outside the hand-over adds nothing: the broker decides.
func (u *Updater) Activity(ctx context.Context) Activity {
	var a Activity
	u.mu.Lock()
	if j := u.job; j != nil && j.kind == jobApply && !j.done {
		if j.step == stepAwaiting {
			a.Staged = true
		} else {
			a.Running = true
		}
	}
	u.mu.Unlock()
	if u.cfg.Helper == nil {
		return a
	}
	st, e := u.cfg.Helper.Status(ctx)
	if e != nil {
		u.mu.Lock()
		if u.handingOverLocked(e) {
			a.Running = true
		}
		u.mu.Unlock()
		return a
	}
	if st.Phase == "staged" {
		a.Staged = true
	}
	if !finishedPhases[st.Phase] {
		a.Running = true
	}
	return a
}
