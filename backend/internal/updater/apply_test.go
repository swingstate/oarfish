package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	v6 = "0.1.0-alpha.6"
	v9 = "0.1.0-alpha.9"
)

func bundleName(version string) string { return "oarfish_" + version + "_linux_arm64.tar.gz" }

// ready is an environment with one newer release, checked.
func ready(t *testing.T, o relOpts) (*env, relFiles) {
	t.Helper()
	e := newEnv(t, v6)
	rf := e.publish(v9, o, "notes")
	if st := e.check(false); st.Available == nil || !st.Available.Verified {
		t.Fatalf("the release was not verified: %+v", st.Available)
	}
	return e, rf
}

func (e *env) interceptBundle(f func(w http.ResponseWriter, r *http.Request)) {
	e.gh.mu.Lock()
	e.gh.intercept = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/"+bundleName(v9)) {
			f(w, r)
			return true
		}
		return false
	}
	e.gh.mu.Unlock()
}

func TestApplyDownloadsVerifiesStagesAndActivates(t *testing.T) {
	e, rf := ready(t, relOpts{})
	e.helper.stage = func(_ context.Context, version, _ string) (StageInfo, *Error) {
		dir := filepath.Join(e.cache, "update", version)
		if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o755 {
			t.Errorf("cache directory: %v %v", fi, err)
		}
		for name, want := range rf.files() {
			p := filepath.Join(dir, name)
			fi, err := os.Stat(p)
			if err != nil || fi.Mode().Perm() != 0o644 {
				t.Errorf("%s: %v %v", name, fi, err)
				continue
			}
			got, _ := os.ReadFile(p)
			if string(got) != string(want) {
				t.Errorf("%s differs from the release", name)
			}
		}
		if names := listDir(t, dir); len(names) != 4 {
			t.Errorf("cache holds %v", names)
		}
		return StageInfo{Version: version}, nil
	}
	if err := e.apply(v9); err != nil {
		t.Fatal(err)
	}
	if err := e.jobErr(); err != nil {
		t.Fatalf("job failed: %v", err)
	}
	if got := e.helper.nonStatus(); got != "stage "+v9+",activate "+v9 {
		t.Fatalf("helper calls: %s", got)
	}
	for _, c := range e.helper.cids {
		if c != "0123456789abcdef" {
			t.Errorf("correlation id %q", c)
		}
	}
	if e.helper.confirmed[0] {
		t.Error("components_confirmed was set")
	}
	// The cache is empty after the stage.
	if names := listDir(t, filepath.Join(e.cache, "update")); len(names) != 0 {
		t.Errorf("cache keeps %v", names)
	}
	// update_started is the only entry: the finishing one comes from the helper's result.
	evs := e.audits()
	if len(evs) != 1 || evs[0].Action != "update_started" || evs[0].User != "admin" || evs[0].Target != "update/"+v9 ||
		evs[0].Result != "success" || evs[0].CorrelationID != "0123456789abcdef" {
		t.Fatalf("%+v", evs)
	}
	if j := e.u.State(context.Background()).Job; j == nil || j.Running || j.Step != "done" || j.Error != nil {
		t.Fatalf("%+v", j)
	}
}

func TestApplyOnlyTheVersionOfTheLastCheck(t *testing.T) {
	e := newEnv(t, v6)
	if _, err := e.u.Reserve(); err != nil {
		t.Fatal(err)
	}
	e.u.mu.Lock()
	e.u.reserved = false
	e.u.mu.Unlock()
	if err := e.u.Applicable(v9); err == nil || err.Code != CodeNotChecked {
		t.Fatalf("%v", err)
	}
	e.publish(v9, relOpts{}, "")
	e.check(false)
	for _, v := range []string{"0.1.0-alpha.10", "9.9.9", "0.1.0-alpha.9.1", ""} {
		if err := e.u.Applicable(v); err == nil || err.Code != CodeVersionMismatch {
			t.Errorf("%q: %v", v, err)
		}
	}
	if err := e.u.Applicable(v9); err != nil {
		t.Fatal(err)
	}
	// Start re-checks it: the slot cannot be turned into a job for another version.
	slot, _ := e.u.Reserve()
	if err := slot.Start(ApplyRequest{Version: "9.9.9", User: "admin", CorrelationID: "0123456789abcdef"}); err == nil || err.Code != CodeVersionMismatch {
		t.Fatalf("%v", err)
	}
	if _, err := e.u.Reserve(); err != nil {
		t.Fatalf("a refused Start did not give the slot back: %v", err)
	}
}

func TestDownloadLimitsAndTamper(t *testing.T) {
	cases := []struct {
		name string
		do   func(e *env, rf relFiles)
		want Code
		reqs int // bundle requests
	}{
		{"more bytes than signed, no Content-Length", func(e *env, rf relFiles) {
			e.interceptBundle(func(w http.ResponseWriter, r *http.Request) {
				w.Write(rf.Bundle)
				w.Write(make([]byte, 5000))
			})
		}, CodeTooLarge, 1},
		{"Content-Length above the signed size", func(e *env, rf relFiles) {
			e.interceptBundle(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", "99999999")
				w.WriteHeader(200)
			})
		}, CodeTooLarge, 1},
		{"other bytes of the signed size", func(e *env, rf relFiles) {
			e.interceptBundle(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(strings.Repeat("Y", len(rf.Bundle)))) })
		}, CodeChecksumMismatch, 1},
		{"shorter than signed", func(e *env, rf relFiles) {
			e.interceptBundle(func(w http.ResponseWriter, r *http.Request) { w.Write(rf.Bundle[:len(rf.Bundle)-1]) })
		}, CodeChecksumMismatch, 1},
		{"not found", func(e *env, rf relFiles) {
			e.interceptBundle(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
		}, "http_404", 1},
		{"rate limited", func(e *env, rf relFiles) {
			e.interceptBundle(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "60"); w.WriteHeader(429) })
		}, CodeRateLimited, 1},
		{"server error twice", func(e *env, rf relFiles) {
			e.interceptBundle(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
		}, "http_503", 2},
		{"cut connection twice", func(e *env, rf relFiles) {
			e.interceptBundle(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", "4096")
				w.Write(rf.Bundle[:100])
				panic(http.ErrAbortHandler)
			})
		}, CodeUnreachable, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, rf := ready(t, relOpts{})
			tc.do(e, rf)
			if err := e.apply(v9); err != nil {
				t.Fatal(err)
			}
			got := e.jobErr()
			if got == nil || got.Code != tc.want {
				t.Fatalf("job error %v, want %s", got, tc.want)
			}
			if n := e.gh.dl(bundleName(v9)); n != tc.reqs {
				t.Errorf("bundle requests = %d, want %d", n, tc.reqs)
			}
			if e.helper.count("stage") != 0 || e.helper.count("activate") != 0 {
				t.Error("the helper was called for a download that failed")
			}
			if names := listDir(t, filepath.Join(e.cache, "update")); len(names) != 0 {
				t.Errorf("a failed download left %v", names)
			}
			evs := e.audits()
			if len(evs) != 2 || evs[1].Action != "update_finished" || evs[1].User != "-" || evs[1].CorrelationID != evs[0].CorrelationID {
				t.Fatalf("%+v", evs)
			}
		})
	}
}

func TestDownloadRetriesOnceAndNeverResumes(t *testing.T) {
	e, rf := ready(t, relOpts{})
	var n int
	var mu sync.Mutex
	e.interceptBundle(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		first := n == 1
		mu.Unlock()
		if first {
			w.Header().Set("Content-Length", "4096")
			w.Write(rf.Bundle[:1000]) // half a download, then the connection breaks
			panic(http.ErrAbortHandler)
		}
		w.Write(rf.Bundle)
	})
	if err := e.apply(v9); err != nil {
		t.Fatal(err)
	}
	if err := e.jobErr(); err != nil {
		t.Fatal(err)
	}
	if e.gh.dl(bundleName(v9)) != 2 {
		t.Fatalf("requests = %d", e.gh.dl(bundleName(v9)))
	}
	e.gh.mu.Lock()
	defer e.gh.mu.Unlock()
	for _, h := range e.gh.dlHdr {
		if h.Get("Range") != "" || h.Get("If-Range") != "" {
			t.Errorf("a download used a range request: %v", h)
		}
	}
}

func TestDownloadStallAndAbsoluteLimit(t *testing.T) {
	t.Run("no bytes for the stall time", func(t *testing.T) {
		e, rf := ready(t, relOpts{})
		e.u.stall = 150 * time.Millisecond
		e.interceptBundle(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "4096")
			w.Write(rf.Bundle[:100])
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		})
		start := time.Now()
		e.apply(v9)
		if err := e.jobErr(); err == nil || err.Code != CodeTimeout {
			t.Fatalf("%v", err)
		}
		if time.Since(start) > 10*time.Second {
			t.Fatal("the stall was not cut")
		}
		if names := listDir(t, filepath.Join(e.cache, "update")); len(names) != 0 {
			t.Errorf("left %v", names)
		}
	})
	t.Run("slow but steady hits the absolute limit", func(t *testing.T) {
		e, rf := ready(t, relOpts{bundleSize: 4096})
		e.u.stall, e.u.abs = 5*time.Second, 300*time.Millisecond
		e.interceptBundle(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "4096")
			for i := 0; i < len(rf.Bundle); i++ {
				if _, err := w.Write(rf.Bundle[i : i+1]); err != nil {
					return
				}
				w.(http.Flusher).Flush()
				time.Sleep(40 * time.Millisecond)
			}
		})
		e.apply(v9)
		if err := e.jobErr(); err == nil || err.Code != CodeTimeout {
			t.Fatalf("%v", err)
		}
	})
}

// U12: the download is streamed, never held in memory. A buffered read of the
// 24 MB body alone would allocate at least that much.
func TestTheBundleIsStreamedNotBuffered(t *testing.T) {
	const size = 24 << 20
	e, _ := ready(t, relOpts{bundleSize: size})
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if err := e.apply(v9); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if err := e.jobErr(); err != nil {
		t.Fatal(err)
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown > size/2 {
		t.Fatalf("a %d MB download allocated %d MB", size>>20, grown>>20)
	}
}

func TestDownloadRedirectsFollowTheAllowList(t *testing.T) {
	t.Run("to a second allowed host", func(t *testing.T) {
		e, rf := ready(t, relOpts{})
		cdn := newFakeGH(t)
		cdn.intercept = func(w http.ResponseWriter, r *http.Request) bool { w.Write(rf.Bundle); return true }
		e.allowHosts(mustHost(t, e.gh.srv.URL), mustHost(t, cdn.srv.URL))
		e.u.gh.hc.Transport.(*http.Transport).TLSClientConfig.RootCAs.AddCert(cdn.srv.Certificate())
		e.interceptBundle(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, cdn.srv.URL+"/asset", http.StatusFound)
		})
		e.apply(v9)
		if err := e.jobErr(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("to a host that is not allowed", func(t *testing.T) {
		e, rf := ready(t, relOpts{})
		evil := newFakeGH(t)
		evil.intercept = func(w http.ResponseWriter, r *http.Request) bool { w.Write(rf.Bundle); return true }
		e.interceptBundle(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, evil.srv.URL+"/asset", http.StatusFound)
		})
		e.apply(v9)
		if err := e.jobErr(); err == nil || err.Code != CodeRedirectRefused {
			t.Fatalf("%v", err)
		}
		evil.mu.Lock()
		defer evil.mu.Unlock()
		if evil.apiReqs+len(evil.dlHdr) != 0 {
			t.Error("the request reached the host that is not allowed")
		}
	})
}

// The small files are checked before the bundle is fetched, so a bad release
// costs no bundle download (U3).
func TestABadReleaseCostsNoBundleDownload(t *testing.T) {
	e, _ := ready(t, relOpts{})
	// The release changes after the check: the signature no longer matches.
	e.gh.mu.Lock()
	rf := e.gh.files[v9]
	rf.Sig = append([]byte(nil), rf.Sig...)
	rf.Sig[70] ^= 1
	e.gh.files[v9] = rf
	e.gh.mu.Unlock()
	e.apply(v9)
	if err := e.jobErr(); err == nil || err.Code != CodeSignatureInvalid {
		t.Fatalf("%v", err)
	}
	if e.gh.dl(bundleName(v9)) != 0 {
		t.Fatal("the bundle was downloaded")
	}
}

// ---- the helper's errors ---------------------------------------------------------------

func TestHelperErrorsAreMappedToTypedErrors(t *testing.T) {
	codes := []Code{CodeBadRequest, CodeBusy, CodeNotFound, CodeSignatureInvalid, CodeChecksumMismatch, CodeManifestInvalid,
		CodeDowngradeRefused, CodeAlreadyInstalled, CodeRequirementsNotMet, CodeHelperTooOld, CodeKeyChanged, CodeUnitRejected,
		CodeComponentsDamaged, CodeStageExpired, CodeNoSpace, CodeCurrentUnhealthy, CodeCooldown, CodeIOError, CodeInternal, CodeUnavailable, CodeTimeout}
	for _, c := range codes {
		for _, where := range []string{"stage", "activate"} {
			e, _ := ready(t, relOpts{})
			if where == "stage" {
				e.helper.stage = func(context.Context, string, string) (StageInfo, *Error) { return StageInfo{}, fail(c) }
			} else {
				e.helper.activateErr = fail(c)
			}
			e.apply(v9)
			got := e.jobErr()
			if got == nil || got.Code != c {
				t.Fatalf("%s at %s: %v", c, where, got)
			}
			if got.Error() != string(c)+": "+Text(c) {
				t.Errorf("error text %q", got.Error())
			}
			// A refusal of activate discards the staged release (ADR 0010 A17, F2); a
			// failed call does not, because the helper may have taken the request.
			if where == "activate" {
				want := 1
				if c == CodeUnavailable || c == CodeTimeout {
					want = 0
				}
				if n := e.helper.count("discard"); n != want {
					t.Errorf("%s at activate: %d discards, want %d", c, n, want)
				}
			}
		}
	}
}

func TestInstallerRequiredCarriesTheFixedInstruction(t *testing.T) {
	e, _ := ready(t, relOpts{})
	e.helper.stage = func(context.Context, string, string) (StageInfo, *Error) {
		return StageInfo{}, &Error{Code: CodeInstallerRequired, Differs: []string{"oarfish-update-helper", "oarfish.service"}}
	}
	e.apply(v9)
	st := e.u.State(context.Background())
	in := st.Installer
	if in == nil || !in.Required || in.Version != v9 || in.Instruction != InstallerInstruction ||
		strings.Join(in.Differs, ",") != "oarfish-update-helper,oarfish.service" {
		t.Fatalf("%+v", in)
	}
	if st.Job == nil || st.Job.Error == nil || st.Job.Error.Code != CodeInstallerRequired {
		t.Fatalf("%+v", st.Job)
	}
	// Nothing built from outside data: no command, no URL, no path in the text.
	for _, bad := range []string{"http", "curl", "sh install.sh", "/", "$", "`"} {
		if strings.Contains(InstallerInstruction, bad) {
			t.Errorf("the instruction contains %q", bad)
		}
	}
	if e.helper.count("activate") != 0 {
		t.Error("activate ran")
	}
}

// ---- the job model ------------------------------------------------------------------------

func TestOneJobAtATimeAndTheStepsAreShown(t *testing.T) {
	e, _ := ready(t, relOpts{})
	in, release := make(chan struct{}), make(chan struct{})
	e.helper.stage = func(ctx context.Context, v, _ string) (StageInfo, *Error) {
		close(in)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return StageInfo{Version: v}, nil
	}
	steps := []string{}
	var mu sync.Mutex
	note := func() {
		e.u.mu.Lock()
		defer e.u.mu.Unlock()
		mu.Lock()
		steps = append(steps, string(e.u.job.step))
		mu.Unlock()
	}
	e.interceptBundle(func(w http.ResponseWriter, r *http.Request) { note(); w.Write(e.gh.files[v9].Bundle) })

	slot, err := e.u.Reserve()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.u.Reserve(); err == nil || err.Code != CodeBusy {
		t.Fatalf("a second reservation: %v", err)
	}
	if err := slot.Start(ApplyRequest{Version: v9, User: "admin", CorrelationID: "0123456789abcdef"}); err != nil {
		t.Fatal(err)
	}
	<-in
	st := e.u.State(context.Background())
	if st.Job == nil || !st.Job.Running || st.Job.Step != "staging" || st.Job.Kind != "apply" || st.Job.Version != v9 {
		t.Fatalf("%+v", st.Job)
	}
	if _, err := e.u.Reserve(); err == nil || err.Code != CodeBusy {
		t.Fatalf("second apply: %v", err)
	}
	if _, err := e.u.Check(context.Background(), true); err == nil || err.Code != CodeBusy {
		t.Fatalf("check during a job: %v", err)
	}
	if err := e.u.Discard(context.Background(), v9, "0123456789abcdef"); err == nil || err.Code != CodeBusy {
		t.Fatalf("discard during a job: %v", err)
	}
	if n := e.u.Notice(context.Background()); n.Available == nil {
		t.Fatal("the notice does not answer from memory during a job")
	}
	close(release)
	e.settle()
	st = e.u.State(context.Background())
	if st.Job.Running || st.Job.Step != "done" || st.Job.Error != nil {
		t.Fatalf("%+v", st.Job)
	}
	if strings.Join(steps, ",") != "downloading" {
		t.Errorf("steps at the bundle download: %v", steps)
	}
	// And the next job may start.
	if _, err := e.u.Reserve(); err != nil {
		t.Fatalf("after the job: %v", err)
	}
}

// A11.3: the check handler waits only a short bound; the job goes on and the
// page polls. The caller's context ending (the browser went away) does not stop it.
func TestCheckReturnsAfterTheBoundAndTheJobContinues(t *testing.T) {
	e := newEnv(t, v6)
	e.publish(v9, relOpts{}, "")
	release := make(chan struct{})
	e.gh.mu.Lock()
	e.gh.intercept = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/repos/swingstate/oarfish/releases" {
			<-release
		}
		return false
	}
	e.gh.mu.Unlock()
	e.u.bound = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	start := time.Now()
	st, err := e.u.Check(ctx, false)
	cancel()
	if err != nil || time.Since(start) > 2*time.Second || st.Job == nil || !st.Job.Running || st.Job.Step != "checking" {
		t.Fatalf("%v %+v after %v", err, st.Job, time.Since(start))
	}
	close(release)
	e.settle()
	if st := e.u.State(context.Background()); st.Available == nil {
		t.Fatal("the job did not finish after the caller left")
	}
}

// A restart before stage ends the job harmlessly: nothing privileged happened.
func TestShutdownDuringAJobIsHarmless(t *testing.T) {
	e, _ := ready(t, relOpts{})
	in := make(chan struct{})
	e.helper.stage = func(ctx context.Context, v, _ string) (StageInfo, *Error) {
		close(in)
		<-ctx.Done()
		return StageInfo{}, fail(CodeCancelled)
	}
	slot, _ := e.u.Reserve()
	if err := slot.Start(ApplyRequest{Version: v9, User: "admin", CorrelationID: "0123456789abcdef"}); err != nil {
		t.Fatal(err)
	}
	<-in
	e.u.Close()
	if e.helper.count("activate") != 0 || e.helper.count("discard") != 0 {
		t.Fatalf("calls: %v", e.helper.calls)
	}
	if names := listDir(t, filepath.Join(e.cache, "update")); len(names) != 0 {
		t.Errorf("left %v", names)
	}
	for _, ev := range e.audits() {
		if ev.Action == "update_finished" {
			t.Errorf("a shutdown wrote a finishing entry: %+v", ev)
		}
	}
	// A new process has no job and clears what a crash would have left.
	leftover := filepath.Join(e.cache, "update", v9)
	if err := os.MkdirAll(leftover, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(leftover, bundleName(v9)+".part"), []byte("half"), 0o644)
	e2 := newEnv(t, v6)
	e2.u.cfg.CacheDir = e.cache
	e2.u.CleanCache()
	if _, err := os.Stat(filepath.Join(e.cache, "update")); !os.IsNotExist(err) {
		t.Error("the cache was not cleaned")
	}
	if st := e2.u.State(context.Background()); st.Job != nil {
		t.Errorf("%+v", st.Job)
	}
}

func TestComponentUpdatePausesForTheSecondConfirmation(t *testing.T) {
	differ := func(e *env) {
		e.helper.stage = func(_ context.Context, v, _ string) (StageInfo, *Error) {
			return StageInfo{Version: v, ComponentsDiffer: true}, nil
		}
	}
	waitFor := func(e *env, step string) {
		t.Helper()
		for i := 0; i < 2000; i++ {
			if j := e.u.State(context.Background()).Job; j != nil && j.Step == step {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Fatalf("never reached %s", step)
	}
	start := func(e *env) {
		slot, _ := e.u.Reserve()
		if err := slot.Start(ApplyRequest{Version: v9, User: "admin", CorrelationID: "0123456789abcdef"}); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("resume sends activate with components_confirmed", func(t *testing.T) {
		e, _ := ready(t, relOpts{})
		differ(e)
		start(e)
		waitFor(e, "awaiting_components_confirmation")
		if e.helper.count("activate") != 0 {
			t.Fatal("activate ran before the second confirmation")
		}
		if !e.u.Awaiting(v9) || e.u.Awaiting("0.0.1") {
			t.Fatal("Awaiting")
		}
		if err := e.u.Resume("0.1.0-alpha.10"); err == nil || err.Code != CodeNotFound {
			t.Fatalf("%v", err)
		}
		if err := e.u.Resume(v9); err != nil {
			t.Fatal(err)
		}
		e.settle()
		if e.jobErr() != nil || len(e.helper.confirmed) != 1 || !e.helper.confirmed[0] {
			t.Fatalf("%v %v", e.jobErr(), e.helper.confirmed)
		}
	})
	t.Run("a job never starts confirmed", func(t *testing.T) {
		// ADR 0010 A17, S7: ApplyRequest has no confirmation. A job whose stage
		// reports a difference always pauses, and only Resume lets it go on.
		e, _ := ready(t, relOpts{})
		differ(e)
		start(e)
		waitFor(e, "awaiting_components_confirmation")
		if e.helper.count("activate") != 0 || len(e.helper.confirmed) != 0 {
			t.Fatalf("activate reached before Resume: %v", e.helper.calls)
		}
		if evs := e.audits(); len(evs) != 1 || evs[0].Action != "update_started" {
			t.Fatalf("%+v", evs)
		}
		if err := e.u.Resume(v9); err != nil {
			t.Fatal(err)
		}
		e.settle()
		if e.jobErr() != nil || len(e.helper.confirmed) != 1 || !e.helper.confirmed[0] {
			t.Fatalf("%v %v", e.jobErr(), e.helper.confirmed)
		}
		if evs := e.audits(); len(evs) != 1 || evs[0].Action != "update_started" {
			t.Fatalf("%+v", evs)
		}
	})
	t.Run("discard ends the wait", func(t *testing.T) {
		e, _ := ready(t, relOpts{})
		differ(e)
		start(e)
		waitFor(e, "awaiting_components_confirmation")
		if err := e.u.Discard(context.Background(), v9, "fedcba9876543210"); err != nil {
			t.Fatal(err)
		}
		e.settle()
		if err := e.jobErr(); err == nil || err.Code != CodeCancelled {
			t.Fatalf("%v", err)
		}
		if e.helper.count("discard "+v9) != 1 || e.helper.count("activate") != 0 {
			t.Fatalf("%v", e.helper.calls)
		}
		var fin []string
		for _, ev := range e.audits() {
			if ev.Action == "update_finished" {
				fin = append(fin, string(ev.Result)+" "+ev.CorrelationID)
			}
		}
		if len(fin) != 1 || fin[0] != "aborted fedcba9876543210" {
			t.Fatalf("finishing entries: %v", fin)
		}
	})
	t.Run("the staged release expires after the pause", func(t *testing.T) {
		e, _ := ready(t, relOpts{})
		e.u.pause = 50 * time.Millisecond
		differ(e)
		start(e)
		e.settle()
		if err := e.jobErr(); err == nil || err.Code != CodeStageExpired {
			t.Fatalf("%v", err)
		}
		if e.helper.count("discard "+v9) != 1 {
			t.Fatalf("%v", e.helper.calls)
		}
	})
}

// ---- audit (ADR 0009 A6) ---------------------------------------------------------------------

func TestReconcileWritesTheFinishingEntryOnceAndAcknowledges(t *testing.T) {
	e := newEnv(t, v9)
	e.helper.status.LastResult = &HelperResult{TransactionID: "00112233445566ff", To: v9, From: v6, Outcome: "rolled_back",
		ReasonCode: "health_timeout", CorrelationID: "0123456789abcdef", FinishedAt: "2026-11-06T10:00:00Z"}
	st := e.u.State(context.Background())
	if st.LastResult == nil || st.LastResult.Outcome != "rolled_back" || st.LastResult.ReasonCode != "health_timeout" {
		t.Fatalf("%+v", st.LastResult)
	}
	e.u.State(context.Background())
	e.u.State(context.Background())
	evs := e.audits()
	if len(evs) != 1 {
		t.Fatalf("%+v", evs)
	}
	ev := evs[0]
	if ev.Action != "update_finished" || ev.User != "-" || ev.Target != "update/"+v9 || ev.Result != "rolled_back" || ev.CorrelationID != "0123456789abcdef" {
		t.Fatalf("%+v", ev)
	}
	if e.helper.count("ack 00112233445566ff") != 1 {
		t.Fatalf("acks: %v", e.helper.calls)
	}
	// A failed ack is retried; the entry is not written twice.
	e2 := newEnv(t, v9)
	e2.helper.status.LastResult = e.helper.status.LastResult
	e2.helper.ackErr = fail(CodeIOError)
	e2.u.State(context.Background())
	e2.u.State(context.Background())
	e2.helper.mu.Lock()
	e2.helper.ackErr = nil
	e2.helper.mu.Unlock()
	e2.u.State(context.Background())
	e2.u.State(context.Background())
	if len(e2.audits()) != 1 || e2.helper.count("ack") != 3 {
		t.Fatalf("entries %d, acks %d", len(e2.audits()), e2.helper.count("ack"))
	}
	// An acknowledged result is not audited again.
	e3 := newEnv(t, v9)
	r := *e.helper.status.LastResult
	r.Acknowledged = true
	e3.helper.status.LastResult = &r
	e3.u.State(context.Background())
	if len(e3.audits()) != 0 || e3.helper.count("ack") != 0 {
		t.Fatal("an acknowledged result was handled again")
	}
}

func TestOutcomesMapToAuditResults(t *testing.T) {
	for outcome, want := range map[string]string{"success": "success", "rolled_back": "rolled_back", "components_rolled_back": "components_rolled_back",
		"rollback_failed": "rollback_failed", "aborted": "aborted"} {
		e := newEnv(t, v9)
		e.helper.status.LastResult = &HelperResult{TransactionID: "00112233445566ff", To: v9, Outcome: outcome, CorrelationID: "0123456789abcdef"}
		e.u.State(context.Background())
		if evs := e.audits(); len(evs) != 1 || string(evs[0].Result) != want {
			t.Errorf("%s: %+v", outcome, evs)
		}
	}
}

func TestStatusAnswerWithoutAHelper(t *testing.T) {
	e := newEnv(t, v6)
	e.u.cfg.Helper = nil
	st := e.u.State(context.Background())
	if st.Helper.Present || st.Helper.Error == nil || st.Helper.Error.Code != CodeUnavailable {
		t.Fatalf("%+v", st.Helper)
	}
	e2 := newEnv(t, v6)
	e2.helper.statusErr = fail(CodeUnavailable)
	if st := e2.u.State(context.Background()); st.Helper.Present {
		t.Fatalf("%+v", st.Helper)
	}
}

func TestHashOfAFileInTheCacheIsTheSignedOne(t *testing.T) {
	// The bundle that reaches the helper is the signed one: its hash is the listed one.
	e, rf := ready(t, relOpts{})
	var got string
	e.helper.stage = func(_ context.Context, v string, _ string) (StageInfo, *Error) {
		b, _ := os.ReadFile(filepath.Join(e.cache, "update", v, bundleName(v)))
		s := sha256.Sum256(b)
		got = hex.EncodeToString(s[:])
		return StageInfo{Version: v}, nil
	}
	e.apply(v9)
	s := sha256.Sum256(rf.Bundle)
	if got != hex.EncodeToString(s[:]) {
		t.Fatal("the cached bundle is not the signed one")
	}
	_ = io.Discard
}
