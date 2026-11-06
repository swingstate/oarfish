package main

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseShow(t *testing.T) {
	good := "ActiveState=active\nSubState=running\nMainPID=412\nNRestarts=2\nExecStart={ path=/x ; argv[]=/x -listen 127.0.0.1:9 ; ignore_errors=no }\n"
	p, err := parseShow([]byte(good))
	if err != nil || p.Active != "active" || p.Sub != "running" || p.MainPID != 412 || p.NRestarts != 2 || !strings.Contains(p.ExecStart, "-listen") {
		t.Fatalf("%+v %v", p, err)
	}
	for name, in := range map[string]string{
		"empty":           "",
		"missing":         "ActiveState=active\nSubState=running\nMainPID=1\nNRestarts=0\n",
		"negative":        "ActiveState=a\nSubState=b\nMainPID=-1\nNRestarts=0\nExecStart=\n",
		"not a number":    "ActiveState=a\nSubState=b\nMainPID=x\nNRestarts=0\nExecStart=\n",
		"line without =":  "ActiveState=a\nSubState=b\nMainPID=1\nNRestarts=0\nExecStart=\nfoo\n",
		"huge":            "ActiveState=a\nSubState=b\nMainPID=99999999999999999999\nNRestarts=0\nExecStart=\n",
		"only whitespace": "\n\n",
	} {
		if _, err := parseShow([]byte(in)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestListenAddress(t *testing.T) {
	ex := func(argv string) string {
		return "{ path=/x ; argv[]=" + argv + " ; ignore_errors=no ; start_time=[n/a] }"
	}
	for _, tt := range []struct{ in, want string }{
		{ex("/x"), defaultListen},
		{ex("/x -listen 127.0.0.1:9000"), "127.0.0.1:9000"},
		{ex("/x -listen=127.0.0.1:9000"), "127.0.0.1:9000"},
		{ex("/x --listen 127.0.0.2:81"), "127.0.0.2:81"},
		{ex("/x -listen [::1]:8484"), "[::1]:8484"},
		{ex("/x -listen 0.0.0.0:8484"), defaultListen},
		{ex("/x -listen :8484"), defaultListen},
		{ex("/x -listen localhost:8484"), defaultListen},
		{ex("/x -listen 192.168.1.2:8484"), defaultListen},
		{ex("/x -listen example.com:80"), defaultListen},
		{ex("/x -listen 127.0.0.1"), defaultListen},
		{ex("/x -listen 127.0.0.1:0"), defaultListen},
		{ex("/x -listen"), defaultListen},
		{ex("/x -listen http://127.0.0.1:1"), defaultListen},
		{"", defaultListen},
		{"garbage", defaultListen},
	} {
		if got := listenAddress(tt.in); got != tt.want {
			t.Errorf("%q -> %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The unit's own listen address is what the check connects to.
func TestHealthUsesTheListenAddressFromExecStart(t *testing.T) {
	v := newActiveEnv(t)
	v.sd.listen = "127.0.0.1:9191"
	wantOK(t, v.activate("1.1.0"))
	if len(v.sd.healthCalls) == 0 {
		t.Fatal("no health call")
	}
	for _, c := range v.sd.healthCalls {
		if c != "127.0.0.1:9191/healthz" && c != "127.0.0.1:9191/readyz" {
			t.Fatalf("health call %q", c)
		}
	}
	v2 := newActiveEnv(t)
	v2.sd.listen = "10.0.0.5:80" // not loopback: the default is used
	wantOK(t, v2.activate("1.1.0"))
	for _, c := range v2.sd.healthCalls {
		if c != "127.0.0.1:8484/healthz" && c != "127.0.0.1:8484/readyz" {
			t.Fatalf("health call %q", c)
		}
	}
}

func TestHTTPGetAgainstALoopbackServer(t *testing.T) {
	serve := func(answer string) string {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Skip("no loopback")
		}
		t.Cleanup(func() { ln.Close() })
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				buf := make([]byte, 1024)
				c.Read(buf)
				c.Write([]byte(answer))
				c.Close()
			}
		}()
		return ln.Addr().String()
	}
	body := `{"status":"ok","version":"1.2.3"}`
	code, got, err := httpGet(serve("HTTP/1.0 200 OK\r\nContent-Type: application/json\r\n\r\n"+body), "/healthz")
	if err != nil || code != 200 || string(got) != body {
		t.Fatalf("%d %q %v", code, got, err)
	}
	code, _, err = httpGet(serve("HTTP/1.1 503 Service Unavailable\r\n\r\n{}"), "/healthz")
	if err != nil || code != 503 {
		t.Fatalf("%d %v", code, err)
	}
	for _, bad := range []string{"", "garbage", "HTTP/1.1 abc\r\n\r\n", "SMTP/1.1 200 OK\r\n\r\n"} {
		if _, _, err := httpGet(serve(bad), "/healthz"); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	// Nothing listens.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	if _, _, err := httpGet(addr, "/healthz"); err == nil {
		t.Fatal("connected to a closed port")
	}
	// A very large body is cut at the bound.
	_, got, err = httpGet(serve("HTTP/1.0 200 OK\r\n\r\n"+strings.Repeat("x", 100000)), "/healthz")
	if err != nil || len(got) > maxHTTPBytes {
		t.Fatalf("body of %d bytes, %v", len(got), err)
	}
}

// The health criteria one by one (U6), against a fake that changes between
// polls. The check passes only when every criterion holds for 3 polls and 10 s.
func TestWaitHealthyCriteria(t *testing.T) {
	type tc struct {
		name   string
		setup  func(f *fakeSystemd)
		params healthParams
		want   healthResult
		minSec int
	}
	base := healthParams{version: "1.1.0", prevPID: 100, baseRestarts: 0, deadline: healthDeadline, polls: healthPolls, window: healthStability}
	for _, c := range []tc{
		{"all hold", func(f *fakeSystemd) { f.running, f.pid = "1.1.0", 101 }, base, healthOK, 9},
		{"same MainPID as before", func(f *fakeSystemd) { f.running, f.pid = "1.1.0", 100 }, base, healthTimeout, 45},
		{"MainPID 0", func(f *fakeSystemd) { f.running, f.pid = "1.1.0", 0 }, base, healthTimeout, 45},
		{"not running", func(f *fakeSystemd) { f.running, f.pid, f.sub = "1.1.0", 101, "exited" }, base, healthTimeout, 45},
		{"activating", func(f *fakeSystemd) { f.running, f.pid, f.active = "1.1.0", 101, "activating" }, base, healthTimeout, 45},
		{"failed", func(f *fakeSystemd) { f.running, f.pid, f.active = "1.1.0", 101, "failed" }, base, healthFailFast, 0},
		{"NRestarts increased", func(f *fakeSystemd) { f.running, f.pid, f.nrest = "1.1.0", 101, 1 }, base, healthFailFast, 0},
		{"wrong version", func(f *fakeSystemd) { f.running, f.pid = "1.0.0", 101 }, base, healthTimeout, 45},
		{"no version member", func(f *fakeSystemd) { f.running, f.pid = "1.1.0", 101; f.beh["1.1.0"] = bNoVersion }, base, healthTimeout, 45},
		{"no 200", func(f *fakeSystemd) { f.running, f.pid = "1.1.0", 101; f.beh["1.1.0"] = bHang }, base, healthTimeout, 45},
		{"show fails", func(f *fakeSystemd) { f.running, f.pid = "1.1.0", 101; f.showFail = true }, base, healthTimeout, 45},
		{"pre-check ignores PID and counter", func(f *fakeSystemd) { f.running, f.pid, f.nrest = "1.1.0", 100, 5 },
			healthParams{version: "1.1.0", prevPID: -1, baseRestarts: -1, deadline: precheckLimit, polls: precheckPolls}, healthOK, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := newTenv(t)
			f := v.fakeSystemd()
			c.setup(f)
			got, _ := v.h.waitHealthy(c.params)
			if got != c.want {
				t.Fatalf("result %d, want %d", got, c.want)
			}
			if el := int(time.Duration(v.mono.Load()) / time.Second); el < c.minSec {
				t.Fatalf("returned after %d s, want at least %d", el, c.minSec)
			}
		})
	}
}

// A single bad poll restarts the stability window.
func TestStabilityWindowRestartsOnABadPoll(t *testing.T) {
	v := newTenv(t)
	f := v.fakeSystemd()
	f.running, f.pid = "1.1.0", 101
	polls := 0
	inner := v.h.healthGet
	v.h.healthGet = func(a, p string) (int, []byte, error) {
		if p == readyPath {
			return inner(a, p)
		}
		polls++
		if polls == 8 {
			return 503, nil, nil
		}
		return inner(a, p)
	}
	res, _ := v.h.waitHealthy(healthParams{version: "1.1.0", prevPID: 100, baseRestarts: 0, deadline: healthDeadline, polls: healthPolls, window: healthStability})
	if res != healthOK {
		t.Fatal("not healthy")
	}
	if el := time.Duration(v.mono.Load()); el < 17*time.Second {
		t.Fatalf("passed after %v: the window did not restart", el)
	}
}

func TestSystemctlClosedShapes(t *testing.T) {
	v := newTenv(t)
	f := v.fakeSystemd()
	for _, bad := range [][]string{
		nil, {}, {"restart"}, {"restart", "oarfish-update-helper.service"}, {"restart", "oarfish-update-helper.socket"},
		{"restart", "oarfish.service", "--no-block"}, {"stop", "oarfish.service"}, {"start", "oarfish.service"},
		{"enable", "oarfish.service"}, {"reset-failed"}, {"reset-failed", "other.service"}, {"daemon-reload", "x"},
		{"show", "oarfish.service"}, {"show", "oarfish.service", "--property=MainPID", "--no-pager"},
		{"show", "oarfish.service", "--no-pager", "--property=ActiveState,SubState,MainPID,NRestarts,ExecStart"},
		{"--user", "restart", "oarfish.service"}, {"restart", "oarfish.service\n"}, {"RESTART", "oarfish.service"},
		{"reboot"}, {"poweroff"}, {"-H", "x", "restart", "oarfish.service"},
	} {
		if _, err := v.h.systemctl(bad...); err == nil {
			t.Errorf("ran %q", bad)
		}
	}
	if len(f.Calls()) != 0 {
		t.Fatalf("the runner saw %v", f.Calls())
	}
	for _, ok := range systemctlShapes {
		if _, err := v.h.systemctl(ok...); err != nil && ok[0] != "daemon-reload" && ok[0] != "restart" {
			// the fake knows restart, show and reset-failed only
			t.Errorf("%q: %v", ok, err)
		}
	}
	// The shape passed to the runner is a copy.
	v.h.sysrun = func(args []string) ([]byte, error) { args[0] = "poweroff"; return nil, nil }
	v.h.systemctl(sysRestart...)
	if sysRestart[0] != "restart" {
		t.Fatal("the closed list was changed through the runner")
	}
}

// The production runner: no shell, a cleared environment, bounded output, a
// fixed error.
func TestRunProgram(t *testing.T) {
	t.Setenv("OARFISH_TEST_SECRET", "s3cret")
	out, err := runProgram("/usr/bin/env", nil)
	if err != nil || len(out) != 0 {
		t.Fatalf("environment not cleared: %q %v", out, err)
	}
	out, err = runProgram("/usr/bin/head", []string{"-c", "100000", "/dev/zero"})
	if err != nil || len(out) != maxSystemctlOutput {
		t.Fatalf("output %d bytes, %v", len(out), err)
	}
	if _, err := runProgram("/usr/bin/false", nil); err == nil {
		t.Fatal("exit status 1 was not an error")
	}
	if _, err := runProgram("/nonexistent/systemctl", nil); err == nil {
		t.Fatal("a missing program was not an error")
	}
	// No shell: a metacharacter is just an argument.
	dir := t.TempDir()
	out, err = runProgram("/bin/echo", []string{"a;touch " + filepath.Join(dir, "x") + "|b"})
	if err != nil || !strings.Contains(string(out), ";touch") {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "x")); err == nil {
		t.Fatal("a shell ran")
	}
	if systemctlPath != "/usr/bin/systemctl" {
		t.Fatal("path")
	}
	_ = exec.ErrNotFound
}

// The closed list of programs and argument shapes is the table of U2.
func TestSystemctlShapesMatchTheADR(t *testing.T) {
	adr, err := os.ReadFile("../../../docs/adr/0010-update-helper.md")
	if err != nil {
		t.Fatal(err)
	}
	var row string
	for _, l := range strings.Split(string(adr), "\n") {
		if strings.HasPrefix(l, "  | Programs executed |") {
			row = l
		}
	}
	if row == "" {
		t.Fatal("no row for the programs in the ADR")
	}
	if !strings.Contains(row, "`/usr/bin/systemctl` only (absolute path, cleared environment, no shell)") {
		t.Fatal("the ADR names another program")
	}
	_, after, _ := strings.Cut(row, "argument shapes: ")
	after, _, _ = strings.Cut(after, " |")
	var want []string
	for _, part := range strings.Split(after, ";") {
		_, rest, ok := strings.Cut(part, "`")
		if !ok {
			continue
		}
		shape, _, _ := strings.Cut(rest, "`")
		want = append(want, shape)
	}
	var have []string
	for _, s := range systemctlShapes {
		have = append(have, strings.Join(s, " "))
	}
	if strings.Join(have, "\n") != strings.Join(want, "\n") {
		t.Fatalf("shapes in the code:\n%s\nshapes in the ADR:\n%s", strings.Join(have, "\n"), strings.Join(want, "\n"))
	}
	if len(want) != 5 {
		t.Fatalf("%d shapes in the ADR", len(want))
	}
}

// The requests, error codes and unit names of the closed lists.
func TestClosedRequestListMatchesTheADR(t *testing.T) {
	adr, _ := os.ReadFile("../../../docs/adr/0010-update-helper.md")
	var row string
	for _, l := range strings.Split(string(adr), "\n") {
		if strings.HasPrefix(l, "  | Requests |") {
			row = l
		}
	}
	for _, op := range []string{opStatus, opStage, opActivate, opDiscard, opAck} {
		if !strings.Contains(row, "`"+op+"`") {
			t.Errorf("the ADR row does not list %s", op)
		}
	}
	if len(opKeys) != 5 {
		t.Fatalf("%d ops in the decoder", len(opKeys))
	}
}

// A11.2: from strictHealthFrom on, the answering process must be the one systemd
// reports (pid == MainPID) and /readyz must answer 200. An older version is
// checked as before, so a rollback to a release without pid stays possible.
func TestHealthzOKRequiresPIDAndReadyzFromTheMinimumVersion(t *testing.T) {
	const strict, old = "0.1.0-alpha.7", "0.1.0-alpha.6.1"
	answer := func(health, ready string, readyCode int) func(string, string) (int, []byte, error) {
		return func(_, path string) (int, []byte, error) {
			if path == readyPath {
				return readyCode, []byte(ready), nil
			}
			return 200, []byte(health), nil
		}
	}
	for _, c := range []struct {
		name    string
		version string
		health  string
		ready   string
		code    int
		mainPID int64
		want    bool
	}{
		{"strict, pid equals MainPID, ready", strict, `{"status":"ok","version":"0.1.0-alpha.7","pid":42}`, `{"status":"ok"}`, 200, 42, true},
		{"strict, another process answers", strict, `{"status":"ok","version":"0.1.0-alpha.7","pid":43}`, `{"status":"ok"}`, 200, 42, false},
		{"strict, no pid member", strict, `{"status":"ok","version":"0.1.0-alpha.7"}`, `{"status":"ok"}`, 200, 42, false},
		{"strict, pid is not a number", strict, `{"status":"ok","version":"0.1.0-alpha.7","pid":"42"}`, `{"status":"ok"}`, 200, 42, false},
		{"strict, MainPID unknown", strict, `{"status":"ok","version":"0.1.0-alpha.7","pid":0}`, `{"status":"ok"}`, 200, 0, false},
		{"strict, readyz 503", strict, `{"status":"ok","version":"0.1.0-alpha.7","pid":42}`, `{"status":"not_ready"}`, 503, 42, false},
		{"strict, readyz 200 without ok", strict, `{"status":"ok","version":"0.1.0-alpha.7","pid":42}`, `{}`, 200, 42, false},
		{"newer than the minimum", "1.0.0", `{"status":"ok","version":"1.0.0","pid":42}`, `{"status":"ok"}`, 200, 42, true},
		{"older version: no pid needed", old, `{"status":"ok","version":"0.1.0-alpha.6.1"}`, `{"status":"not_ready"}`, 503, 42, true},
		{"older version: wrong pid is not looked at", old, `{"status":"ok","version":"0.1.0-alpha.6.1","pid":1}`, ``, 500, 42, true},
		{"older version: still needs ok and version", old, `{"status":"ok","version":"0.0.1"}`, ``, 200, 42, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := &handler{healthGet: answer(c.health, c.ready, c.code)}
			if got := h.healthzOK("127.0.0.1:1", c.version, c.mainPID); got != c.want {
				t.Fatalf("healthzOK = %v, want %v", got, c.want)
			}
		})
	}
}

func TestStrictHealthBoundary(t *testing.T) {
	for v, want := range map[string]bool{
		"0.1.0-alpha.6": false, "0.1.0-alpha.6.1": false, "0.1.0-alpha.7": true, "0.1.0-alpha.8": true,
		"0.1.0": true, "1.0.0": true, "0.0.9": false, "not a version": true,
	} {
		if got := strictHealth(v); got != want {
			t.Errorf("strictHealth(%q) = %v, want %v", v, got, want)
		}
	}
}

// A process that answers /healthz with the right version but is not the unit's
// main process fails the activation (G6.6 review finding 1).
func TestActivationRollsBackWhenAnotherProcessAnswers(t *testing.T) {
	v := newActiveEnv(t)
	inner := v.sd.health
	v.h.healthGet = func(a, p string) (int, []byte, error) {
		code, body, err := inner(a, p)
		if p == healthPath && strings.Contains(string(body), `"1.1.0"`) { // the new version, not the pre-check
			body = []byte(strings.Replace(string(body), "\"pid\":", "\"pid\":9", 1))
		}
		return code, body, err
	}
	wantOK(t, v.activate("1.1.0"))
	l := v.lastResult()
	if v.current() != "1.0.0" || l == nil || l.Outcome != outcomeRolledBack || l.ReasonCode != reasonHealthTimeout {
		t.Fatalf("an impostor on the port passed the health check: current %q last %+v", v.current(), l)
	}
}

// FuzzHealthzBody feeds arbitrary bytes as the answer of /healthz and /readyz.
// Nothing may panic, and a version from strictHealthFrom on is accepted only
// when the body names exactly the main PID and /readyz said ok (A11.2).
func FuzzHealthzBody(f *testing.F) {
	for _, s := range []string{`{"status":"ok","version":"1.0.0","pid":42}`, `{"status":"ok","version":"1.0.0"}`, `{"status":"ok","version":"1.0.0","pid":"42"}`,
		`{"status":"ok","version":"1.0.0","pid":42.5}`, `{"status":"ok","version":"1.0.0","pid":1e2}`, `{}`, `null`, `[]`, ``,
		`{"status":"ok","status":"no","version":"1.0.0","pid":42}`} {
		f.Add([]byte(s), []byte(`{"status":"ok"}`), int64(42))
	}
	f.Fuzz(func(t *testing.T, health, ready []byte, mainPID int64) {
		h := &handler{healthGet: func(_, path string) (int, []byte, error) {
			if path == readyPath {
				return 200, ready, nil
			}
			return 200, health, nil
		}}
		if !h.healthzOK("127.0.0.1:1", "1.0.0", mainPID) {
			return
		}
		var r struct {
			Status  *string `json:"status"`
			Version *string `json:"version"`
			PID     *int64  `json:"pid"`
		}
		if json.Unmarshal(health, &r) != nil || r.Status == nil || *r.Status != "ok" || r.Version == nil || *r.Version != "1.0.0" {
			t.Fatalf("accepted %q", health)
		}
		if r.PID == nil || *r.PID != mainPID || mainPID <= 0 {
			t.Fatalf("accepted pid %v for MainPID %d: %q", r.PID, mainPID, health)
		}
		var rd struct{ Status *string }
		if json.Unmarshal(ready, &rd) != nil || rd.Status == nil || *rd.Status != "ok" {
			t.Fatalf("accepted readyz %q", ready)
		}
	})
}
