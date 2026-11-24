package main

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMain is also the entry point of the re-executed test binary: a child
// started with OARFISH_BROKER_TEST_CHILD runs the real run() with real
// descriptors, as systemd would start the broker. The variables below exist
// only in this test file; the production binary reads none of them.
func TestMain(m *testing.M) {
	if os.Getenv("OARFISH_BROKER_TEST_CHILD") != "" {
		os.Exit(childMain())
	}
	os.Exit(m.Run())
}

func childMain() int {
	o := productionOptions()
	o.lookupUID = func(string) (uint32, error) { return uint32(os.Getuid()), nil }
	if os.Getenv("OARFISH_BROKER_TEST_NO_USER") != "" {
		o.lookupUID = func(string) (uint32, error) { return 0, errors.New("unknown user") }
	}
	if runtime.GOOS != "linux" {
		// SO_PEERCRED exists only on Linux; elsewhere the child trusts the peer.
		o.peerUID = func(net.Conn) (uint32, error) { return uint32(os.Getuid()), nil }
	}
	o.systemctlPath = os.Getenv("OARFISH_BROKER_TEST_SYSTEMCTL")
	o.updateDir = os.Getenv("OARFISH_BROKER_TEST_UPDATE_DIR")
	o.idle = 300 * time.Millisecond
	o.verifyDelay = 10 * time.Millisecond
	if os.Getenv("OARFISH_BROKER_TEST_LISTEN_PID") == "self" {
		os.Setenv("LISTEN_PID", strconv.Itoa(os.Getpid()))
	}
	args := strings.Fields(os.Getenv("OARFISH_BROKER_TEST_ARGS"))
	return run(args, os.Stderr, o)
}

type child struct {
	cmd    *exec.Cmd
	stderr syncBuffer
}

func startChild(t *testing.T, env []string, files ...*os.File) *child {
	t.Helper()
	c := &child{}
	c.cmd = exec.Command(os.Args[0])
	c.cmd.Dir = t.TempDir()
	c.cmd.Env = append([]string{"OARFISH_BROKER_TEST_CHILD=1", "OARFISH_BROKER_TEST_ARGS=serve",
		"OARFISH_BROKER_TEST_UPDATE_DIR=" + filepath.Join(c.cmd.Dir, "absent")}, env...)
	c.cmd.Stderr = &c.stderr
	c.cmd.ExtraFiles = files
	if err := c.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.cmd.Process.Kill(); c.cmd.Wait() })
	return c
}

func (c *child) wait(t *testing.T) int {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		c.cmd.Process.Kill()
		t.Fatal("child did not exit")
	}
	return c.cmd.ProcessState.ExitCode()
}

// listenerFile returns a listening Unix socket as a file (to become fd 3)
// and its path.
func listenerFile(t *testing.T) (*os.File, string) {
	t.Helper()
	path := filepath.Join(shortDir(t), "b.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	f, err := ln.File()
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	t.Cleanup(func() { f.Close() })
	return f, path
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {}, {"recover"}, {"serve", "x"}, {"--help"}, {"serve", "--debug"}, {"Serve"}, {"-serve"}} {
		var stderr bytes.Buffer
		if code := run(args, &stderr, productionOptions()); code != 2 || stderr.String() != usage+"\n" {
			t.Errorf("%q: %d %q", args, code, stderr.String())
		}
	}
}

// serve refuses to run without exactly one socket from systemd.
func TestActivationRequired(t *testing.T) {
	f, _ := listenerFile(t)
	cases := []struct {
		env  []string
		want string
	}{
		{nil, "not started by socket activation"},
		{[]string{"LISTEN_FDS=1"}, "not started by socket activation"},
		{[]string{"LISTEN_PID=1", "LISTEN_FDS=1"}, "not started by socket activation"},
		{[]string{"OARFISH_BROKER_TEST_LISTEN_PID=self", "LISTEN_FDS=2"}, "expected exactly one passed socket"},
		{[]string{"OARFISH_BROKER_TEST_LISTEN_PID=self", "LISTEN_FDS=x"}, "expected exactly one passed socket"},
	}
	for _, c := range cases {
		ch := startChild(t, c.env, f)
		if code := ch.wait(t); code != 1 || !strings.Contains(ch.stderr.String(), c.want) {
			t.Errorf("%q: exit %d, %q", c.env, code, ch.stderr.String())
		}
	}
	// fd 3 is not a listening socket.
	r, w, _ := os.Pipe()
	defer r.Close()
	defer w.Close()
	ch := startChild(t, []string{"OARFISH_BROKER_TEST_LISTEN_PID=self", "LISTEN_FDS=1"}, r)
	if code := ch.wait(t); code != 1 || !strings.Contains(ch.stderr.String(), "not a Unix stream listener") {
		t.Errorf("pipe: exit %d, %q", code, ch.stderr.String())
	}
}

func ask(t *testing.T, path, line string) string {
	t.Helper()
	var c net.Conn
	var err error
	for i := 0; i < 100; i++ {
		if c, err = net.Dial("unix", path); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(15 * time.Second))
	io.WriteString(c, line+"\n")
	out, _ := io.ReadAll(c)
	return string(out)
}

// The real serve path: activation, peer check (SO_PEERCRED on Linux), one
// request, the journal line, the idle exit with status 0.
func TestServeProcess(t *testing.T) {
	f, path := listenerFile(t)
	ch := startChild(t, []string{"OARFISH_BROKER_TEST_LISTEN_PID=self", "LISTEN_FDS=1"}, f)
	out := ask(t, path, `{"v":1,"op":"status","correlation_id":"`+testCID+`"}`)
	if !strings.HasPrefix(out, `{"v":1,"code":"success","protocol_min":1`) {
		t.Fatalf("status: %q", out)
	}
	if code := ch.wait(t); code != 0 {
		t.Fatalf("exit %d: %s", code, ch.stderr.String())
	}
	if got := ch.stderr.String(); got != "op=status unit=- cid="+testCID+" result=success\n" {
		t.Fatalf("journal %q", got)
	}
}

// B4: without the user oarfish the broker says so in one journal line and
// closes every connection without an answer.
func TestServeWithoutUser(t *testing.T) {
	f, path := listenerFile(t)
	ch := startChild(t, []string{"OARFISH_BROKER_TEST_LISTEN_PID=self", "LISTEN_FDS=1", "OARFISH_BROKER_TEST_NO_USER=1"}, f)
	if out := ask(t, path, power(opSystemReboot)); out != "" {
		t.Fatalf("answered %q", out)
	}
	if code := ch.wait(t); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := ch.stderr.String(); got != "oarfish-broker: the user oarfish does not exist; every connection is refused\n" {
		t.Fatalf("journal %q", got)
	}
}

// BF20 (class C part): the broker dies while systemctl runs. The connection
// ends without an answer (the service maps that to unavailable, G7.4), and
// the next connection is served by a new broker.
func TestCrashDuringOperation(t *testing.T) {
	fake := newFakeSystemctl(t)
	fake.set(1, "out", showOut(demo))
	fake.set(2, "sleep", "30")
	f, path := listenerFile(t)
	ch := startChild(t, []string{"OARFISH_BROKER_TEST_LISTEN_PID=self", "LISTEN_FDS=1", "OARFISH_BROKER_TEST_SYSTEMCTL=" + fake.path}, f)
	result := make(chan string, 1)
	go func() { result <- ask(t, path, svc(opServiceRestart, demo)) }()
	for i := 0; fake.count() < 2; i++ {
		if i > 500 {
			t.Fatal("the job did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ch.cmd.Process.Signal(syscall.SIGKILL)
	ch.wait(t)
	select {
	case out := <-result:
		if out != "" {
			t.Fatalf("answered %q", out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the connection did not end")
	}
	ch2 := startChild(t, []string{"OARFISH_BROKER_TEST_LISTEN_PID=self", "LISTEN_FDS=1"}, f)
	if out := ask(t, path, `{"v":1,"op":"status","correlation_id":"`+testCID+`"}`); !strings.Contains(out, `"busy":false`) {
		t.Fatalf("new broker: %q", out)
	}
	ch2.wait(t)
}

func TestSoftMemoryLimit(t *testing.T) {
	old := debug.SetMemoryLimit(-1)
	defer debug.SetMemoryLimit(old)
	configureRuntime()
	if got := debug.SetMemoryLimit(-1); got != 16<<20 {
		t.Fatalf("soft memory limit %d", got)
	}
}

// The production options carry the constants of the ADR and no seam.
func TestProductionOptions(t *testing.T) {
	o := productionOptions()
	if o.runner != nil || o.systemctlPath != "/usr/bin/systemctl" || o.updateDir != "/var/lib/oarfish-update" {
		t.Fatal("a seam is set in the production options")
	}
	if o.idle != 30*time.Second || o.opDeadline != 100*time.Second || o.verifyDelay != time.Second ||
		o.rateWindow != 60*time.Second || o.rateMax != 10 || o.showDeadline != 5*time.Second || o.requestDeadline != 5*time.Second {
		t.Fatalf("%+v", o)
	}
	if _, ok := o.policy(1).(corePolicy); !ok {
		t.Fatal("the production policy is not the constant core policy")
	}
	if maxRequestBytes != 1024 || maxSystemctlOutput != 256<<10 || softMemoryLimit != 16<<20 || serviceUser != "oarfish" || buildID != "broker-1" {
		t.Fatal("a bound differs from the ADR")
	}
}
