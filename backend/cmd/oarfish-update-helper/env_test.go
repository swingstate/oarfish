package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// tenv is a complete helper environment in temporary directories: an installed
// release 1.0.0 with a component set, a state directory, a cache, a throwaway
// key and fake clocks. Requests go to h.respond; the socket-level tests use
// startServer.
type tenv struct {
	t        *testing.T
	root     string
	stateDir string
	cacheDir string
	key      testKey
	h        *handler
	fs       *testFS
	journal  *syncBuffer

	uptime atomic.Int64
	mono   atomic.Int64 // nanoseconds
	ids    atomic.Int64

	installed string
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

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

const (
	installedVersionString = "1.0.0"
	freeSpaceForTests      = 1 << 40
)

// newTenv builds the environment. Nothing is staged and the state file does not
// exist yet.
func newTenv(t *testing.T) *tenv {
	t.Helper()
	v := &tenv{t: t, installed: installedVersionString, journal: &syncBuffer{}}
	v.root, v.stateDir, v.cacheDir = shortDir(t), shortDir(t), shortDir(t)
	v.key = newTestKey(t, "release key")

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(v.root, "releases", v.installed), 0o755))
	must(os.WriteFile(filepath.Join(v.root, "releases", v.installed, "oarfish.service"), baseServiceUnit, 0o644))
	must(os.Symlink("releases/"+v.installed, filepath.Join(v.root, "current")))
	must(os.MkdirAll(filepath.Join(v.root, "components", v.installed), 0o755))
	for n, d := range baseComponents {
		must(os.WriteFile(filepath.Join(v.root, "components", v.installed, n), d, 0o755))
	}
	must(os.Symlink(v.installed, filepath.Join(v.root, "components", "active")))
	must(os.Symlink(v.installed, filepath.Join(v.root, "components", "known-good")))
	must(os.Chmod(v.stateDir, 0o700))
	must(os.MkdirAll(filepath.Join(v.cacheDir, "update"), 0o755))

	o := testOptions(v.root)
	o.stateDir, o.cacheDir, o.arch, o.journal = v.stateDir, v.cacheDir, "arm64", v.journal
	v.h = newHandler(o, v.key.key)
	v.h.keyID = testKeyID
	v.uptime.Store(1000)
	v.h.uptime = func() (int64, error) { return v.uptime.Load(), nil }
	v.h.monotonic = func() time.Duration { return time.Duration(v.mono.Load()) }
	v.h.wall = func() time.Time { return time.Unix(1_700_000_000, 0) }
	v.h.newID = func() (string, error) { return fmt.Sprintf("%016x", 0xa0000+v.ids.Add(1)), nil }
	v.h.freeBytes = func(*os.File) (uint64, error) { return freeSpaceForTests, nil }
	v.fs = nil
	v.h.wrapFS = func(inner fsops) fsops {
		v.fs = newTestFS(inner)
		return v.fs
	}
	return v
}

// release builds a signed components-shape release of a version.
func (v *tenv) release(version string) *release {
	v.t.Helper()
	return newRelease(v.t, v.key, version, true)
}

// place writes the four files of a build into the cache as the service would
// (directory 0755, files 0644).
func (v *tenv) place(version string, b built) {
	v.t.Helper()
	dir := filepath.Join(v.cacheDir, "update", version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		v.t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		nameChecksums: b.Checksums, nameSignature: b.Sig, nameManifest: b.Manifest, b.BundleName: b.Bundle,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			v.t.Fatal(err)
		}
	}
}

// stageable builds, places and returns a release of a version.
func (v *tenv) stageable(version string) (*release, built) {
	r := v.release(version)
	b := r.build()
	v.place(version, b)
	return r, b
}

const testCID = "0123456789abcdef"

func stageLine(version, cid string) []byte {
	return []byte(fmt.Sprintf(`{"v":1,"op":"stage","version":%q,"correlation_id":%q}`+"\n", version, cid))
}

func discardLine(version string) []byte {
	return []byte(fmt.Sprintf(`{"v":1,"op":"discard","version":%q}`+"\n", version))
}

// request sends a request line to the handler and returns the answer. The line
// goes through readRequest like on the socket, minus the newline.
func (v *tenv) request(line []byte) []byte {
	v.t.Helper()
	l, err := readRequest(bytes.NewReader(line))
	if err != nil {
		v.t.Fatalf("request line: %v", err)
	}
	return v.h.respond(l)
}

func (v *tenv) stage(version string) []byte { return v.request(stageLine(version, testCID)) }

func (v *tenv) status() reply { return parseReply(v.t, v.request([]byte(okStatus))) }

// stateFile reads the state file; ok false means it does not exist.
func (v *tenv) readState() (*state, bool) {
	v.t.Helper()
	data, err := os.ReadFile(filepath.Join(v.stateDir, stateFile))
	if os.IsNotExist(err) {
		return nil, false
	}
	if err != nil {
		v.t.Fatal(err)
	}
	st, err := decodeState(data)
	if err != nil {
		v.t.Fatalf("state.json: %v\n%s", err, data)
	}
	return st, true
}

func (v *tenv) writeState(st *state) {
	v.t.Helper()
	if err := os.WriteFile(filepath.Join(v.stateDir, stateFile), encodeState(st), 0o600); err != nil {
		v.t.Fatal(err)
	}
}

// phase is the phase of the state file ("idle" when there is none).
func (v *tenv) phase() string {
	if st, ok := v.readState(); ok {
		return st.Txn.Phase
	}
	return phaseIdle
}

// releasesEntries lists the names below releases/.
func (v *tenv) releasesEntries() []string {
	v.t.Helper()
	ents, err := os.ReadDir(filepath.Join(v.root, "releases"))
	if err != nil {
		v.t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

// wantClean fails when anything but the installed release is below releases/.
func (v *tenv) wantClean() {
	v.t.Helper()
	if got := v.releasesEntries(); len(got) != 1 || got[0] != v.installed {
		v.t.Fatalf("releases/ holds %v, want only %s", got, v.installed)
	}
}

// wantErr checks that raw is the error answer for a code.
func wantErrCode(t *testing.T, raw []byte, code errorCode) reply {
	t.Helper()
	r := parseReply(t, raw)
	if r.OK || r.Error != string(code) || r.Message != errorTexts[code] {
		t.Fatalf("answer %s, want error %s", raw, code)
	}
	return r
}

func wantOK(t *testing.T, raw []byte) reply {
	t.Helper()
	r := parseReply(t, raw)
	if !r.OK {
		t.Fatalf("answer %s, want ok", raw)
	}
	return r
}

func mustJSON(t testing.TB, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
