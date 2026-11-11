package updater

import (
	"context"
	"crypto/x509"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/swingstate/oarfish/backend/internal/audit"
	"github.com/swingstate/oarfish/backend/internal/clock"
	"github.com/swingstate/oarfish/backend/internal/relverify"
	"github.com/swingstate/oarfish/backend/internal/trustkey"
)

// The rules of ADR 0010 U3 and A11.1 (D-C68).
const (
	pageInterval  = 60 * time.Minute // update page check: at most once
	forceInterval = 60 * time.Second // "Check now": at most once, device-wide
	noticeAge     = 24 * time.Hour   // the notice checks only when the result is older
	noticeFloor   = 60 * time.Minute // the notice never retries a failed attempt sooner
	shortBound    = 3 * time.Second  // check handlers never wait longer for the work
	pauseLimit    = 30 * time.Minute // a staged release expires after 30 minutes (U5)
	maxNotesBytes = 4096
)

// Config configures New.
type Config struct {
	// Version is the version of the running service, the installed version.
	Version string
	// CacheDir is the cache directory (systemd's CACHE_DIRECTORY). Empty: a
	// download is not possible and apply answers cache_error.
	CacheDir string
	// Helper is the client of the update helper. Nil: the helper is absent.
	Helper HelperAPI
	Clock  clock.Clock
	// Audit receives the update entries (ADR 0009 A6); nil discards them.
	Audit func(audit.Event)
	// Log gets plain messages that contain codes only; nil discards them.
	Log *slog.Logger
	// Arch is the architecture the manifest must name; empty is runtime.GOARCH.
	Arch string
	// Key is the trusted release key (trustkey.Release).
	Key trustkey.Key
}

// Updater is the service side of the update. Its methods are safe for
// concurrent use. It has no timer and no goroutine of its own: a job runs only
// after a request started it.
type Updater struct {
	cfg  Config
	gh   *ghClient
	now  func() time.Time
	arch string

	// Overridable by the tests of this package.
	sleep        func(ctx context.Context, d time.Duration) error
	stall, abs   time.Duration
	retry        time.Duration
	bound        time.Duration
	pause        time.Duration
	pageEvery    time.Duration
	forceEvery   time.Duration
	noticeEvery  time.Duration
	noticeBackof time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu          sync.Mutex
	etag        string
	releases    []releaseWire
	haveList    bool
	res         *checkResult // the last successful check; the notice and the page share it
	attempted   bool
	lastAttempt time.Time // the last attempt of any outcome
	lastErr     *Error
	waitUntil   time.Time // rate-limit wait
	job         *jobState
	reserved    bool
	audited     map[string]bool
	acked       map[string]bool
	helperProto int
}

// New returns an updater. It does nothing until it is asked.
func New(cfg Config) *Updater { return newUpdater(cfg, nil) }

func newUpdater(cfg Config, roots *x509.CertPool) *Updater {
	if cfg.Clock == nil {
		cfg.Clock = clock.System()
	}
	if cfg.Arch == "" {
		cfg.Arch = runtime.GOARCH
	}
	ctx, cancel := context.WithCancel(context.Background())
	u := &Updater{
		cfg: cfg, arch: cfg.Arch, now: cfg.Clock.Now, sleep: sleepCtx,
		stall: stallTimeout, abs: absoluteTimeout, retry: retryDelay, bound: shortBound, pause: pauseLimit,
		pageEvery: pageInterval, forceEvery: forceInterval, noticeEvery: noticeAge, noticeBackof: noticeFloor,
		ctx: ctx, cancel: cancel, audited: map[string]bool{}, acked: map[string]bool{}, helperProto: protocolVersion,
	}
	u.gh = newGHClient(cfg.Version, roots, u.now)
	return u
}

// Close ends a running job and waits for it. It is called at shutdown.
func (u *Updater) Close() {
	u.cancel()
	u.wg.Wait()
}

func (u *Updater) logf(msg string, code Code) {
	if u.cfg.Log != nil {
		u.cfg.Log.Warn(msg, "code", string(code))
	}
}

func (u *Updater) record(e audit.Event) {
	if u.cfg.Audit != nil {
		u.cfg.Audit(e)
	}
}

// installed parses the running version.
func (u *Updater) installed() (relverify.Version, *Error) {
	v, err := relverify.ParseVersion(u.cfg.Version)
	if err != nil {
		return relverify.Version{}, fail(CodeVersionUnknown)
	}
	return v, nil
}

// ---- the views of the routes ---------------------------------------------

// ErrView is an error as the routes show it: the code and its fixed text.
type ErrView struct {
	Code              Code   `json:"code"`
	Message           string `json:"message"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
}

func viewOf(e *Error) *ErrView {
	if e == nil {
		return nil
	}
	v := &ErrView{Code: e.Code, Message: Text(e.Code)}
	if e.RetryAfter > 0 {
		v.RetryAfterSeconds = int((e.RetryAfter + time.Second - 1) / time.Second)
	}
	return v
}

// Available is the release the last check found.
type Available struct {
	Version string `json:"version"`
	Channel string `json:"channel"` // stable | prerelease
	// Notes are the release notes from GitHub: plain text, not signed.
	Notes        string   `json:"notes"`
	DownloadSize int64    `json:"download_size"`
	Verified     bool     `json:"verified"` // signature, checksums and manifest checked (advisory)
	VerifyError  *ErrView `json:"verify_error,omitempty"`
}

// HelperView is what the page shows about the helper.
type HelperView struct {
	Present            bool     `json:"present"`
	Error              *ErrView `json:"error,omitempty"`
	Phase              string   `json:"phase,omitempty"`
	StagedVersion      string   `json:"staged_version,omitempty"`
	NeedsAttention     bool     `json:"needs_attention,omitempty"`
	CooldownVersion    string   `json:"cooldown_version,omitempty"`
	CooldownRemainingS int64    `json:"cooldown_remaining_s,omitempty"`
	// SocketRestartPending: a component update changed the socket unit of the
	// helper; the new text takes effect at the next boot (ADR 0010 A15 D4).
	SocketRestartPending bool `json:"socket_restart_pending,omitempty"`
}

// CheckView is the state of the shared check result.
type CheckView struct {
	State       string   `json:"state"` // never | ok | error
	CheckedAt   string   `json:"checked_at,omitempty"`
	Error       *ErrView `json:"error,omitempty"`
	Throttled   bool     `json:"throttled,omitempty"`
	NextCheckAt string   `json:"next_check_at,omitempty"`
}

// JobView is the job that runs or ran last.
type JobView struct {
	Kind       string   `json:"kind"` // check | apply
	Step       string   `json:"step"`
	Version    string   `json:"version,omitempty"`
	Running    bool     `json:"running"`
	StartedAt  string   `json:"started_at"`
	FinishedAt string   `json:"finished_at,omitempty"`
	Error      *ErrView `json:"error,omitempty"`
	// Differs names what the staged release replaces besides the release
	// itself, from the closed list of ADR 0010 U2 (A15 D6). It is set while the
	// job waits for the second confirmation and after it.
	Differs []string `json:"differs,omitempty"`
}

// ResultView is the last result of the helper (ADR 0010 U8).
type ResultView struct {
	To         string `json:"to"`
	From       string `json:"from"`
	Outcome    string `json:"outcome"`
	ReasonCode string `json:"reason_code,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
	// ComponentsUpdated: the helper components were replaced, whatever the
	// outcome of the release (ADR 0010 U13 F31).
	ComponentsUpdated bool `json:"components_updated,omitempty"`
}

// InstallerView is the transitional instruction (ADR 0010 U9): a release that
// replaces privileged parts needs one run of the installer until G6.10.
type InstallerView struct {
	Required    bool     `json:"required"`
	Version     string   `json:"version"`
	Instruction string   `json:"instruction"`
	Differs     []string `json:"differs"`
}

// InstallerInstruction is the fixed text. It contains nothing from GitHub, the
// helper or the user, and it builds no command: the page names the release and
// the installation guide describes the run.
const InstallerInstruction = "This release replaces privileged parts of Oarfish, so it cannot be applied from this page. " +
	"Install it with the installer of this release: download install.sh from the release page of this version, " +
	"read it, and run it on the device with sudo. Your settings and your account are kept. " +
	"The installation guide describes the steps."

// State is the answer of GET /api/v1/update.
type State struct {
	SchemaVersion    int            `json:"schema_version"`
	InstalledVersion string         `json:"installed_version"`
	Helper           HelperView     `json:"helper"`
	Check            CheckView      `json:"check"`
	Available        *Available     `json:"available"`
	Job              *JobView       `json:"job"`
	LastResult       *ResultView    `json:"last_result"`
	Installer        *InstallerView `json:"installer"`
}

// StateSchemaVersion is the version of the State layout. Adding members does
// not change it.
const StateSchemaVersion = 1

func ts(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// State builds the answer of GET /api/v1/update. It asks the helper (at most
// 10 s), writes the finishing audit entry for a result nobody has seen yet and
// acknowledges it (ADR 0010 U8). It starts no check and no job.
func (u *Updater) State(ctx context.Context) State { return u.stateWith(ctx, false) }

func (u *Updater) stateWith(ctx context.Context, throttled bool) State {
	hv, status := u.helperView(ctx)
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.stateLocked(hv, status, throttled)
}

func (u *Updater) stateLocked(hv HelperView, status *HelperStatus, throttled bool) State {
	s := State{SchemaVersion: StateSchemaVersion, InstalledVersion: u.cfg.Version, Helper: hv}
	s.Check = CheckView{State: "never"}
	switch {
	case u.res != nil:
		s.Check = CheckView{State: "ok", CheckedAt: ts(u.res.at)}
		s.Available = u.res.avail
	}
	if u.lastErr != nil {
		c := s.Check
		c.State, c.Error = "error", viewOf(u.lastErr)
		s.Check = c
	}
	if throttled {
		s.Check.Throttled = true
	}
	if u.attempted {
		s.Check.NextCheckAt = ts(u.lastAttempt.Add(u.pageEvery))
	}
	if j := u.job; j != nil {
		s.Job = &JobView{Kind: string(j.kind), Step: string(j.step), Version: j.version, Running: !j.done,
			StartedAt: ts(j.started), FinishedAt: ts(j.finished), Error: viewOf(j.err), Differs: append([]string(nil), j.differs...)}
		if j.err != nil && j.err.Code == CodeInstallerRequired && j.kind == jobApply {
			s.Installer = &InstallerView{Required: true, Version: j.version, Instruction: InstallerInstruction, Differs: append([]string{}, j.err.Differs...)}
		}
	}
	if status != nil && status.LastResult != nil {
		r := status.LastResult
		s.LastResult = &ResultView{To: r.To, From: r.From, Outcome: r.Outcome, ReasonCode: r.ReasonCode, FinishedAt: r.FinishedAt,
			ComponentsUpdated: r.ComponentsUpdated}
	}
	return s
}

// helperView asks the helper for its status and reconciles its last result.
func (u *Updater) helperView(ctx context.Context) (HelperView, *HelperStatus) {
	if u.cfg.Helper == nil {
		return HelperView{Present: false, Error: viewOf(fail(CodeUnavailable))}, nil
	}
	st, e := u.cfg.Helper.Status(ctx)
	if e != nil {
		return HelperView{Present: false, Error: viewOf(e)}, nil
	}
	u.mu.Lock()
	if st.ProtocolMax > 0 {
		u.helperProto = st.ProtocolMax
	}
	u.mu.Unlock()
	u.reconcile(ctx, &st)
	return HelperView{Present: true, Phase: st.Phase, StagedVersion: st.StagedVersion, NeedsAttention: st.NeedsAttention,
		CooldownVersion: st.CooldownVersion, CooldownRemainingS: st.CooldownRemainingS, SocketRestartPending: st.SocketRestartPending}, &st
}

// reconcile writes update_finished for a last result that nobody has seen
// (usually after the restart that the update caused), with the correlation ID
// of the matching update_started and the user "-", and then acknowledges it
// (ADR 0010 U8, ADR 0009 A6). Each result is audited once per process.
func (u *Updater) reconcile(ctx context.Context, st *HelperStatus) {
	r := st.LastResult
	if r == nil || r.Acknowledged {
		return
	}
	u.mu.Lock()
	first := !u.audited[r.TransactionID]
	u.audited[r.TransactionID] = true
	acked := u.acked[r.TransactionID]
	u.mu.Unlock()
	if first && r.CorrelationID != "" {
		u.record(audit.Event{User: audit.NoUser, Action: audit.UpdateFinished, Target: audit.UpdateTarget(r.To),
			Result: audit.UpdateResult(r.Outcome), CorrelationID: r.CorrelationID})
	}
	if acked {
		return
	}
	if e := u.cfg.Helper.Ack(ctx, r.TransactionID); e == nil {
		u.mu.Lock()
		u.acked[r.TransactionID] = true
		u.mu.Unlock()
	} else {
		u.logf("update: the last result could not be acknowledged", e.Code)
	}
}
