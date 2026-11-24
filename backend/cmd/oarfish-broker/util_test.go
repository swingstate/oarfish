package main

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Throwaway values only. No fixture holds a value of a real device.
const (
	testUID = 4100
	testCID = "0123456789abcdef"
)

// ---- show fixtures -----------------------------------------------------------

// fixtureOrder is the order of the show fixtures: systemctl prints the
// service properties before the unit properties, not in the order of
// --property= (ADR 0011 A2 S7).
var fixtureOrder = []string{
	"Type", "Result", "MainPID", "ExecMainStartTimestampMonotonic",
	"Id", "Names", "LoadState", "ActiveState", "SubState", "UnitFileState",
	"RefuseManualStart", "RefuseManualStop", "Transient",
}

// showOut returns a show output for name with the given overrides. A value
// "<omit>" leaves the key out.
func showOut(name string, kv ...string) string {
	v := map[string]string{
		"Type": "exec", "Result": "success", "MainPID": "4242", "ExecMainStartTimestampMonotonic": "1000000",
		"Id": name, "Names": name, "LoadState": "loaded", "ActiveState": "active", "SubState": "running",
		"UnitFileState": "enabled", "RefuseManualStart": "no", "RefuseManualStop": "no", "Transient": "no",
	}
	for i := 0; i+1 < len(kv); i += 2 {
		v[kv[i]] = kv[i+1]
	}
	var b strings.Builder
	for _, k := range fixtureOrder {
		if v[k] == "<omit>" {
			continue
		}
		b.WriteString(k + "=" + v[k] + "\n")
	}
	return b.String()
}

// notFoundOut is what show answers, with exit code 0, for a name systemd does
// not know (A2 S7).
func notFoundOut(name string) string {
	return showOut(name, "Type", "simple", "MainPID", "0", "ExecMainStartTimestampMonotonic", "0",
		"LoadState", "not-found", "ActiveState", "inactive", "SubState", "dead", "UnitFileState", "")
}

// ---- the Go-level fake systemctl ---------------------------------------------

// fakeSys stands in for runProgram. It records every argument list and
// answers show from a queue (the last entry repeats) and every job with job.
type fakeSys struct {
	mu    sync.Mutex
	calls [][]string
	times []time.Time
	shows []runResult
	job   runResult
	// during runs while a job (not show) call is in progress.
	during func()
}

func (f *fakeSys) run(path string, _ time.Duration, args []string) (runResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, slices.Clone(args))
	f.times = append(f.times, time.Now())
	if path != "/fake/systemctl" {
		f.mu.Unlock()
		panic("unexpected path " + path)
	}
	if args[0] == verbShow {
		if len(f.shows) == 0 {
			f.mu.Unlock()
			return runResult{Exit: 1}, nil
		}
		r := f.shows[0]
		if len(f.shows) > 1 {
			f.shows = f.shows[1:]
		}
		f.mu.Unlock()
		return r, nil
	}
	during, job := f.during, f.job
	f.mu.Unlock()
	if during != nil {
		during()
	}
	return job, nil
}

func (f *fakeSys) show(outs ...string) *fakeSys {
	for _, o := range outs {
		f.shows = append(f.shows, runResult{Out: []byte(o)})
	}
	return f
}

func (f *fakeSys) callList() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// verbs returns the first argument of every call.
func (f *fakeSys) verbs() []string {
	var v []string
	for _, c := range f.callList() {
		v = append(v, c[0])
	}
	return v
}

// ---- a broker under test -------------------------------------------------------

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

type testBroker struct {
	*broker
	sys     *fakeSys
	clock   *clock
	journal *syncBuffer
	dir     string // the fake root's update directory
}

func testOptions(t *testing.T, sys *fakeSys) (options, *clock, *syncBuffer) {
	t.Helper()
	c := &clock{t: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	j := &syncBuffer{}
	o := productionOptions()
	o.getenv = func(string) string { return "" }
	o.lookupUID = func(string) (uint32, error) { return testUID, nil }
	o.peerUID = func(net.Conn) (uint32, error) { return testUID, nil }
	o.journal = j
	o.now = c.Now
	o.systemctlPath = "/fake/systemctl"
	o.runner = sys.run
	o.updateDir = filepath.Join(t.TempDir(), "var-lib-oarfish-update") // absent: no helper
	o.verifyDelay = 20 * time.Millisecond
	o.idle = 200 * time.Millisecond
	return o, c, j
}

func newTestBroker(t *testing.T, sys *fakeSys) *testBroker {
	t.Helper()
	o, c, j := testOptions(t, sys)
	return &testBroker{broker: newBroker(o, testUID, &journal{w: j}), sys: sys, clock: c, journal: j, dir: o.updateDir}
}

// ask sends one request line as the core caller and decodes the answer.
func (tb *testBroker) ask(t *testing.T, line string) answer {
	t.Helper()
	out := tb.respond(Caller{UID: testUID}, []byte(line))
	return decodeAnswer(t, out)
}

func decodeAnswer(t *testing.T, out []byte) answer {
	t.Helper()
	if !bytes.HasSuffix(out, []byte("\n")) || bytes.Count(out, []byte("\n")) != 1 {
		t.Fatalf("answer is not one line: %q", out)
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	var a answer
	if err := dec.Decode(&a); err != nil {
		t.Fatalf("answer %q: %v", out, err)
	}
	if a.V != 1 {
		t.Fatalf("answer v = %d", a.V)
	}
	return a
}

func svc(op, unit string) string {
	return `{"v":1,"op":"` + op + `","unit":` + jsonString(unit) + `,"correlation_id":"` + testCID + `"}`
}

func power(op string) string {
	return `{"v":1,"op":"` + op + `","correlation_id":"` + testCID + `"}`
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// writeFile writes a test file, failing the test on error.
func writeFile(t *testing.T, path, data string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}

// shortDir is a directory whose path is short enough for a Unix socket.
func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "ob")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}
