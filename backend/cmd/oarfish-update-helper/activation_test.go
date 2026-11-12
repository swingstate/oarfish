package main

import (
	"bytes"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMain also serves as the entry point of the re-executed test binary: a
// child started with OARFISH_TEST_CHILD runs the real run() with the real
// environment and descriptors, as systemd would start the helper.
func TestMain(m *testing.M) {
	if mode := os.Getenv("OARFISH_TEST_CHILD"); mode != "" {
		os.Exit(childMain(mode))
	}
	os.Exit(m.Run())
}

func childMain(mode string) int {
	o := productionOptions()
	o.lookupUID = func(string) (uint32, error) { return uint32(os.Getuid()), nil }
	if os.Getenv("OARFISH_TEST_NO_USER") != "" {
		o.lookupUID = func(string) (uint32, error) { return 0, errors.New("unknown user") }
	}
	o.root = os.Getenv("OARFISH_TEST_ROOT")
	if d := os.Getenv("OARFISH_TEST_STATE"); d != "" {
		o.stateDir = d
	} else {
		o.stateDir = o.root + "-no-state" // never the real /var/lib/oarfish-update
	}
	o.idle = 500 * time.Millisecond
	if os.Getenv("OARFISH_TEST_LISTEN_PID") == "self" {
		// The parent cannot know the child's pid before it starts.
		os.Setenv("LISTEN_PID", strconv.Itoa(os.Getpid()))
	}
	return run([]string{mode}, os.Stderr, o)
}

type child struct {
	cmd    *exec.Cmd
	stderr bytes.Buffer
	stdout bytes.Buffer
	dir    string
}

// startChild re-executes the test binary. env entries are added to a minimal
// environment; files become descriptors 3, 4, ...
func startChild(t *testing.T, mode string, env []string, files ...*os.File) *child {
	t.Helper()
	c := &child{dir: shortDir(t)}
	c.cmd = exec.Command(os.Args[0])
	c.cmd.Dir = c.dir
	c.cmd.Env = append([]string{"OARFISH_TEST_CHILD=" + mode, "OARFISH_TEST_ROOT=" + testRoot(t)}, env...)
	c.cmd.Stderr, c.cmd.Stdout = &c.stderr, &c.stdout
	c.cmd.ExtraFiles = files
	if err := c.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.cmd.Process.Kill(); c.cmd.Wait() })
	return c
}

// wait returns the exit status.
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

func listenerFile(t *testing.T) (*net.UnixListener, *os.File, string) {
	t.Helper()
	path := filepath.Join(shortDir(t), "s")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := ln.File()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close(); ln.Close() })
	return ln, f, path
}

// In-process: the refusals that come before any descriptor is touched. (The
// cases that reach descriptor 3 run in a child, because the test binary's own
// descriptor 3 must not be closed.)
func TestActivatedListenerRefusals(t *testing.T) {
	self := strconv.Itoa(os.Getpid())
	tests := []struct {
		name string
		env  map[string]string
		want error
	}{
		{"no variables", nil, errNotActivated},
		{"only LISTEN_FDS", map[string]string{"LISTEN_FDS": "1"}, errNotActivated},
		{"only LISTEN_PID", map[string]string{"LISTEN_PID": self}, errNotActivated},
		{"LISTEN_PID of another process", map[string]string{"LISTEN_PID": "1", "LISTEN_FDS": "1"}, errNotActivated},
		{"LISTEN_PID not a number", map[string]string{"LISTEN_PID": "x", "LISTEN_FDS": "1"}, errNotActivated},
		{"LISTEN_PID with a suffix", map[string]string{"LISTEN_PID": self + "0", "LISTEN_FDS": "1"}, errNotActivated},
		{"LISTEN_FDS 0", map[string]string{"LISTEN_PID": self, "LISTEN_FDS": "0"}, errWrongFDs},
		{"LISTEN_FDS 2", map[string]string{"LISTEN_PID": self, "LISTEN_FDS": "2"}, errWrongFDs},
		{"LISTEN_FDS negative", map[string]string{"LISTEN_PID": self, "LISTEN_FDS": "-1"}, errWrongFDs},
		{"LISTEN_FDS not a number", map[string]string{"LISTEN_PID": self, "LISTEN_FDS": "one"}, errWrongFDs},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ln, err := activatedListener(func(k string) string { return tt.env[k] }, os.Getpid())
			if err != tt.want || ln != nil {
				t.Fatalf("got (%v, %v), want %v", ln, err, tt.want)
			}
		})
	}
}

func TestServeWithoutActivationExitsNonZero(t *testing.T) {
	o := testOptions(shortDir(t))
	o.lookupUID = func(string) (uint32, error) { t.Fatal("looked up a user before activation"); return 0, nil }
	var stderr bytes.Buffer
	if code := run([]string{"serve"}, &stderr, o); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if got := stderr.String(); got != "oarfish-update-helper: not started by socket activation\n" {
		t.Fatalf("stderr %q", got)
	}
}

// Child processes: the real binary logic with real descriptors.
func TestActivationInChildProcess(t *testing.T) {
	_, listenerF, _ := listenerFile(t)
	plain, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { plain.Close() })
	tcp, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	tcpF, err := tcp.File()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tcpF.Close(); tcp.Close() })

	self := []string{"OARFISH_TEST_LISTEN_PID=self"}
	tests := []struct {
		name    string
		env     []string
		files   []*os.File
		message string
	}{
		{"no variables", nil, nil, "not started by socket activation"},
		{"LISTEN_PID of another process", []string{"LISTEN_PID=424242", "LISTEN_FDS=1"}, []*os.File{listenerF}, "not started by socket activation"},
		{"LISTEN_FDS missing", append([]string{}, self...), []*os.File{listenerF}, "not started by socket activation"},
		{"LISTEN_FDS 2", append([]string{"LISTEN_FDS=2"}, self...), []*os.File{listenerF, listenerF}, "expected exactly one passed socket"},
		{"LISTEN_FDS 0", append([]string{"LISTEN_FDS=0"}, self...), []*os.File{listenerF}, "expected exactly one passed socket"},
		{"descriptor 3 does not exist", append([]string{"LISTEN_FDS=1"}, self...), nil, "the passed descriptor is not a Unix stream listener"},
		{"descriptor 3 is a regular file", append([]string{"LISTEN_FDS=1"}, self...), []*os.File{plain}, "the passed descriptor is not a Unix stream listener"},
		{"descriptor 3 is a TCP listener", append([]string{"LISTEN_FDS=1"}, self...), []*os.File{tcpF}, "the passed descriptor is not a Unix stream listener"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := startChild(t, "serve", tt.env, tt.files...)
			if code := c.wait(t); code != 1 {
				t.Fatalf("exit %d, want 1 (stderr %q)", code, c.stderr.String())
			}
			if got, want := c.stderr.String(), "oarfish-update-helper: "+tt.message+"\n"; got != want {
				t.Fatalf("stderr %q, want %q", got, want)
			}
			if strings.Contains(c.stderr.String(), "424242") || c.stdout.Len() != 0 {
				t.Fatalf("output echoes an environment value or writes to stdout: %q %q", c.stderr.String(), c.stdout.String())
			}
			// It binds and creates nothing.
			if entries, _ := os.ReadDir(c.dir); len(entries) != 0 {
				t.Fatalf("the helper created %d entries in its working directory", len(entries))
			}
		})
	}
}

// Activated, but the service user cannot be looked up: exit 1 with a fixed
// message that names the user and carries no text of the lookup error. The
// wording of this message changed after v0.1.0-alpha.11 so that the next
// release carries new helper bytes (D-C85); nothing else about the path did.
func TestServeWithoutServiceUserExitsNonZero(t *testing.T) {
	ln, f, path := listenerFile(t)
	c := startChild(t, "serve", []string{"LISTEN_FDS=1", "OARFISH_TEST_LISTEN_PID=self", "OARFISH_TEST_NO_USER=1"}, f)
	if code := c.wait(t); code != 1 {
		t.Fatalf("exit %d, want 1 (stderr %q)", code, c.stderr.String())
	}
	if got, want := c.stderr.String(), "oarfish-update-helper: cannot determine the uid of the service user oarfish\n"; got != want {
		t.Fatalf("stderr %q, want %q", got, want)
	}
	if c.stdout.Len() != 0 {
		t.Fatalf("stdout %q", c.stdout.String())
	}
	if entries, _ := os.ReadDir(c.dir); len(entries) != 0 {
		t.Fatalf("the helper created %d entries in its working directory", len(entries))
	}
	// The socket belongs to systemd and stays.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the helper removed the socket: %v", err)
	}
	ln.Close()
}

// Started as systemd starts it, the helper serves a status request on the
// passed socket and leaves after the idle time with status 0. This needs
// SO_PEERCRED, so it runs on Linux only.
func TestSocketActivatedServe(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("peer credentials are only available on Linux")
	}
	ln, f, path := listenerFile(t)
	c := startChild(t, "serve", []string{"LISTEN_FDS=1", "OARFISH_TEST_LISTEN_PID=self"}, f)

	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	conn.Write([]byte(okStatus))
	buf := make([]byte, 4096)
	n, _ := conn.Read(buf)
	r := parseReply(t, buf[:n])
	if !r.OK || r.KeyID != testKeyID || r.InstalledVersion != "0.1.0-alpha.8" || r.Phase != "idle" || r.HelperVersion != helperID {
		t.Fatalf("status answer %q", buf[:n])
	}
	conn.Close()

	if code := c.wait(t); code != 0 {
		t.Fatalf("exit %d after the idle time, want 0 (stderr %q)", code, c.stderr.String())
	}
	if c.stderr.Len() != 0 {
		t.Fatalf("stderr %q", c.stderr.String())
	}
	// The socket still exists: it belongs to systemd, not to the helper.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the helper removed the socket: %v", err)
	}
	ln.Close()
}
