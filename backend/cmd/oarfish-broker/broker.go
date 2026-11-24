package main

import (
	"sync"
	"time"
)

// broker handles one decoded request at a time per connection and keeps the
// state that lives as long as the process (B4, D-C89 items 6 and 7): the busy
// flag, the rate window and the power flag. None of it is written anywhere.
type broker struct {
	o      options
	uid    uint32
	policy Policy
	j      *journal
	now    func() time.Time

	mu      sync.Mutex
	running bool        // an operation runs (B4: one at a time)
	window  []time.Time // when each slot of the rate window was used, oldest first
	power   bool        // a power operation was answered accepted (D-C89 item 6)
}

func newBroker(o options, uid uint32, j *journal) *broker {
	return &broker{o: o, uid: uid, policy: o.policy(uid), j: j, now: o.now}
}

// respond answers one request line from caller and writes its journal line.
func (b *broker) respond(c Caller, line []byte) []byte {
	req, err := decodeRequest(line)
	if err != nil {
		b.logLine(request{}, codeBadRequest)
		return encode(answer{V: protocolMax, Code: codeBadRequest})
	}
	if req.Op == opStatus {
		b.logLine(req, codeSuccess)
		return encode(b.status())
	}
	var a answer
	switch {
	case isServiceOp(req.Op):
		a = b.service(c, req)
	case isPowerOp(req.Op):
		a = b.powerOp(c, req)
	}
	a.V = protocolMax
	b.logLine(req, a.Code)
	return encode(a)
}

// logLine is the one journal line per request of B5:
// op=<op> unit=<name|-> cid=<id> result=<code>. op and cid come from the
// decoded request, whose values are closed or validated; the unit is printed
// only when it passes rule 1 of B3, so no request byte outside those forms
// reaches the journal. Nothing of systemctl is logged.
func (b *broker) logLine(r request, c code) {
	op, unit, cid := "-", "-", "-"
	if r.Op != "" {
		op = r.Op
	}
	if validUnitName(r.Unit) {
		unit = r.Unit
	}
	if r.CorrelationID != "" {
		cid = r.CorrelationID
	}
	b.j.line("op=" + op + " unit=" + unit + " cid=" + cid + " result=" + string(c))
}

func (b *broker) status() statusAnswer {
	b.mu.Lock()
	defer b.mu.Unlock()
	return statusAnswer{
		V: protocolMax, Code: codeSuccess, ProtocolMin: protocolMin, ProtocolMax: protocolMax,
		BuildID: buildID, PowerRequested: b.power, Busy: b.running,
	}
}

// ---- busy, rate window and idle (B4, B8, D-C89 items 6 and 7) ---------------

// prune drops the slots that left the window. Called with b.mu held.
func (b *broker) prune(now time.Time) {
	i := 0
	for i < len(b.window) && !now.Before(b.window[i].Add(b.o.rateWindow)) {
		i++
	}
	b.window = b.window[i:]
}

// takeRateSlot is the rate check of a mutating service operation. A slot is
// used as soon as the request passes it, whatever the later result (D-C89
// item 7). When the window is full it returns the seconds to wait (at least 1).
func (b *broker) takeRateSlot() (ok bool, waitS int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	b.prune(now)
	if len(b.window) >= b.o.rateMax {
		wait := b.window[0].Add(b.o.rateWindow).Sub(now)
		s := int((wait + time.Second - 1) / time.Second)
		return false, max(s, 1)
	}
	b.window = append(b.window, now)
	return true, 0
}

// begin takes the one operation slot; false means busy.
func (b *broker) begin() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running {
		return false
	}
	b.running = true
	return true
}

func (b *broker) end() {
	b.mu.Lock()
	b.running = false
	b.mu.Unlock()
}

// mayExit reports whether the broker may exit when it has been idle long
// enough: no operation runs, the rate window is empty, and no power operation
// was accepted (D-C89 item 6: after `accepted` the broker runs until the
// shutdown ends it or RuntimeMaxSec= does).
func (b *broker) mayExit() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune(b.now())
	return !b.running && len(b.window) == 0 && !b.power
}

// windowEmptyAt is when the last slot leaves the window (zero when empty).
func (b *broker) windowEmptyAt() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.window) == 0 {
		return time.Time{}
	}
	return b.window[len(b.window)-1].Add(b.o.rateWindow)
}

// ---- service operations (B2, B3 as amended by A3, D-C89) -------------------

var serviceVerb = map[string]string{
	opServiceStart: verbStart, opServiceStop: verbStop, opServiceRestart: verbRestart,
}

// service runs the checks in the order of B2: policy, rate, busy, the unit
// rules of B3 in their order, the state precondition, then the call and its
// verification.
func (b *broker) service(c Caller, req request) answer {
	if ok, refusal := b.policy.Allow(c, Operation(req.Op), req.Unit); !ok {
		return answer{Code: refusal}
	}
	if ok, wait := b.takeRateSlot(); !ok {
		return answer{Code: codeRateLimited, RetryAfterS: wait}
	}
	if !b.begin() {
		return answer{Code: codeBusy}
	}
	defer b.end()
	u, a, ok := b.checkUnit(req.Op, req.Unit)
	if !ok {
		return a
	}
	return b.runServiceJob(req.Op, u)
}

// checkUnit applies rules 1 to 11 of B3 (A3.4, D-C89 items 2 to 4) and the
// state precondition of B2. ok is true when the operation may run; otherwise
// a is the refusal.
func (b *broker) checkUnit(op, name string) (unitInfo, answer, bool) {
	refuse := func(c code) (unitInfo, answer, bool) { return unitInfo{}, answer{Code: c}, false }

	// Rule 1: syntax. systemctl is not started for an invalid name.
	if !validUnitName(name) {
		return refuse(codeUnitInvalid)
	}
	// Rule 2: type and template. `.service` is enforced by rule 1; a template
	// without an instance is refused.
	isTemplate, instance := splitTemplate(name)
	if isTemplate && instance == "" {
		return refuse(codeUnitTemplate)
	}
	// Rule 3: known to systemd, by the broker's own read.
	raw, c := b.showRaw(name)
	if c != "" {
		return refuse(c)
	}
	if ls := raw["LoadState"]; ls != "loaded" && ls != "masked" {
		return refuse(codeUnitUnknown) // not-found (exit 0, A2 S7) and every other LoadState
	}
	u, err := unitFacts(raw)
	if err != nil {
		return refuse(codeInternal)
	}
	// Rule 4: the canonical name.
	if u.ID != name {
		return refuse(codeUnitNotCanonical)
	}
	// Rule 5: template instances (A3.4, D-C89). The broker never brings an
	// instance into existence. An active instance passes for start and an
	// inactive one for stop: both end in already_in_state below, after the
	// deny list (D-C89 items 2 and 4).
	if isTemplate {
		switch op {
		case opServiceStart:
			if u.ActiveState != stateActive && u.ActiveState != stateFailed {
				return refuse(codeUnitTemplate)
			}
		case opServiceRestart:
			if u.ActiveState == stateInactive {
				return refuse(codeUnitTemplate) // a restart would start it (D-C89 item 3)
			}
		}
	}
	// Rule 6: masked. Stop stays allowed.
	if u.LoadState == "masked" && op != opServiceStop {
		return refuse(codeUnitMasked)
	}
	// Rule 7: transient, every operation.
	if u.Transient {
		return refuse(codeUnitTransient)
	}
	// Rule 8: one-shot tasks, every operation (A3.4).
	if u.Type == "oneshot" {
		return refuse(codeUnitOneshot)
	}
	// Rule 9: the unit's own refusal (A3.4).
	if u.RefuseManualStop && (op == opServiceStop || op == opServiceRestart) {
		return refuse(codeRefusedByUnit)
	}
	if u.RefuseManualStart && (op == opServiceStart || op == opServiceRestart) {
		return refuse(codeRefusedByUnit)
	}
	// Rule 10: the deny list, against Id and every entry of Names.
	if denyList.anyMatches(u) {
		return refuse(codeUnitDenied)
	}
	// Rule 11: restart only.
	if op == opServiceStop && restartOnly.anyMatches(u) {
		return refuse(codeOperationNotOffered)
	}
	// The state precondition of B2: start not active, stop not inactive.
	// restart has none: it starts an ordinary inactive unit (D-C89).
	if op == opServiceStart && u.ActiveState == stateActive || op == opServiceStop && u.ActiveState == stateInactive {
		return refuse(codeAlreadyInState)
	}
	return u, answer{}, true
}

// showRaw runs the show shape and checks its shape. A failed call, a non-zero
// exit, a timeout, output beyond the bound and a wrong shape are internal.
func (b *broker) showRaw(name string) (rawShow, code) {
	r, err := b.systemctl(b.o.showDeadline, shapeShow(name))
	if err != nil || r.TimedOut || r.Exit != 0 || r.Overflow {
		return nil, codeInternal
	}
	raw, err := parseShow(r.Out)
	if err != nil {
		return nil, codeInternal
	}
	return raw, ""
}

// readState reads the unit again after the call. ok is false when the read
// fails in any way; the state word is then not reported.
func (b *broker) readState(name string) (unitInfo, bool) {
	raw, c := b.showRaw(name)
	if c != "" {
		return unitInfo{}, false
	}
	u, err := unitFacts(raw)
	if err != nil {
		return unitInfo{}, false
	}
	return u, true
}

// stateAnswer is c with the state word read now, or without one when the
// read fails.
func (b *broker) stateAnswer(c code, name string) answer {
	if u, ok := b.readState(name); ok {
		return answer{Code: c, State: u.ActiveState}
	}
	return answer{Code: c}
}

// runServiceJob runs the job and verifies the result (B2, A3.3, D-C89 item 5).
func (b *broker) runServiceJob(op string, before unitInfo) answer {
	name := before.ID
	r, err := b.systemctl(b.o.opDeadline, shapeUnitJob(serviceVerb[op], name))
	if err != nil {
		return answer{Code: codeInternal}
	}
	if r.TimedOut {
		return b.stateAnswer(codeTimeout, name) // the job may continue inside systemd
	}
	if r.Exit != 0 {
		return b.stateAnswer(codeJobFailed, name)
	}
	if op != opServiceStop {
		time.Sleep(b.o.verifyDelay) // A3.3: the read about 1 s after systemctl returned
	}
	after, ok := b.readState(name)
	if !ok {
		return answer{Code: codeInternal}
	}
	good := false
	switch op {
	case opServiceStart:
		good = after.ActiveState == stateActive
	case opServiceRestart:
		good = after.ActiveState == stateActive &&
			(after.StartMonotonic != before.StartMonotonic || after.MainPID != before.MainPID)
	case opServiceStop:
		good = after.ActiveState == stateInactive || after.ActiveState == stateFailed
	}
	if !good {
		return answer{Code: codeStateUnexpected, State: after.ActiveState}
	}
	return answer{Code: codeSuccess, State: after.ActiveState}
}

// ---- power operations (B2, B6, D-C89 item 6) --------------------------------

var powerVerb = map[string]string{opSystemReboot: verbReboot, opSystemPoweroff: verbPoweroff}

// powerOp runs the checks in the order of B2: policy, busy, the update check
// of B6, once per lifetime (D-C89 item 6). Power operations use no slot of the
// rate window (D-C89 item 7). The shared lock is held until systemctl has
// returned, that is, until systemd has queued the job.
func (b *broker) powerOp(c Caller, req request) answer {
	if ok, refusal := b.policy.Allow(c, Operation(req.Op), ""); !ok {
		return answer{Code: refusal}
	}
	if !b.begin() {
		return answer{Code: codeBusy}
	}
	defer b.end()
	lock, ok := checkUpdate(b.o.updateDir)
	if !ok {
		return answer{Code: codeUpdateRunning}
	}
	defer lock.release()
	b.mu.Lock()
	done := b.power
	b.mu.Unlock()
	if done {
		return answer{Code: codeAlreadyRequested}
	}
	r, err := b.systemctl(b.o.opDeadline, shapePower(powerVerb[req.Op]))
	switch {
	case err != nil:
		return answer{Code: codeInternal}
	case r.TimedOut:
		return answer{Code: codeTimeout}
	case r.Exit != 0:
		return answer{Code: codeJobFailed}
	}
	b.mu.Lock()
	b.power = true
	b.mu.Unlock()
	return answer{Code: codeAccepted}
}
