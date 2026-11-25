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

// cacheFor is the device-wide cache of the list (B2): a second read within 5 s
// gets the same result and starts no process.
const cacheFor = 5 * time.Second

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
	// StartMode is the unit-file state, or "" when the files do not name it.
	StartMode   string
	Denied      bool
	RestartOnly bool
	// Critical is a kind of rule 11, or "".
	Critical string
	// Actions are the offered actions, a subset of start, stop, restart.
	Actions []string
}

// NotLoaded is the load state of a row that only list-unit-files names: the
// unit is not loaded now (a word of this service, not of systemd).
const NotLoaded = "not-loaded"

// Counts are the numbers shown above the list.
type Counts struct {
	Total, Active, Failed, Inactive int
}

// List is one read: rows sorted by name. It is shared between callers and
// never changed after it was made.
type List struct {
	ReadAt time.Time
	Rows   []Row
	Counts Counts
}

type result struct {
	list List
	err  *Error
}

type flight struct {
	done chan struct{}
	res  result
}

// reader is the read path R1: at most one read at a time (a second caller
// waits for the running one), the result kept for 5 s, no background work.
type reader struct {
	clock      clock.Clock
	run        runner
	systemctl  string
	systemdDir string
	stat       func(string) (fs.FileInfo, error)

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.Mutex
	cached   *result
	cachedAt time.Time
	inflight *flight
	gen      int
	reads    int // processes started by reads, for the tests
}

func newReader(c clock.Clock) *reader {
	ctx, cancel := context.WithCancel(context.Background())
	return &reader{clock: c, run: runProgram, systemctl: SystemctlPath, systemdDir: systemdDir, stat: os.Stat, ctx: ctx, cancel: cancel}
}

// read returns the cached list or joins or starts the one read. The read does
// not belong to the request: a caller that leaves does not stop it, and it
// ends at its own deadlines (5 s per call).
func (r *reader) read(ctx context.Context) (List, *Error) {
	r.mu.Lock()
	if r.cached != nil && r.clock.Now().Sub(r.cachedAt) < cacheFor {
		res := *r.cached
		r.mu.Unlock()
		return res.list, res.err
	}
	f := r.inflight
	if f == nil {
		f = &flight{done: make(chan struct{})}
		r.inflight = f
		gen := r.gen
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			res := r.readOnce()
			r.mu.Lock()
			f.res = res
			if r.gen == gen {
				r.cached, r.cachedAt = &res, r.clock.Now()
			}
			r.inflight = nil
			r.mu.Unlock()
			close(f.done)
		}()
	}
	r.mu.Unlock()
	select {
	case <-f.done:
		return f.res.list, f.res.err
	case <-ctx.Done():
		return List{}, &Error{Code: CodeReadFailed}
	}
}

// invalidate drops the cache; a read that is running keeps its result for its
// callers but does not fill the cache.
func (r *reader) invalidate() {
	r.mu.Lock()
	r.cached = nil
	r.gen++
	r.mu.Unlock()
}

func (r *reader) close() {
	r.cancel()
	r.wg.Wait()
}

func (r *reader) readOnce() result {
	fail := func(c Code) result { return result{err: &Error{Code: c}} }
	if fi, err := r.stat(r.systemdDir); err != nil || !fi.IsDir() {
		return fail(CodeUnavailable)
	}
	if fi, err := r.stat(r.systemctl); err != nil || fi.IsDir() {
		return fail(CodeUnavailable)
	}
	units, c := r.call(shapeListUnits)
	if c != "" {
		return fail(c)
	}
	files, c := r.call(shapeListUnitFiles)
	if c != "" {
		return fail(c)
	}
	ul, err := parseListUnits(units)
	if err != nil {
		return fail(CodeReadFailed)
	}
	uf, err := parseListUnitFiles(files)
	if err != nil {
		return fail(CodeReadFailed)
	}
	rows, ok := join(ul, uf)
	if !ok {
		return fail(CodeReadFailed)
	}
	return result{list: List{ReadAt: r.clock.Now().UTC(), Rows: rows, Counts: count(rows)}}
}

func (r *reader) call(args []string) ([]byte, Code) {
	r.mu.Lock()
	r.reads++
	r.mu.Unlock()
	res, err := r.run(r.ctx, r.systemctl, callDeadline, args)
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
