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

// The closed list of requests (U2). Only opStatus is answered in the stub.
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

// request is the stub's request: `v` and `op`. The arguments of stage,
// activate, discard and ack (U2) are added with those requests in G6.5/G6.6;
// until then they are unknown fields and the request is bad_request.
type request struct {
	V  *int    `json:"v"`
	Op *string `json:"op"`
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

// decodeRequest is the strict decoder of U2. It rejects, with errMalformed:
// anything that is not one JSON object, unknown fields
// (DisallowUnknownFields), trailing data after the object, a missing or null
// `v` or `op`, a `v` that is not an integer in the accepted range, and key
// names that encoding/json would match loosely (see checkKeys).
func decodeRequest(line []byte) (request, error) {
	if len(line) >= maxRequestBytes || checkKeys(line) != nil {
		return request{}, errMalformed
	}
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	var r request
	if err := dec.Decode(&r); err != nil {
		return request{}, errMalformed
	}
	if _, err := dec.Token(); err != io.EOF {
		return request{}, errMalformed // trailing data
	}
	if r.V == nil || r.Op == nil || *r.V < protocolMin || *r.V > protocolMax {
		return request{}, errMalformed
	}
	return r, nil
}

// checkKeys closes two gaps of encoding/json: it matches object keys
// case-insensitively ("OP" would set `op`) and lets the last of two equal keys
// win. The request must be an object whose keys are exactly the known names,
// each at most once.
func checkKeys(line []byte) error {
	dec := json.NewDecoder(bytes.NewReader(line))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return errMalformed
	}
	seen := map[string]bool{}
	for dec.More() {
		t, err := dec.Token()
		key, ok := t.(string)
		if err != nil || !ok || (key != "v" && key != "op") || seen[key] {
			return errMalformed
		}
		seen[key] = true
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return errMalformed
		}
	}
	return nil
}

// errorCode is one of the closed codes of U2.
type errorCode string

// The closed set of error codes (U2). The stub produces bad_request, io_error
// and internal; the others are listed because the set is closed, and
// TestErrorCodesMatchTheADR compares the list with the ADR.
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

// errorResponse is the answer to a request that failed.
type errorResponse struct {
	V       int       `json:"v"`
	OK      bool      `json:"ok"`
	Error   errorCode `json:"error"`
	Message string    `json:"message"`
}

// statusResponse is the answer to `status` in the stub: a subset of U2.
// Field names follow the ADR text (state.json, manifest) and may only be
// extended additively (U15 rule 6). See docs/ARCHITECTURE.md for each field.
type statusResponse struct {
	V                int    `json:"v"`
	OK               bool   `json:"ok"`
	ProtocolMin      int    `json:"protocol_min"`
	ProtocolMax      int    `json:"protocol_max"`
	HelperVersion    string `json:"helper_version"`
	KeyID            string `json:"key_id"`
	InstalledVersion string `json:"installed_version"`
	Phase            string `json:"phase"`
	FreeBytes        uint64 `json:"free_bytes"`
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
