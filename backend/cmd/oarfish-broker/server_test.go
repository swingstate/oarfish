package main

import (
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type testServer struct {
	path    string
	ln      *net.UnixListener
	done    chan error
	broker  *broker
	journal *syncBuffer
}

// startServer serves on a real Unix socket with options o (real time).
func startServer(t *testing.T, o options, refuseAll bool) *testServer {
	t.Helper()
	path := filepath.Join(shortDir(t), "b.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	j, _ := o.journal.(*syncBuffer)
	b := newBroker(o, testUID, &journal{w: o.journal})
	s := newServer(o, testUID, refuseAll, b)
	ts := &testServer{path: path, ln: ln, done: make(chan error, 1), broker: b, journal: j}
	go func() { ts.done <- s.serve(ln) }()
	t.Cleanup(func() { ln.Close(); <-ts.done })
	return ts
}

// exchange sends line and returns everything the broker wrote before closing.
func exchange(t *testing.T, path, line string) string {
	t.Helper()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.WriteString(c, line); err != nil && !errors.Is(err, io.EOF) {
		// The broker may close before reading; that is an answer of its own.
		return ""
	}
	b, _ := io.ReadAll(c)
	return string(b)
}

func realTimeOptions(t *testing.T, sys *fakeSys) options {
	o, _, _ := testOptions(t, sys)
	o.now = time.Now
	return o
}

// BF1: another uid, root included, or an unreadable uid: closed without an
// answer, and nothing runs or is logged.
func TestWrongPeer(t *testing.T) {
	for name, peer := range map[string]func(net.Conn) (uint32, error){
		"root":       func(net.Conn) (uint32, error) { return 0, nil },
		"other user": func(net.Conn) (uint32, error) { return testUID + 1, nil },
		"unreadable": func(net.Conn) (uint32, error) { return testUID, errors.New("x") },
	} {
		sys := &fakeSys{}
		o := realTimeOptions(t, sys)
		o.peerUID = peer
		ts := startServer(t, o, false)
		for _, line := range []string{power(opSystemReboot) + "\n", svc(opServiceStart, demo) + "\n", "garbage\n"} {
			if out := exchange(t, ts.path, line); out != "" {
				t.Errorf("%s: answered %q", name, out)
			}
		}
		if len(sys.callList()) != 0 || ts.journal.String() != "" {
			t.Errorf("%s: calls %q journal %q", name, sys.callList(), ts.journal.String())
		}
	}
}

// B4: without the user oarfish every connection is closed without an answer.
func TestRefuseAll(t *testing.T) {
	sys := &fakeSys{}
	ts := startServer(t, realTimeOptions(t, sys), true)
	if out := exchange(t, ts.path, power(opSystemReboot)+"\n"); out != "" {
		t.Fatalf("answered %q", out)
	}
	if len(sys.callList()) != 0 {
		t.Fatal("ran systemctl")
	}
}

// BF2 over the socket: an oversize line and a line without a newline. Data
// after the line is covered by TestReadRequest.
func TestMalformedOverSocket(t *testing.T) {
	sys := &fakeSys{}
	ts := startServer(t, realTimeOptions(t, sys), false)
	want := `{"v":1,"code":"bad_request"}` + "\n"
	for _, line := range []string{
		strings.Repeat(" ", maxRequestBytes) + "\n",
		power(opSystemReboot),
	} {
		c, err := net.Dial("unix", ts.path)
		if err != nil {
			t.Fatal(err)
		}
		c.SetDeadline(time.Now().Add(10 * time.Second))
		io.WriteString(c, line)
		c.(*net.UnixConn).CloseWrite()
		out, _ := io.ReadAll(c)
		c.Close()
		if string(out) != want {
			t.Errorf("%.40q: %q", line, out)
		}
	}
	if len(sys.callList()) != 0 {
		t.Fatal("ran systemctl")
	}
}

// BF29 (class C part): a compromised service sends requests straight to the
// socket as oarfish. Exactly the allowlist works; every refusal of B2 and B3
// (as amended) holds.
func TestCompromisedServiceBounded(t *testing.T) {
	type tc struct {
		line  string
		shows []string
		want  code
	}
	inst := "demo-worker@one.service"
	cases := []tc{
		{svc(opServiceRestart, demo), []string{showOut(demo), showOut(demo, "MainPID", "1")}, codeSuccess},
		{svc(opServiceStop, demo), []string{showOut(demo), showOut(demo, "ActiveState", "inactive")}, codeSuccess},
		{svc(opServiceStart, demo), []string{showOut(demo, "ActiveState", "inactive"), showOut(demo)}, codeSuccess},
		{`{"v":1,"op":"exec","correlation_id":"` + testCID + `","unit":"/bin/sh"}`, nil, codeBadRequest},
		{`{"v":1,"op":"service.enable","unit":"a.service","correlation_id":"` + testCID + `"}`, nil, codeBadRequest},
		{svc(opServiceStart, "--global.service"), nil, codeUnitInvalid},
		{svc(opServiceStart, "x.timer"), nil, codeUnitInvalid},
		{svc(opServiceStart, "demo-worker@.service"), nil, codeUnitTemplate},
		{svc(opServiceStart, inst), []string{showOut(inst, "ActiveState", "inactive")}, codeUnitTemplate},
		{svc(opServiceStart, "gone.service"), []string{notFoundOut("gone.service")}, codeUnitUnknown},
		{svc(opServiceStop, "alias.service"), []string{showOut(demo, "Names", demo+" alias.service")}, codeUnitNotCanonical},
		{svc(opServiceStart, demo), []string{showOut(demo, "LoadState", "masked")}, codeUnitMasked},
		{svc(opServiceRestart, demo), []string{showOut(demo, "Transient", "yes")}, codeUnitTransient},
		{svc(opServiceStop, demo), []string{showOut(demo, "Type", "oneshot")}, codeUnitOneshot},
		{svc(opServiceRestart, demo), []string{showOut(demo, "RefuseManualStop", "yes")}, codeRefusedByUnit},
		{svc(opServiceRestart, "systemd-reboot.service"), []string{showOut("systemd-reboot.service")}, codeUnitDenied},
		{svc(opServiceStop, "oarfish.service"), []string{showOut("oarfish.service")}, codeUnitDenied},
		{svc(opServiceStop, "ssh.service"), []string{showOut("ssh.service")}, codeOperationNotOffered},
		{svc(opServiceStart, demo), []string{showOut(demo)}, codeAlreadyInState},
		{power(opSystemReboot), nil, codeAccepted},
		{power(opSystemPoweroff), nil, codeAlreadyRequested},
	}
	for _, c := range cases {
		sys := (&fakeSys{}).show(c.shows...)
		o := realTimeOptions(t, sys)
		o.rateMax = 1000 // the rate limit has its own test
		if c.want == codeAlreadyRequested {
			continue // run below on one broker
		}
		ts := startServer(t, o, false)
		out := exchange(t, ts.path, c.line+"\n")
		a := decodeAnswer(t, []byte(out))
		if a.Code != c.want {
			t.Errorf("%s: %s, want %s", c.line, a.Code, c.want)
		}
		for _, call := range sys.callList() {
			if !allowedShape(call) {
				t.Errorf("%s: call outside the shapes: %q", c.line, call)
			}
		}
	}
	sys := &fakeSys{}
	ts := startServer(t, realTimeOptions(t, sys), false)
	decodeAnswer(t, []byte(exchange(t, ts.path, power(opSystemReboot)+"\n")))
	if a := decodeAnswer(t, []byte(exchange(t, ts.path, power(opSystemPoweroff)+"\n"))); a.Code != codeAlreadyRequested {
		t.Fatalf("second power request: %s", a.Code)
	}
}

// B8: the idle exit, 30 s (here 200 ms) after the last connection.
func TestIdleExit(t *testing.T) {
	o := realTimeOptions(t, &fakeSys{})
	ts := startServer(t, o, false)
	exchange(t, ts.path, `{"v":1,"op":"status","correlation_id":"`+testCID+`"}`+"\n")
	select {
	case err := <-ts.done:
		ts.done <- err
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no idle exit")
	}
}

// BF18 (class C part): the rate window survives the idle exit, because the
// broker stays while the window holds entries.
func TestWindowKeepsBroker(t *testing.T) {
	o := realTimeOptions(t, &fakeSys{})
	o.rateWindow = 900 * time.Millisecond
	ts := startServer(t, o, false)
	start := time.Now()
	exchange(t, ts.path, svc(opServiceStart, "bad name")+"\n") // uses a slot
	select {
	case <-ts.done:
		ts.done <- nil
	case <-time.After(5 * time.Second):
		t.Fatal("no exit after the window emptied")
	}
	if d := time.Since(start); d < 900*time.Millisecond {
		t.Fatalf("exited after %v, while the window still held an entry", d)
	}
}

// D-C89 item 6: after `accepted` the broker does not idle-exit.
func TestPowerKeepsBroker(t *testing.T) {
	o := realTimeOptions(t, &fakeSys{})
	ts := startServer(t, o, false)
	if a := decodeAnswer(t, []byte(exchange(t, ts.path, power(opSystemReboot)+"\n"))); a.Code != codeAccepted {
		t.Fatal(a.Code)
	}
	select {
	case <-ts.done:
		ts.done <- nil
		t.Fatal("idle exit after an accepted power operation")
	case <-time.After(1500 * time.Millisecond):
	}
	if a := decodeAnswer(t, []byte(exchange(t, ts.path, power(opSystemPoweroff)+"\n"))); a.Code != codeAlreadyRequested {
		t.Fatal(a.Code)
	}
}

// BF17 over the socket: status is answered while an operation runs, and a
// second operation is busy.
func TestConcurrentConnections(t *testing.T) {
	sys := (&fakeSys{}).show(showOut(demo), showOut(demo, "MainPID", "8"))
	inJob, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	sys.during = func() { once.Do(func() { close(inJob) }); <-release }
	ts := startServer(t, realTimeOptions(t, sys), false)
	done := make(chan string)
	go func() { done <- exchange(t, ts.path, svc(opServiceRestart, demo)+"\n") }()
	<-inJob
	st := exchange(t, ts.path, `{"v":1,"op":"status","correlation_id":"`+testCID+`"}`+"\n")
	if !strings.Contains(st, `"busy":true`) {
		t.Errorf("status: %s", st)
	}
	if a := decodeAnswer(t, []byte(exchange(t, ts.path, svc(opServiceStop, demo)+"\n"))); a.Code != codeBusy {
		t.Errorf("second operation: %s", a.Code)
	}
	close(release)
	if a := decodeAnswer(t, []byte(<-done)); a.Code != codeSuccess {
		t.Errorf("first operation: %s", a.Code)
	}
}
