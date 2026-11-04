package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/swingstate/oarfish/backend/internal/trustkey"
)

const (
	// stageDeadline is the deadline of one stage request (U3), measured on a
	// monotonic clock.
	stageDeadline = 180 * time.Second
	// spaceReserve is the reserve that stays free after a stage (U5, the
	// installer's MIN_FREE_KB).
	spaceReserve = 64 << 20

	stateDirPath = "/var/lib/oarfish-update"
	cacheDirPath = "/var/cache/oarfish"
	// chunk is the buffer size of every copy and hash (U12).
	chunk = 32 << 10
)

// handler answers the requests. Its fields are set once before serving and are
// read only afterwards, so it is safe for concurrent connections. Everything
// the tests replace is a field; productionHandler leaves the seams nil.
type handler struct {
	helperVersion string
	keyID         string
	key           trustkey.Key
	root          string // /opt/oarfish
	stateDir      string // /var/lib/oarfish-update
	cacheDir      string // /var/cache/oarfish
	arch          string
	maxResponse   int
	deadline      time.Duration

	uptime    func() (int64, error)          // CLOCK_BOOTTIME seconds
	monotonic func() time.Duration           // deadline clock
	wall      func() time.Time               // display only
	newID     func() (string, error)         // 16 random hexadecimal digits
	freeBytes func(*os.File) (uint64, error) // space of the file system of an open directory
	journal   io.Writer

	// Test seams. They are nil in production.
	wrapFS  func(fsops) fsops
	onChunk func(file string, copied int64) // between two chunks of a copy
}

func newHandler(o options, key trustkey.Key) *handler {
	h := &handler{
		helperVersion: o.helperVersion, keyID: key.ID(), key: key,
		root: o.root, stateDir: o.stateDir, cacheDir: o.cacheDir, arch: o.arch,
		maxResponse: o.maxResponse, deadline: stageDeadline,
		uptime: bootSeconds, wall: time.Now, newID: randomID, freeBytes: fstatfsFree, journal: o.journal,
	}
	start := time.Now()
	h.monotonic = func() time.Duration { return time.Since(start) }
	return h
}

// randomID returns 16 random lowercase hexadecimal digits.
func randomID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func fstatfsFree(f *os.File) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Fstatfs(int(f.Fd()), &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}

// log writes the journal line of a phase change (U8): exactly
// phase=<name> to=<version> from=<version> cid=<id> result=<code>. Every value
// is a validated version, id or fixed code, so the line holds no path and no
// operating-system error string.
func (h *handler) log(phase, to, from, cid, result string) {
	if h.journal == nil {
		return
	}
	fmt.Fprintf(h.journal, "phase=%s to=%s from=%s cid=%s result=%s\n", phase, to, from, cid, result)
}

// env is what one request works with: read access to both trees, the mutating
// seam, and the handler.
type env struct {
	h     *handler
	opt   *os.Root
	state *os.Root // nil when the state directory does not exist
	fs    fsops
}

// open opens the trees. needState makes a missing state directory an error;
// status works without it.
func (h *handler) open(needState bool) (*env, errorCode) {
	opt, err := os.OpenRoot(h.root)
	if err != nil {
		return nil, codeIOError
	}
	st, err := os.OpenRoot(h.stateDir)
	if err != nil {
		if needState {
			opt.Close()
			return nil, codeIOError
		}
		st = nil
	}
	roots := map[tree]*os.Root{treeOpt: opt}
	if st != nil {
		roots[treeState] = st
	}
	var f fsops = realFS{roots: roots}
	if h.wrapFS != nil {
		f = h.wrapFS(f)
	}
	return &env{h: h, opt: opt, state: st, fs: f}, ""
}

func (e *env) close() {
	e.opt.Close()
	if e.state != nil {
		e.state.Close()
	}
}

// loadState reads state.json; a missing state directory is a fresh state.
func (e *env) loadState() (*state, error) {
	if e.state == nil {
		return freshState(e.h.helperVersion), nil
	}
	return loadState(e.state, e.h.helperVersion)
}

// lockState takes the shared lock without waiting.
func (h *handler) lockState() (*dirLock, errorCode) {
	l, err := lockDir(h.stateDir)
	switch {
	case err == nil:
		return l, ""
	case err == errLockBusy:
		return nil, codeBusy
	}
	return nil, codeIOError
}

// respond answers one request line.
func (h *handler) respond(line []byte) []byte {
	req, err := decodeRequest(line)
	if err != nil {
		return errorBody(codeBadRequest)
	}
	var out []byte
	switch req.Op {
	case opStatus:
		out = h.status()
	case opStage:
		out = h.stage(req)
	case opDiscard:
		out = h.discard(req)
	default:
		return errorBody(codeBadRequest)
	}
	if len(out) > h.maxResponse {
		return errorBody(codeInternal)
	}
	return out
}

// status answers `status`. When a staged release has expired and the lock can
// be taken without waiting, the expiry cleanup runs first (U5, U7 row 4). A
// status request that has no cleanup work never touches the lock.
func (h *handler) status() []byte {
	free, err := freeBytes(h.root)
	if err != nil {
		return errorBody(codeIOError)
	}
	e, code := h.open(false)
	if code != "" {
		return errorBody(code)
	}
	defer e.close()
	st, needs := h.statusState(e)
	resp := statusResponse{
		V: protocolMax, OK: true, ProtocolMin: protocolMin, ProtocolMax: protocolMax,
		HelperVersion: h.helperVersion, KeyID: h.keyID, InstalledVersion: installedVersion(h.root),
		Phase: st.Txn.Phase, PreviousVersion: st.Previous, LastResult: toJSONLast(st.Last),
		NeedsAttention: needs || st.Txn.Phase == "rollback_failed", FreeBytes: free,
	}
	if st.Txn.Phase == phaseStaged {
		resp.StagedVersion = st.Txn.To
	}
	return encode(resp)
}

// statusState reads the state. A file that is not valid is reported as idle
// with needs_attention; nothing is repaired (G6.6).
func (h *handler) statusState(e *env) (*state, bool) {
	st, err := e.loadState()
	if err != nil {
		return freshState(h.helperVersion), true
	}
	if st.Txn.Phase != phaseStaged || e.state == nil {
		return st, false
	}
	now, err := h.uptime()
	if err != nil || !stageExpired(st.Txn.StagedAtUptimeS, now) {
		return st, false
	}
	installed := installedVersion(h.root)
	if installed == "" {
		return st, false
	}
	lk, err := lockDir(h.stateDir)
	if err != nil {
		return st, false // busy or unavailable: the answer shows the staged release
	}
	defer lk.release()
	// Re-read under the lock: another request may have changed it.
	cur, err := e.loadState()
	if err != nil {
		return freshState(h.helperVersion), true
	}
	if cur.Txn.Phase == phaseStaged && stageExpired(cur.Txn.StagedAtUptimeS, now) {
		if e.expireStaged(cur, installed) == "" {
			if again, err := e.loadState(); err == nil {
				return again, false
			}
		}
	}
	return cur, false
}
