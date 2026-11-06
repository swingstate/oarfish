package updater

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swingstate/oarfish/backend/internal/audit"
	"github.com/swingstate/oarfish/backend/internal/clock/clocktest"
)

func TestErrorTextsAreFixedAndCarryNothingFromOutside(t *testing.T) {
	for c := range texts {
		e := fail(c)
		if e.Error() != string(c)+": "+Text(c) {
			t.Errorf("%s: %q", c, e.Error())
		}
		for _, bad := range []string{"127.0.0.1", "github", "http://", "https://", "/var", "/etc", "error:", "dial", "lookup"} {
			if strings.Contains(strings.ToLower(Text(c)), bad) {
				t.Errorf("the text of %s contains %q", c, bad)
			}
		}
	}
	if Text(httpCode(503)) == Text(CodeInternal) || httpCode(99) != CodeBadResponse || httpCode(600) != CodeBadResponse {
		t.Error("http codes")
	}
	if s, ok := httpStatus("http_503"); !ok || s != 503 {
		t.Error("httpStatus")
	}
	for _, bad := range []Code{"http_50", "http_5033", "http_abc", "http_099", "other"} {
		if _, ok := httpStatus(bad); ok {
			t.Errorf("%q parsed as an HTTP status", bad)
		}
	}
	if codeOf(errors.New("dial tcp 10.0.0.1:443: connection refused")) != CodeInternal {
		t.Error("a Go error was passed on")
	}
}

func TestNetworkErrorsAreClassifiedByCodeOnly(t *testing.T) {
	ctx := context.Background()
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Nanosecond)
	defer cancel()
	<-timeoutCtx.Done()
	for name, tc := range map[string]struct {
		err  error
		want Code
	}{
		"dns":               {&net.DNSError{Err: "no such host", Name: "api.github.com"}, CodeDNSFailure},
		"unknown authority": {x509.UnknownAuthorityError{}, CodeTLSFailure},
		"expired":           {x509.CertificateInvalidError{Reason: x509.Expired}, CodeTLSFailure},
		"hostname":          {x509.HostnameError{Host: "x"}, CodeTLSFailure},
		"tls verification":  {&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, CodeTLSFailure},
		"tls alert":         {tls.AlertError(40), CodeTLSFailure},
		"deadline":          {context.DeadlineExceeded, CodeTimeout},
		"refused":           {&net.OpError{Op: "dial", Err: errors.New("connection refused")}, CodeUnreachable},
		"redirect":          {errRedirect, CodeRedirectRefused},
		"wrapped redirect":  {&net.OpError{Err: errRedirect}, CodeRedirectRefused},
		"anything else":     {errors.New("boom 10.1.2.3"), CodeUnreachable},
		"already typed":     {fail(CodeTooLarge), CodeTooLarge},
	} {
		if got := classifyNet(ctx, tc.err); got == nil || got.Code != tc.want {
			t.Errorf("%s: %v, want %s", name, got, tc.want)
		}
	}
	if got := classifyNet(timeoutCtx, context.DeadlineExceeded); got.Code != CodeTimeout {
		t.Error(got)
	}
	cctx, ccancel := context.WithCancel(ctx)
	ccancel()
	if got := classifyNet(cctx, context.Canceled); got.Code != CodeCancelled {
		t.Error(got)
	}
}

// A certificate the system does not trust is tls_failure, whatever Go says about it.
func TestAnUntrustedCertificateIsATLSFailure(t *testing.T) {
	e := newEnv(t, v6)
	e.publish(v9, relOpts{}, "")
	e.u.gh.hc.Transport.(*http.Transport).TLSClientConfig.RootCAs = x509.NewCertPool() // trusts nothing
	st := e.check(false)
	if st.Check.Error == nil || st.Check.Error.Code != CodeTLSFailure {
		t.Fatalf("%+v", st.Check)
	}
	if !strings.Contains(st.Check.Error.Message, "date and time") {
		t.Errorf("the message does not point to the clock: %q", st.Check.Error.Message)
	}
}

// ADR 0010 U8: nothing that identifies the server, the device or a credential in
// any answer, any log line or any audit entry, whatever fails.
func TestRedactionInAnswersLogsAndAudit(t *testing.T) {
	scenarios := map[string]func(e *env){
		"offline": func(e *env) { e.gh.srv.Close(); e.check(false) },
		"untrusted certificate": func(e *env) {
			e.u.gh.hc.Transport.(*http.Transport).TLSClientConfig.RootCAs = x509.NewCertPool()
			e.check(false)
		},
		"rate limited": func(e *env) {
			e.gh.mu.Lock()
			e.gh.apiStatus, e.gh.apiExtra = 429, http.Header{"Retry-After": {"60"}}
			e.gh.mu.Unlock()
			e.check(false)
		},
		"server error body": func(e *env) {
			e.gh.mu.Lock()
			e.gh.intercept = func(w http.ResponseWriter, r *http.Request) bool {
				w.WriteHeader(500)
				w.Write([]byte("internal error at 10.9.8.7 token=ghp_SECRET"))
				return true
			}
			e.gh.mu.Unlock()
			e.check(false)
		},
		"tampered release": func(e *env) {
			e.publish(v9, relOpts{badSignature: true}, "")
			e.check(false)
		},
		"failed download and helper error": func(e *env) {
			e.publish(v9, relOpts{}, "")
			e.check(false)
			e.interceptBundle(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) })
			e.apply(v9)
			e.helper.status.LastResult = &HelperResult{TransactionID: "00112233445566ff", To: v9, Outcome: "rollback_failed", ReasonCode: "rollback_unhealthy", CorrelationID: "0123456789abcdef"}
			e.helper.statusErr = nil
			e.u.State(context.Background())
		},
	}
	for name, run := range scenarios {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, v6)
			run(e)
			host := mustHost(t, e.gh.srv.URL)
			port := host[strings.LastIndex(host, ":")+1:]
			st := e.u.State(context.Background())
			dump := jsonOf(t, st) + "\n" + e.log.String() + "\n" + jsonOf(t, e.audits())
			for _, bad := range []string{host, "127.0.0.1", "localhost", ":" + port, "api.github.com", "github.com", "https://", "ghp_", "SECRET",
				"10.9.8.7", "token", "Bearer", "Authorization", "password", "cookie", e.cache, "x509", "tls:", "dial tcp", "lookup"} {
				if strings.Contains(strings.ToLower(dump), strings.ToLower(bad)) {
					t.Errorf("%s: the output contains %q:\n%s", name, bad, dump)
				}
			}
		})
	}
}

// The audit entries pass the real audit package unchanged: nothing is dropped
// by the closed sets of ADR 0009 A6.
func TestUpdateEntriesPassTheRealAuditLog(t *testing.T) {
	dir := t.TempDir()
	clk := clocktest.New()
	log, err := audit.Open(dir, clk, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t, v6)
	e.u.cfg.Audit = log.Record
	e.publish(v9, relOpts{}, "")
	e.check(false)
	e.apply(v9)
	e.helper.status.LastResult = &HelperResult{TransactionID: "00112233445566ff", To: v9, From: v6, Outcome: "components_rolled_back", CorrelationID: "0123456789abcdef"}
	e.u.State(context.Background())
	e.u.Discard(context.Background(), v9, "fedcba9876543210")
	e.helper.discardErr = fail(CodeNotFound)
	e.u.Discard(context.Background(), v9, "fedcba9876543211")
	log.Close()
	if st := log.Stats(); st.Dropped != 0 || st.Failures != 0 {
		t.Fatalf("the audit log dropped events: %+v", st)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "audit", "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	type line struct {
		User, Action, Target, Result, CorrelationID string
		Count                                       int
	}
	var got []line
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) != nil {
			t.Fatalf("%q", l)
		}
		got = append(got, line{m["user"].(string), m["action"].(string), m["target"].(string), m["result"].(string), m["correlation_id"].(string), int(m["count"].(float64))})
	}
	want := []line{
		{"admin", "update_started", "update/" + v9, "success", "0123456789abcdef", 1},
		{"-", "update_finished", "update/" + v9, "components_rolled_back", "0123456789abcdef", 1},
		{"-", "update_finished", "update/" + v9, "aborted", "fedcba9876543210", 1},
		{"-", "update_finished", "update/" + v9, "error", "fedcba9876543211", 1}, // not_found is recorded as error (A6)
	}
	if len(got) != len(want) {
		t.Fatalf("%d entries:\n%s", len(got), raw)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d: %+v, want %+v", i, got[i], want[i])
		}
	}
	// Only the version-shaped targets are accepted: nothing from outside.
	l2, _ := audit.Open(t.TempDir(), clk, nil)
	for _, bad := range []string{"update/", "update/../x", "update/1.2", "update/1.2.3/../../x", "update/" + strings.Repeat("1", 70) + ".0.0", "update/ 1.2.3", "update/1.2.3\n"} {
		l2.Record(audit.Event{User: "-", Action: audit.UpdateStarted, Target: audit.Target(bad), Result: audit.Success, CorrelationID: "0123456789abcdef"})
	}
	if st := l2.Stats(); st.Dropped != 7 {
		t.Errorf("dropped %d of 7 bad targets", st.Dropped)
	}
	l2.Close()
}

func TestAuditResultsOfTheHelperCodes(t *testing.T) {
	for _, c := range []Code{CodeBusy, CodeSignatureInvalid, CodeChecksumMismatch, CodeManifestInvalid, CodeDowngradeRefused, CodeAlreadyInstalled,
		CodeRequirementsNotMet, CodeHelperTooOld, CodeInstallerRequired, CodeConfirmationRequired, CodeKeyChanged, CodeUnitRejected,
		CodeComponentsDamaged, CodeStageExpired, CodeNoSpace, CodeCurrentUnhealthy, CodeCooldown, CodeIOError, CodeUnavailable} {
		if audit.UpdateResult(string(c)) != audit.Result(c) {
			t.Errorf("%s is not recorded as itself", c)
		}
	}
	for _, c := range []Code{CodeBadRequest, CodeNotFound, CodeInternal, "http_404", CodeTooLarge, "", "x"} {
		if audit.UpdateResult(string(c)) != audit.Failed {
			t.Errorf("%s is not recorded as error", c)
		}
	}
}

// ---- fuzz targets -----------------------------------------------------------------------------

// FuzzHelperResponse feeds arbitrary bytes as the helper's answer to every
// request. Nothing may panic, and what is accepted must satisfy the patterns.
func FuzzHelperResponse(f *testing.F) {
	for _, s := range []string{statusAnswer, `{"v":1,"ok":true}` + "\n", `{"v":1,"ok":false,"error":"busy","message":"x"}` + "\n",
		`{"v":1,"ok":true,"status":"accepted","transaction_id":"00112233445566ff"}` + "\n", "", "\n", "{", `{"v":1,"ok":null}` + "\n",
		`{"v":1,"ok":false,"error":"installer_required","differs":["oarfish.service"]}` + "\n"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, ans []byte) {
		p := fixed(string(ans))
		h := helperWith(p)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if st, e := h.Status(ctx); e == nil {
			if !st.valid() {
				t.Fatalf("accepted a status that is not valid: %+v", st)
			}
			if st.LastResult != nil && !idPattern.MatchString(st.LastResult.TransactionID) {
				t.Fatal("transaction id")
			}
		}
		if id, e := h.Activate(ctx, "1.2.3", "0123456789abcdef", false); e == nil && !idPattern.MatchString(id) {
			t.Fatalf("accepted transaction id %q", id)
		}
		if _, e := h.Stage(ctx, "1.2.3", "0123456789abcdef"); e != nil {
			for _, d := range e.Differs {
				if !componentNames[d] {
					t.Fatalf("differs %q", d)
				}
			}
			if _, known := texts[e.Code]; !known {
				t.Fatalf("code %q is not in the closed set", e.Code)
			}
		}
	})
}

// FuzzReleaseList feeds arbitrary bytes as GitHub's release list. The
// candidates that come out have a valid tag, and the notes are plain text.
func FuzzReleaseList(f *testing.F) {
	for _, s := range []string{`[{"tag_name":"v1.2.3","prerelease":false,"draft":false,"body":"x"}]`, `[]`, `{}`, `[{"tag_name":"../x"}]`,
		`[{"tag_name":"v1.2.3-rc.1","body":"\u0000\u001b[31m"}]`, `null`, `[null]`, `[{"tag_name":5}]`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		var list []releaseWire
		if json.Unmarshal(body, &list) != nil {
			return
		}
		for _, c := range candidates(list) {
			if !validVersion(c.ver.String()) || len(c.notes) > maxNotesBytes || strings.ContainsAny(c.notes, "\x00\x1b\r") {
				t.Fatalf("candidate %q %q", c.ver.String(), c.notes)
			}
		}
	})
}

func FuzzPlainNotes(f *testing.F) {
	for _, s := range []string{"", "plain", "a\x00b", "\xff\xfe", "line\r\nline", strings.Repeat("é", 5000)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := plainNotes(s)
		if len(out) > maxNotesBytes {
			t.Fatal("too long")
		}
		for _, r := range out {
			if r < 0x20 && r != '\n' && r != '\t' || r == 0x7f || r == 0xfffd {
				t.Fatalf("control or invalid rune %q", r)
			}
		}
		if plainNotes(out) != out {
			t.Fatal("not idempotent")
		}
	})
}
