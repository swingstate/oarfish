// Package brokerclient is the service's client of the privilege broker's
// socket for the three service operations (ADR 0011 B2, B4, A4.3; step G7.4)
// and the two power operations (step G7.5). It speaks protocol 1 exactly: one
// request per connection, one JSON line of at most 1,024 bytes with the member
// set of the operation, one bounded answer line, decoded strictly against the
// closed code set of that kind of operation. A mutating request is never sent
// twice: a retry could run the operation twice. status is not part of this
// package: the "status" of ADR 0011 B6 is the update helper's.
package brokerclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"time"

	"github.com/swingstate/oarfish/backend/internal/unitname"
)

// SocketPath is the broker's socket (ADR 0011 B1, B8). Its directory is
// root-owned; the socket is 0660 root:oarfish.
const SocketPath = "/run/oarfish-broker/broker.sock"

// The bounds of the protocol (A4.3) and of this client.
const (
	protocolVersion = 1
	maxRequest      = 1 << 10 // the whole request line including its newline
	maxAnswer       = 8 << 10 // one answer line including its newline
	// Deadline is the whole time the client waits for one operation: the
	// broker's deadline of 100 s for the systemctl job, its verification read
	// about 1 s later, and a margin (A3.3).
	Deadline = 110 * time.Second
	// PowerDeadline is the whole time the client waits for a power operation.
	// The power request is answered inside the HTTP request, so it stays below
	// the server's write timeout of 30 s; the broker answers as soon as systemd
	// has queued the job (A4.4 item 7), which takes about a second.
	PowerDeadline = 20 * time.Second
)

// Op is one of the three service operations. Nothing else is ever sent.
type Op string

const (
	OpStart   Op = "service.start"
	OpStop    Op = "service.stop"
	OpRestart Op = "service.restart"
)

// PowerOp is one of the two power operations (A4.3). Nothing else is sent
// without a unit.
type PowerOp string

const (
	OpReboot   PowerOp = "system.reboot"
	OpPoweroff PowerOp = "system.poweroff"
)

// Code is the result of one operation: a broker code of the closed set below,
// or the client's own CodeUnavailable.
type Code string

// The codes the service accepts for a service operation (ADR 0011 B2, A3.3,
// A4.3). A power-only code (accepted, update_running, already_requested) or any
// other word in an answer becomes CodeInternal.
const (
	CodeSuccess             Code = "success"
	CodeBadRequest          Code = "bad_request"
	CodeBusy                Code = "busy"
	CodeRateLimited         Code = "rate_limited"
	CodeUnitInvalid         Code = "unit_invalid"
	CodeUnitUnknown         Code = "unit_unknown"
	CodeUnitNotCanonical    Code = "unit_not_canonical"
	CodeUnitDenied          Code = "unit_denied"
	CodeUnitMasked          Code = "unit_masked"
	CodeUnitTransient       Code = "unit_transient"
	CodeUnitOneshot         Code = "unit_oneshot"
	CodeUnitTemplate        Code = "unit_template"
	CodeRefusedByUnit       Code = "refused_by_unit"
	CodeOperationNotOffered Code = "operation_not_offered"
	CodeAlreadyInState      Code = "already_in_state"
	CodeJobFailed           Code = "job_failed"
	CodeTimeout             Code = "timeout"
	CodeStateUnexpected     Code = "state_unexpected"
	CodeInternal            Code = "internal"
	// CodeUnavailable is the service's own: the socket is missing, the
	// connection was refused or reset, or no answer came.
	CodeUnavailable Code = "unavailable"

	// The codes only a power operation produces (ADR 0011 B2, A4.1, A4.4 item 7).
	CodeAccepted         Code = "accepted"
	CodeUpdateRunning    Code = "update_running"
	CodeAlreadyRequested Code = "already_requested"
)

// Codes is the closed set of broker codes for a service operation.
var Codes = []Code{
	CodeSuccess, CodeBadRequest, CodeBusy, CodeRateLimited, CodeUnitInvalid, CodeUnitUnknown, CodeUnitNotCanonical,
	CodeUnitDenied, CodeUnitMasked, CodeUnitTransient, CodeUnitOneshot, CodeUnitTemplate, CodeRefusedByUnit,
	CodeOperationNotOffered, CodeAlreadyInState, CodeJobFailed, CodeTimeout, CodeStateUnexpected, CodeInternal,
}

// PowerCodes is the closed set of broker codes for a power operation: what
// powerOp and the decoder of the broker can answer (A4.3, A4.4 items 7 and 9).
// A service-only code (unit_*, rate_limited, already_in_state,
// state_unexpected, success) or any other word becomes CodeInternal.
var PowerCodes = []Code{
	CodeAccepted, CodeBadRequest, CodeBusy, CodeUpdateRunning, CodeAlreadyRequested, CodeJobFailed, CodeTimeout, CodeInternal,
}

// States is the closed set of ActiveState words an answer may carry (A4.3).
var States = []string{"active", "reloading", "inactive", "failed", "activating", "deactivating", "maintenance", "refreshing"}

// Answer is the decoded result. State is a word of States or ""; RetryAfterS
// is set on rate_limited only. Nothing else of the answer is kept.
type Answer struct {
	Code        Code
	State       string
	RetryAfterS int
}

// API is what the action job needs. *Client is the socket client; the tests
// of the job use a fake.
type API interface {
	// Available tells whether the broker's socket exists, by a stat of its path
	// only: connecting would start the root process (socket activation).
	Available() bool
	// Do sends one service operation and waits for its answer, at most
	// Deadline. It never retries.
	Do(ctx context.Context, op Op, unit, correlationID string) Answer
}

// PowerAPI is what the power route needs. *Client is the socket client; the
// tests of the route use a fake.
type PowerAPI interface {
	// Available is the same stat of the socket as API.Available.
	Available() bool
	// Power sends one power operation and waits for its answer, at most
	// PowerDeadline. It never retries.
	Power(ctx context.Context, op PowerOp, correlationID string) Code
}

// Client is the client of the socket at a path.
type Client struct {
	socket        string
	deadline      time.Duration
	powerDeadline time.Duration
	dial          func(ctx context.Context, path string) (net.Conn, error)
	stat          func(path string) (fs.FileInfo, error)
}

var (
	_ API      = (*Client)(nil)
	_ PowerAPI = (*Client)(nil)
)

// New returns a client of the socket at path.
func New(path string) *Client {
	return &Client{socket: path, deadline: Deadline, powerDeadline: PowerDeadline,
		dial: func(ctx context.Context, p string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", p)
		},
		stat: os.Stat,
	}
}

// Available is a stat of the socket path: it exists and is a socket.
func (c *Client) Available() bool {
	fi, err := c.stat(c.socket)
	return err == nil && fi.Mode().Type() == fs.ModeSocket
}

// Do sends one request on one connection and reads one answer. A request that
// cannot be built (an operation outside the three, a unit that fails rule 1, a
// correlation ID that is not 16 lowercase hexadecimal digits) is CodeInternal
// and nothing is sent.
func (c *Client) Do(ctx context.Context, op Op, unit, correlationID string) Answer {
	line, ok := request(op, unit, correlationID)
	if !ok {
		return Answer{Code: CodeInternal}
	}
	raw, code := c.exchange(ctx, c.deadline, line)
	if code != "" {
		return Answer{Code: code}
	}
	a, ok := decodeAnswer(raw)
	if !ok {
		return Answer{Code: CodeInternal}
	}
	return a
}

// Power sends one power operation on one connection and reads one answer. An
// operation outside the two or a correlation ID that is not 16 lowercase
// hexadecimal digits is CodeInternal and nothing is sent.
func (c *Client) Power(ctx context.Context, op PowerOp, correlationID string) Code {
	line, ok := powerRequest(op, correlationID)
	if !ok {
		return CodeInternal
	}
	raw, code := c.exchange(ctx, c.powerDeadline, line)
	if code != "" {
		return code
	}
	got, ok := decodePowerAnswer(raw)
	if !ok {
		return CodeInternal
	}
	return got
}

// exchange sends line once and returns the answer line, or the code that
// replaces it: CodeInternal for an unusable answer, CodeUnavailable for no
// answer at all.
func (c *Client) exchange(ctx context.Context, deadline time.Duration, line []byte) ([]byte, Code) {
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	raw, err := c.once(ctx, line)
	if err != nil {
		if errors.Is(err, errBadAnswer) {
			return nil, CodeInternal
		}
		// Not reachable, reset, closed without an answer, or no answer in time:
		// the operation may or may not have run; it is not sent again.
		return nil, CodeUnavailable
	}
	return raw, ""
}

// powerRequest is the request of A4.3 for a power operation: exactly v, op and
// correlation_id, no unit.
func powerRequest(op PowerOp, cid string) ([]byte, bool) {
	if op != OpReboot && op != OpPoweroff || !validCorrelationID(cid) {
		return nil, false
	}
	b, err := json.Marshal(struct {
		V             int     `json:"v"`
		Op            PowerOp `json:"op"`
		CorrelationID string  `json:"correlation_id"`
	}{protocolVersion, op, cid})
	if err != nil {
		return nil, false
	}
	return append(b, '\n'), true
}

func request(op Op, unit, cid string) ([]byte, bool) {
	if op != OpStart && op != OpStop && op != OpRestart {
		return nil, false
	}
	if !unitname.Valid(unit) || !validCorrelationID(cid) {
		return nil, false
	}
	b, err := json.Marshal(struct {
		V             int    `json:"v"`
		Op            Op     `json:"op"`
		Unit          string `json:"unit"`
		CorrelationID string `json:"correlation_id"`
	}{protocolVersion, op, unit, cid})
	if err != nil {
		return nil, false
	}
	b = append(b, '\n')
	if len(b) > maxRequest {
		return nil, false
	}
	return b, true
}

// validCorrelationID is the bound of the broker (A4.1): 16 lowercase hex digits.
func validCorrelationID(s string) bool {
	if len(s) != 16 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

var errBadAnswer = errors.New("unusable answer")

func (c *Client) once(ctx context.Context, line []byte) ([]byte, error) {
	conn, err := c.dial(ctx, c.socket)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	// A cancelled context closes the connection, so a blocked read returns.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if _, err := conn.Write(line); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(conn, maxAnswer+1))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, io.ErrUnexpectedEOF // closed without an answer
	}
	if len(raw) > maxAnswer || raw[len(raw)-1] != '\n' || bytes.Count(raw, []byte("\n")) != 1 {
		return nil, errBadAnswer
	}
	return raw[:len(raw)-1], nil
}

var (
	codeSet      = setOf(Codes)
	powerCodeSet = setOf(PowerCodes)
	stateSet     = setOf(States)
	// The codes with which the broker reports the state it read (A4.3).
	stateCodes = map[Code]bool{CodeSuccess: true, CodeJobFailed: true, CodeTimeout: true, CodeStateUnexpected: true}
)

func setOf[T comparable](xs []T) map[T]bool {
	m := map[T]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// answerMembers splits one answer line into its members: exactly one JSON
// object, keys from the four names of A4.3 in their exact letter case, each at
// most once, and nothing after it. v must be the literal 1.
func answerMembers(line []byte) (map[string]json.RawMessage, bool) {
	dec := json.NewDecoder(bytes.NewReader(line))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, false
	}
	m := map[string]json.RawMessage{}
	for dec.More() {
		t, err := dec.Token()
		key, ok := t.(string)
		if err != nil || !ok {
			return nil, false
		}
		switch key {
		case "v", "code", "state", "retry_after_s":
		default:
			return nil, false
		}
		if _, dup := m[key]; dup {
			return nil, false
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, false
		}
		m[key] = v
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('}') {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	if string(m["v"]) != "1" {
		return nil, false
	}
	return m, true
}

// codeIn decodes the code member: a JSON string of the set.
func codeIn(m map[string]json.RawMessage, set map[Code]bool) (Code, bool) {
	var code *string
	if json.Unmarshal(m["code"], &code) != nil || code == nil || !set[Code(*code)] {
		return "", false
	}
	return Code(*code), true
}

// decodeAnswer is the strict decoder of the answer to a service operation:
// one JSON object with exactly the members v (the literal 1), code (a word of
// Codes), state (a word of States, only with a code that carries one) and
// retry_after_s (a whole number of at least 1, with rate_limited and only
// then). An unknown member, a member in another letter case, a repeated
// member, null, a wrong type or data after the object fails.
func decodeAnswer(line []byte) (Answer, bool) {
	m, ok := answerMembers(line)
	if !ok {
		return Answer{}, false
	}
	var a Answer
	if a.Code, ok = codeIn(m, codeSet); !ok {
		return Answer{}, false
	}
	if raw, has := m["state"]; has {
		var s *string
		if json.Unmarshal(raw, &s) != nil || s == nil || !stateSet[*s] || !stateCodes[a.Code] {
			return Answer{}, false
		}
		a.State = *s
	}
	raw, has := m["retry_after_s"]
	if has != (a.Code == CodeRateLimited) {
		return Answer{}, false
	}
	if has {
		n, ok := wholeNumber(raw)
		if !ok || n < 1 {
			return Answer{}, false
		}
		a.RetryAfterS = n
	}
	return a, true
}

// decodePowerAnswer is the strict decoder of the answer to a power operation:
// exactly the members v (the literal 1) and code (a word of PowerCodes). The
// broker sends no state and no wait time for a power operation (A4.4 item 7),
// so either member fails, as does everything decodeAnswer refuses.
func decodePowerAnswer(line []byte) (Code, bool) {
	m, ok := answerMembers(line)
	if !ok || len(m) != 2 {
		return "", false
	}
	return codeIn(m, powerCodeSet)
}

// wholeNumber accepts plain digits without a sign, a fraction or an exponent,
// up to 86,400 (one day).
func wholeNumber(raw []byte) (int, bool) {
	if len(raw) == 0 || len(raw) > 5 || (len(raw) > 1 && raw[0] == '0') {
		return 0, false
	}
	n := 0
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, n <= 86400
}
