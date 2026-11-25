package brokerclient

import (
	"bufio"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const cid = "0123456789abcdef"

// fakeBroker is a real Unix socket with a scripted server: each connection
// reads one line and then does what handle says. A nil handle closes the
// connection without reading.
type fakeBroker struct {
	path  string
	ln    *net.UnixListener
	conns atomic.Int32
	mu    sync.Mutex
	lines []string
	wg    sync.WaitGroup
}

func shortDir(t *testing.T) string {
	t.Helper()
	// Unix socket paths are short (104 bytes on macOS); t.TempDir can be longer.
	d, err := os.MkdirTemp("", "bc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

func newFakeBroker(t *testing.T, handle func(c net.Conn, line string)) *fakeBroker {
	t.Helper()
	path := filepath.Join(shortDir(t), "b.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeBroker{path: path, ln: ln}
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			f.conns.Add(1)
			f.wg.Add(1)
			go func() {
				defer f.wg.Done()
				defer c.Close()
				if handle == nil {
					return
				}
				line, _ := bufio.NewReader(c).ReadString('\n')
				f.mu.Lock()
				f.lines = append(f.lines, line)
				f.mu.Unlock()
				handle(c, line)
			}()
		}
	}()
	t.Cleanup(func() { ln.Close(); f.wg.Wait() })
	return f
}

func answering(s string) func(net.Conn, string) {
	return func(c net.Conn, _ string) { _, _ = c.Write([]byte(s)) }
}

func client(path string) *Client { c := New(path); c.deadline = 2 * time.Second; return c }

// Protocol 1 exactly (A4.3): the member set of a service operation, v the
// literal 1, one line ending in a newline.
func TestRequestShape(t *testing.T) {
	f := newFakeBroker(t, answering(`{"v":1,"code":"success","state":"active"}`+"\n"))
	for op, want := range map[Op]string{
		OpStart:   `{"v":1,"op":"service.start","unit":"demo-app.service","correlation_id":"0123456789abcdef"}` + "\n",
		OpStop:    `{"v":1,"op":"service.stop","unit":"demo-app.service","correlation_id":"0123456789abcdef"}` + "\n",
		OpRestart: `{"v":1,"op":"service.restart","unit":"demo-app.service","correlation_id":"0123456789abcdef"}` + "\n",
	} {
		before := len(f.lines)
		a := client(f.path).Do(context.Background(), op, "demo-app.service", cid)
		if a != (Answer{Code: CodeSuccess, State: "active"}) {
			t.Errorf("%s: %+v", op, a)
		}
		f.mu.Lock()
		got := f.lines[before]
		f.mu.Unlock()
		if got != want {
			t.Errorf("%s sent %q\nwant %q", op, got, want)
		}
	}
	if Deadline != 110*time.Second || New(SocketPath).deadline != Deadline {
		t.Error("deadline")
	}
	if SocketPath != "/run/oarfish-broker/broker.sock" {
		t.Error(SocketPath)
	}
}

// A request that cannot be built is internal, and no connection is made.
func TestRequestRefusedBeforeSending(t *testing.T) {
	f := newFakeBroker(t, answering(`{"v":1,"code":"success"}`+"\n"))
	for _, tc := range []struct {
		op        Op
		unit, cid string
	}{
		{"status", "a.service", cid}, {"system.reboot", "a.service", cid}, {"system.poweroff", "a.service", cid},
		{"service.reload", "a.service", cid}, {OpStart, "-a.service", cid}, {OpStart, "a.socket", cid},
		{OpStart, "../a.service", cid}, {OpStart, strings.Repeat("a", 248) + ".service", cid},
		{OpStart, "a.service", "0123456789ABCDEF"}, {OpStart, "a.service", "short"},
	} {
		if a := client(f.path).Do(context.Background(), tc.op, tc.unit, tc.cid); a.Code != CodeInternal {
			t.Errorf("%+v: %+v", tc, a)
		}
	}
	if n := f.conns.Load(); n != 0 {
		t.Errorf("%d connections", n)
	}
}

// Every code of the closed set reaches the caller; the state words and the
// wait time are kept where the broker sends them.
func TestEveryCode(t *testing.T) {
	for _, c := range Codes {
		ans := `{"v":1,"code":"` + string(c) + `"}`
		want := Answer{Code: c}
		switch c {
		case CodeRateLimited:
			ans = `{"v":1,"code":"rate_limited","retry_after_s":42}`
			want.RetryAfterS = 42
		case CodeSuccess, CodeJobFailed, CodeTimeout, CodeStateUnexpected:
			ans = `{"v":1,"code":"` + string(c) + `","state":"failed"}`
			want.State = "failed"
		}
		f := newFakeBroker(t, answering(ans+"\n"))
		if got := client(f.path).Do(context.Background(), OpRestart, "demo-app.service", cid); got != want {
			t.Errorf("%s: %+v, want %+v", c, got, want)
		}
	}
	// The same codes without a state are fine too (the broker could not read it).
	for _, c := range []Code{CodeJobFailed, CodeTimeout} {
		f := newFakeBroker(t, answering(`{"v":1,"code":"`+string(c)+`"}`+"\n"))
		if got := client(f.path).Do(context.Background(), OpRestart, "demo-app.service", cid); got != (Answer{Code: c}) {
			t.Errorf("%s: %+v", c, got)
		}
	}
	for _, s := range States {
		f := newFakeBroker(t, answering(`{"v":1,"code":"success","state":"`+s+`"}`+"\n"))
		if got := client(f.path).Do(context.Background(), OpStart, "demo-app.service", cid); got.State != s {
			t.Errorf("%s: %+v", s, got)
		}
	}
	if len(Codes) != 19 {
		t.Errorf("%d codes", len(Codes))
	}
}

// The power-only codes and any other word are internal for a service operation.
func TestPowerOnlyAndUnknownCodesAreInternal(t *testing.T) {
	for _, c := range []string{"accepted", "update_running", "already_requested", "unavailable", "wrong_password", "SUCCESS", "ok", ""} {
		f := newFakeBroker(t, answering(`{"v":1,"code":"`+c+`"}`+"\n"))
		if got := client(f.path).Do(context.Background(), OpStop, "demo-app.service", cid); got != (Answer{Code: CodeInternal}) {
			t.Errorf("%q: %+v", c, got)
		}
	}
}

// Malformed answers: wrong v, unknown member, a member in another case, a
// repeated member, null, a wrong type, a state or wait time where it does not
// belong, trailing data, two lines, no newline, an oversize line.
func TestMalformedAnswersAreInternal(t *testing.T) {
	for _, ans := range []string{
		`{"v":2,"code":"success"}` + "\n",
		`{"v":1.0,"code":"success"}` + "\n",
		`{"v":"1","code":"success"}` + "\n",
		`{"code":"success"}` + "\n",
		`{"v":1}` + "\n",
		`{"v":1,"code":null}` + "\n",
		`{"v":1,"code":7}` + "\n",
		`{"v":1,"code":"success","message":"x"}` + "\n",
		`{"v":1,"Code":"success"}` + "\n",
		`{"v":1,"code":"success","code":"busy"}` + "\n",
		`{"v":1,"code":"success","state":"running"}` + "\n",
		`{"v":1,"code":"success","state":null}` + "\n",
		`{"v":1,"code":"unit_denied","state":"active"}` + "\n",
		`{"v":1,"code":"busy","retry_after_s":3}` + "\n",
		`{"v":1,"code":"rate_limited"}` + "\n",
		`{"v":1,"code":"rate_limited","retry_after_s":0}` + "\n",
		`{"v":1,"code":"rate_limited","retry_after_s":-1}` + "\n",
		`{"v":1,"code":"rate_limited","retry_after_s":1.5}` + "\n",
		`{"v":1,"code":"rate_limited","retry_after_s":1e1}` + "\n",
		`{"v":1,"code":"rate_limited","retry_after_s":"5"}` + "\n",
		`{"v":1,"code":"success"} {}` + "\n",
		`{"v":1,"code":"success"}` + "\n" + `{"v":1,"code":"success"}` + "\n",
		`{"v":1,"code":"success"}`,
		`[1]` + "\n",
		"\n",
		`{"v":1,"code":"success","state":"` + strings.Repeat("a", 9000) + `"}` + "\n",
	} {
		f := newFakeBroker(t, answering(ans))
		if got := client(f.path).Do(context.Background(), OpRestart, "demo-app.service", cid); got != (Answer{Code: CodeInternal}) {
			t.Errorf("%.80q: %+v", ans, got)
		}
		if n := f.conns.Load(); n != 1 {
			t.Errorf("%.40q: %d connections", ans, n)
		}
	}
}

// No answer, a reset, a refused connection, a missing socket and a timeout are
// unavailable, and the request is never sent twice.
func TestUnavailableAndNoRetry(t *testing.T) {
	closeAtOnce := newFakeBroker(t, func(net.Conn, string) {})
	if got := client(closeAtOnce.path).Do(context.Background(), OpRestart, "demo-app.service", cid); got.Code != CodeUnavailable {
		t.Errorf("closed: %+v", got)
	}
	// Closed before the request was read: a reset on Linux, an end of input elsewhere.
	reset := newFakeBroker(t, nil)
	if got := client(reset.path).Do(context.Background(), OpStop, "demo-app.service", cid); got.Code != CodeUnavailable {
		t.Errorf("reset: %+v", got)
	}
	hang := newFakeBroker(t, func(c net.Conn, _ string) { time.Sleep(600 * time.Millisecond) })
	c := client(hang.path)
	c.deadline = 200 * time.Millisecond
	start := time.Now()
	if got := c.Do(context.Background(), OpStart, "demo-app.service", cid); got.Code != CodeUnavailable {
		t.Errorf("timeout: %+v", got)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("waited %v past the deadline", d)
	}
	for name, f := range map[string]*fakeBroker{"closed": closeAtOnce, "reset": reset, "timeout": hang} {
		if n := f.conns.Load(); n != 1 {
			t.Errorf("%s: %d connections, want exactly 1 (no retry)", name, n)
		}
	}

	// A socket file that nobody listens on: connection refused.
	dir := shortDir(t)
	refused := filepath.Join(dir, "r.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: refused, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	ln.Close()
	if got := client(refused).Do(context.Background(), OpStart, "demo-app.service", cid); got.Code != CodeUnavailable {
		t.Errorf("refused: %+v", got)
	}
	if got := client(filepath.Join(dir, "missing.sock")).Do(context.Background(), OpStart, "demo-app.service", cid); got.Code != CodeUnavailable {
		t.Errorf("missing: %+v", got)
	}

	// A cancelled context (the service stops) ends the wait as well.
	slow := newFakeBroker(t, func(c net.Conn, _ string) { time.Sleep(600 * time.Millisecond) })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if got := client(slow.path).Do(ctx, OpStart, "demo-app.service", cid); got.Code != CodeUnavailable {
		t.Errorf("cancelled: %+v", got)
	}
}

// Available is a stat of the path only: it never connects (a connection would
// start the root process), and only a socket counts.
func TestAvailableIsStatOnly(t *testing.T) {
	f := newFakeBroker(t, answering(`{"v":1,"code":"success"}`+"\n"))
	c := New(f.path)
	for i := 0; i < 3; i++ {
		if !c.Available() {
			t.Fatal("socket not seen")
		}
	}
	time.Sleep(50 * time.Millisecond)
	if n := f.conns.Load(); n != 0 {
		t.Errorf("Available connected %d times", n)
	}
	dir := shortDir(t)
	regular := filepath.Join(dir, "file")
	if err := os.WriteFile(regular, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{regular, dir, filepath.Join(dir, "missing")} {
		if New(p).Available() {
			t.Errorf("%s counted as the socket", p)
		}
	}
	// The source pins it: Available calls the stat and nothing else.
	src, _ := os.ReadFile("client.go")
	body := string(src)
	body = body[strings.Index(body, "func (c *Client) Available() bool {"):]
	body = body[:strings.Index(body, "\n}\n")]
	if strings.Contains(body, "dial") || strings.Contains(body, "Dial") || !strings.Contains(body, "c.stat(") {
		t.Errorf("Available must be a stat only:\n%s", body)
	}
}

// This package knows the three service operations and nothing else of the
// protocol: no string literal in its code names status, a power operation or
// a power-only code (G7.5), and no file imports os/exec.
func TestNoOtherOperation(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	var ops []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			if imp.Path.Value == `"os/exec"` {
				t.Errorf("%s imports os/exec", name)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, _ := strconv.Unquote(lit.Value)
			for _, bad := range []string{"status", "system.reboot", "system.poweroff", "accepted", "update_running", "already_requested"} {
				if v == bad {
					t.Errorf("%s has the literal %q", name, v)
				}
			}
			if strings.HasPrefix(v, "service.") {
				ops = append(ops, v)
			}
			return true
		})
	}
	slices.Sort(ops)
	if !slices.Equal(ops, []string{"service.restart", "service.start", "service.stop"}) {
		t.Errorf("operations %v", ops)
	}
}
