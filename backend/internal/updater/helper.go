package updater

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"regexp"
	"syscall"
	"time"

	"github.com/swingstate/oarfish/backend/internal/relverify"
)

// SocketPath is the socket of the update helper (ADR 0010 U2). The unit's
// RuntimeDirectory is root-owned, so the service cannot replace the socket.
const SocketPath = "/run/oarfish-update/helper.sock"

// The client timeouts of ADR 0010 A11.3 (D-C68): the helper holds its lock
// during the synchronous pre-check of activate for up to 10 s, and stage has a
// request deadline of 180 s.
const (
	TimeoutStatus   = 10 * time.Second
	TimeoutDiscard  = 10 * time.Second
	TimeoutAck      = 10 * time.Second
	TimeoutActivate = 30 * time.Second
	TimeoutStage    = 200 * time.Second

	protocolVersion = 1
	maxResponse     = 8 << 10 // U2: at most 8 KiB including the newline
	retryPause      = 200 * time.Millisecond
)

// HelperResult is the last result of the helper (ADR 0010 U8).
type HelperResult struct {
	TransactionID string `json:"transaction_id"`
	To            string `json:"to"`
	From          string `json:"from"`
	Outcome       string `json:"outcome"`
	ReasonCode    string `json:"reason_code"`
	CorrelationID string `json:"correlation_id"`
	FinishedAt    string `json:"finished_at"`
	Acknowledged  bool   `json:"acknowledged"`
}

// HelperStatus is the answer to `status`, reduced to what the service uses.
type HelperStatus struct {
	ProtocolMin        int           `json:"protocol_min"`
	ProtocolMax        int           `json:"protocol_max"`
	HelperVersion      string        `json:"helper_version"`
	InstalledVersion   string        `json:"installed_version"`
	Phase              string        `json:"phase"`
	StagedVersion      string        `json:"staged_version"`
	PreviousVersion    string        `json:"previous_version"`
	LastResult         *HelperResult `json:"last_result"`
	NeedsAttention     bool          `json:"needs_attention"`
	FreeBytes          uint64        `json:"free_bytes"`
	CooldownVersion    string        `json:"cooldown_version"`
	CooldownRemainingS int64         `json:"cooldown_remaining_s"`
}

// StageInfo is the answer to a successful `stage`.
type StageInfo struct {
	Version          string `json:"version"`
	Commit           string `json:"commit"`
	ComponentsDiffer bool   `json:"components_differ"`
}

// HelperAPI is the five typed requests of ADR 0010 U2. *Helper is the socket
// client; the tests of the job model use a fake.
type HelperAPI interface {
	Status(ctx context.Context) (HelperStatus, *Error)
	Stage(ctx context.Context, version, correlationID string) (StageInfo, *Error)
	Activate(ctx context.Context, version, correlationID string, componentsConfirmed bool) (transactionID string, err *Error)
	Discard(ctx context.Context, version string) *Error
	Ack(ctx context.Context, transactionID string) *Error
}

// Helper is the client of the update helper's socket. It sends one request per
// connection, newline-terminated JSON of at most 4 KiB, and reads one bounded
// answer. It retries once when the connection fails with ECONNRESET or ENOENT,
// which is what a restart of the socket looks like (A2, V13).
type Helper struct {
	socket string
	dial   func(ctx context.Context, path string) (net.Conn, error)
	sleep  func(ctx context.Context, d time.Duration) error
}

// NewHelper returns a client of the socket at path.
func NewHelper(path string) *Helper {
	return &Helper{socket: path,
		dial: func(ctx context.Context, p string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", p)
		},
		sleep: sleepCtx,
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var (
	idPattern      = regexp.MustCompile(`^[0-9a-f]{16}$`)
	reasonPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	phaseNames     = map[string]bool{"idle": true, "staging": true, "staged": true, "switching": true, "verifying": true, "committed": true, "rolling_back": true, "rolled_back": true, "rollback_failed": true}
	outcomeNames   = map[string]bool{"success": true, "rolled_back": true, "components_rolled_back": true, "rollback_failed": true, "aborted": true}
	componentNames = map[string]bool{"oarfish-update-helper": true, "oarfish-update-helper.socket": true, "oarfish-update-helper.service": true, "oarfish-update-recover.service": true, "oarfish.service": true}
	helperCodes    = map[Code]bool{
		CodeBadRequest: true, CodeBusy: true, CodeNotFound: true, CodeSignatureInvalid: true, CodeChecksumMismatch: true,
		CodeManifestInvalid: true, CodeDowngradeRefused: true, CodeAlreadyInstalled: true, CodeRequirementsNotMet: true,
		CodeHelperTooOld: true, CodeInstallerRequired: true, CodeConfirmationRequired: true, CodeKeyChanged: true,
		CodeUnitRejected: true, CodeComponentsDamaged: true, CodeStageExpired: true, CodeNoSpace: true,
		CodeCurrentUnhealthy: true, CodeCooldown: true, CodeIOError: true, CodeInternal: true,
	}
)

// validVersion is the version rule of ADR 0010 U2 (the same as relverify).
func validVersion(s string) bool { _, err := relverify.ParseVersion(s); return err == nil }

type envelope struct {
	V       int      `json:"v"`
	OK      bool     `json:"ok"`
	Error   Code     `json:"error"`
	Differs []string `json:"differs"`
}

// call sends line and decodes the answer into out (when ok). It returns a typed
// error: the helper's own code, unavailable when the socket cannot be reached,
// timeout when the deadline passes, internal for an answer that cannot be used.
func (h *Helper) call(ctx context.Context, timeout time.Duration, line []byte, out any) *Error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var raw []byte
	var err error
	for attempt := 0; ; attempt++ {
		raw, err = h.once(ctx, line)
		if err == nil || attempt > 0 || !(errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ENOENT)) {
			break
		}
		if h.sleep(ctx, retryPause) != nil {
			break
		}
	}
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			return fail(CodeCancelled)
		case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
			return fail(CodeTimeout)
		case errors.Is(err, errBadAnswer):
			return fail(CodeInternal)
		}
		return fail(CodeUnavailable)
	}
	var env envelope
	if json.Unmarshal(raw, &env) != nil || env.V != protocolVersion {
		return fail(CodeInternal)
	}
	if !env.OK {
		if !helperCodes[env.Error] {
			return fail(CodeInternal)
		}
		e := fail(env.Error)
		if env.Error == CodeInstallerRequired {
			for _, n := range env.Differs {
				if componentNames[n] {
					e.Differs = append(e.Differs, n)
				}
			}
		}
		return e
	}
	if out != nil && json.Unmarshal(raw, out) != nil {
		return fail(CodeInternal)
	}
	return nil
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func (h *Helper) once(ctx context.Context, line []byte) ([]byte, error) {
	conn, err := h.dial(ctx, h.socket)
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
		return nil, ctxOr(ctx, err)
	}
	raw, err := io.ReadAll(io.LimitReader(conn, maxResponse+1))
	if err != nil {
		return nil, ctxOr(ctx, err)
	}
	if len(raw) > maxResponse || len(raw) == 0 || raw[len(raw)-1] != '\n' {
		return nil, errBadAnswer
	}
	return bytes.TrimSuffix(raw, []byte("\n")), nil
}

// ctxOr prefers the context's error: a closed connection after a deadline is a
// timeout, not an unreachable helper.
func ctxOr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

var errBadAnswer = errors.New("unusable answer")

func line(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic("updater: " + err.Error()) // plain structs only
	}
	return append(b, '\n')
}

// Status asks for the helper's state. Whatever the helper reports is checked
// against the patterns and closed sets before it is used.
func (h *Helper) Status(ctx context.Context) (HelperStatus, *Error) {
	var st HelperStatus
	if e := h.call(ctx, TimeoutStatus, line(struct {
		V  int    `json:"v"`
		Op string `json:"op"`
	}{protocolVersion, "status"}), &st); e != nil {
		return HelperStatus{}, e
	}
	if !st.valid() {
		return HelperStatus{}, fail(CodeInternal)
	}
	return st, nil
}

func (st *HelperStatus) valid() bool {
	okVer := func(s string) bool { return s == "" || validVersion(s) }
	if !okVer(st.InstalledVersion) || !okVer(st.StagedVersion) || !okVer(st.PreviousVersion) || !okVer(st.CooldownVersion) ||
		!phaseNames[st.Phase] || st.ProtocolMax < st.ProtocolMin || st.ProtocolMin < 0 {
		return false
	}
	if len(st.HelperVersion) > 64 {
		st.HelperVersion = ""
	}
	if r := st.LastResult; r != nil {
		if !idPattern.MatchString(r.TransactionID) || !outcomeNames[r.Outcome] || !validVersion(r.To) || !okVer(r.From) ||
			!(r.ReasonCode == "" || reasonPattern.MatchString(r.ReasonCode)) || !(r.CorrelationID == "" || idPattern.MatchString(r.CorrelationID)) {
			return false
		}
		if _, err := time.Parse(time.RFC3339, r.FinishedAt); err != nil {
			r.FinishedAt = ""
		}
	}
	return true
}

// Stage asks the helper to copy and verify the four cached files of version.
func (h *Helper) Stage(ctx context.Context, version, correlationID string) (StageInfo, *Error) {
	var si StageInfo
	if e := h.call(ctx, TimeoutStage, line(struct {
		V             int    `json:"v"`
		Op            string `json:"op"`
		Version       string `json:"version"`
		CorrelationID string `json:"correlation_id"`
	}{protocolVersion, "stage", version, correlationID}), &si); e != nil {
		return StageInfo{}, e
	}
	if si.Version != version {
		return StageInfo{}, fail(CodeInternal)
	}
	if len(si.Commit) > 64 {
		si.Commit = ""
	}
	return si, nil
}

// Activate starts the transaction. The helper answers at once with "accepted".
func (h *Helper) Activate(ctx context.Context, version, correlationID string, confirmed bool) (string, *Error) {
	var r struct {
		Status        string `json:"status"`
		TransactionID string `json:"transaction_id"`
	}
	if e := h.call(ctx, TimeoutActivate, line(struct {
		V             int    `json:"v"`
		Op            string `json:"op"`
		Version       string `json:"version"`
		CorrelationID string `json:"correlation_id"`
		Confirmed     bool   `json:"components_confirmed"`
	}{protocolVersion, "activate", version, correlationID, confirmed}), &r); e != nil {
		return "", e
	}
	if r.Status != "accepted" || !idPattern.MatchString(r.TransactionID) {
		return "", fail(CodeInternal)
	}
	return r.TransactionID, nil
}

// Discard removes a staged release.
func (h *Helper) Discard(ctx context.Context, version string) *Error {
	return h.call(ctx, TimeoutDiscard, line(struct {
		V       int    `json:"v"`
		Op      string `json:"op"`
		Version string `json:"version"`
	}{protocolVersion, "discard", version}), nil)
}

// Ack marks the last result as seen.
func (h *Helper) Ack(ctx context.Context, transactionID string) *Error {
	return h.call(ctx, TimeoutAck, line(struct {
		V             int    `json:"v"`
		Op            string `json:"op"`
		TransactionID string `json:"transaction_id"`
	}{protocolVersion, "ack", transactionID}), nil)
}
