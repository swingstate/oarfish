package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
)

// state.json (ADR 0010 U8) lives in /var/lib/oarfish-update, mode 0600, and is
// replaced by write to a temporary file, fsync, rename and fsync of the
// directory. Only the fields that G6.5 populates are written with values;
// the others are declared so that a file written by a later helper is read
// without loss and an older helper ignores what it does not know (U15 rule 6).

const (
	stateFile        = "state.json"
	stateTempPrefix  = ".state.tmp."
	stateSchema      = 1
	maxStateBytes    = 64 << 10
	maxUptimeSeconds = int64(1) << 40
)

// The phases of U7 and, for a newer file, the component phases of U15 rule 4.
const (
	phaseIdle           = "idle"
	phaseStaging        = "staging"
	phaseStaged         = "staged"
	phaseSwitching      = "switching"
	phaseVerifying      = "verifying"
	phaseCommitted      = "committed"
	phaseRollingBack    = "rolling_back"
	phaseRolledBack     = "rolled_back" // a phase name only a state file can hold; same text as the outcome
	phaseRollbackFailed = "rollback_failed"

	// The component phases of U15 rule 4. They sit in front of the release
	// phases of the same transaction.
	phaseCSwitching   = "c_switching"
	phaseCSwitched    = "c_switched"
	phaseCVerified    = "c_verified"
	phaseCCommitted   = "c_committed"
	phaseCRollingBack = "c_rolling_back"
)

var knownPhases = map[string]bool{
	phaseIdle: true, phaseStaging: true, phaseStaged: true,
	phaseSwitching: true, phaseVerifying: true, phaseCommitted: true, phaseRollingBack: true,
	phaseRolledBack: true, phaseRollbackFailed: true,
	phaseCSwitching: true, phaseCSwitched: true, phaseCVerified: true, phaseCCommitted: true, phaseCRollingBack: true,
}

// The outcomes of a finished transaction (U2).
const (
	outcomeSuccess              = "success"
	outcomeRolledBack           = "rolled_back"
	outcomeComponentsRolledBack = "components_rolled_back"
	outcomeRollbackFailed       = "rollback_failed"
	outcomeAborted              = "aborted"
)

var knownOutcomes = map[string]bool{
	outcomeSuccess: true, outcomeRolledBack: true, outcomeComponentsRolledBack: true, outcomeRollbackFailed: true, outcomeAborted: true,
}

// Reason codes written by this gate.
const (
	reasonInterrupted  = "interrupted"
	reasonStageExpired = "stage_expired"
	// G6.6.
	reasonSwitchFailed   = "switch_failed"
	reasonRestartFailed  = "restart_failed"
	reasonStartFailed    = "start_failed"
	reasonHealthTimeout  = "health_timeout"
	reasonTargetDamaged  = "rollback_target_damaged"
	reasonRollbackUnheal = "rollback_unhealthy"
	reasonCurrentUnknown = "current_unknown"
	reasonStateRebuilt   = "state_rebuilt"
	reasonUnknownPhase   = "unknown_phase"
	reasonRecordFailed   = "state_write_failed"
	// G6.10 step b. The first five are the closed set of reasons of a
	// components_rolled_back result that a self-check or a recovery gives
	// (A16); switch_failed, state_write_failed and reload_failed are the
	// reasons of a call that failed in the helper that handed over.
	reasonHelperMismatch   = "helper_mismatch"
	reasonKeyChanged       = "key_changed"
	reasonProtocolMismatch = "protocol_mismatch"
	reasonStateUnreadable  = "state_unreadable"
	reasonReloadFailed     = "reload_failed"
	reasonKnownGoodDamaged = "known_good_damaged"
	reasonUnitRejected     = "unit_rejected"
	reasonStagedChanged    = "staged_changed"
)

// componentRollbackReasons is the closed set of reasons of the outcome
// components_rolled_back (A16).
var componentRollbackReasons = []string{
	reasonHelperMismatch, reasonKeyChanged, reasonProtocolMismatch, reasonStateUnreadable, reasonInterrupted,
	reasonSwitchFailed, reasonRecordFailed, reasonReloadFailed,
}

var reasonPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// state is the content of state.json.
type state struct {
	HelperVersion string
	// Previous is the previous healthy version, the rollback target. G6.5 never
	// sets it; status reports it and the guards of discard, expiry and the
	// staging recovery never touch its directory. U8 lists no member for it,
	// so this one is added (additively).
	Previous string
	// NeedsAttention is set when a human has to look (a failed rollback, a
	// state file rebuilt from the disk) and cleared by `ack` of the last
	// result. U8 lists no member for it, so this one is added (additively).
	NeedsAttention bool
	// ComponentsPrevious is the component set that the last component commit
	// replaced. It is kept by the retention (U5); nothing switches back to it.
	ComponentsPrevious string
	Txn                txn
	Cool               cooldown
	Last               *lastResult
}

type txn struct {
	ID                   string
	Phase                string
	To, From             string
	FromBinarySHA256     string
	CorrelationID        string
	StagedAtUptimeS      int64
	ComponentsFrom       string
	ComponentsTo         string
	KnownGoodSHA256      string
	SocketRestartPending bool

	// The additive members of G6.10 step b (A16). Written by stage:
	// Differs is the record of D6, the names of the closed list whose staged
	// file differs from the installed one; ServiceUnitSHA256 is the SHA-256 of
	// the staged oarfish.service; with a component set, ComponentsSetSHA256 is
	// the set digest of the staged set and ComponentsHelperSHA256 the SHA-256
	// of its helper, the expected hash of D1.
	Differs                []string
	ServiceUnitSHA256      string
	ComponentsSetSHA256    string
	ComponentsHelperSHA256 string
	// Written with c_switching by the helper that hands over (E3): its embedded
	// key id, its protocol range, and whether the normalised socket unit text
	// changes (the intent of D4).
	ComponentsKeyID       string
	ComponentsProtocolMin int64
	ComponentsProtocolMax int64
	SocketTextChanged     bool
	// ComponentsUpdated is set with c_committed and copied into the result.
	ComponentsUpdated bool
	// RollbackReason is written with c_rolling_back, so that a recovery that
	// finishes the rollback reports the reason that started it.
	RollbackReason string

	// Status members that outlive a transaction, like SocketRestartPending:
	// the set that KnownGoodSHA256 was computed from (E2), and the time since
	// boot at which SocketRestartPending was set (D4).
	KnownGoodID               string
	SocketRestartSetAtUptimeS int64
}

type cooldown struct {
	RefuseVersion            string
	RefuseUntilUptimeS       int64
	RefuseSetAtUptimeS       int64
	LastActivateUptimeS      int64
	LastActivateSetAtUptimeS int64
}

// refuse is the cooldownMarker for RefuseVersion.
func (c cooldown) refuse() cooldownMarker {
	return cooldownMarker{SetAtUptimeS: c.RefuseSetAtUptimeS, UntilUptimeS: c.RefuseUntilUptimeS}
}

type lastResult struct {
	TransactionID string
	To, From      string
	Outcome       string
	ReasonCode    string
	CorrelationID string
	FinishedAt    string // wall-clock RFC 3339, for display only
	Acknowledged  bool
	// ComponentsUpdated: the transaction replaced the helper components, whatever
	// the outcome of its release part (U13 F31).
	ComponentsUpdated bool
}

func freshState(helperVersion string) *state {
	return &state{HelperVersion: helperVersion, Txn: txn{Phase: phaseIdle}}
}

// resetTxn ends a transaction. What is a status of the installation and not of
// one transaction stays: the socket flag with its time (U15 rule 4 step 6, D4)
// and the reference hash of the known-good set with the set it names (E2).
func (s *state) resetTxn() {
	t := s.Txn
	s.Txn = txn{Phase: phaseIdle, SocketRestartPending: t.SocketRestartPending, SocketRestartSetAtUptimeS: t.SocketRestartSetAtUptimeS,
		KnownGoodSHA256: t.KnownGoodSHA256, KnownGoodID: t.KnownGoodID}
}

// ---- encoding --------------------------------------------------------------

type jsonState struct {
	SchemaVersion   int    `json:"schema_version"`
	HelperVersion   string `json:"helper_version"`
	PreviousVersion string `json:"previous_version"`
	NeedsAttention  bool   `json:"needs_attention"`
	// components_previous is additive (G6.10 step b).
	ComponentsPrevious string          `json:"components_previous"`
	Transaction        jsonTxn         `json:"transaction"`
	Cooldown           jsonCooldown    `json:"cooldown"`
	LastResult         *jsonLastResult `json:"last_result"`
}

type jsonTxn struct {
	ID                   string `json:"id"`
	Phase                string `json:"phase"`
	To                   string `json:"to"`
	From                 string `json:"from"`
	FromBinarySHA256     string `json:"from_binary_sha256"`
	CorrelationID        string `json:"correlation_id"`
	StagedAtUptimeS      int64  `json:"staged_at_uptime_s"`
	ComponentsFrom       string `json:"components_from"`
	ComponentsTo         string `json:"components_to"`
	KnownGoodSHA256      string `json:"known_good_sha256"`
	SocketRestartPending bool   `json:"socket_restart_pending"`

	// Additive members of G6.10 step b; the order is the order of txn.
	Differs                   []string `json:"differs"`
	ServiceUnitSHA256         string   `json:"service_unit_sha256"`
	ComponentsSetSHA256       string   `json:"components_set_sha256"`
	ComponentsHelperSHA256    string   `json:"components_helper_sha256"`
	ComponentsKeyID           string   `json:"components_key_id"`
	ComponentsProtocolMin     int64    `json:"components_protocol_min"`
	ComponentsProtocolMax     int64    `json:"components_protocol_max"`
	SocketTextChanged         bool     `json:"socket_text_changed"`
	ComponentsUpdated         bool     `json:"components_updated"`
	RollbackReason            string   `json:"rollback_reason"`
	KnownGoodID               string   `json:"known_good_id"`
	SocketRestartSetAtUptimeS int64    `json:"socket_restart_set_at_uptime_s"`
}

type jsonCooldown struct {
	RefuseVersion            string `json:"refuse_version"`
	RefuseUntilUptimeS       int64  `json:"refuse_until_uptime_s"`
	RefuseSetAtUptimeS       int64  `json:"refuse_set_at_uptime_s"`
	LastActivateUptimeS      int64  `json:"last_activate_uptime_s"`
	LastActivateSetAtUptimeS int64  `json:"last_activate_set_at_uptime_s"`
}

type jsonLastResult struct {
	TransactionID string `json:"transaction_id"`
	To            string `json:"to"`
	From          string `json:"from"`
	Outcome       string `json:"outcome"`
	ReasonCode    string `json:"reason_code"`
	CorrelationID string `json:"correlation_id"`
	FinishedAt    string `json:"finished_at"`
	Acknowledged  bool   `json:"acknowledged"`
	// components_updated is additive (G6.10 step b, U13 F31).
	ComponentsUpdated bool `json:"components_updated"`
}

func toJSONLast(l *lastResult) *jsonLastResult {
	if l == nil {
		return nil
	}
	j := jsonLastResult(*l)
	return &j
}

// encodeState returns the bytes of state.json.
func encodeState(s *state) []byte {
	js := jsonState{
		SchemaVersion: stateSchema, HelperVersion: s.HelperVersion, PreviousVersion: s.Previous, NeedsAttention: s.NeedsAttention,
		ComponentsPrevious: s.ComponentsPrevious,
		Transaction:        jsonTxn(s.Txn), Cooldown: jsonCooldown(s.Cool), LastResult: toJSONLast(s.Last),
	}
	if js.Transaction.Differs == nil {
		js.Transaction.Differs = []string{}
	}
	b, err := json.MarshalIndent(js, "", "  ")
	if err != nil {
		panic("encodeState: " + err.Error())
	}
	return append(b, '\n')
}

// ---- decoding --------------------------------------------------------------

var errStateInvalid = errors.New("state file is not valid")

// object splits a JSON object into its members. Duplicate keys, and a key that
// differs from a known name only in letter case (which encoding/json would
// match), are errors; unknown keys are kept and ignored by the caller.
func object(raw []byte, known ...string) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errStateInvalid
	}
	lower := map[string]string{}
	for _, k := range known {
		lower[strings.ToLower(k)] = k
	}
	m := map[string]json.RawMessage{}
	for dec.More() {
		t, err := dec.Token()
		key, ok := t.(string)
		if err != nil || !ok {
			return nil, errStateInvalid
		}
		if _, dup := m[key]; dup {
			return nil, errStateInvalid
		}
		if want, isKnown := lower[strings.ToLower(key)]; isKnown && want != key {
			return nil, errStateInvalid
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, errStateInvalid
		}
		m[key] = v
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('}') {
		return nil, errStateInvalid
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errStateInvalid // data after the object
	}
	return m, nil
}

// field decoders: a missing member keeps the zero value, a present one must
// have the exact type (null is a type error).
func str(m map[string]json.RawMessage, key string, ok func(string) bool, dst *string) error {
	raw, present := m[key]
	if !present {
		return nil
	}
	var p *string
	if json.Unmarshal(raw, &p) != nil || p == nil || !ok(*p) {
		return errStateInvalid
	}
	*dst = *p
	return nil
}

func num(m map[string]json.RawMessage, key string, dst *int64) error {
	raw, present := m[key]
	if !present {
		return nil
	}
	var p *int64
	if json.Unmarshal(raw, &p) != nil || p == nil || *p < 0 || *p > maxUptimeSeconds {
		return errStateInvalid
	}
	*dst = *p
	return nil
}

func flag(m map[string]json.RawMessage, key string, dst *bool) error {
	raw, present := m[key]
	if !present {
		return nil
	}
	var p *bool
	if json.Unmarshal(raw, &p) != nil || p == nil {
		return errStateInvalid
	}
	*dst = *p
	return nil
}

// maxProtocol bounds a protocol number in the state file.
const maxProtocol = 1 << 20

func protocolNum(m map[string]json.RawMessage, key string, dst *int64) error {
	var v int64
	if err := num(m, key, &v); err != nil || v > maxProtocol {
		return errStateInvalid
	}
	if _, present := m[key]; present {
		*dst = v
	}
	return nil
}

// names decodes the record of D6: an array of names of the closed list of U2,
// each at most once, in the fixed order. An empty array is no difference.
func names(m map[string]json.RawMessage, key string, dst *[]string) error {
	raw, present := m[key]
	if !present {
		return nil
	}
	var p *[]string
	if json.Unmarshal(raw, &p) != nil || p == nil || len(*p) > len(componentNames) {
		return errStateInvalid
	}
	next := 0
	for _, n := range *p {
		i := slices.Index(componentNames, n)
		if i < next {
			return errStateInvalid // unknown, repeated or out of order
		}
		next = i + 1
	}
	if len(*p) > 0 {
		*dst = *p
	}
	return nil
}

func emptyOr(f func(string) bool) func(string) bool {
	return func(s string) bool { return s == "" || f(s) }
}

func isHex(n int) func(string) bool {
	return func(s string) bool {
		if len(s) != n {
			return false
		}
		for i := 0; i < len(s); i++ {
			if c := s[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
		return true
	}
}

var (
	okVersion  = emptyOr(validVersion)
	okID       = emptyOr(isHex(16))
	okSHA      = emptyOr(isHex(64))
	okPhase    = func(s string) bool { return knownPhases[s] }
	okOutcome  = func(s string) bool { return knownOutcomes[s] }
	okReason   = emptyOr(reasonPattern.MatchString)
	okHelperV  = func(s string) bool { return len(s) <= 64 && printableASCII(s) }
	okFinished = emptyOr(func(s string) bool { _, err := time.Parse(time.RFC3339, s); return err == nil && len(s) <= 40 })
)

func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// decodeState parses state.json. Unknown members are ignored at every level;
// known members are strict (type, range, pattern, phase and outcome names).
func decodeState(data []byte) (*state, error) {
	if len(data) > maxStateBytes {
		return nil, errStateInvalid
	}
	top, err := object(data, "schema_version", "helper_version", "previous_version", "needs_attention", "components_previous", "transaction", "cooldown", "last_result")
	if err != nil {
		return nil, err
	}
	raw, ok := top["schema_version"]
	if !ok {
		return nil, errStateInvalid
	}
	var sp *int64
	if json.Unmarshal(raw, &sp) != nil || sp == nil || *sp != stateSchema {
		return nil, errStateInvalid
	}
	s := &state{Txn: txn{Phase: phaseIdle}}
	if err := str(top, "helper_version", okHelperV, &s.HelperVersion); err != nil {
		return nil, err
	}
	if err := str(top, "previous_version", okVersion, &s.Previous); err != nil {
		return nil, err
	}
	if err := flag(top, "needs_attention", &s.NeedsAttention); err != nil {
		return nil, err
	}
	if err := str(top, "components_previous", okVersion, &s.ComponentsPrevious); err != nil {
		return nil, err
	}
	if raw, ok := top["transaction"]; ok {
		if err := decodeTxn(raw, &s.Txn); err != nil {
			return nil, err
		}
	}
	if raw, ok := top["cooldown"]; ok {
		if err := decodeCooldown(raw, &s.Cool); err != nil {
			return nil, err
		}
	}
	if raw, ok := top["last_result"]; ok && string(bytes.TrimSpace(raw)) != "null" {
		l := &lastResult{}
		if err := decodeLast(raw, l); err != nil {
			return nil, err
		}
		s.Last = l
	}
	return s, nil
}

func decodeTxn(raw []byte, t *txn) error {
	m, err := object(raw, "id", "phase", "to", "from", "from_binary_sha256", "correlation_id", "staged_at_uptime_s",
		"components_from", "components_to", "known_good_sha256", "socket_restart_pending",
		"differs", "service_unit_sha256", "components_set_sha256", "components_helper_sha256", "components_key_id",
		"components_protocol_min", "components_protocol_max", "socket_text_changed", "components_updated", "rollback_reason",
		"known_good_id", "socket_restart_set_at_uptime_s")
	if err != nil {
		return err
	}
	for _, e := range []error{
		str(m, "id", okID, &t.ID), str(m, "phase", okPhase, &t.Phase),
		str(m, "to", okVersion, &t.To), str(m, "from", okVersion, &t.From),
		str(m, "from_binary_sha256", okSHA, &t.FromBinarySHA256), str(m, "correlation_id", okID, &t.CorrelationID),
		num(m, "staged_at_uptime_s", &t.StagedAtUptimeS),
		str(m, "components_from", okVersion, &t.ComponentsFrom), str(m, "components_to", okVersion, &t.ComponentsTo),
		str(m, "known_good_sha256", okSHA, &t.KnownGoodSHA256), flag(m, "socket_restart_pending", &t.SocketRestartPending),
		names(m, "differs", &t.Differs),
		str(m, "service_unit_sha256", okSHA, &t.ServiceUnitSHA256), str(m, "components_set_sha256", okSHA, &t.ComponentsSetSHA256),
		str(m, "components_helper_sha256", okSHA, &t.ComponentsHelperSHA256), str(m, "components_key_id", okID, &t.ComponentsKeyID),
		protocolNum(m, "components_protocol_min", &t.ComponentsProtocolMin), protocolNum(m, "components_protocol_max", &t.ComponentsProtocolMax),
		flag(m, "socket_text_changed", &t.SocketTextChanged), flag(m, "components_updated", &t.ComponentsUpdated),
		str(m, "rollback_reason", okReason, &t.RollbackReason), str(m, "known_good_id", okVersion, &t.KnownGoodID),
		num(m, "socket_restart_set_at_uptime_s", &t.SocketRestartSetAtUptimeS),
	} {
		if e != nil {
			return e
		}
	}
	if t.Phase == "" {
		t.Phase = phaseIdle
	}
	return nil
}

func decodeCooldown(raw []byte, c *cooldown) error {
	m, err := object(raw, "refuse_version", "refuse_until_uptime_s", "refuse_set_at_uptime_s", "last_activate_uptime_s", "last_activate_set_at_uptime_s")
	if err != nil {
		return err
	}
	for _, e := range []error{
		str(m, "refuse_version", okVersion, &c.RefuseVersion),
		num(m, "refuse_until_uptime_s", &c.RefuseUntilUptimeS), num(m, "refuse_set_at_uptime_s", &c.RefuseSetAtUptimeS),
		num(m, "last_activate_uptime_s", &c.LastActivateUptimeS), num(m, "last_activate_set_at_uptime_s", &c.LastActivateSetAtUptimeS),
	} {
		if e != nil {
			return e
		}
	}
	return nil
}

func decodeLast(raw []byte, l *lastResult) error {
	m, err := object(raw, "transaction_id", "to", "from", "outcome", "reason_code", "correlation_id", "finished_at", "acknowledged", "components_updated")
	if err != nil {
		return err
	}
	if _, ok := m["outcome"]; !ok {
		return errStateInvalid
	}
	for _, e := range []error{
		str(m, "transaction_id", okID, &l.TransactionID), str(m, "to", okVersion, &l.To), str(m, "from", okVersion, &l.From),
		str(m, "outcome", okOutcome, &l.Outcome), str(m, "reason_code", okReason, &l.ReasonCode),
		str(m, "correlation_id", okID, &l.CorrelationID), str(m, "finished_at", okFinished, &l.FinishedAt),
		flag(m, "acknowledged", &l.Acknowledged), flag(m, "components_updated", &l.ComponentsUpdated),
	} {
		if e != nil {
			return e
		}
	}
	return nil
}

// ---- reading and writing ---------------------------------------------------

// loadState reads state.json through the state root. A missing file is a fresh
// idle state without a last result (ok true, s fresh). A file that exists and
// can not be read or is not valid gives errStateInvalid: the caller reports
// it and repairs nothing (G6.6).
func loadState(root *os.Root, helperVersion string) (*state, error) {
	f, fi, err := openRegular(root, stateFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return freshState(helperVersion), nil
		}
		return nil, errStateInvalid
	}
	defer f.Close()
	if fi.Size() > maxStateBytes {
		return nil, errStateInvalid
	}
	data, err := io.ReadAll(io.LimitReader(f, maxStateBytes+1))
	if err != nil {
		return nil, errStateInvalid
	}
	return decodeState(data)
}

// saveState replaces state.json: a temporary file in the same directory
// (O_EXCL, 0600), write, fsync, rename, fsync of the directory. A leftover
// temporary file is never read; a failed save removes its own.
func (e *env) saveState(s *state) error {
	s.HelperVersion = e.h.helperVersion
	data := encodeState(s)
	id, err := e.h.newID()
	if err != nil {
		return err
	}
	tmp := stateTempPrefix + id
	f, err := e.fs.Create(treeState, tmp, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		e.fs.RemoveAll(treeState, tmp)
		return werr
	}
	if err := e.fs.Rename(treeState, tmp, stateFile); err != nil {
		e.fs.RemoveAll(treeState, tmp)
		return err
	}
	return e.fs.SyncDir(treeState, ".")
}
