package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// Protocol P is 1 and the accepted range is {1} (U2, U15 rule 6).
const (
	protocolMin = 1
	protocolMax = 1

	// maxRequestBytes is the whole request including its newline (U2).
	maxRequestBytes = 4 << 10
	// maxResponseBytes bounds every response including its newline (U2).
	maxResponseBytes = 8 << 10
)

// The closed list of requests (U2).
const (
	opStatus   = "status"
	opStage    = "stage"
	opActivate = "activate"
	opDiscard  = "discard"
	opAck      = "ack"
)

// errMalformed is the one decode failure. Every cause maps to bad_request, and
// the cause is not kept: an error must never echo the request.
var errMalformed = errors.New("malformed request")

// request is a decoded request. Only the members that the op takes are set.
type request struct {
	V             int
	Op            string
	Version       string // stage, activate, discard
	CorrelationID string // stage, activate
	Confirmed     bool   // activate: components_confirmed
	TransactionID string // ack
}

// readRequest reads one newline-terminated request of at most maxRequestBytes
// (newline included). It stops reading at the newline. Anything after the
// newline, a missing newline, an end of input or an error before the newline
// and an oversize request are all errMalformed. The buffer is fixed; the
// deadline is set by the caller on the connection.
func readRequest(r io.Reader) ([]byte, error) {
	buf := make([]byte, 0, maxRequestBytes+1)
	for len(buf) <= maxRequestBytes {
		n, err := r.Read(buf[len(buf):cap(buf)])
		prev := len(buf)
		buf = buf[:prev+n]
		if i := bytes.IndexByte(buf[prev:], '\n'); i >= 0 {
			end := prev + i // index of the newline
			if end+1 > maxRequestBytes || end+1 != len(buf) {
				return nil, errMalformed // too long, or data after the newline
			}
			return buf[:end], nil
		}
		if err != nil {
			return nil, errMalformed
		}
	}
	return nil, errMalformed
}

// opKeys is the exact member set of each op (U2): a request has these members
// and no others.
var opKeys = map[string][]string{
	opStatus:   {"v", "op"},
	opStage:    {"v", "op", "version", "correlation_id"},
	opActivate: {"v", "op", "version", "correlation_id", "components_confirmed"},
	opDiscard:  {"v", "op", "version"},
	opAck:      {"v", "op", "transaction_id"},
}

// decodeRequest is the strict decoder of U2. It rejects, with errMalformed:
// anything that is not one JSON object, a member that is not one of the four
// names (also a name in another letter case), a repeated member, trailing data
// after the object, null, a member of the wrong type, a `v` that is not an
// integer in the accepted range, an op that is unknown, a member set that is
// not exactly the op's, a version that does not match the pattern of U2, a
// correlation_id or transaction_id that is not 16 lowercase hexadecimal digits
// and a components_confirmed that is not a JSON boolean.
func decodeRequest(line []byte) (request, error) {
	if len(line) >= maxRequestBytes {
		return request{}, errMalformed
	}
	m, err := requestMembers(line)
	if err != nil {
		return request{}, err
	}
	var r request
	var v *int
	var op *string
	if json.Unmarshal(m["v"], &v) != nil || v == nil || *v < protocolMin || *v > protocolMax {
		return request{}, errMalformed
	}
	if json.Unmarshal(m["op"], &op) != nil || op == nil {
		return request{}, errMalformed
	}
	r.V, r.Op = *v, *op
	want, ok := opKeys[r.Op]
	if !ok || len(m) != len(want) {
		return request{}, errMalformed
	}
	for _, k := range want {
		if _, present := m[k]; !present {
			return request{}, errMalformed
		}
	}
	if _, has := m["version"]; has {
		var ver *string
		if json.Unmarshal(m["version"], &ver) != nil || ver == nil || !validVersion(*ver) {
			return request{}, errMalformed
		}
		r.Version = *ver
	}
	if _, has := m["correlation_id"]; has {
		var cid *string
		if json.Unmarshal(m["correlation_id"], &cid) != nil || cid == nil || !isHex(16)(*cid) {
			return request{}, errMalformed
		}
		r.CorrelationID = *cid
	}
	if _, has := m["transaction_id"]; has {
		var tid *string
		if json.Unmarshal(m["transaction_id"], &tid) != nil || tid == nil || !isHex(16)(*tid) {
			return request{}, errMalformed
		}
		r.TransactionID = *tid
	}
	if _, has := m["components_confirmed"]; has {
		var c *bool
		if json.Unmarshal(m["components_confirmed"], &c) != nil || c == nil {
			return request{}, errMalformed
		}
		r.Confirmed = *c
	}
	return r, nil
}

// requestMembers splits the line into its members. It closes two gaps of
// encoding/json: it matches object keys case-insensitively ("OP" would set
// `op`) and lets the last of two equal keys win. The request must be one
// object whose keys are exactly known names, each at most once, with nothing
// after it.
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
		case "v", "op", "version", "correlation_id", "components_confirmed", "transaction_id":
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

// errorCode is one of the closed codes of U2.
type errorCode string

// The closed set of error codes (U2). TestErrorCodesMatchTheADR compares the
// list with the ADR.
const (
	codeBadRequest           errorCode = "bad_request"
	codeBusy                 errorCode = "busy"
	codeNotFound             errorCode = "not_found"
	codeSignatureInvalid     errorCode = "signature_invalid"
	codeChecksumMismatch     errorCode = "checksum_mismatch"
	codeManifestInvalid      errorCode = "manifest_invalid"
	codeDowngradeRefused     errorCode = "downgrade_refused"
	codeAlreadyInstalled     errorCode = "already_installed"
	codeRequirementsNotMet   errorCode = "requirements_not_met"
	codeHelperTooOld         errorCode = "helper_too_old"
	codeInstallerRequired    errorCode = "installer_required"
	codeConfirmationRequired errorCode = "confirmation_required"
	codeKeyChanged           errorCode = "key_changed"
	codeUnitRejected         errorCode = "unit_rejected"
	codeComponentsDamaged    errorCode = "components_damaged"
	codeStageExpired         errorCode = "stage_expired"
	codeNoSpace              errorCode = "no_space"
	codeCurrentUnhealthy     errorCode = "current_unhealthy"
	codeCooldown             errorCode = "cooldown"
	codeIOError              errorCode = "io_error"
	codeInternal             errorCode = "internal"
)

// errorTexts are the fixed texts. They are constants: no text contains the
// request, a path or an operating-system error string.
var errorTexts = map[errorCode]string{
	codeBadRequest:           "The request is not valid.",
	codeBusy:                 "The helper is busy with another operation.",
	codeNotFound:             "The requested item was not found.",
	codeSignatureInvalid:     "The release signature is not valid.",
	codeChecksumMismatch:     "A checksum does not match.",
	codeManifestInvalid:      "The release manifest is not valid.",
	codeDowngradeRefused:     "The release is older than the installed version.",
	codeAlreadyInstalled:     "This version is already installed.",
	codeRequirementsNotMet:   "This device does not meet the release requirements.",
	codeHelperTooOld:         "The installed helper is too old for this release.",
	codeInstallerRequired:    "This release must be installed with the installer.",
	codeConfirmationRequired: "An explicit confirmation is required.",
	codeKeyChanged:           "The release changes the trusted key.",
	codeUnitRejected:         "A unit file was rejected.",
	codeComponentsDamaged:    "The installed helper components are damaged.",
	codeStageExpired:         "The verified release has expired.",
	codeNoSpace:              "There is not enough free space.",
	codeCurrentUnhealthy:     "The installed version is not healthy.",
	codeCooldown:             "Try again later.",
	codeIOError:              "A storage error occurred.",
	codeInternal:             "An internal error occurred.",
}

// errorResponse is the answer to a request that failed. Differs is set only
// for installer_required: the names, from the closed list of U2, of the
// components of the release that differ from the installed ones.
type errorResponse struct {
	V       int       `json:"v"`
	OK      bool      `json:"ok"`
	Error   errorCode `json:"error"`
	Message string    `json:"message"`
	Differs []string  `json:"differs,omitempty"`
}

// statusResponse is the answer to `status`: the members of the stub and the
// additive members of G6.5. Field names follow the ADR text (state.json,
// manifest) and may only be extended additively (U15 rule 6). See
// docs/ARCHITECTURE.md for each field.
type statusResponse struct {
	V                int             `json:"v"`
	OK               bool            `json:"ok"`
	ProtocolMin      int             `json:"protocol_min"`
	ProtocolMax      int             `json:"protocol_max"`
	HelperVersion    string          `json:"helper_version"`
	KeyID            string          `json:"key_id"`
	InstalledVersion string          `json:"installed_version"`
	Phase            string          `json:"phase"`
	StagedVersion    string          `json:"staged_version"`
	PreviousVersion  string          `json:"previous_version"`
	LastResult       *jsonLastResult `json:"last_result"`
	NeedsAttention   bool            `json:"needs_attention"`
	FreeBytes        uint64          `json:"free_bytes"`
	// Cooldown (G6.6): the version that is refused and the seconds left of the
	// longer of the two markers; 0 when no marker holds.
	CooldownVersion    string `json:"cooldown_version"`
	CooldownRemainingS int64  `json:"cooldown_remaining_s"`
}

// stageResponse is the answer to a successful `stage`. ComponentsDiffer is
// always false in G6.5: a difference is installer_required. The member exists
// for G6.10.
type stageResponse struct {
	V                int    `json:"v"`
	OK               bool   `json:"ok"`
	Version          string `json:"version"`
	Commit           string `json:"commit"`
	ComponentsDiffer bool   `json:"components_differ"`
}

// activateResponse is the answer to an accepted `activate`: the transaction
// continues in the helper without the caller.
type activateResponse struct {
	V             int    `json:"v"`
	OK            bool   `json:"ok"`
	Status        string `json:"status"`
	TransactionID string `json:"transaction_id"`
}

// okResponse is the answer to a successful `discard`.
type okResponse struct {
	V  int  `json:"v"`
	OK bool `json:"ok"`
}

// encode returns the newline-terminated JSON of v. The values it is used for
// are plain structs, so Marshal cannot fail.
func encode(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic("encode: " + err.Error())
	}
	return append(b, '\n')
}

func errorBody(c errorCode) []byte {
	return encode(errorResponse{V: protocolMax, OK: false, Error: c, Message: errorTexts[c]})
}

// differsBody is the installer_required answer with the differing names.
func differsBody(names []string) []byte {
	return encode(errorResponse{V: protocolMax, OK: false, Error: codeInstallerRequired, Message: errorTexts[codeInstallerRequired], Differs: names})
}
