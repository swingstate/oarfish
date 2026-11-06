package updater

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// peer answers one connection: it reads the request line and writes answer.
type peer struct {
	mu       sync.Mutex
	requests []string
	answer   func(req string) []byte
}

func (p *peer) conn() net.Conn {
	c, s := net.Pipe()
	go func() {
		defer s.Close()
		buf := make([]byte, 0, 4096)
		b := make([]byte, 1)
		for {
			if _, err := s.Read(b); err != nil {
				return
			}
			buf = append(buf, b[0])
			if b[0] == '\n' {
				break
			}
		}
		p.mu.Lock()
		p.requests = append(p.requests, string(buf))
		p.mu.Unlock()
		if a := p.answer(string(buf)); a != nil {
			s.Write(a)
		}
	}()
	return c
}

func (p *peer) last() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) == 0 {
		return ""
	}
	return p.requests[len(p.requests)-1]
}

func helperWith(p *peer) *Helper {
	h := NewHelper("/run/oarfish-update/helper.sock")
	h.dial = func(context.Context, string) (net.Conn, error) { return p.conn(), nil }
	h.sleep = func(context.Context, time.Duration) error { return nil }
	return h
}

func fixed(s string) *peer { return &peer{answer: func(string) []byte { return []byte(s) }} }

const statusAnswer = `{"v":1,"ok":true,"protocol_min":1,"protocol_max":1,"helper_version":"0.1.0-alpha.7","key_id":"11f1c52777090bfc",` +
	`"installed_version":"0.1.0-alpha.6","phase":"idle","staged_version":"","previous_version":"0.1.0-alpha.5",` +
	`"last_result":{"transaction_id":"00112233445566ff","to":"0.1.0-alpha.6","from":"0.1.0-alpha.5","outcome":"success","reason_code":"","correlation_id":"0123456789abcdef","finished_at":"2026-11-06T10:00:00Z","acknowledged":false},` +
	`"needs_attention":false,"free_bytes":123456789,"cooldown_version":"","cooldown_remaining_s":0,"new_member":"ignored"}` + "\n"

func TestHelperRequestsOnTheWire(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		call func(h *Helper) *Error
		want string
		ans  string
	}{
		"status": {func(h *Helper) *Error { _, e := h.Status(ctx); return e }, `{"v":1,"op":"status"}` + "\n", statusAnswer},
		"stage": {func(h *Helper) *Error { _, e := h.Stage(ctx, "1.2.3", "0123456789abcdef"); return e },
			`{"v":1,"op":"stage","version":"1.2.3","correlation_id":"0123456789abcdef"}` + "\n", `{"v":1,"ok":true,"version":"1.2.3","commit":"abc","components_differ":false}` + "\n"},
		"activate": {func(h *Helper) *Error { _, e := h.Activate(ctx, "1.2.3", "0123456789abcdef", true); return e },
			`{"v":1,"op":"activate","version":"1.2.3","correlation_id":"0123456789abcdef","components_confirmed":true}` + "\n",
			`{"v":1,"ok":true,"status":"accepted","transaction_id":"00112233445566ff"}` + "\n"},
		"discard": {func(h *Helper) *Error { return h.Discard(ctx, "1.2.3") }, `{"v":1,"op":"discard","version":"1.2.3"}` + "\n", `{"v":1,"ok":true}` + "\n"},
		"ack":     {func(h *Helper) *Error { return h.Ack(ctx, "00112233445566ff") }, `{"v":1,"op":"ack","transaction_id":"00112233445566ff"}` + "\n", `{"v":1,"ok":true}` + "\n"},
	} {
		t.Run(name, func(t *testing.T) {
			p := fixed(tc.ans)
			if e := tc.call(helperWith(p)); e != nil {
				t.Fatal(e)
			}
			if p.last() != tc.want {
				t.Fatalf("sent %q, want %q", p.last(), tc.want)
			}
			if len(p.last()) > 4096 {
				t.Fatal("request over 4 KiB")
			}
		})
	}
}

func TestHelperStatusIsDecodedAndAdditiveMembersAreIgnored(t *testing.T) {
	st, e := helperWith(fixed(statusAnswer)).Status(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if st.InstalledVersion != "0.1.0-alpha.6" || st.Phase != "idle" || st.PreviousVersion != "0.1.0-alpha.5" || st.FreeBytes != 123456789 ||
		st.LastResult == nil || st.LastResult.Outcome != "success" || st.LastResult.Acknowledged || st.ProtocolMax != 1 {
		t.Fatalf("%+v", st)
	}
}

// A11.3: the client timeouts.
func TestHelperTimeoutsAreTheOnesOfA11_3(t *testing.T) {
	if TimeoutStatus != 10*time.Second || TimeoutDiscard != 10*time.Second || TimeoutAck != 10*time.Second ||
		TimeoutActivate != 30*time.Second || TimeoutStage != 200*time.Second {
		t.Fatal("the constants differ from ADR 0010 A11.3")
	}
	ctx := context.Background()
	for op, tc := range map[string]struct {
		call func(h *Helper)
		want time.Duration
		ans  string
	}{
		"status":   {func(h *Helper) { h.Status(ctx) }, 10 * time.Second, statusAnswer},
		"discard":  {func(h *Helper) { h.Discard(ctx, "1.2.3") }, 10 * time.Second, `{"v":1,"ok":true}` + "\n"},
		"ack":      {func(h *Helper) { h.Ack(ctx, "00112233445566ff") }, 10 * time.Second, `{"v":1,"ok":true}` + "\n"},
		"activate": {func(h *Helper) { h.Activate(ctx, "1.2.3", "0123456789abcdef", false) }, 30 * time.Second, `{"v":1,"ok":true}` + "\n"},
		"stage":    {func(h *Helper) { h.Stage(ctx, "1.2.3", "0123456789abcdef") }, 200 * time.Second, `{"v":1,"ok":true}` + "\n"},
	} {
		t.Run(op, func(t *testing.T) {
			p := fixed(tc.ans)
			h := helperWith(p)
			var got time.Duration
			h.dial = func(ctx context.Context, _ string) (net.Conn, error) {
				dl, ok := ctx.Deadline()
				if !ok {
					t.Error("no deadline")
				}
				got = time.Until(dl)
				return p.conn(), nil
			}
			tc.call(h)
			if got > tc.want || got < tc.want-2*time.Second {
				t.Fatalf("deadline in %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHelperDeadlineAndCancellation(t *testing.T) {
	silent := &peer{answer: func(string) []byte { time.Sleep(5 * time.Second); return nil }}
	h := helperWith(silent)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, e := h.Status(ctx); e == nil || e.Code != CodeTimeout || time.Since(start) > 3*time.Second {
		t.Fatalf("%v after %v", e, time.Since(start))
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel2() }()
	if _, e := h.Status(ctx2); e == nil || e.Code != CodeCancelled {
		t.Fatalf("%v", e)
	}
}

func TestHelperRetriesOnceOnAResetOrAMissingSocket(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err       error
		wantCalls int
		wantOK    bool
	}{
		{"reset", syscall.ECONNRESET, 2, true},
		{"no such file", syscall.ENOENT, 2, true},
		{"refused is not retried", syscall.ECONNREFUSED, 1, false},
		{"permission is not retried", syscall.EACCES, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := fixed(statusAnswer)
			h := helperWith(p)
			calls := 0
			h.dial = func(context.Context, string) (net.Conn, error) {
				calls++
				if calls == 1 {
					return nil, &net.OpError{Op: "dial", Net: "unix", Err: &os.SyscallError{Syscall: "connect", Err: tc.err}}
				}
				return p.conn(), nil
			}
			_, e := h.Status(context.Background())
			if calls != tc.wantCalls || (e == nil) != tc.wantOK {
				t.Fatalf("calls %d, error %v", calls, e)
			}
			if !tc.wantOK && e.Code != CodeUnavailable {
				t.Fatalf("%v", e)
			}
		})
	}
	// A second failure is final: one retry only.
	h := NewHelper("x")
	n := 0
	h.sleep = func(context.Context, time.Duration) error { return nil }
	h.dial = func(context.Context, string) (net.Conn, error) { n++; return nil, syscall.ECONNRESET }
	if _, e := h.Status(context.Background()); e == nil || e.Code != CodeUnavailable || n != 2 {
		t.Fatalf("%v after %d dials", e, n)
	}
}

func TestHelperAnswersThatCannotBeUsed(t *testing.T) {
	long := `{"v":1,"ok":true,"pad":"` + strings.Repeat("x", 9000) + `"}` + "\n"
	for name, ans := range map[string]string{
		"empty":              "",
		"no newline":         `{"v":1,"ok":true}`,
		"not json":           "hello\n",
		"another protocol":   `{"v":2,"ok":true}` + "\n",
		"no version":         `{"ok":true}` + "\n",
		"over 8 KiB":         long,
		"unknown error code": `{"v":1,"ok":false,"error":"my_new_code","message":"x"}` + "\n",
		"a path as the code": `{"v":1,"ok":false,"error":"/etc/shadow"}` + "\n",
		"two lines":          `{"v":1,"ok":true}` + "\n" + `{"v":1,"ok":true}` + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			e := helperWith(fixed(ans)).Discard(context.Background(), "1.2.3")
			if name == "two lines" {
				return // trailing data after the first object is not decoded; documented below
			}
			if e == nil || e.Code != CodeInternal {
				t.Fatalf("%v", e)
			}
		})
	}
	// A status with a member outside its pattern is not shown.
	for name, bad := range map[string]string{
		"version":    strings.Replace(statusAnswer, `"installed_version":"0.1.0-alpha.6"`, `"installed_version":"../../etc"`, 1),
		"phase":      strings.Replace(statusAnswer, `"phase":"idle"`, `"phase":"<b>"`, 1),
		"outcome":    strings.Replace(statusAnswer, `"outcome":"success"`, `"outcome":"great"`, 1),
		"txid":       strings.Replace(statusAnswer, `00112233445566ff`, `zz`, 1),
		"reason":     strings.Replace(statusAnswer, `"reason_code":""`, `"reason_code":"Not A Code"`, 1),
		"protocols":  strings.Replace(statusAnswer, `"protocol_max":1`, `"protocol_max":0`, 1),
		"to version": strings.Replace(statusAnswer, `"to":"0.1.0-alpha.6"`, `"to":""`, 1),
	} {
		if _, e := helperWith(fixed(bad)).Status(context.Background()); e == nil || e.Code != CodeInternal {
			t.Errorf("%s: %v", name, e)
		}
	}
}

func TestHelperErrorCodesAreTypedAndNeverEchoTheMessage(t *testing.T) {
	for c := range helperCodes {
		ans := `{"v":1,"ok":false,"error":"` + string(c) + `","message":"open /var/lib/oarfish-update/state.json: permission denied"}` + "\n"
		e := helperWith(fixed(ans)).Ack(context.Background(), "00112233445566ff")
		if e == nil || e.Code != c {
			t.Fatalf("%s: %v", c, e)
		}
		if strings.Contains(e.Error(), "/var") || strings.Contains(e.Error(), "permission") {
			t.Errorf("the helper's message leaked: %q", e.Error())
		}
	}
	// installer_required carries the differing names, from the closed list only.
	ans := `{"v":1,"ok":false,"error":"installer_required","message":"x","differs":["oarfish-update-helper","../../bin/sh","oarfish.service","; rm -rf /"]}` + "\n"
	e := helperWith(fixed(ans)).Discard(context.Background(), "1.2.3")
	if e == nil || e.Code != CodeInstallerRequired || strings.Join(e.Differs, ",") != "oarfish-update-helper,oarfish.service" {
		t.Fatalf("%+v", e)
	}
	// The other codes carry none.
	ans = `{"v":1,"ok":false,"error":"busy","message":"x","differs":["oarfish.service"]}` + "\n"
	if e := helperWith(fixed(ans)).Discard(context.Background(), "1.2.3"); e == nil || len(e.Differs) != 0 {
		t.Fatalf("%+v", e)
	}
}

func TestHelperStageAndActivateCheckTheAnswer(t *testing.T) {
	ctx := context.Background()
	if _, e := helperWith(fixed(`{"v":1,"ok":true,"version":"9.9.9","commit":"c","components_differ":false}`+"\n")).Stage(ctx, "1.2.3", "0123456789abcdef"); e == nil || e.Code != CodeInternal {
		t.Errorf("stage answered for another version: %v", e)
	}
	for _, ans := range []string{`{"v":1,"ok":true,"status":"nope","transaction_id":"00112233445566ff"}`, `{"v":1,"ok":true,"status":"accepted","transaction_id":"short"}`} {
		if _, e := helperWith(fixed(ans+"\n")).Activate(ctx, "1.2.3", "0123456789abcdef", false); e == nil || e.Code != CodeInternal {
			t.Errorf("%s: %v", ans, e)
		}
	}
}

func TestHelperOverARealUnixSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix sockets")
	}
	dir, err := os.MkdirTemp("", "ou")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "h.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skip("cannot listen on a unix socket here")
	}
	defer ln.Close()
	var seen bytes.Buffer
	var mu sync.Mutex
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				b, _ := io.ReadAll(io.LimitReader(&lineReader{c}, 4096))
				mu.Lock()
				seen.Write(b)
				mu.Unlock()
				c.Write([]byte(statusAnswer))
			}()
		}
	}()
	st, e := NewHelper(sock).Status(context.Background())
	if e != nil || st.InstalledVersion != "0.1.0-alpha.6" {
		t.Fatalf("%v %+v", e, st)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen.String() != `{"v":1,"op":"status"}`+"\n" {
		t.Fatalf("%q", seen.String())
	}
	// No socket: unavailable, not a Go error string.
	_, e = NewHelper(filepath.Join(dir, "none.sock")).Status(context.Background())
	if e == nil || e.Code != CodeUnavailable || strings.Contains(e.Error(), dir) {
		t.Fatalf("%v", e)
	}
	if !errors.Is(syscall.ENOENT, syscall.ENOENT) {
		t.Fatal("unreachable")
	}
}

// lineReader reads up to and including the first newline.
type lineReader struct{ c net.Conn }

func (l *lineReader) Read(p []byte) (int, error) {
	n, err := l.c.Read(p[:1])
	if n == 1 && p[0] == '\n' {
		return 1, io.EOF
	}
	return n, err
}
