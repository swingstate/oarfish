package services

import (
	"context"
	"io/fs"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/swingstate/oarfish/backend/internal/clock"
	"github.com/swingstate/oarfish/backend/internal/unitname"
)

// cacheFor is the device-wide cache of the unit list (B2): a second read
// within 5 s gets the same list-units result and starts no process.
const cacheFor = 5 * time.Second

// The unit-file states (ADR 0011 A9): list-unit-files is computed by PID 1,
// which answers nothing else meanwhile (2.6 to 2.7 s on Raspberry Pi OS,
// 17.0 s on Ubuntu 24.04, docs/UBUNTU_PROBE.md U1). So the states have their
// own store and their own single flight, are read only when a list request
// finds them missing, older than filesMaxAge or invalidated, never sooner than
// filesFloor after the previous read ended (also after a failure), and a list
// request waits at most filesGrace for them.
const (
	filesMaxAge = 30 * time.Minute
	filesFloor  = 60 * time.Second
	filesGrace  = 4 * time.Second
)

// The status of the start modes of a List (closed set).
const (
	// StartModesKnown: the start modes come from a successful unit-file read
	// (StartModesReadAt), possibly an older one while a renewal runs or after
	// a failed renewal.
	StartModesKnown = "known"
	// StartModesPending: no states are known yet and a read runs; every start
	// mode is "" and the units that only list-unit-files names are missing.
	StartModesPending = "pending"
	// StartModesUnavailable: no states are known and the last read failed;
	// the next one starts at a list request after filesFloor.
	StartModesUnavailable = "unavailable"
)

// Code is the typed error of the read path.
type Code string

const (
	// CodeUnavailable: no systemd, or no systemctl, or it cannot be started.
	CodeUnavailable Code = "services_unavailable"
	// CodeReadFailed: systemctl ran but the read failed, exceeded a bound or
	// printed something this version does not accept.
	CodeReadFailed Code = "services_read_failed"
)

// Error is a typed error of the read path. It carries no text of the
// operating system and no output of systemctl.
type Error struct{ Code Code }

// Row is one service of the list. Every word is from a closed set; the
// description is cleaned plain text.
type Row struct {
	Name        string
	Description string
	// LoadState is systemd's word, or NotLoaded for a unit that exists only as
	// a unit file.
	LoadState   string
	ActiveState string
	SubState    string
	// StartMode is the unit-file state, or "" when the files do not name it
	// or no unit-file states are known.
	StartMode   string
	Denied      bool
	RestartOnly bool
	// Critical is a kind of rule 11, or "".
	Critical string
	// Actions are the offered actions, a subset of start, stop, restart. They
	// never depend on the start mode.
	Actions []string
}

// NotLoaded is the load state of a row that only list-unit-files names: the
// unit is not loaded now (a word of this service, not of systemd).
const NotLoaded = "not-loaded"

// Counts are the numbers shown above the list.
type Counts struct {
	Total, Active, Failed, Inactive int
}

// List is one answer: rows sorted by name. ReadAt is the time of the
// list-units read; StartModes and StartModesReadAt say where the start modes
// come from (StartModesReadAt is zero unless StartModes is known).
type List struct {
	ReadAt           time.Time
	Rows             []Row
	Counts           Counts
	StartModes       string
	StartModesReadAt time.Time
}

// unitsResult is one list-units read, shared between callers and never
// changed after it was made.
type unitsResult struct {
	lines  []unitLine
	readAt time.Time
	err    *Error
}

type flight struct {
	done chan struct{}
	res  unitsResult
}

type filesFlight struct {
	done chan struct{}
}

// reader is the read path R1: list-units at most once at a time (a second
// caller waits for the running one), its result kept for 5 s; the unit-file
// states in their own store (A9). No timer and no work without a request.
type reader struct {
	clock      clock.Clock
	run        runner
	systemctl  string
	systemdDir string
	stat       func(string) (fs.FileInfo, error)
	// filesDeadline is filesCallDeadline; tests shorten it.
	filesDeadline time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.Mutex
	cached   *unitsResult
	cachedAt time.Time
	inflight *flight
	gen      int
	// lastUnits is the last successful list-units read. It answers while a
	// unit-file read runs, because PID 1 would hold a list-units call behind
	// it (A9); invalidate does not drop it.
	lastUnits *unitsResult

	files       map[string]string // nil: no states known
	filesAt     time.Time         // when the known states were read
	filesStale  bool              // invalidated since filesAt
	filesFailed bool              // the last unit-file read failed
	filesEnded  time.Time         // when the last unit-file read ended; zero: none yet
	filesRun    *filesFlight
	filesGen    int

	reads int // processes started by reads, for the tests
}

func newReader(c clock.Clock) *reader {
	ctx, cancel := context.WithCancel(context.Background())
	return &reader{clock: c, run: runProgram, systemctl: SystemctlPath, systemdDir: systemdDir, stat: os.Stat,
		filesDeadline: filesCallDeadline, ctx: ctx, cancel: cancel}
}

// read answers one list request: the unit list (cached, single flight), joined
// with the unit-file states that are known. A failed list-units fails the
// request; a missing, slow or failed unit-file read does not (A9). The reads do
// not belong to the request: a caller that leaves does not stop them, and they
// end at their own deadlines.
func (r *reader) read(ctx context.Context) (List, *Error) {
	units, e := r.readUnits(ctx)
	if e != nil {
		return List{}, e
	}
	files, at, status := r.fileStates(ctx)
	rows, ok := join(units.lines, files)
	if !ok {
		return List{}, &Error{Code: CodeReadFailed}
	}
	l := List{ReadAt: units.readAt, Rows: rows, Counts: count(rows), StartModes: status}
	if status == StartModesKnown {
		l.StartModesReadAt = at
	}
	return l, nil
}

// readUnits returns the cached list-units result, the last successful one
// while a unit-file read runs, or joins or starts the one list-units read.
func (r *reader) readUnits(ctx context.Context) (unitsResult, *Error) {
	r.mu.Lock()
	if r.cached != nil && r.clock.Now().Sub(r.cachedAt) < cacheFor {
		res := *r.cached
		r.mu.Unlock()
		return res, res.err
	}
	if r.filesRun != nil && r.lastUnits != nil {
		res := *r.lastUnits
		r.mu.Unlock()
		return res, nil
	}
	f := r.inflight
	if f == nil {
		f = &flight{done: make(chan struct{})}
		r.inflight = f
		gen := r.gen
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			res := r.readUnitsOnce()
			r.mu.Lock()
			f.res = res
			if r.gen == gen {
				r.cached, r.cachedAt = &res, r.clock.Now()
			}
			if res.err == nil {
				r.lastUnits = &res
			}
			r.inflight = nil
			r.mu.Unlock()
			close(f.done)
		}()
	}
	r.mu.Unlock()
	select {
	case <-f.done:
		return f.res, f.res.err
	case <-ctx.Done():
		return unitsResult{}, &Error{Code: CodeReadFailed}
	}
}

// fileStates returns the known unit-file states and their status. It starts a
// unit-file read when one is due and allowed, and waits for it at most
// filesGrace when no states are known yet. A known state stays in use while a
// renewal runs.
func (r *reader) fileStates(ctx context.Context) (map[string]string, time.Time, string) {
	r.mu.Lock()
	r.startFilesLocked()
	f := r.filesRun
	if r.files != nil || f == nil {
		defer r.mu.Unlock()
		return r.files, r.filesAt, r.filesStatusLocked()
	}
	r.mu.Unlock()
	grace := make(chan struct{})
	t := r.clock.AfterFunc(filesGrace, func() { close(grace) })
	select {
	case <-f.done:
	case <-grace:
	case <-ctx.Done():
	}
	t.Stop()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.files, r.filesAt, r.filesStatusLocked()
}

func (r *reader) filesStatusLocked() string {
	switch {
	case r.files != nil:
		return StartModesKnown
	case r.filesRun != nil:
		return StartModesPending
	}
	return StartModesUnavailable
}

// startFilesLocked starts the one unit-file read when (1) no states are known,
// (2) they are older than filesMaxAge or (3) they were invalidated, and no
// read runs, and the previous read ended at least filesFloor ago. The clock is
// the system clock in production, whose readings are monotonic.
func (r *reader) startFilesLocked() {
	if r.filesRun != nil {
		return
	}
	now := r.clock.Now()
	if r.files != nil && !r.filesStale && now.Sub(r.filesAt) < filesMaxAge {
		return
	}
	if !r.filesEnded.IsZero() && now.Sub(r.filesEnded) < filesFloor {
		return
	}
	f := &filesFlight{done: make(chan struct{})}
	r.filesRun = f
	gen := r.filesGen
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		states, ok := r.readFilesOnce()
		r.mu.Lock()
		r.filesEnded = r.clock.Now()
		r.filesFailed = !ok
		if ok {
			r.files, r.filesAt = states, r.filesEnded
			// Invalidated while it ran: the states may predate the change,
			// so the next request after the floor reads again.
			r.filesStale = r.filesGen != gen
		}
		r.filesRun = nil
		r.mu.Unlock()
		close(f.done)
	}()
}

// invalidate drops the cache of the unit list; a read that is running keeps
// its result for its callers but does not fill the cache. The unit-file
// states stay: start, stop and restart do not change them.
func (r *reader) invalidate() {
	r.mu.Lock()
	r.cached = nil
	r.gen++
	r.mu.Unlock()
}

// invalidateFiles marks the unit-file states as out of date (for a later step
// that changes unit files, for example a package installation). They stay in
// use until a list request renews them, at the earliest filesFloor after the
// previous read ended.
func (r *reader) invalidateFiles() {
	r.mu.Lock()
	r.filesStale = true
	r.filesGen++
	r.mu.Unlock()
}

func (r *reader) close() {
	r.cancel()
	r.wg.Wait()
}

func (r *reader) available() bool {
	if fi, err := r.stat(r.systemdDir); err != nil || !fi.IsDir() {
		return false
	}
	fi, err := r.stat(r.systemctl)
	return err == nil && !fi.IsDir()
}

func (r *reader) readUnitsOnce() unitsResult {
	fail := func(c Code) unitsResult { return unitsResult{err: &Error{Code: c}} }
	if !r.available() {
		return fail(CodeUnavailable)
	}
	units, c := r.call(shapeListUnits, callDeadline)
	if c != "" {
		return fail(c)
	}
	ul, err := parseListUnits(units)
	if err != nil {
		return fail(CodeReadFailed)
	}
	return unitsResult{lines: ul, readAt: r.clock.Now().UTC()}
}

// readFilesOnce reads and parses the unit-file states. Any failure (no
// systemctl, a non-zero exit, the deadline, output over a bound or not
// accepted) is one answer: not ok, and nothing of the read is kept.
func (r *reader) readFilesOnce() (map[string]string, bool) {
	if !r.available() {
		return nil, false
	}
	out, c := r.call(shapeListUnitFiles, r.filesDeadline)
	if c != "" {
		return nil, false
	}
	uf, err := parseListUnitFiles(out)
	if err != nil {
		return nil, false
	}
	return uf, true
}

func (r *reader) call(args []string, deadline time.Duration) ([]byte, Code) {
	r.mu.Lock()
	r.reads++
	r.mu.Unlock()
	res, err := r.run(r.ctx, r.systemctl, deadline, args)
	switch {
	case err != nil:
		return nil, CodeUnavailable
	case res.TimedOut || res.Overflow || res.Exit != 0:
		return nil, CodeReadFailed
	}
	return res.Out, ""
}

// join builds the rows (A3.2): the names of list-units without the not-found
// entries, with the start mode from list-unit-files (an instance takes the
// state of its template when the files do not name it), and the names that
// only list-unit-files has, as inactive and not loaded. Aliases and templates
// are not rows. More than maxUnits rows fail.
func join(units []unitLine, files map[string]string) ([]Row, bool) {
	rows := make([]Row, 0, len(units))
	inUnits := map[string]bool{}
	for _, u := range units {
		inUnits[u.name] = true
		if u.load == "not-found" || unitname.IsTemplate(u.name) {
			continue
		}
		mode, ok := files[u.name]
		if !ok {
			if tpl, isInstance := unitname.Template(u.name); isInstance {
				mode = files[tpl]
			}
		}
		if mode == "alias" {
			mode = ""
		}
		rows = append(rows, row(u.name, u.description, u.load, u.active, u.sub, mode))
	}
	for name, state := range files {
		if inUnits[name] || state == "alias" || unitname.IsTemplate(name) {
			continue
		}
		rows = append(rows, row(name, "", NotLoaded, "inactive", "dead", state))
	}
	if len(rows) > maxUnits {
		return nil, false
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows, true
}

func row(name, desc, load, active, sub, mode string) Row {
	c := classify(name)
	return Row{Name: name, Description: desc, LoadState: load, ActiveState: active, SubState: sub, StartMode: mode,
		Denied: c.denied, RestartOnly: c.restartOnly, Critical: c.critical, Actions: offeredActions(c, active)}
}

func count(rows []Row) Counts {
	c := Counts{Total: len(rows)}
	for _, r := range rows {
		switch r.ActiveState {
		case "active":
			c.Active++
		case "failed":
			c.Failed++
		case "inactive":
			c.Inactive++
		}
	}
	return c
}
