package updater

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/swingstate/oarfish/backend/internal/audit"
	"github.com/swingstate/oarfish/backend/internal/clock"
	"github.com/swingstate/oarfish/backend/internal/clock/clocktest"
	"github.com/swingstate/oarfish/backend/internal/relverify"
	"github.com/swingstate/oarfish/backend/internal/trustkey"
)

// ---- throwaway signing key and releases ----------------------------------------

type testKey struct {
	priv ed25519.PrivateKey
	id   [8]byte
	key  trustkey.Key
}

func newTestKey(t testing.TB, label string) testKey {
	t.Helper()
	seed := sha256.Sum256([]byte("updater test key seed " + label))
	idSrc := sha256.Sum256([]byte("updater test key id " + label))
	var id [8]byte
	copy(id[:], idSrc[:8])
	priv := ed25519.NewKeyFromSeed(seed[:])
	payload := append(append([]byte("Ed"), id[:]...), priv.Public().(ed25519.PublicKey)...)
	file := "untrusted comment: minisign public key test\n" + base64.StdEncoding.EncodeToString(payload) + "\n"
	k, err := trustkey.Parse([]byte(file))
	if err != nil {
		t.Fatal(err)
	}
	return testKey{priv: priv, id: id, key: k}
}

func (k testKey) sign(msg []byte) []byte {
	p := append(append([]byte("Ed"), k.id[:]...), ed25519.Sign(k.priv, msg)...)
	global := make([]byte, 64)
	return []byte("untrusted comment: signature from minisign secret key\n" + base64.StdEncoding.EncodeToString(p) + "\n" +
		"trusted comment: test\n" + base64.StdEncoding.EncodeToString(global) + "\n")
}

// relFiles is a release as the four files a device downloads.
type relFiles struct {
	version                          string
	Checksums, Sig, Manifest, Bundle []byte
}

func (r relFiles) files() map[string][]byte {
	ver, _ := relverify.ParseVersion(r.version)
	return map[string][]byte{
		"checksums.txt": r.Checksums, "checksums.txt.sig": r.Sig, "manifest.json": r.Manifest,
		relverify.BundleFileName(ver): r.Bundle,
	}
}

type relOpts struct {
	arch          string // manifest architecture, default arm64
	bundleSize    int    // default 4096
	minInstalled  string
	helperProto   int
	manifestEdit  func(m map[string]any)
	badSignature  bool
	badChecksum   bool // the manifest is not what checksums.txt lists
	otherKey      *testKey
	bundleDiffers bool // serve other bytes than the signed ones (same size)
}

func mkRelease(t testing.TB, key testKey, version string, o relOpts) relFiles {
	t.Helper()
	if o.arch == "" {
		o.arch = "arm64"
	}
	if o.bundleSize == 0 {
		o.bundleSize = 4096
	}
	root := "oarfish_" + version + "_linux_" + "arm64"
	bundle := bytes.Repeat([]byte("oarfish-bundle-"), o.bundleSize/15+1)[:o.bundleSize]
	files := []struct {
		name string
		mode int
		data []byte
	}{
		{"LICENSE", 0o644, []byte("licence\n")}, {"THIRD_PARTY_NOTICES.txt", 0o644, []byte("notices\n")},
		{"oarfish", 0o755, []byte("binary " + version)}, {"oarfish.service", 0o644, []byte("[Service]\n")},
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	var contents []any
	for _, f := range files {
		contents = append(contents, map[string]any{"path": root + "/" + f.name, "mode": fmt.Sprintf("%04o", f.mode),
			"size": len(f.data), "sha256": relverify.SumHex(f.data)})
	}
	req := map[string]any{"os": "linux", "architecture": o.arch, "cpu": "ARMv8-A 64-bit (aarch64)",
		"libc": "none (static binary, CGO_ENABLED=0)", "runtime_dependencies": []any{}}
	if o.minInstalled != "" {
		req["min_installed_version"] = o.minInstalled
	}
	if o.helperProto != 0 {
		req["helper_protocol"] = o.helperProto
	}
	m := map[string]any{
		"schema_version": 1, "name": "oarfish", "version": version, "tag": "v" + version,
		"prerelease": strings.Contains(version, "-"), "commit": "0123456789abcdef0123456789abcdef01234567",
		"build_date": "2026-11-06T10:00:00Z", "os": "linux", "architecture": o.arch, "requirements": req,
		"bundle":   map[string]any{"file": root + ".tar.gz", "sha256": relverify.SumHex(bundle), "size": len(bundle)},
		"contents": contents,
	}
	if o.manifestEdit != nil {
		o.manifestEdit(m)
	}
	mj, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	mj = append(mj, '\n')
	listed := mj
	if o.badChecksum {
		listed = append([]byte("tampered"), mj...)
	}
	names := map[string][]byte{"manifest.json": listed, root + ".tar.gz": bundle, "install.sh": []byte("#!/bin/sh\n")}
	var ns []string
	for n := range names {
		ns = append(ns, n)
	}
	sort.Strings(ns)
	var sb strings.Builder
	for _, n := range ns {
		fmt.Fprintf(&sb, "%s  %s\n", relverify.SumHex(names[n]), n)
	}
	cs := []byte(sb.String())
	signer := key
	if o.otherKey != nil {
		signer = *o.otherKey
	}
	sig := signer.sign(cs)
	if o.badSignature {
		sig = bytes.Replace(sig, []byte("\n"), []byte("\n"), 1)
		sig[60] ^= 0x01
	}
	served := bundle
	if o.bundleDiffers {
		served = bytes.Repeat([]byte("X"), len(bundle))
	}
	return relFiles{version: version, Checksums: cs, Sig: sig, Manifest: mj, Bundle: served}
}

// ---- the fake GitHub -----------------------------------------------------------

type releaseSpec struct {
	Tag        string
	Prerelease bool
	Draft      bool
	Body       string
}

type fakeGH struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	releases []releaseSpec
	etag     string
	files    map[string]relFiles // by version
	apiReqs  int
	apiHdr   []http.Header
	dlReqs   map[string]int // file name -> requests
	dlHdr    []http.Header
	// intercept, when set, may answer a download or API request itself.
	intercept func(w http.ResponseWriter, r *http.Request) bool
	apiStatus int         // when non-zero, the API answers with it
	apiExtra  http.Header // extra headers of the API answer
}

func newFakeGH(t *testing.T) *fakeGH {
	g := &fakeGH{t: t, files: map[string]relFiles{}, dlReqs: map[string]int{}, etag: `"v1"`}
	g.srv = httptest.NewTLSServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *fakeGH) serve(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	icpt := g.intercept
	if rest, ok := strings.CutPrefix(r.URL.Path, "/swingstate/oarfish/releases/download/v"); ok {
		_, name, _ := strings.Cut(rest, "/")
		g.dlReqs[name]++
		g.dlHdr = append(g.dlHdr, r.Header.Clone())
	}
	g.mu.Unlock()
	if icpt != nil && icpt(w, r) {
		return
	}
	switch {
	case r.URL.Path == "/repos/swingstate/oarfish/releases":
		g.mu.Lock()
		g.apiReqs++
		g.apiHdr = append(g.apiHdr, r.Header.Clone())
		status, extra, etag := g.apiStatus, g.apiExtra, g.etag
		list := append([]releaseSpec(nil), g.releases...)
		g.mu.Unlock()
		for k, v := range extra {
			w.Header()[k] = v
		}
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		if r.Header.Get("If-None-Match") == etag && etag != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		var out []map[string]any
		for _, s := range list {
			out = append(out, map[string]any{"tag_name": s.Tag, "prerelease": s.Prerelease, "draft": s.Draft, "body": s.Body,
				"browser_download_url": "https://evil.invalid/" + s.Tag, "html_url": "https://evil.invalid/x"})
		}
		_ = json.NewEncoder(w).Encode(out)
	case strings.HasPrefix(r.URL.Path, "/swingstate/oarfish/releases/download/v"):
		rest := strings.TrimPrefix(r.URL.Path, "/swingstate/oarfish/releases/download/v")
		ver, name, _ := strings.Cut(rest, "/")
		g.mu.Lock()
		rf, ok := g.files[ver]
		g.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		data, ok := rf.files()[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = w.Write(data)
	default:
		http.NotFound(w, r)
	}
}

func (g *fakeGH) add(rf relFiles, spec releaseSpec) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.files[rf.version] = rf
	g.releases = append(g.releases, spec)
}

func (g *fakeGH) api() int { g.mu.Lock(); defer g.mu.Unlock(); return g.apiReqs }

func (g *fakeGH) dl(name string) int { g.mu.Lock(); defer g.mu.Unlock(); return g.dlReqs[name] }

// ---- the fake helper -------------------------------------------------------------

type fakeHelper struct {
	mu          sync.Mutex
	status      HelperStatus
	statusErr   *Error
	stage       func(ctx context.Context, version, cid string) (StageInfo, *Error)
	activateErr *Error
	discardErr  *Error
	ackErr      *Error
	calls       []string
	cids        []string
	confirmed   []bool
}

func newFakeHelper() *fakeHelper {
	return &fakeHelper{status: HelperStatus{ProtocolMin: 1, ProtocolMax: 1, InstalledVersion: "0.1.0-alpha.6", Phase: "idle"}}
}

func (f *fakeHelper) rec(s string) { f.calls = append(f.calls, s) }

func (f *fakeHelper) Status(context.Context) (HelperStatus, *Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("status")
	return f.status, f.statusErr
}

func (f *fakeHelper) Stage(ctx context.Context, v, cid string) (StageInfo, *Error) {
	f.mu.Lock()
	f.rec("stage " + v)
	f.cids = append(f.cids, cid)
	fn := f.stage
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, v, cid)
	}
	return StageInfo{Version: v}, nil
}

func (f *fakeHelper) Activate(_ context.Context, v, cid string, confirmed bool) (string, *Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("activate " + v)
	f.cids = append(f.cids, cid)
	f.confirmed = append(f.confirmed, confirmed)
	if f.activateErr != nil {
		return "", f.activateErr
	}
	return "00112233445566ff", nil
}

func (f *fakeHelper) Discard(_ context.Context, v string) *Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("discard " + v)
	return f.discardErr
}

func (f *fakeHelper) Ack(_ context.Context, id string) *Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("ack " + id)
	return f.ackErr
}

func (f *fakeHelper) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// ---- the updater under test --------------------------------------------------------

// countingClock counts AfterFunc calls: the updater must never set a timer.
type countingClock struct {
	*clocktest.Fake
	mu     sync.Mutex
	timers int
}

func (c *countingClock) AfterFunc(d time.Duration, f func()) clock.Timer {
	c.mu.Lock()
	c.timers++
	c.mu.Unlock()
	return c.Fake.AfterFunc(d, f)
}

func (c *countingClock) count() int { c.mu.Lock(); defer c.mu.Unlock(); return c.timers }

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

type env struct {
	t      *testing.T
	gh     *fakeGH
	key    testKey
	clk    *countingClock
	helper *fakeHelper
	u      *Updater
	cache  string

	mu     sync.Mutex
	events []audit.Event
	log    *syncBuf
}

func (e *env) audits() []audit.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]audit.Event(nil), e.events...)
}

// newEnv builds an updater that talks to the fake GitHub over TLS with the
// fake's certificate, redirects only to the fake's own host, and does not sleep.
func newEnv(t *testing.T, installed string) *env {
	t.Helper()
	e := &env{t: t, gh: newFakeGH(t), key: newTestKey(t, "release"), helper: newFakeHelper(), cache: t.TempDir(), log: &syncBuf{}}
	e.clk = &countingClock{Fake: clocktest.New()}
	pool := x509.NewCertPool()
	pool.AddCert(e.gh.srv.Certificate())
	e.u = newUpdater(Config{
		Version: installed, CacheDir: e.cache, Helper: e.helper, Clock: e.clk, Arch: "arm64", Key: e.key.key,
		Audit: func(ev audit.Event) { e.mu.Lock(); e.events = append(e.events, ev); e.mu.Unlock() },
		Log:   slog.New(slog.NewTextHandler(e.log, nil)),
	}, pool)
	e.u.gh.apiBase, e.u.gh.dlBase = e.gh.srv.URL, e.gh.srv.URL
	host := mustHost(t, e.gh.srv.URL)
	e.allowHosts(host)
	e.u.sleep = func(context.Context, time.Duration) error { return nil }
	e.u.bound = 10 * time.Second
	t.Cleanup(e.u.Close)
	return e
}

func mustHost(t testing.TB, raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

// allowHosts replaces the production redirect allow-list with exact host:port
// values of the fakes. The production function is tested on its own.
func (e *env) allowHosts(hosts ...string) {
	set := map[string]bool{}
	for _, h := range hosts {
		set[h] = true
	}
	e.u.gh.hc.CheckRedirect = e.u.gh.checkRedirect(func(u *url.URL) bool { return u.Scheme == "https" && set[u.Host] })
}

// publish adds a release (signed with the test key) to the fake GitHub.
func (e *env) publish(version string, o relOpts, notes string) relFiles {
	e.t.Helper()
	rf := mkRelease(e.t, e.key, version, o)
	e.gh.add(rf, releaseSpec{Tag: "v" + version, Prerelease: strings.Contains(version, "-"), Body: notes})
	return rf
}

// settle waits until the current job is done and its goroutine has returned.
// The done flag is set by finishJob before runApply writes the finishing audit
// entry; the job goroutine is in the updater's wait group, so waiting for the
// group is the condition the tests check (the entry is written), not a sleep.
func (e *env) settle() {
	e.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		e.u.mu.Lock()
		j := e.u.job
		running := e.u.reserved || (j != nil && !j.done)
		e.u.mu.Unlock()
		if !running {
			groupDone := make(chan struct{})
			go func() { e.u.wg.Wait(); close(groupDone) }()
			select {
			case <-groupDone:
				return
			case <-time.After(time.Until(deadline)):
			}
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	e.t.Fatal("the job did not finish")
}

// resumeAfterPause waits for the pause of a component update, delivers the
// second confirmation and waits for the job to end.
func (e *env) resumeAfterPause(version string) {
	e.t.Helper()
	for i := 0; i < 5000 && !e.u.Awaiting(version); i++ {
		time.Sleep(2 * time.Millisecond)
	}
	if err := e.u.Resume(version); err != nil {
		e.t.Fatalf("Resume: %v", err)
	}
	e.settle()
}

func (e *env) check(force bool) State {
	e.t.Helper()
	first, err := e.u.Check(context.Background(), force)
	if err != nil {
		e.t.Fatalf("Check: %v", err)
	}
	e.settle()
	st := e.u.State(context.Background())
	st.Check.Throttled = first.Check.Throttled
	return st
}

// apply runs a whole apply job.
func (e *env) apply(version string) *Error {
	e.t.Helper()
	slot, err := e.u.Reserve()
	if err != nil {
		return err
	}
	if err := slot.Start(ApplyRequest{Version: version, User: "admin", CorrelationID: "0123456789abcdef"}); err != nil {
		return err
	}
	e.settle()
	return nil
}

func (e *env) jobErr() *Error {
	e.u.mu.Lock()
	defer e.u.mu.Unlock()
	if e.u.job == nil {
		return nil
	}
	return e.u.job.err
}

// nonStatus is the calls to the helper other than status, which every State
// makes.
func (f *fakeHelper) nonStatus() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if c != "status" {
			out = append(out, c)
		}
	}
	return strings.Join(out, ",")
}
