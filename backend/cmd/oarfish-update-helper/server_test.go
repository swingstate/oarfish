package main

import (
	"net"
	"path/filepath"
	"testing"
	"time"
)

// A peer with another uid is closed without an answer (U2). The expected uid
// is injected; the check against the kernel's value is in peercred_linux_test.go.
func TestPeerUIDMismatchClosesWithoutAnswer(t *testing.T) {
	tests := []struct {
		name string
		peer func(net.Conn) (uint32, error)
	}{
		{"other uid", func(net.Conn) (uint32, error) { return testUID + 1, nil }},
		{"root", func(net.Conn) (uint32, error) { return 0, nil }},
		{"uid cannot be read", func(net.Conn) (uint32, error) { return testUID, net.ErrClosed }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := startServer(t, func(o *options) { o.peerUID = tt.peer })
			if raw := ts.exchange(t, []byte(okStatus), false); len(raw) != 0 {
				t.Fatalf("a refused peer got an answer: %q", raw)
			}
			// Not even a malformed request is answered.
			if raw := ts.exchange(t, []byte(marker+"\n"), false); len(raw) != 0 {
				t.Fatalf("a refused peer got an answer: %q", raw)
			}
		})
	}
}

func TestPeerUIDMatchIsServed(t *testing.T) {
	ts := startServer(t, nil)
	if r := parseReply(t, ts.exchange(t, []byte(okStatus), false)); !r.OK {
		t.Fatal("expected a status answer")
	}
}

// At most four connections are served at once; the others wait to be accepted.
func TestConnectionLimit(t *testing.T) {
	const deadline = 600 * time.Millisecond
	ts := startServer(t, func(o *options) { o.requestDeadline = deadline })

	// Four silent connections hold the four slots until their deadline.
	for i := 0; i < maxConnections; i++ {
		ts.dial(t)
	}
	waitFor(t, "four connections being served", func() bool { return ts.srv.active.Load() == maxConnections })

	// A fifth sends a complete request at once. It is not answered while the
	// slots are taken.
	fifth := ts.dial(t)
	fifth.Write([]byte(okStatus))
	fifth.SetReadDeadline(time.Now().Add(deadline / 4))
	buf := make([]byte, 4096)
	if n, err := fifth.Read(buf); n != 0 || err == nil {
		t.Fatalf("the fifth connection was served while four were active: %q", buf[:n])
	}
	if got := ts.srv.active.Load(); got > maxConnections {
		t.Fatalf("%d connections active", got)
	}

	// When a slot is free it is served.
	fifth.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, _ := fifth.Read(buf)
	if r := parseReply(t, buf[:n]); !r.OK {
		t.Fatalf("fifth connection: %q", buf[:n])
	}
	if got := ts.srv.peak.Load(); got != maxConnections {
		t.Fatalf("peak of active connections %d, want %d", got, maxConnections)
	}
}

func TestConnectionLimitHoldsUnderLoad(t *testing.T) {
	ts := startServer(t, func(o *options) { o.requestDeadline = 100 * time.Millisecond })
	conns := make([]*net.UnixConn, 12)
	for i := range conns {
		conns[i] = ts.dial(t)
	}
	for i, c := range conns {
		c.SetReadDeadline(time.Now().Add(10 * time.Second))
		buf := make([]byte, 4096)
		n, _ := c.Read(buf)
		if n == 0 {
			t.Fatalf("connection %d was never answered", i)
		}
		wantError(t, buf[:n], codeBadRequest)
	}
	if got := ts.srv.peak.Load(); got > maxConnections || got < 1 {
		t.Fatalf("peak %d", got)
	}
}

// The helper ends after the idle time with no connection (U1), and
// serve returns nil so that the process exits 0.
func TestIdleExit(t *testing.T) {
	root := testRoot(t)
	o := testOptions(root)
	o.idle = 250 * time.Millisecond
	ts := startServerWith(t, o)
	start := time.Now()
	select {
	case err := <-ts.done:
		if err != nil {
			t.Fatalf("serve returned %v, want nil", err)
		}
		if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
			t.Fatalf("exited after %v, before the idle time", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no idle exit")
	}
	ts.done <- nil // the cleanup waits for a value
}

// A request restarts the idle time.
func TestRequestRestartsTheIdleTime(t *testing.T) {
	o := testOptions(testRoot(t))
	o.idle = 400 * time.Millisecond
	ts := startServerWith(t, o)
	time.Sleep(250 * time.Millisecond)
	raw := ts.exchange(t, []byte(okStatus), false)
	answered := time.Now()
	if r := parseReply(t, raw); !r.OK {
		t.Fatalf("not served: %q", raw)
	}
	select {
	case <-ts.done:
		if elapsed := time.Since(answered); elapsed < 300*time.Millisecond {
			t.Fatalf("exited %v after the last request, idle time is 400ms", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no idle exit")
	}
	ts.done <- nil
}

// A connection being served keeps the helper alive past the idle time.
func TestServedConnectionKeepsTheHelperAlive(t *testing.T) {
	o := testOptions(testRoot(t))
	o.idle = 100 * time.Millisecond
	o.requestDeadline = 600 * time.Millisecond
	ts := startServerWith(t, o)
	start := time.Now()
	ts.dial(t) // silent: served until the request deadline
	select {
	case <-ts.done:
		if elapsed := time.Since(start); elapsed < 550*time.Millisecond {
			t.Fatalf("exited after %v while a connection was being served", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no idle exit")
	}
	ts.done <- nil
}

// startServerWith is startServer for options that are already built.
func startServerWith(t *testing.T, o options) *testServer {
	t.Helper()
	return startServer(t, func(dst *options) { *dst = o })
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// serveEnv serves the handler of a test environment on a fresh socket, with a
// fake peer check.
func serveEnv(t *testing.T, v *tenv, peer func(net.Conn) (uint32, error)) *testServer {
	t.Helper()
	o := testOptions(v.root)
	o.peerUID = peer
	path := filepath.Join(shortDir(t), "s")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	srv := newServer(o, testUID, v.h)
	ts := &testServer{path: path, srv: srv, done: make(chan error, 1), ln: ln}
	go func() { ts.done <- srv.serve(ln) }()
	t.Cleanup(func() {
		ln.Close()
		select {
		case <-ts.done:
		case <-time.After(10 * time.Second):
			t.Error("server did not stop")
		}
	})
	return ts
}

// The peer check runs before any byte of the request body is decoded. For all
// five request names and garbage, a peer with the wrong uid gets the
// connection closed without an answer; the state file is not read or written
// (the file system is never even opened), and nothing is created on disk.
func TestPeerCheckBeforeAnyRequestByte(t *testing.T) {
	payloads := map[string][]byte{
		"status":   []byte(okStatus),
		"stage":    stageLine("1.1.0", testCID),
		"discard":  discardLine("1.1.0"),
		"activate": []byte(`{"v":1,"op":"activate","version":"1.1.0","correlation_id":"0123456789abcdef","components_confirmed":true}` + "\n"),
		"ack":      []byte(`{"v":1,"op":"ack","transaction_id":"0123456789abcdef"}` + "\n"),
		"garbage":  []byte(marker + "\x00\xff{{{\n"),
		"oversize": append(make([]byte, 5000), '\n'),
		"empty":    {},
	}
	peers := map[string]func(net.Conn) (uint32, error){
		"other uid":      func(net.Conn) (uint32, error) { return testUID + 1, nil },
		"root":           func(net.Conn) (uint32, error) { return 0, nil },
		"uid max":        func(net.Conn) (uint32, error) { return ^uint32(0), nil },
		"uid unreadable": func(net.Conn) (uint32, error) { return testUID, net.ErrClosed },
	}
	for pname, peer := range peers {
		for name, payload := range payloads {
			t.Run(pname+"/"+name, func(t *testing.T) {
				v := newTenv(t)
				v.stageable("1.1.0")
				// A staged release and an expired one would be touched by status.
				opened := 0
				v.h.wrapFS = func(inner fsops) fsops { opened++; return inner }
				ts := serveEnv(t, v, peer)
				snapRoot, snapState, snapCache := snapshot(t, v.root), snapshot(t, v.stateDir), snapshot(t, v.cacheDir)
				start := time.Now()
				if raw := ts.exchange(t, payload, false); len(raw) != 0 {
					t.Fatalf("a refused peer got an answer: %q", raw)
				}
				// The server closes at once. It does not wait for the request (the
				// request deadline of these tests is 2 s) because it checks the peer first.
				if elapsed := time.Since(start); elapsed > time.Second {
					t.Fatalf("the refused connection stayed open for %v", elapsed)
				}
				// The server closed without reading: let the handler goroutine finish.
				waitFor(t, "the connection to end", func() bool { return ts.srv.active.Load() == 0 })
				if opened != 0 {
					t.Fatalf("the file system was opened %d times for a refused peer", opened)
				}
				for what, pair := range map[string][2]map[string]string{
					"release tree": {snapRoot, snapshot(t, v.root)}, "state directory": {snapState, snapshot(t, v.stateDir)}, "cache": {snapCache, snapshot(t, v.cacheDir)},
				} {
					if d := diffSnapshots(pair[0], pair[1]); len(d) != 0 {
						t.Fatalf("%s changed: %v", what, d)
					}
				}
			})
		}
	}
	// The same server with the right uid answers, so the test above is not
	// passing because nothing works.
	v := newTenv(t)
	ts := serveEnv(t, v, func(net.Conn) (uint32, error) { return testUID, nil })
	if r := parseReply(t, ts.exchange(t, []byte(okStatus), false)); !r.OK {
		t.Fatal("no answer for the right uid")
	}
	wantErrCode(t, ts.exchange(t, []byte(`{"v":1,"op":"activate"}`+"\n"), false), codeBadRequest)
}

// A full stage over the socket, with the right uid.
func TestStageOverTheSocket(t *testing.T) {
	v := newTenv(t)
	v.stageable("1.1.0")
	ts := serveEnv(t, v, func(net.Conn) (uint32, error) { return testUID, nil })
	r := wantOK(t, ts.exchange(t, stageLine("1.1.0", testCID), false))
	if r.Version != "1.1.0" {
		t.Fatalf("%+v", r)
	}
	if s := parseReply(t, ts.exchange(t, []byte(okStatus), false)); s.Phase != phaseStaged || s.StagedVersion != "1.1.0" {
		t.Fatalf("%+v", s)
	}
	wantOK(t, ts.exchange(t, discardLine("1.1.0"), false))
}

// A running transaction keeps the helper alive past the idle time (U1).
func TestTransactionKeepsTheHelperAlive(t *testing.T) {
	ts := startServer(t, func(o *options) { o.idle = 200 * time.Millisecond })
	ts.srv.h.running.Add(1)
	select {
	case <-ts.done:
		ts.done <- nil
		t.Fatal("exited while a transaction was running")
	case <-time.After(700 * time.Millisecond):
	}
	ts.srv.h.running.Add(-1)
	select {
	case <-ts.done:
	case <-time.After(3 * time.Second):
		t.Fatal("no idle exit after the transaction")
	}
	ts.done <- nil // the cleanup waits for a value
}
