package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testUID   = 4242 // the uid the test peer check reports as the service user
	testKeyID = "11f1c52777090bfc"
)

// marker is put into requests; no answer may ever contain it (U2: an error
// never echoes the request).
const marker = "REQUEST-MARKER-7f3a"

// shortDir returns a short temporary directory: socket paths are limited to
// about 100 bytes, which t.TempDir() can exceed on macOS.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "oh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// testRoot builds a release root with current -> releases/0.1.0-alpha.8.
func testRoot(t *testing.T) string {
	t.Helper()
	root := shortDir(t)
	if err := os.MkdirAll(filepath.Join(root, "releases", "0.1.0-alpha.8"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("releases/0.1.0-alpha.8", filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	return root
}

// testOptions are options with a fake peer check that reports testUID.
func testOptions(root string) options {
	return options{
		stateDir:        root + "-state-does-not-exist",
		cacheDir:        root + "-cache-does-not-exist",
		arch:            "arm64",
		getenv:          func(string) string { return "" },
		pid:             os.Getpid(),
		lookupUID:       func(string) (uint32, error) { return testUID, nil },
		peerUID:         func(net.Conn) (uint32, error) { return testUID, nil },
		root:            root,
		helperVersion:   helperID,
		idle:            time.Minute,
		requestDeadline: 2 * time.Second,
		maxConnections:  maxConnections,
		maxResponse:     maxResponseBytes,
	}
}

type testServer struct {
	path string
	srv  *server
	done chan error
	ln   *net.UnixListener
}

// startServer serves on a fresh Unix socket until the test ends.
func startServer(t *testing.T, mutate func(*options)) *testServer {
	t.Helper()
	root := testRoot(t)
	o := testOptions(root)
	o.stateDir = shortDir(t)
	if mutate != nil {
		mutate(&o)
	}
	path := filepath.Join(shortDir(t), "s")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	uid, err := o.lookupUID(serviceUser) // the same lookup as in production
	if err != nil {
		t.Fatal(err)
	}
	key := newTestKey(t, "release key")
	h := newHandler(o, key.key)
	h.keyID = testKeyID
	srv := newServer(o, uid, h)
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

func (ts *testServer) dial(t *testing.T) *net.UnixConn {
	t.Helper()
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: ts.path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// exchange sends payload on a new connection and returns everything the
// server sends before it closes. closeWrite shuts down the sending side
// after the payload, which is how a client signals "that was all".
func (ts *testServer) exchange(t *testing.T, payload []byte, closeWrite bool) []byte {
	t.Helper()
	c := ts.dial(t)
	c.SetDeadline(time.Now().Add(10 * time.Second))
	c.Write(payload) // may fail for an oversize payload once the server answered
	if closeWrite {
		c.CloseWrite()
	}
	got, err := io.ReadAll(c)
	if err != nil && !errors.Is(err, io.EOF) && len(got) == 0 {
		return nil // reset without an answer
	}
	return got
}

// reply is the decoded form of any answer.
type reply struct {
	V       int    `json:"v"`
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
	Message string `json:"message"`

	Differs []string `json:"differs"`

	// stage
	Version          string `json:"version"`
	Commit           string `json:"commit"`
	ComponentsDiffer bool   `json:"components_differ"`

	// status
	ProtocolMin      int          `json:"protocol_min"`
	ProtocolMax      int          `json:"protocol_max"`
	HelperVersion    string       `json:"helper_version"`
	KeyID            string       `json:"key_id"`
	InstalledVersion string       `json:"installed_version"`
	Phase            string       `json:"phase"`
	StagedVersion    string       `json:"staged_version"`
	PreviousVersion  string       `json:"previous_version"`
	LastResult       *lastResult2 `json:"last_result"`
	NeedsAttention   bool         `json:"needs_attention"`
	FreeBytes        uint64       `json:"free_bytes"`

	CooldownVersion      string   `json:"cooldown_version"`
	CooldownRemainingS   int64    `json:"cooldown_remaining_s"`
	StagedDiffers        []string `json:"staged_differs"`
	SocketRestartPending bool     `json:"socket_restart_pending"`
	TransactionID        string   `json:"transaction_id"`
	Status               string   `json:"status"`
}

type lastResult2 struct {
	Outcome       string `json:"outcome"`
	ReasonCode    string `json:"reason_code"`
	To            string `json:"to"`
	From          string `json:"from"`
	CorrelationID string `json:"correlation_id"`
	TransactionID string `json:"transaction_id"`
	Acknowledged  bool   `json:"acknowledged"`
	FinishedAt    string `json:"finished_at"`

	ComponentsUpdated bool `json:"components_updated"`
}

// parseReply requires exactly one newline-terminated JSON object.
func parseReply(t *testing.T, raw []byte) reply {
	t.Helper()
	if len(raw) == 0 {
		t.Fatal("no answer")
	}
	if raw[len(raw)-1] != '\n' || bytes.Count(raw, []byte("\n")) != 1 {
		t.Fatalf("answer is not one newline-terminated line: %q", raw)
	}
	if len(raw) > maxResponseBytes {
		t.Fatalf("answer of %d bytes exceeds %d", len(raw), maxResponseBytes)
	}
	var r reply
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("answer %q: %v", raw, err)
	}
	return r
}

func wantError(t *testing.T, raw []byte, code errorCode) {
	t.Helper()
	r := parseReply(t, raw)
	if r.OK || r.Error != string(code) || r.Message != errorTexts[code] || r.V != 1 {
		t.Fatalf("answer %q, want error %s with its fixed text", raw, code)
	}
	if strings.Contains(string(raw), marker) {
		t.Fatalf("the answer echoes the request: %q", raw)
	}
}

func padded(total int) []byte {
	const req = `{"v":1,"op":"status"}`
	return []byte(req + strings.Repeat(" ", total-len(req)-1) + "\n")
}
