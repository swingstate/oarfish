package updater

import (
	"context"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCheckPicksTheLatestEligibleRelease(t *testing.T) {
	notes := "notes"
	for _, tc := range []struct {
		installed string
		want      string // "" = nothing offered
		channel   string
	}{
		{"0.1.0-alpha.6", "0.2.0-rc.1", "prerelease"}, // a prerelease device sees prereleases and stable ones (Q7)
		{"0.1.0", "0.1.5", "stable"},                  // a stable device sees stable releases only
		{"0.1.5", "", ""},                             // nothing newer that is eligible
		{"0.3.0-alpha.1", "", ""},
	} {
		e := newEnv(t, tc.installed)
		e.publish("0.1.0-alpha.6", relOpts{}, "old")
		e.publish("0.1.0-alpha.9", relOpts{}, notes)
		e.publish("0.1.5", relOpts{}, "stable")
		e.publish("0.2.0-rc.1", relOpts{}, "candidate")
		// Not offered: a draft, a tag that is not "v" plus a version, a leading zero.
		e.gh.add(relFiles{version: "9.9.9"}, releaseSpec{Tag: "v9.9.9", Draft: true})
		e.gh.add(relFiles{version: "8.8.8"}, releaseSpec{Tag: "8.8.8"})
		e.gh.add(relFiles{version: "7.7.7"}, releaseSpec{Tag: "v07.7.7"})
		e.gh.add(relFiles{version: "6.6.6"}, releaseSpec{Tag: "v6.6.6/../x"})
		st := e.check(false)
		if tc.want == "" {
			if st.Available != nil || st.Check.State != "ok" {
				t.Errorf("installed %s: %+v", tc.installed, st.Available)
			}
			continue
		}
		a := st.Available
		if a == nil || a.Version != tc.want || a.Channel != tc.channel || !a.Verified || a.DownloadSize != 4096 || a.VerifyError != nil {
			t.Errorf("installed %s: %+v", tc.installed, a)
			continue
		}
	}
}

func TestReleaseNotesAreReducedToPlainText(t *testing.T) {
	notes := "Fixes.\r\n\x00<script>alert(1)</script>\x1b[31m red\n" + strings.Repeat("x", 6000)
	e := newEnv(t, "0.1.0-alpha.6")
	e.publish("0.1.0-alpha.9", relOpts{}, notes)
	a := e.check(false).Available
	if a == nil || len(a.Notes) > maxNotesBytes || strings.ContainsAny(a.Notes, "\x00\x1b\r") || !strings.HasPrefix(a.Notes, "Fixes.\n") {
		t.Fatalf("release notes are not plain text: %+v", a)
	}
}

// U3: nothing of the API answer but the tag, the flags and the notes is used. The
// fake puts a hostile browser_download_url into every release; the client must
// build its own URLs, so no request goes there (it would fail on TLS anyway, but
// the request log shows it).
func TestCheckBuildsItsOwnURLs(t *testing.T) {
	e := newEnv(t, "0.1.0-alpha.6")
	e.publish("0.1.0-alpha.9", relOpts{}, "")
	e.check(false)
	e.gh.mu.Lock()
	defer e.gh.mu.Unlock()
	for name := range e.gh.dlReqs {
		if name != "checksums.txt" && name != "checksums.txt.sig" && name != "manifest.json" {
			t.Errorf("the check asked for %q; only the three small files are fetched", name)
		}
	}
	h := e.gh.apiHdr[0]
	if ua := h.Get("User-Agent"); ua != "oarfish/0.1.0-alpha.6" {
		t.Errorf("User-Agent = %q", ua)
	}
	for k := range h {
		switch k {
		case "User-Agent", "Accept", "Accept-Encoding", "If-None-Match", "Connection":
		default:
			t.Errorf("unexpected request header %s", k)
		}
	}
}

func TestETagIsSentAndA304UsesTheCachedList(t *testing.T) {
	e := newEnv(t, "0.1.0-alpha.6")
	e.publish("0.1.0-alpha.9", relOpts{}, "")
	e.check(false)
	e.clk.Advance(2 * time.Hour)
	st := e.check(false)
	if e.gh.api() != 2 {
		t.Fatalf("api requests = %d", e.gh.api())
	}
	if got := e.gh.apiHdr[1].Get("If-None-Match"); got != `"v1"` {
		t.Errorf("If-None-Match = %q", got)
	}
	if st.Available == nil || st.Available.Version != "0.1.0-alpha.9" {
		t.Errorf("a 304 lost the list: %+v", st.Available)
	}
	// The list changes: the next answer is a 200 with the new list.
	e.publish("0.1.0-alpha.10", relOpts{}, "")
	e.gh.mu.Lock()
	e.gh.etag = `"v2"`
	e.gh.mu.Unlock()
	e.clk.Advance(2 * time.Hour)
	if st := e.check(false); st.Available == nil || st.Available.Version != "0.1.0-alpha.10" {
		t.Errorf("%+v", st.Available)
	}
}

// U3: the update page checks at most once per 60 minutes, "Check now" at most
// once per 60 s device-wide, and both count the last attempt of any kind.
func TestCheckIntervals(t *testing.T) {
	e := newEnv(t, "0.1.0-alpha.6")
	e.publish("0.1.0-alpha.9", relOpts{}, "")
	e.check(false)
	if e.gh.api() != 1 {
		t.Fatal(e.gh.api())
	}
	st := e.check(false) // the page again, at once
	if e.gh.api() != 1 || !st.Check.Throttled {
		t.Fatalf("page check repeated: api %d, throttled %v", e.gh.api(), st.Check.Throttled)
	}
	e.clk.Advance(30 * time.Second)
	if e.check(true); e.gh.api() != 1 {
		t.Fatal("Check now after 30 s went to GitHub")
	}
	e.clk.Advance(31 * time.Second)
	if e.check(true); e.gh.api() != 2 {
		t.Fatal("Check now after 61 s did not go to GitHub")
	}
	e.clk.Advance(59 * time.Minute)
	if e.check(false); e.gh.api() != 2 {
		t.Fatal("the page check ran 59 min after Check now")
	}
	e.clk.Advance(2 * time.Minute)
	if e.check(false); e.gh.api() != 3 {
		t.Fatal("the page check did not run after 61 min")
	}
}

func TestRateLimitHandling(t *testing.T) {
	reset := func(e *env, after time.Duration) string {
		return http403Reset(e.clk.Now().Add(after))
	}
	t.Run("403 with Remaining 0 waits until the reset and does not loop", func(t *testing.T) {
		e := newEnv(t, "0.1.0-alpha.6")
		e.publish("0.1.0-alpha.9", relOpts{}, "")
		e.gh.mu.Lock()
		e.gh.apiStatus = 403
		e.gh.apiExtra = http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {reset(e, 10*time.Minute)}}
		e.gh.mu.Unlock()
		st := e.check(false)
		if st.Check.State != "error" || st.Check.Error.Code != CodeRateLimited || st.Check.Error.RetryAfterSeconds < 590 || st.Check.Error.RetryAfterSeconds > 601 {
			t.Fatalf("%+v", st.Check.Error)
		}
		if e.gh.api() != 1 {
			t.Fatalf("api requests = %d", e.gh.api())
		}
		// Forced checks during the wait make no request at all.
		for i := 0; i < 3; i++ {
			e.clk.Advance(2 * time.Minute)
			st = e.check(true)
			if st.Check.Error == nil || st.Check.Error.Code != CodeRateLimited {
				t.Fatalf("%+v", st.Check)
			}
		}
		if e.gh.api() != 1 {
			t.Fatalf("a request was made during the wait: %d", e.gh.api())
		}
		// After the reset the next check works.
		e.gh.mu.Lock()
		e.gh.apiStatus, e.gh.apiExtra = 0, nil
		e.gh.mu.Unlock()
		e.clk.Advance(5 * time.Minute)
		st = e.check(true)
		if st.Check.State != "ok" || e.gh.api() != 2 {
			t.Fatalf("%+v api %d", st.Check, e.gh.api())
		}
	})
	t.Run("429 with Retry-After", func(t *testing.T) {
		e := newEnv(t, "0.1.0-alpha.6")
		e.gh.mu.Lock()
		e.gh.apiStatus = 429
		e.gh.apiExtra = http.Header{"Retry-After": {"120"}}
		e.gh.mu.Unlock()
		st := e.check(false)
		if st.Check.Error == nil || st.Check.Error.Code != CodeRateLimited || st.Check.Error.RetryAfterSeconds != 120 {
			t.Fatalf("%+v", st.Check.Error)
		}
	})
	t.Run("a plain 403 is http_403, not a rate limit", func(t *testing.T) {
		e := newEnv(t, "0.1.0-alpha.6")
		e.gh.mu.Lock()
		e.gh.apiStatus = 403
		e.gh.mu.Unlock()
		st := e.check(false)
		if st.Check.Error == nil || st.Check.Error.Code != "http_403" {
			t.Fatalf("%+v", st.Check.Error)
		}
	})
	t.Run("a huge wait in a header is capped", func(t *testing.T) {
		e := newEnv(t, "0.1.0-alpha.6")
		e.gh.mu.Lock()
		e.gh.apiStatus = 429
		e.gh.apiExtra = http.Header{"Retry-After": {"99999999"}}
		e.gh.mu.Unlock()
		st := e.check(false)
		if st.Check.Error.RetryAfterSeconds != 3600 {
			t.Fatalf("%d", st.Check.Error.RetryAfterSeconds)
		}
	})
	t.Run("Remaining 0 on a 200 still uses the answer and starts a wait", func(t *testing.T) {
		e := newEnv(t, "0.1.0-alpha.6")
		e.publish("0.1.0-alpha.9", relOpts{}, "")
		e.gh.mu.Lock()
		e.gh.apiExtra = http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {reset(e, 20*time.Minute)}}
		e.gh.mu.Unlock()
		st := e.check(false)
		if st.Available == nil {
			t.Fatal("the answer was dropped")
		}
		e.clk.Advance(2 * time.Minute)
		if e.check(true); e.gh.api() != 1 {
			t.Fatal("a request was made during the wait")
		}
	})
}

func http403Reset(t time.Time) string { return strconv.FormatInt(t.Unix(), 10) }

// ---- the redirect allow-list ----------------------------------------------------

func TestAllowedRedirect(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://github.com/swingstate/oarfish/releases/download/v1/x": true,
		"https://GitHub.com/x": true,
		"https://release-assets.githubusercontent.com/github-production/x": true,
		"https://objects.githubusercontent.com/x":                          true,
		"https://github.com:443/x":                                         true,
		"http://github.com/x":                                              false, // not https
		"https://github.com:8443/x":                                        false,
		"https://githubusercontent.com/x":                                  false, // a subdomain is required
		"https://.githubusercontent.com/x":                                 false,
		"https://evilgithubusercontent.com/x":                              false,
		"https://github.com.evil.example/x":                                false,
		"https://githubusercontent.com.evil.example/x":                     false,
		"https://evil.example/github.com":                                  false,
		"https://user@github.com/x":                                        false,
		"https://api.github.com/x":                                         false, // U3 lists github.com and *.githubusercontent.com
		"https://raw.githubusercontent.com.attacker.example/x":             false,
		"https://xn--github-9q4a.com/x":                                    false,
		"ftp://github.com/x":                                               false,
		"https://127.0.0.1/x":                                              false,
		"https://[::1]/x":                                                  false,
		"https://github.com.:443/x":                                        false, // trailing dot
		"https://sub.release-assets.githubusercontent.com/x":               true,
		"https://objects-origin.githubusercontent.com@evil.example/x":      false,
		"https://github.com․evil.example/x":                                false,
	} {
		u, err := url.Parse(raw)
		if err != nil {
			continue // not even a URL: the client never sees it
		}
		if got := allowedRedirect(u); got != want {
			t.Errorf("allowedRedirect(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestRedirectsStayOnTheAllowListAndAreLimited(t *testing.T) {
	e := newEnv(t, "0.1.0-alpha.6")
	e.publish("0.1.0-alpha.9", relOpts{}, "")
	other := newFakeGH(t) // a second host that is not on the allow-list
	// The API redirects to a host that is not allowed.
	e.gh.mu.Lock()
	e.gh.intercept = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/repos/swingstate/oarfish/releases" {
			http.Redirect(w, r, other.srv.URL+"/repos/swingstate/oarfish/releases", http.StatusFound)
			return true
		}
		return false
	}
	e.gh.mu.Unlock()
	st := e.check(false)
	if st.Check.Error == nil || st.Check.Error.Code != CodeRedirectRefused || other.api() != 0 {
		t.Fatalf("off-list redirect: %+v, other host got %d requests", st.Check.Error, other.api())
	}

	// Three redirects on allowed hosts are followed, a fourth is refused.
	for _, n := range []int{3, 4} {
		e := newEnv(t, "0.1.0-alpha.6")
		e.publish("0.1.0-alpha.9", relOpts{}, "")
		hops := 0
		e.gh.mu.Lock()
		e.gh.intercept = func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Path == "/repos/swingstate/oarfish/releases" {
				if hops < n {
					hops++
					http.Redirect(w, r, e.gh.srv.URL+"/repos/swingstate/oarfish/releases", http.StatusFound)
					return true
				}
			}
			return false
		}
		e.gh.mu.Unlock()
		st := e.check(false)
		switch n {
		case 3:
			if st.Check.State != "ok" || st.Available == nil {
				t.Errorf("3 redirects: %+v", st.Check.Error)
			}
		case 4:
			if st.Check.Error == nil || st.Check.Error.Code != CodeRedirectRefused {
				t.Errorf("4 redirects: %+v", st.Check)
			}
		}
	}
}

// The allow-list is what the client uses when nothing replaces it.
func TestTheProductionClientUsesTheAllowList(t *testing.T) {
	g := newGHClient("1.0.0", nil, time.Now)
	req := mustReq(t, "https://evil.example/x")
	if err := g.hc.CheckRedirect(req, nil); err == nil {
		t.Error("an off-list host passed the production redirect check")
	}
	if err := g.hc.CheckRedirect(mustReq(t, "https://github.com/x"), nil); err != nil {
		t.Errorf("github.com refused: %v", err)
	}
	via := make([]*http.Request, 4)
	if err := g.hc.CheckRedirect(mustReq(t, "https://github.com/x"), via); err == nil {
		t.Error("a fourth redirect passed")
	}
	tr := g.hc.Transport.(*http.Transport)
	if tr.Proxy != nil {
		t.Error("the transport uses a proxy")
	}
	if tr.TLSClientConfig.MinVersion < 0x0303 {
		t.Error("TLS below 1.2 is allowed")
	}
	if !tr.DisableKeepAlives {
		t.Error("keep-alive would leave a goroutine behind a request")
	}
}

func mustReq(t *testing.T, raw string) *http.Request {
	r, err := http.NewRequestWithContext(context.Background(), http.MethodGet, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// HTTP is refused even to the fake's own host.
func TestPlainHTTPRedirectIsRefused(t *testing.T) {
	e := newEnv(t, "0.1.0-alpha.6")
	e.publish("0.1.0-alpha.9", relOpts{}, "")
	e.gh.mu.Lock()
	e.gh.intercept = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/repos/swingstate/oarfish/releases" {
			http.Redirect(w, r, "http://"+r.Host+"/repos/swingstate/oarfish/releases", http.StatusFound)
			return true
		}
		return false
	}
	e.gh.mu.Unlock()
	if st := e.check(false); st.Check.Error == nil || st.Check.Error.Code != CodeRedirectRefused {
		t.Fatalf("%+v", st.Check)
	}
}

// ---- advisory verification --------------------------------------------------------

func TestAdvisoryVerificationRefusesTamperedReleases(t *testing.T) {
	other := newTestKey(t, "other")
	for _, tc := range []struct {
		name string
		o    relOpts
		ver  string
		want Code
	}{
		{"bad signature", relOpts{badSignature: true}, "0.1.0-alpha.9", CodeSignatureInvalid},
		{"signed by another key", relOpts{otherKey: &other}, "0.1.0-alpha.9", CodeSignatureInvalid},
		{"manifest is not the listed one", relOpts{badChecksum: true}, "0.1.0-alpha.9", CodeChecksumMismatch},
		{"wrong architecture", relOpts{arch: "amd64"}, "0.1.0-alpha.9", CodeRequirementsNotMet},
		{"needs a newer installed version", relOpts{minInstalled: "0.1.0-alpha.7"}, "0.1.0-alpha.9", CodeRequirementsNotMet},
		{"needs a newer helper protocol", relOpts{helperProto: 2}, "0.1.0-alpha.9", CodeHelperTooOld},
		{"manifest names another version", relOpts{manifestEdit: func(m map[string]any) { m["version"] = "0.1.0-alpha.2"; m["tag"] = "v0.1.0-alpha.2" }}, "0.1.0-alpha.9", CodeManifestInvalid},
		{"prerelease flag does not match the version", relOpts{manifestEdit: func(m map[string]any) { m["prerelease"] = false }}, "0.1.0-alpha.9", CodeManifestInvalid},
		{"bundle larger than the cap", relOpts{manifestEdit: func(m map[string]any) { m["bundle"].(map[string]any)["size"] = MaxBundleBytes + 1 }}, "0.1.0-alpha.9", CodeTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, "0.1.0-alpha.6")
			e.publish(tc.ver, tc.o, "")
			st := e.check(false)
			a := st.Available
			if a == nil || a.Verified || a.VerifyError == nil {
				t.Fatalf("a tampered release was accepted: %+v", a)
			}
			if a.VerifyError.Code != tc.want {
				t.Errorf("code = %s, want %s", a.VerifyError.Code, tc.want)
			}
			// It cannot be applied.
			if err := e.u.Applicable(tc.ver); err == nil {
				t.Error("Applicable accepted a release whose verification failed")
			}
		})
	}
}

// A validly signed release that is older than the installed version is refused
// by the strictly-newer rule even if a tag says otherwise.
func TestAdvisoryDowngradeIsRefused(t *testing.T) {
	e := newEnv(t, "0.2.0")
	rf := mkRelease(t, e.key, "0.1.0", relOpts{})
	installed, _ := e.u.installed()
	_, err := e.u.verifyMetadata(installed, "0.1.0", rf.Checksums, rf.Sig, rf.Manifest)
	if err == nil || err.Code != CodeDowngradeRefused {
		t.Fatalf("%v", err)
	}
	same := mkRelease(t, e.key, "0.2.0", relOpts{})
	if _, err := e.u.verifyMetadata(installed, "0.2.0", same.Checksums, same.Sig, same.Manifest); err == nil || err.Code != CodeAlreadyInstalled {
		t.Fatalf("%v", err)
	}
}

// ---- the notice ---------------------------------------------------------------------

func TestNoticeChecksOnlyWhenTheResultIsOlderThan24Hours(t *testing.T) {
	e := newEnv(t, "0.1.0-alpha.6")
	e.publish("0.1.0-alpha.9", relOpts{}, "")
	n := e.u.Notice(context.Background())
	if e.gh.api() != 1 || n.Available == nil || n.Available.Version != "0.1.0-alpha.9" || n.Available.Channel != "prerelease" || n.InstalledVersion != "0.1.0-alpha.6" {
		t.Fatalf("first notice: api %d %+v", e.gh.api(), n)
	}
	for _, d := range []time.Duration{time.Minute, 5 * time.Hour, 17 * time.Hour, 118 * time.Minute} {
		e.clk.Advance(d)
		e.u.Notice(context.Background())
	}
	if e.gh.api() != 1 {
		t.Fatalf("the notice asked GitHub again inside 24 hours: %d", e.gh.api())
	}
	e.clk.Advance(2 * time.Minute) // 24 h and 1 min
	e.u.Notice(context.Background())
	e.settle()
	if e.gh.api() != 2 {
		t.Fatalf("api = %d after 24 hours", e.gh.api())
	}
}

func TestNoticeSharesTheCacheWithThePage(t *testing.T) {
	e := newEnv(t, "0.1.0-alpha.6")
	e.publish("0.1.0-alpha.9", relOpts{}, "")
	e.u.Notice(context.Background())
	// The page check right after is inside the 60 minutes of the notice's attempt.
	st := e.check(false)
	if e.gh.api() != 1 || !st.Check.Throttled || st.Available == nil || st.Available.Version != "0.1.0-alpha.9" {
		t.Fatalf("api %d %+v", e.gh.api(), st.Check)
	}
	// And the other way round: a page check makes the notice answer from memory.
	e2 := newEnv(t, "0.1.0-alpha.6")
	e2.publish("0.1.0-alpha.9", relOpts{}, "")
	e2.check(false)
	if n := e2.u.Notice(context.Background()); e2.gh.api() != 1 || n.Available == nil {
		t.Fatalf("api %d %+v", e2.gh.api(), n)
	}
}

func TestNoticeIsSilentOfflineAndRetriesNoSoonerThanAnHour(t *testing.T) {
	e := newEnv(t, "0.1.0-alpha.6")
	e.publish("0.1.0-alpha.9", relOpts{}, "")
	e.gh.srv.Close() // offline
	n := e.u.Notice(context.Background())
	e.settle()
	if n.Available != nil || n.InstalledVersion != "0.1.0-alpha.6" {
		t.Fatalf("%+v", n)
	}
	// The notice answer has no error member at all.
	if strings.Contains(strings.ToLower(jsonOf(t, n)), "error") {
		t.Fatalf("the notice shows an error: %s", jsonOf(t, n))
	}
	// The page, however, keeps the typed error.
	st := e.u.State(context.Background())
	if st.Check.State != "error" || st.Check.Error == nil {
		t.Fatalf("%+v", st.Check)
	}
	// The notice does not try again at once, nor within an hour, but after.
	if e.u.mu.Lock(); e.u.job != nil && !e.u.job.done {
		t.Fatal("a job still runs")
	}
	e.u.mu.Unlock()
	before := e.u.lastAttempt
	e.clk.Advance(59 * time.Minute)
	e.u.Notice(context.Background())
	e.u.mu.Lock()
	same := e.u.lastAttempt.Equal(before)
	e.u.mu.Unlock()
	if !same {
		t.Fatal("the notice retried a failed attempt inside an hour")
	}
	e.clk.Advance(2 * time.Minute)
	e.u.Notice(context.Background())
	e.settle()
	e.u.mu.Lock()
	again := !e.u.lastAttempt.Equal(before)
	e.u.mu.Unlock()
	if !again {
		t.Fatal("the notice never retried")
	}
}

func TestNoticeNeverAnswersBusyAndRespectsTheRateLimitWait(t *testing.T) {
	e := newEnv(t, "0.1.0-alpha.6")
	e.publish("0.1.0-alpha.9", relOpts{}, "")
	e.u.mu.Lock()
	e.u.waitUntil = e.clk.Now().Add(3 * time.Hour) // as left by an earlier answer
	e.u.mu.Unlock()
	e.u.Notice(context.Background())
	if e.gh.api() != 0 {
		t.Fatalf("the notice ignored the rate-limit wait: %d requests", e.gh.api())
	}
	// A running job: the notice answers from memory.
	e2 := newEnv(t, "0.1.0-alpha.6")
	slot, err := e2.u.Reserve()
	if err != nil {
		t.Fatal(err)
	}
	defer slot.Release()
	n := e2.u.Notice(context.Background())
	if n.SchemaVersion != NoticeSchemaVersion || e2.gh.api() != 0 {
		t.Fatalf("%+v api %d", n, e2.gh.api())
	}
}

// A11.1: no timer and no goroutine that runs without a request.
func TestNoTimerAndNoGoroutineWithoutARequest(t *testing.T) {
	e := newEnv(t, "0.1.0-alpha.6")
	e.publish("0.1.0-alpha.9", relOpts{}, "")
	runtime.GC()
	before := runtime.NumGoroutine()
	e.u.Notice(context.Background())
	e.check(true)
	e.clk.Advance(48 * time.Hour) // nothing may fire
	time.Sleep(50 * time.Millisecond)
	if e.gh.api() != 1 && e.gh.api() != 2 {
		t.Fatalf("api = %d", e.gh.api())
	}
	if n := e.clk.count(); n != 0 {
		t.Fatalf("the updater set %d timers on its clock", n)
	}
	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines before %d, after %d: something outlives its request", before, after)
	}
	// Nothing is written to disk by a check.
	if entries := listDir(t, e.cache); len(entries) != 0 {
		t.Fatalf("a check wrote to the cache: %v", entries)
	}
}

// A restart forgets the result and allows one new check (A11.1).
func TestARestartForgetsTheResult(t *testing.T) {
	e := newEnv(t, "0.1.0-alpha.6")
	e.publish("0.1.0-alpha.9", relOpts{}, "")
	e.u.Notice(context.Background())
	e.settle()
	e2 := newEnv(t, "0.1.0-alpha.6")
	e2.gh.srv.Close()
	e2.gh = e.gh // a new updater against the same fake
	e2.u.gh.apiBase, e2.u.gh.dlBase = e.gh.srv.URL, e.gh.srv.URL
	e2.allowHosts(mustHost(t, e.gh.srv.URL))
	pool := e.u.gh.hc.Transport
	e2.u.gh.hc.Transport = pool
	e2.u.Notice(context.Background())
	e2.settle()
	if e.gh.api() != 2 {
		t.Fatalf("api = %d", e.gh.api())
	}
}

func TestInstalledVersionMustBeAReleaseVersion(t *testing.T) {
	e := newEnv(t, "dev")
	st := e.check(false)
	if st.Check.Error == nil || st.Check.Error.Code != CodeVersionUnknown || e.gh.api() != 0 {
		t.Fatalf("%+v api %d", st.Check, e.gh.api())
	}
}

// The 24 hours are the age of the last successful result, not of the last
// attempt: a failed forced check in between does not postpone the notice.
func TestNoticeAgeIsTheAgeOfTheLastSuccessfulResult(t *testing.T) {
	e := newEnv(t, "0.1.0-alpha.6")
	e.publish("0.1.0-alpha.9", relOpts{}, "")
	e.u.Notice(context.Background())
	e.settle()
	e.clk.Advance(23 * time.Hour)
	e.gh.mu.Lock()
	e.gh.apiStatus = 500
	e.gh.mu.Unlock()
	if st := e.check(true); st.Check.State != "error" {
		t.Fatalf("%+v", st.Check)
	}
	e.gh.mu.Lock()
	e.gh.apiStatus = 0
	e.gh.mu.Unlock()
	api := e.gh.api()
	e.clk.Advance(90 * time.Minute) // the result is 24 h 30 min old, the failed attempt 90 min
	e.u.Notice(context.Background())
	e.settle()
	if e.gh.api() != api+1 {
		t.Fatalf("the notice did not check although the last result is older than 24 hours (api %d -> %d)", api, e.gh.api())
	}
}
