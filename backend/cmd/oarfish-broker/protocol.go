package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// The protocol (ADR 0011 B2, B4, A4). One request per connection: one JSON
// object on one line, at most maxRequestBytes including the newline,
//
//	{"v":1,"op":"…","unit":"…","correlation_id":"…"}
//
// with `unit` for the three service operations only and `correlation_id` for
// every operation. The answer is one JSON line (see answer and statusAnswer).
const (
	protocolMin = 1
	protocolMax = 1

	maxRequestBytes = 1 << 10 // the whole request including its newline (B2)
	cidLength       = 16      // correlation_id: exactly 16 lowercase hexadecimal digits (D-C89)
)

// The closed list of operations (B2). Nothing else is ever accepted.
const (
	opStatus         = "status"
	opServiceStart   = "service.start"
	opServiceStop    = "service.stop"
	opServiceRestart = "service.restart"
	opSystemReboot   = "system.reboot"
	opSystemPoweroff = "system.poweroff"
)

// opKeys is the exact member set of each operation.
var opKeys = map[string][]string{
	opStatus:         {"v", "op", "correlation_id"},
	opServiceStart:   {"v", "op", "unit", "correlation_id"},
	opServiceStop:    {"v", "op", "unit", "correlation_id"},
	opServiceRestart: {"v", "op", "unit", "correlation_id"},
	opSystemReboot:   {"v", "op", "correlation_id"},
	opSystemPoweroff: {"v", "op", "correlation_id"},
}

func isServiceOp(op string) bool {
	return op == opServiceStart || op == opServiceStop || op == opServiceRestart
}

func isPowerOp(op string) bool { return op == opSystemReboot || op == opSystemPoweroff }

// errMalformed is the one decode failure; every cause maps to bad_request and
// the cause is not kept, so nothing of the request can be echoed.
var errMalformed = errors.New("malformed request")

// request is a decoded request. Unit is set only for a service operation and
// is not validated by the decoder: rule 1 of B3 answers unit_invalid for it.
type request struct {
	Op            string
	Unit          string
	CorrelationID string
}

// readRequest reads one newline-terminated request of at most maxRequestBytes
// including the newline and stops at the newline. Data after the newline, a
// missing newline, an end of input or a read error before the newline, and a
// longer request are all errMalformed.
func readRequest(r io.Reader) ([]byte, error) {
	buf := make([]byte, 0, maxRequestBytes+1)
	for len(buf) <= maxRequestBytes {
		prev := len(buf)
		n, err := r.Read(buf[prev:cap(buf)])
		buf = buf[:prev+n]
		if i := bytes.IndexByte(buf[prev:], '\n'); i >= 0 {
			end := prev + i
			if end+1 > maxRequestBytes || end+1 != len(buf) {
				return nil, errMalformed
			}
			return buf[:end], nil
		}
		if err != nil {
			return nil, errMalformed
		}
	}
	return nil, errMalformed
}

// decodeRequest is the strict decoder. It returns errMalformed for anything
// that is not exactly one JSON object with the member set of its operation:
// an unknown member (also a known name in another letter case), a repeated
// member, a missing member, `unit` where the operation takes none, a value of
// the wrong type or null, a `v` other than the integer 1, an unknown `op`, a
// correlation_id that is not 16 lowercase hexadecimal digits, and any data
// after the object.
func decodeRequest(line []byte) (request, error) {
	if len(line) >= maxRequestBytes {
		return request{}, errMalformed
	}
	m, err := requestMembers(line)
	if err != nil {
		return request{}, err
	}
	// The literal 1 only: not 1.0, 1e0, "1" or a larger number.
	if string(bytes.TrimSpace(m["v"])) != "1" {
		return request{}, errMalformed
	}
	var op *string
	if json.Unmarshal(m["op"], &op) != nil || op == nil {
		return request{}, errMalformed
	}
	want, ok := opKeys[*op]
	if !ok || len(m) != len(want) {
		return request{}, errMalformed
	}
	for _, k := range want {
		if _, present := m[k]; !present {
			return request{}, errMalformed
		}
	}
	r := request{Op: *op}
	var cid *string
	if json.Unmarshal(m["correlation_id"], &cid) != nil || cid == nil || !validCorrelationID(*cid) {
		return request{}, errMalformed
	}
	r.CorrelationID = *cid
	if raw, has := m["unit"]; has {
		var u *string
		if json.Unmarshal(raw, &u) != nil || u == nil {
			return request{}, errMalformed
		}
		r.Unit = *u
	}
	return r, nil
}

// requestMembers splits the line into its members. It closes two gaps of
// encoding/json: object keys would match case-insensitively, and the last of
// two equal keys would win.
func requestMembers(line []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(line))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errMalformed
	}
	m := map[string]json.RawMessage{}
	for dec.More() {
		t, err := dec.Token()
		key, ok := t.(string)
		if err != nil || !ok {
			return nil, errMalformed
		}
		switch key {
		case "v", "op", "unit", "correlation_id":
		default:
			return nil, errMalformed
		}
		if _, dup := m[key]; dup {
			return nil, errMalformed
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, errMalformed
		}
		m[key] = value
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('}') {
		return nil, errMalformed
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errMalformed // trailing data
	}
	if _, ok := m["v"]; !ok {
		return nil, errMalformed
	}
	if _, ok := m["op"]; !ok {
		return nil, errMalformed
	}
	return m, nil
}

// validCorrelationID is the bound of correlation_id: exactly 16 lowercase
// hexadecimal digits, the form the service already uses for the helper.
func validCorrelationID(s string) bool {
	if len(s) != cidLength {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// code is one result code of the closed set of B2 (as amended by A3 and
// D-C89), plus success and accepted (D-C89). TestCodesMatchTheADR compares the
// error codes with the table of B2.
type code string

const (
	codeSuccess  code = "success"  // a service operation, or status
	codeAccepted code = "accepted" // a power operation that systemd queued

	codeBadRequest          code = "bad_request"
	codeBusy                code = "busy"
	codeRateLimited         code = "rate_limited"
	codeUnitInvalid         code = "unit_invalid"
	codeUnitUnknown         code = "unit_unknown"
	codeUnitNotCanonical    code = "unit_not_canonical"
	codeUnitDenied          code = "unit_denied"
	codeUnitMasked          code = "unit_masked"
	codeUnitTransient       code = "unit_transient"
	codeUnitOneshot         code = "unit_oneshot"
	codeUnitTemplate        code = "unit_template"
	codeRefusedByUnit       code = "refused_by_unit"
	codeOperationNotOffered code = "operation_not_offered"
	codeAlreadyInState      code = "already_in_state"
	codeJobFailed           code = "job_failed"
	codeTimeout             code = "timeout"
	codeStateUnexpected     code = "state_unexpected"
	codeUpdateRunning       code = "update_running"
	codeAlreadyRequested    code = "already_requested"
	codeInternal            code = "internal"
)

// allCodes is every code the broker can answer.
var allCodes = []code{
	codeSuccess, codeAccepted,
	codeBadRequest, codeBusy, codeRateLimited, codeUnitInvalid, codeUnitUnknown, codeUnitNotCanonical,
	codeUnitDenied, codeUnitMasked, codeUnitTransient, codeUnitOneshot, codeUnitTemplate, codeRefusedByUnit,
	codeOperationNotOffered, codeAlreadyInState, codeJobFailed, codeTimeout, codeStateUnexpected,
	codeUpdateRunning, codeAlreadyRequested, codeInternal,
}

// answer is the reply to every request except status. State is a word of the
// closed set of ActiveState values (activeStates), never a raw string from
// systemd; it is set on success of a service operation and, where B2 says so,
// on job_failed, timeout and state_unexpected when the state could be read.
// RetryAfterS is set on rate_limited only. Nothing of the request, no path,
// no operating-system error text and no systemctl output is ever in it.
type answer struct {
	V           int    `json:"v"`
	Code        code   `json:"code"`
	State       string `json:"state,omitempty"`
	RetryAfterS int    `json:"retry_after_s,omitempty"`
}

// statusAnswer is the reply to status (B2): the protocol range, the build id,
// whether a power operation was accepted in this broker's lifetime (D-C89 item
// 6) and whether an operation runs now.
type statusAnswer struct {
	V              int    `json:"v"`
	Code           code   `json:"code"`
	ProtocolMin    int    `json:"protocol_min"`
	ProtocolMax    int    `json:"protocol_max"`
	BuildID        string `json:"build_id"`
	PowerRequested bool   `json:"power_requested"`
	Busy           bool   `json:"busy"`
}

// encode returns the newline-terminated JSON of v. Every value is a plain
// struct, so Marshal cannot fail.
func encode(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic("encode: " + err.Error())
	}
	return append(b, '\n')
}
