package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The harness of the activation tests: a fake systemctl that knows which
// release `current` names, a fake health client, a fake clock and a fault gate
// that numbers every mutating call (file system and systemctl) so that a test
// can fail or stop at call k.

type behavior int

const (
	bOK           behavior = iota // starts and answers /healthz with its version
	bCrash                        // the unit goes to failed at once
	bHang                         // runs, but /healthz does not answer 200
	bWrongVersion                 // /healthz reports another version
	bNoVersion                    // /healthz has no version member
	bLoop                         // runs, but NRestarts rises after the start
	bNeverUp                      // stays activating
)

// faultGate numbers the calls of a run. failAt makes call k fail and do
// nothing; stopAt makes call k and every later one fail (power loss).
type faultGate struct {
	mu      sync.Mutex
	n       int
	failAt  int
	stopAt  int
	stopped bool
}

func (g *faultGate) step() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	switch {
	case g.stopped, g.stopAt > 0 && g.n >= g.stopAt:
		g.stopped = true
		return errStopped
	case g.failAt > 0 && g.n == g.failAt:
		return errInjected
	}
	return nil
}

func (g *faultGate) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.n
}

type fakeSystemd struct {
	v  *tenv
	mu sync.Mutex

	calls   [][]string
	pid     int64
	nrest   int64
	active  string
	sub     string
	running string
	beh     map[string]behavior
	gate    *faultGate
	listen  string // value of -listen in ExecStart; "" means no argument

	onRestart   func()
	restartHook chan struct{} // when set, restart blocks until it is closed
	restartFail map[int]bool  // the n-th restart call fails
	restarts    int
	showFail    bool
	healthCalls []string
}

func (v *tenv) fakeSystemd() *fakeSystemd {
	f := &fakeSystemd{v: v, pid: 100, active: "active", sub: "running", running: installedVersionString,
		beh: map[string]behavior{}, restartFail: map[int]bool{}}
	v.sd = f
	v.h.sysrun = f.run
	v.h.healthGet = f.health
	v.h.sleep = func(d time.Duration) {
		v.mono.Add(int64(d))
		v.uptime.Add(int64(d / time.Second))
	}
	v.h.syncTxn = true
	return f
}

func (f *fakeSystemd) behaviorOf(version string) behavior { return f.beh[version] }

func (f *fakeSystemd) run(args []string) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, append([]string(nil), args...))
	f.mu.Unlock()
	switch args[0] {
	case "show":
		if f.showFail {
			return nil, errSystemctl
		}
		return f.show(), nil
	case "restart":
		if f.gate != nil {
			if err := f.gate.step(); err != nil {
				return nil, err
			}
		}
		if f.onRestart != nil {
			f.onRestart()
		}
		if f.restartHook != nil {
			<-f.restartHook
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.restarts++
		if f.restartFail[f.restarts] {
			return nil, errSystemctl
		}
		target, _ := os.Readlink(filepath.Join(f.v.root, "current"))
		f.running = strings.TrimPrefix(target, "releases/")
		f.pid++
		switch f.beh[f.running] {
		case bCrash:
			f.active, f.sub = "failed", "failed"
		case bNeverUp:
			f.active, f.sub = "activating", "start"
		default:
			f.active, f.sub = "active", "running"
		}
		return nil, nil
	case "reset-failed":
		if f.gate != nil {
			if err := f.gate.step(); err != nil {
				return nil, err
			}
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.active == "failed" {
			f.active, f.sub = "inactive", "dead"
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected %v", args)
}

func (f *fakeSystemd) show() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.beh[f.running] == bLoop && f.active == "active" && f.nrest == 0 {
		f.nrest = 1 // a start-then-crash loop: the restart counter rises
	}
	argv := "/opt/oarfish/current/oarfish"
	if f.listen != "" {
		argv += " -listen " + f.listen
	}
	return []byte(fmt.Sprintf("ActiveState=%s\nSubState=%s\nMainPID=%d\nNRestarts=%d\nExecStart={ path=/opt/oarfish/current/oarfish ; argv[]=%s ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }\n",
		f.active, f.sub, f.pid, f.nrest, argv))
}

func (f *fakeSystemd) health(addr, path string) (int, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.healthCalls = append(f.healthCalls, addr+path)
	switch f.beh[f.running] {
	case bHang, bNeverUp, bCrash:
		return 503, []byte(`{"status":"starting"}`), nil
	case bWrongVersion:
		return 200, []byte(`{"status":"ok","version":"0.0.1"}`), nil
	case bNoVersion:
		return 200, []byte(`{"status":"ok"}`), nil
	}
	return 200, []byte(fmt.Sprintf(`{"status":"ok","version":%q}`, f.running)), nil
}

func (f *fakeSystemd) Calls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string(nil), f.calls...)
}

// count returns how many calls had the verb.
func (f *fakeSystemd) count(verb string) int {
	n := 0
	for _, c := range f.Calls() {
		if c[0] == verb {
			n++
		}
	}
	return n
}

// ---- helpers on tenv --------------------------------------------------------

const (
	oarfishFile = "oarfish"
)

// installOarfish gives the installed release its binary (newTenv builds the
// release directory with the unit text only).
func (v *tenv) installOarfish() {
	v.t.Helper()
	if err := os.WriteFile(filepath.Join(v.root, "releases", v.installed, oarfishFile), elfStub("oarfish "+v.installed), 0o755); err != nil {
		v.t.Fatal(err)
	}
}

// stagedRelease stages a version (the stage request must succeed).
func (v *tenv) stagedRelease(version string) {
	v.t.Helper()
	v.stageable(version)
	wantOK(v.t, v.stage(version))
}

func activateLine(version, cid string, confirmed bool) []byte {
	return []byte(fmt.Sprintf(`{"v":1,"op":"activate","version":%q,"correlation_id":%q,"components_confirmed":%v}`+"\n", version, cid, confirmed))
}

func ackLine(id string) []byte {
	return []byte(fmt.Sprintf(`{"v":1,"op":"ack","transaction_id":%q}`+"\n", id))
}

func (v *tenv) activate(version string) []byte {
	return v.request(activateLine(version, testCID, false))
}

// current is the version `current` names ("" if unreadable).
func (v *tenv) current() string { return installedVersion(v.root) }

// lastResult reads the last result from the state file.
func (v *tenv) lastResult() *lastResult {
	st, ok := v.readState()
	if !ok {
		return nil
	}
	return st.Last
}

// setup returns a tenv with the installed binary, the fakes and a staged 1.1.0.
func newActiveEnv(t *testing.T) *tenv {
	t.Helper()
	v := newTenv(t)
	v.installOarfish()
	v.fakeSystemd()
	v.stagedRelease("1.1.0")
	return v
}

// releaseComplete: the directory holds the binary and the unit text.
func releaseComplete(root, version string) bool {
	for _, n := range []string{"oarfish", "oarfish.service"} {
		if fi, err := os.Stat(filepath.Join(root, "releases", version, n)); err != nil || !fi.Mode().IsRegular() {
			return false
		}
	}
	return true
}
