// Package brokerclient is the service's client of the privilege broker's
// socket for the three service operations (ADR 0011 B2, B4, A4.3; step G7.4).
// It speaks protocol 1 exactly: one request per connection, one JSON line of
// at most 1,024 bytes with the member set of the operation, one bounded answer
// line, decoded strictly. A mutating request is never sent twice: a retry
// could run the operation twice. status, system.reboot and system.poweroff
// are not part of this package (G7.5).
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
)

// Op is one of the three service operations. Nothing else is ever sent.
type Op string

const (
	OpStart   Op = "service.start"
	OpStop    Op = "service.stop"
	OpRestart Op = "service.restart"
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
)

// Codes is the closed set of broker codes for a service operation.
var Codes = []Code{
	CodeSuccess, CodeBadRequest, CodeBusy, CodeRateLimited, CodeUnitInvalid, CodeUnitUnknown, CodeUnitNotCanonical,
	CodeUnitDenied, CodeUnitMasked, CodeUnitTransient, CodeUnitOneshot, CodeUnitTemplate, CodeRefusedByUnit,
	CodeOperationNotOffered, CodeAlreadyInState, CodeJobFailed, CodeTimeout, CodeStateUnexpected, CodeInternal,
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

// Client is the client of the socket at a path.
type Client struct {
	socket   string
	deadline time.Duration
	dial     func(ctx context.Context, path string) (net.Conn, error)
	stat     func(path string) (fs.FileInfo, error)
}

var _ API = (*Client)(nil)

// New returns a client of the socket at path.
func New(path string) *Client {
	return &Client{socket: path, deadline: Deadline,
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
	ctx, cancel := context.WithTimeout(ctx, c.deadline)
	defer cancel()
	raw, err := c.once(ctx, line)
	if err != nil {
		if errors.Is(err, errBadAnswer) {
			return Answer{Code: CodeInternal}
		}
		// Not reachable, reset, closed without an answer, or no answer in time:
		// the operation may or may not have run; it is not sent again.
		return Answer{Code: CodeUnavailable}
	}
	a, ok := decodeAnswer(raw)
	if !ok {
		return Answer{Code: CodeInternal}
	}
	return a
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
	codeSet  = setOf(Codes)
	stateSet = setOf(States)
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

// decodeAnswer is the strict decoder of the answer to a service operation:
// one JSON object with exactly the members v (the literal 1), code (a word of
// Codes), state (a word of States, only with a code that carries one) and
// retry_after_s (a whole number of at least 1, with rate_limited and only
// then). An unknown member, a member in another letter case, a repeated
// member, null, a wrong type or data after the object fails.
func decodeAnswer(line []byte) (Answer, bool) {
	dec := json.NewDecoder(bytes.NewReader(line))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return Answer{}, false
	}
	m := map[string]json.RawMessage{}
	for dec.More() {
		t, err := dec.Token()
		key, ok := t.(string)
		if err != nil || !ok {
			return Answer{}, false
		}
		switch key {
		case "v", "code", "state", "retry_after_s":
		default:
			return Answer{}, false
		}
		if _, dup := m[key]; dup {
			return Answer{}, false
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return Answer{}, false
		}
		m[key] = v
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('}') {
		return Answer{}, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return Answer{}, false
	}
	if string(m["v"]) != "1" {
		return Answer{}, false
	}
	var a Answer
	var code *string
	if json.Unmarshal(m["code"], &code) != nil || code == nil || !codeSet[Code(*code)] {
		return Answer{}, false
	}
	a.Code = Code(*code)
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
