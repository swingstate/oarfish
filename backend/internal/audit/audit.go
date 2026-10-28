// Package audit writes the administrative audit trail of ADR 0009, E7: an own
// append-only JSON-lines file below the state directory, with fixed, typed
// fields only. No field takes free text from a request, so a password or token
// cannot end up in an entry by accident (ORB-SEC-006, ORB-SEC-007).
//
// Writes are buffered. The buffer is written 30 seconds after its first entry,
// before a rotation and at Close; there is no fsync per entry (ORB-NFR-009).
// The accepted cost is that the last seconds can be lost on a power failure.
// A failed write never stops the service and never blocks a request: it is
// counted and reported to the journal as a plain message without data.
package audit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/swingstate/oarfish/backend/internal/clock"
	"github.com/swingstate/oarfish/backend/internal/securefile"
)

// File layout and budgets.
const (
	DirName         = "audit"
	FileName        = "audit.jsonl"
	MaxFileSize     = 1 << 20 // rotate before a file would grow past 1 MiB
	Files           = 4       // audit.jsonl and audit.jsonl.1 to .3: at most 4 MiB
	FlushDelay      = 30 * time.Second
	AggregateWindow = time.Minute
	maxBuffered     = 64 << 10
)

// Action is what happened. The set is closed.
type Action string

// The actions of G5.1.
const (
	SetupCompleted       Action = "setup_completed"
	LoginSucceeded       Action = "login_succeeded"
	LoginFailed          Action = "login_failed" // aggregated
	Logout               Action = "logout"
	PasswordChanged      Action = "password_changed"
	PasswordChangeFailed Action = "password_change_failed"
	RequestRejected      Action = "request_rejected" // aggregated
)

// Target is what the action was about. The set is closed.
type Target string

// The targets of G5.1.
const (
	TargetNone     Target = "-"
	TargetSetup    Target = "setup"
	TargetSession  Target = "session"
	TargetPassword Target = "account/password"
	TargetDevice   Target = "device"
	TargetMetrics  Target = "metrics"
	TargetEvents   Target = "events"
)

// Result is the outcome. The set is closed.
type Result string

// The results of G5.1.
const (
	Success            Result = "success"
	InvalidCredentials Result = "invalid_credentials"
	InvalidSetupToken  Result = "invalid_setup_token"
	RateLimited        Result = "rate_limited"
	WrongPassword      Result = "wrong_password"
	PolicyRejected     Result = "policy_rejected"
	Failed             Result = "error"
	HostRejected       Result = "host_rejected"
	OriginRejected     Result = "origin_rejected"
	CSRFFailed         Result = "csrf_failed"
)

var (
	actions = map[Action]bool{SetupCompleted: true, LoginSucceeded: true, LoginFailed: true, Logout: true,
		PasswordChanged: true, PasswordChangeFailed: true, RequestRejected: true}
	aggregated = map[Action]bool{LoginFailed: true, RequestRejected: true}
	targets    = map[Target]bool{TargetNone: true, TargetSetup: true, TargetSession: true, TargetPassword: true,
		TargetDevice: true, TargetMetrics: true, TargetEvents: true}
	results = map[Result]bool{Success: true, InvalidCredentials: true, InvalidSetupToken: true, RateLimited: true,
		WrongPassword: true, PolicyRejected: true, Failed: true, HostRejected: true, OriginRejected: true, CSRFFailed: true}
	// A user is the account name (the rule of auth.ValidateUsername) or "-".
	userPattern = regexp.MustCompile(`^(-|[a-z0-9._-]{1,32})$`)
	// A correlation ID is the server's own 64-bit value in hex.
	correlationPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

// NoUser is the user of an entry that has no authenticated account behind it.
// A failed login never records the name that was typed: people type their
// password into the user name field.
const NoUser = "-"

// Event is one thing to audit. Every field is checked against its closed set
// or pattern; an event that does not pass is dropped and counted.
type Event struct {
	User          string
	Action        Action
	Target        Target
	Result        Result
	CorrelationID string
}

// entry is the JSON line. Count is 1 for a single event and the number of
// events for an aggregated one.
type entry struct {
	Time          string `json:"time"`
	User          string `json:"user"`
	Plugin        string `json:"plugin"`
	Action        Action `json:"action"`
	Target        Target `json:"target"`
	Result        Result `json:"result"`
	CorrelationID string `json:"correlation_id"`
	Count         int    `json:"count"`
}

// ErrInvalidEvent means an event field is outside its set.
var ErrInvalidEvent = errors.New("audit event outside the allowed values")

func (e Event) validate() error {
	if !actions[e.Action] || !targets[e.Target] || !results[e.Result] ||
		!userPattern.MatchString(e.User) || !correlationPattern.MatchString(e.CorrelationID) {
		return ErrInvalidEvent
	}
	return nil
}

type aggKey struct {
	action Action
	target Target
	result Result
}

type aggregate struct {
	first entry
	count int
}

// Log is the audit file writer.
type Log struct {
	dir   string
	clock clock.Clock
	warn  func(msg string)

	mu         sync.Mutex
	buf        bytes.Buffer
	flushTimer clock.Timer
	aggs       map[aggKey]*aggregate
	aggOrder   []aggKey
	aggTimer   clock.Timer
	closed     bool
	failures   int
	dropped    int

	writeMu sync.Mutex // serialises file I/O; never held together with mu
}

// Open prepares the audit directory (0700) and checks the existing files
// (0600, owned by the service user). warn receives plain messages for the
// journal; it is never given entry data.
func Open(stateDir string, c clock.Clock, warn func(msg string)) (*Log, error) {
	dir := filepath.Join(stateDir, DirName)
	if err := securefile.EnsureDir(dir); err != nil {
		return nil, err
	}
	l := &Log{dir: dir, clock: c, warn: warn, aggs: map[aggKey]*aggregate{}}
	for i := 0; i < Files; i++ {
		if _, err := securefile.CheckFile(l.file(i)); err != nil {
			return nil, err
		}
	}
	if l.clock == nil {
		l.clock = clock.System()
	}
	if l.warn == nil {
		l.warn = func(string) {}
	}
	return l, nil
}

// file is audit.jsonl for 0 and audit.jsonl.<i> for the rotated files.
func (l *Log) file(i int) string {
	if i == 0 {
		return filepath.Join(l.dir, FileName)
	}
	return filepath.Join(l.dir, FileName+"."+strconv.Itoa(i))
}

// Record adds an event. It only appends to memory and never waits for disk.
func (l *Log) Record(e Event) {
	if e.validate() != nil {
		l.mu.Lock()
		l.dropped++
		l.mu.Unlock()
		l.warn("audit: an event with invalid fields was dropped")
		return
	}
	now := l.clock.Now().UTC()
	en := entry{Time: now.Format(time.RFC3339), User: e.User, Action: e.Action, Target: e.Target,
		Result: e.Result, CorrelationID: e.CorrelationID, Count: 1}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	if aggregated[e.Action] {
		l.aggregateLocked(en)
		return
	}
	l.appendLocked(en)
}

// aggregateLocked folds an event into the open window of its kind. The first
// event of a window opens it; the window is written as one entry with a count
// when it closes, AggregateWindow later. So each kind gets at most one entry
// per minute, however many attempts there are.
func (l *Log) aggregateLocked(en entry) {
	k := aggKey{en.Action, en.Target, en.Result}
	if a, ok := l.aggs[k]; ok {
		a.count++
		return
	}
	l.aggs[k] = &aggregate{first: en, count: 1}
	l.aggOrder = append(l.aggOrder, k)
	if l.aggTimer == nil {
		l.aggTimer = l.clock.AfterFunc(AggregateWindow, l.closeWindow)
	}
}

func (l *Log) closeWindow() {
	l.mu.Lock()
	l.aggTimer = nil
	l.emitAggregatesLocked()
	l.mu.Unlock()
}

func (l *Log) emitAggregatesLocked() {
	for _, k := range l.aggOrder {
		a := l.aggs[k]
		a.first.Count = a.count
		l.appendLocked(a.first)
	}
	l.aggs = map[aggKey]*aggregate{}
	l.aggOrder = nil
}

func (l *Log) appendLocked(en entry) {
	line, err := json.Marshal(en)
	if err != nil {
		l.dropped++
		return
	}
	if l.buf.Len()+len(line)+1 > maxBuffered {
		// Only reachable when the disk has failed for a long time; memory stays bounded.
		l.dropped++
		return
	}
	l.buf.Write(line)
	l.buf.WriteByte('\n')
	if l.flushTimer == nil && !l.closed {
		l.flushTimer = l.clock.AfterFunc(FlushDelay, func() { l.Flush() })
	}
}

// Flush writes the buffer to the file, rotating first when the file would
// pass MaxFileSize. Aggregation windows that are still open stay open.
func (l *Log) Flush() {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	l.mu.Lock()
	if l.flushTimer != nil {
		l.flushTimer.Stop()
		l.flushTimer = nil
	}
	data := bytes.Clone(l.buf.Bytes())
	l.buf.Reset()
	l.mu.Unlock()
	if len(data) == 0 {
		return
	}
	if err := l.write(data); err != nil {
		l.mu.Lock()
		l.failures++
		n := l.failures
		l.mu.Unlock()
		l.warn(fmt.Sprintf("audit: writing the audit file failed (%d failed writes since start)", n))
	}
}

// write appends whole lines, rotating before a line that would not fit.
func (l *Log) write(data []byte) error {
	size, err := fileSize(l.file(0))
	if err != nil {
		return err
	}
	var f *os.File
	defer func() {
		if f != nil {
			_ = f.Close()
		}
	}()
	for len(data) > 0 {
		n := bytes.IndexByte(data, '\n') + 1
		line := data[:n]
		data = data[n:]
		if size > 0 && size+int64(len(line)) > MaxFileSize {
			if f != nil {
				if err := f.Close(); err != nil {
					return err
				}
				f = nil
			}
			if err := l.rotate(); err != nil {
				return err
			}
			size = 0
		}
		if f == nil {
			if f, err = os.OpenFile(l.file(0), os.O_WRONLY|os.O_APPEND|os.O_CREATE, securefile.FileMode); err != nil {
				return err
			}
		}
		if _, err := f.Write(line); err != nil {
			return err
		}
		size += int64(len(line))
	}
	if f == nil {
		return nil
	}
	err = f.Close()
	f = nil
	return err
}

// rotate shifts audit.jsonl to .1, .1 to .2 and .2 to .3; the old .3 is gone.
func (l *Log) rotate() error {
	for i := Files - 1; i > 0; i-- {
		if err := os.Rename(l.file(i-1), l.file(i)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return securefile.SyncDir(l.dir)
}

func fileSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// Close ends the open aggregation windows, writes everything and stops all
// timers. Events recorded afterwards are ignored.
func (l *Log) Close() {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	if l.aggTimer != nil {
		l.aggTimer.Stop()
		l.aggTimer = nil
	}
	l.emitAggregatesLocked()
	l.closed = true
	l.mu.Unlock()
	l.Flush()
}

// Stats are the counters of dropped events and failed writes.
type Stats struct{ Failures, Dropped int }

// Stats returns the counters.
func (l *Log) Stats() Stats {
	l.mu.Lock()
	defer l.mu.Unlock()
	return Stats{Failures: l.failures, Dropped: l.dropped}
}
