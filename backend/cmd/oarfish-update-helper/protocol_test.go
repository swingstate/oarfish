package main

import (
	"bytes"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	okStatus = `{"v":1,"op":"status"}` + "\n"
)

func TestProtocol(t *testing.T) {
	ts := startServer(t, nil)

	tests := []struct {
		name       string
		in         []byte
		closeWrite bool
		want       errorCode // empty: a valid status answer
	}{
		{name: "valid status", in: []byte(okStatus)},
		{name: "valid status with CRLF", in: []byte(`{"v":1,"op":"status"}` + "\r\n")},
		{name: "valid status with spaces around", in: []byte(` {"v" : 1 , "op" : "status"} ` + "\n")},
		{name: "valid status, keys swapped", in: []byte(`{"op":"status","v":1}` + "\n")},
		{name: "exactly 4096 bytes including the newline", in: padded(4096)},

		{name: "unknown field", in: []byte(`{"v":1,"op":"status","x":"` + marker + `"}` + "\n"), want: codeBadRequest},
		{name: "unknown field with an object value", in: []byte(`{"v":1,"op":"status","x":{"a":[1,2]}}` + "\n"), want: codeBadRequest},
		{name: "trailing text after the object", in: []byte(`{"v":1,"op":"status"} ` + marker + "\n"), want: codeBadRequest},
		{name: "second object after the first", in: []byte(`{"v":1,"op":"status"}{"v":1,"op":"status"}` + "\n"), want: codeBadRequest},
		{name: "trailing comma token", in: []byte(`{"v":1,"op":"status"},` + "\n"), want: codeBadRequest},
		{name: "second request after the newline", in: []byte(okStatus + okStatus), want: codeBadRequest},
		{name: "garbage after the newline", in: []byte(okStatus + marker), want: codeBadRequest},

		{name: "4097 bytes including the newline", in: padded(4097), want: codeBadRequest},
		{name: "10000 bytes with a newline at the end", in: append(bytes.Repeat([]byte("a"), 9999), '\n'), want: codeBadRequest},
		{name: "10000 bytes without a newline", in: bytes.Repeat([]byte(marker), 1000), want: codeBadRequest},

		{name: "no newline, input ends", in: []byte(`{"v":1,"op":"status"}`), closeWrite: true, want: codeBadRequest},
		{name: "partial object, input ends", in: []byte(`{"v":1,"op":"sta`), closeWrite: true, want: codeBadRequest},

		{name: "v 0", in: []byte(`{"v":0,"op":"status"}` + "\n"), want: codeBadRequest},
		{name: "v 2", in: []byte(`{"v":2,"op":"status"}` + "\n"), want: codeBadRequest},
		{name: "v 99", in: []byte(`{"v":99,"op":"status"}` + "\n"), want: codeBadRequest},
		{name: "v negative", in: []byte(`{"v":-1,"op":"status"}` + "\n"), want: codeBadRequest},
		{name: "v fraction", in: []byte(`{"v":1.5,"op":"status"}` + "\n"), want: codeBadRequest},
		{name: "v written as 1.0", in: []byte(`{"v":1.0,"op":"status"}` + "\n"), want: codeBadRequest},
		{name: "v as string", in: []byte(`{"v":"1","op":"status"}` + "\n"), want: codeBadRequest},
		{name: "v null", in: []byte(`{"v":null,"op":"status"}` + "\n"), want: codeBadRequest},
		{name: "v huge", in: []byte(`{"v":1e999,"op":"status"}` + "\n"), want: codeBadRequest},
		{name: "v missing", in: []byte(`{"op":"status"}` + "\n"), want: codeBadRequest},

		{name: "unknown op", in: []byte(`{"v":1,"op":"` + marker + `"}` + "\n"), want: codeBadRequest},
		{name: "op in other case", in: []byte(`{"v":1,"op":"Status"}` + "\n"), want: codeBadRequest},
		{name: "op empty", in: []byte(`{"v":1,"op":""}` + "\n"), want: codeBadRequest},
		{name: "op null", in: []byte(`{"v":1,"op":null}` + "\n"), want: codeBadRequest},
		{name: "op number", in: []byte(`{"v":1,"op":7}` + "\n"), want: codeBadRequest},
		{name: "op missing", in: []byte(`{"v":1}` + "\n"), want: codeBadRequest},
		{name: "status with trailing space in the name", in: []byte(`{"v":1,"op":"status "}` + "\n"), want: codeBadRequest},

		// activate and ack decode; the test server has an empty state directory, so a
		// well-formed one is not_found and a malformed one bad_request.
		{name: "activate, nothing staged", in: []byte(`{"v":1,"op":"activate","version":"1.2.3","correlation_id":"0123456789abcdef","components_confirmed":false}` + "\n"), want: codeNotFound},
		{name: "activate without arguments", in: []byte(`{"v":1,"op":"activate"}` + "\n"), want: codeBadRequest},
		{name: "ack, no last result", in: []byte(`{"v":1,"op":"ack","transaction_id":"0123456789abcdef"}` + "\n"), want: codeNotFound},
		{name: "ack without arguments", in: []byte(`{"v":1,"op":"ack"}` + "\n"), want: codeBadRequest},
		{name: "status with an argument", in: []byte(`{"v":1,"op":"status","version":"1.2.3"}` + "\n"), want: codeBadRequest},

		{name: "array", in: []byte(`[{"v":1,"op":"status"}]` + "\n"), want: codeBadRequest},
		{name: "empty array", in: []byte("[]\n"), want: codeBadRequest},
		{name: "string", in: []byte(`"` + marker + `"` + "\n"), want: codeBadRequest},
		{name: "number", in: []byte("42\n"), want: codeBadRequest},
		{name: "null", in: []byte("null\n"), want: codeBadRequest},
		{name: "true", in: []byte("true\n"), want: codeBadRequest},
		{name: "not JSON", in: []byte(marker + "\n"), want: codeBadRequest},
		{name: "NUL bytes", in: []byte("\x00\x00\x00\n"), want: codeBadRequest},
		{name: "unterminated object", in: []byte(`{"v":1,"op":"status"` + "\n"), want: codeBadRequest},

		{name: "empty line", in: []byte("\n"), want: codeBadRequest},
		{name: "only spaces", in: []byte("   \n"), want: codeBadRequest},
		{name: "empty request, input ends", in: nil, closeWrite: true, want: codeBadRequest},

		{name: "key in other case", in: []byte(`{"V":1,"OP":"status"}` + "\n"), want: codeBadRequest},
		{name: "op in other case as key", in: []byte(`{"v":1,"Op":"status"}` + "\n"), want: codeBadRequest},
		{name: "duplicate op", in: []byte(`{"v":1,"op":"status","op":"status"}` + "\n"), want: codeBadRequest},
		{name: "duplicate v", in: []byte(`{"v":1,"v":1,"op":"status"}` + "\n"), want: codeBadRequest},
		{name: "duplicate v, last one valid", in: []byte(`{"v":2,"v":1,"op":"status"}` + "\n"), want: codeBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := ts.exchange(t, tt.in, tt.closeWrite)
			if tt.want != "" {
				wantError(t, raw, tt.want)
				return
			}
			r := parseReply(t, raw)
			if !r.OK || r.Error != "" || r.V != 1 || r.Phase != "idle" {
				t.Fatalf("not a status answer: %q", raw)
			}
		})
	}
}

// One connection serves one request: the server answers and closes, so a
// second request on the same connection gets nothing.
func TestOneRequestPerConnection(t *testing.T) {
	ts := startServer(t, nil)
	c := ts.dial(t)
	c.SetDeadline(time.Now().Add(10 * time.Second))
	c.Write([]byte(okStatus))
	buf := make([]byte, 4096)
	n, _ := c.Read(buf)
	if r := parseReply(t, buf[:n]); !r.OK {
		t.Fatalf("first request not answered: %q", buf[:n])
	}
	c.Write([]byte(okStatus))
	if n, _ := c.Read(buf); n != 0 {
		t.Fatalf("second request on the same connection was answered: %q", buf[:n])
	}
}

// The five seconds to send a request are enforced; the test shortens them.
func TestNoNewlineWithinTheDeadline(t *testing.T) {
	ts := startServer(t, func(o *options) { o.requestDeadline = 200 * time.Millisecond })
	c := ts.dial(t)
	c.SetDeadline(time.Now().Add(10 * time.Second))
	c.Write([]byte(`{"v":1,"op":"status"}`)) // no newline, and the sender waits
	start := time.Now()
	buf := make([]byte, 4096)
	n, _ := c.Read(buf)
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Fatalf("answered after %v, before the deadline", elapsed)
	}
	wantError(t, buf[:n], codeBadRequest)
}

func TestSlowSenderWithinTheDeadlineIsServed(t *testing.T) {
	ts := startServer(t, func(o *options) { o.requestDeadline = 2 * time.Second })
	c := ts.dial(t)
	c.SetDeadline(time.Now().Add(10 * time.Second))
	c.Write([]byte(`{"v":1,`))
	time.Sleep(100 * time.Millisecond)
	c.Write([]byte(`"op":"status"}` + "\n"))
	buf := make([]byte, 4096)
	n, _ := c.Read(buf)
	if r := parseReply(t, buf[:n]); !r.OK {
		t.Fatalf("not served: %q", buf[:n])
	}
}

func TestResponseSizeBound(t *testing.T) {
	// The real status answer is far below the bound, even with the longest
	// version the pattern allows in every version member and a last result.
	long := "1.2." + strings.Repeat("9", 60) // 64 characters
	v := newTenv(t)
	v.h.helperVersion = long
	st := freshState(long)
	st.Previous = long
	st.Txn = txn{ID: "0123456789abcdef", Phase: phaseStaged, To: long, From: long, CorrelationID: testCID, StagedAtUptimeS: 1000}
	st.Last = &lastResult{TransactionID: testCID, To: long, From: long, Outcome: "components_rolled_back",
		ReasonCode: strings.Repeat("a", 32), CorrelationID: testCID, FinishedAt: "2023-11-14T22:13:20.123456789+01:00"}
	v.writeState(st)
	body := v.request([]byte(okStatus))
	if r := parseReply(t, body); !r.OK || len(body) > maxResponseBytes/4 {
		t.Fatalf("status answer: %d bytes: %s", len(body), body)
	}
	for c := range errorTexts {
		if n := len(errorBody(c)); n > 256 {
			t.Errorf("error answer %s is %d bytes", c, n)
		}
	}
	if n := len(differsBody(componentNames)); n > 512 {
		t.Errorf("installer_required answer is %d bytes", n)
	}

	// A status answer above the bound is replaced by an error, not truncated.
	ts := startServer(t, func(o *options) { o.maxResponse = 120 })
	wantError(t, ts.exchange(t, []byte(okStatus), false), codeInternal)

	if maxResponseBytes != 8192 || maxRequestBytes != 4096 {
		t.Fatalf("limits changed: request %d, response %d", maxRequestBytes, maxResponseBytes)
	}
}

func TestStatusAnswer(t *testing.T) {
	v := newTenv(t)
	free, err := freeBytes(v.root)
	if err != nil {
		t.Fatal(err)
	}
	body := v.request([]byte(okStatus))
	// Exactly these members, in this order.
	want := `{"v":1,"ok":true,"protocol_min":1,"protocol_max":1,"helper_version":"protocol-1",` +
		`"key_id":"11f1c52777090bfc","installed_version":"1.0.0","phase":"idle","staged_version":"",` +
		`"previous_version":"","last_result":null,"needs_attention":false,"free_bytes":`
	if got := string(body); !strings.HasPrefix(got, want) ||
		!regexp.MustCompile(`^\d+,"cooldown_version":"","cooldown_remaining_s":0\}\n$`).MatchString(got[len(want):]) {
		t.Fatalf("status answer:\n got %s\nwant %s<digits>}", got, want)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if len(m) != 15 {
		t.Fatalf("%d members, want 15", len(m))
	}
	if m["free_bytes"].(float64) <= 0 || free == 0 {
		t.Fatalf("free_bytes %v", m["free_bytes"])
	}
}

func TestStatusWithoutInstalledVersionAndWithUnreadableRoot(t *testing.T) {
	v := newTenv(t)
	os.Remove(v.root + "/current")
	if r := v.status(); r.InstalledVersion != "" || !r.OK {
		t.Fatalf("status without current: %+v", r)
	}
	v.h.root = v.root + "/does-not-exist"
	wantErrCode(t, v.request([]byte(okStatus)), codeIOError)
}

func TestInstalledVersion(t *testing.T) {
	tests := []struct {
		target string // "" means: no link at all
		want   string
	}{
		{"releases/0.1.0-alpha.8", "0.1.0-alpha.8"},
		{"releases/1.2.3", "1.2.3"},
		{"releases/10.20.30-rc.1", "10.20.30-rc.1"},
		{"releases/" + "1.2.3-" + strings.Repeat("a", 58), "1.2.3-" + strings.Repeat("a", 58)}, // 64 characters
		{"releases/" + "1.2.3-" + strings.Repeat("a", 59), ""},                                 // 65
		{"", ""},
		{"releases/v1.2.3", ""},
		{"releases/1.2", ""},
		{"releases/01.2.3", ""},
		{"releases/1.2.3+build", ""},
		{"releases/1.2.3-a..b", ""},
		{"releases/1.2.3/", ""},
		{"releases/", ""},
		{"releases/..", ""},
		{"releases/../1.2.3", ""},
		{"releases/x/1.2.3", ""},
		{"other/1.2.3", ""},
		{"1.2.3", ""},
		{"/opt/oarfish/releases/1.2.3", ""},
		{"releases/1.2.3\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			root := shortDir(t)
			if tt.target != "" {
				if err := os.Symlink(tt.target, root+"/current"); err != nil {
					t.Fatal(err)
				}
			}
			if got := installedVersion(root); got != tt.want {
				t.Fatalf("installedVersion = %q, want %q", got, tt.want)
			}
		})
	}
	t.Run("regular file", func(t *testing.T) {
		root := shortDir(t)
		os.WriteFile(root+"/current", []byte("releases/1.2.3"), 0o644)
		if got := installedVersion(root); got != "" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("directory", func(t *testing.T) {
		root := shortDir(t)
		os.Mkdir(root+"/current", 0o755)
		if got := installedVersion(root); got != "" {
			t.Fatalf("got %q", got)
		}
	})
}

// The error codes are the closed list of U2. This reads the ADR, so that the
// code and the text cannot drift apart without a failing test.
func TestErrorCodesMatchTheADR(t *testing.T) {
	adr, err := os.ReadFile("../../../docs/adr/0010-update-helper.md")
	if err != nil {
		t.Fatal(err)
	}
	var line string
	for _, l := range strings.Split(string(adr), "\n") {
		if strings.Contains(l, "errors are typed with a closed set of codes") {
			line = l
		}
	}
	if line == "" {
		t.Fatal("the sentence on the error codes is not in the ADR")
	}
	line, _, _ = strings.Cut(line, "An error never echoes")
	want := map[string]bool{}
	for _, m := range regexp.MustCompile("`([a-z_]+)`").FindAllStringSubmatch(line, -1) {
		want[m[1]] = true
	}
	if len(want) != 21 {
		t.Fatalf("read %d codes from the ADR, want 21", len(want))
	}
	got := map[string]bool{}
	for c, text := range errorTexts {
		got[string(c)] = true
		if text == "" || !strings.HasSuffix(text, ".") {
			t.Errorf("code %s has no fixed text", c)
		}
	}
	for c := range want {
		if !got[c] {
			t.Errorf("code %s of the ADR is missing", c)
		}
	}
	for c := range got {
		if !want[c] {
			t.Errorf("code %s is not in the ADR", c)
		}
	}
}

func TestRequestNamesMatchTheADR(t *testing.T) {
	// U2: status, stage, activate, discard, ack.
	got := []string{opStatus, opStage, opActivate, opDiscard, opAck}
	if strings.Join(got, ",") != "status,stage,activate,discard,ack" {
		t.Fatalf("requests: %v", got)
	}
	if protocolMin != 1 || protocolMax != 1 {
		t.Fatalf("protocol range %d..%d, want 1..1", protocolMin, protocolMax)
	}
}

// Seeds run as a normal test; `go test -fuzz FuzzDecodeRequest` explores.
func FuzzDecodeRequest(f *testing.F) {
	seeds := []string{
		`{"v":1,"op":"stage","version":"1.2.3","correlation_id":"0123456789abcdef"}` + "\n",
		`{"v":1,"op":"discard","version":"0.1.0-alpha.8"}` + "\n",
		`{"v":1,"op":"stage","version":"1.2.3"}`, `{"v":1,"op":"discard","version":"../x"}`,
		`{"v":1,"op":"activate","version":"1.2.3","correlation_id":"0123456789abcdef","components_confirmed":true}`,
		`{"v":1,"op":"ack","transaction_id":"0123456789abcdef"}`,
		`{"v":1,"op":"stage","version":"1.2.3","correlation_id":"0123456789ABCDEF"}`,
		`{"v":1,"op":"stage","Version":"1.2.3","correlation_id":"0123456789abcdef"}`,
		`{"v":1,"op":"stage","version":null,"correlation_id":"0123456789abcdef"}`,
		okStatus, `{"v":1,"op":"status"}`, "\n", "", "{}", "[]", "null", `"x"`, "42",
		`{"v":1,"op":"status","x":1}` + "\n", `{"v":1,"op":"status"}{}` + "\n",
		`{"v":2,"op":"status"}` + "\n", `{"v":1,"op":"stage","version":"1.2.3"}` + "\n",
		`{"V":1,"OP":"status"}` + "\n", `{"v":1,"v":1,"op":"status"}` + "\n",
		`{"v":1e999,"op":"status"}`, `{"v":1,"op":"status"}`, `{"v":1,"op":"status"` + "\x00",
		`{"v":1,"op":"status"}` + "\r\n", okStatus + okStatus,
		strings.Repeat("[", 5000), `{"v":1,"op":"status","x":` + strings.Repeat("[", 3000),
		string(padded(4096)), string(padded(4097)), "\xff\xfe\n",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if line, err := readRequest(bytes.NewReader(data)); err == nil {
			if len(line)+1 > maxRequestBytes || bytes.IndexByte(line, '\n') >= 0 {
				t.Fatalf("readRequest returned a line of %d bytes: %q", len(line), line)
			}
		} else if err != errMalformed {
			t.Fatalf("readRequest error %v", err)
		}

		req, err := decodeRequest(data)
		if err != nil {
			if err != errMalformed {
				t.Fatalf("decodeRequest error %v", err)
			}
			return
		}
		if req.V != 1 || (req.Op != opStatus && req.Op != opStage && req.Op != opDiscard && req.Op != opActivate && req.Op != opAck) {
			t.Fatalf("accepted a request without a valid v and op: %q", data)
		}
		hasVersion := req.Op == opStage || req.Op == opDiscard || req.Op == opActivate
		hasCID := req.Op == opStage || req.Op == opActivate
		if hasVersion != validVersion(req.Version) || hasCID != isHex(16)(req.CorrelationID) ||
			(req.Op == opAck) != isHex(16)(req.TransactionID) || (req.Confirmed && req.Op != opActivate) {
			t.Fatalf("accepted arguments that do not fit the op: %q -> %+v", data, req)
		}
		// An accepted request is one plain object: its canonical form is
		// accepted again and means the same.
		m := map[string]any{"v": req.V, "op": req.Op}
		if req.Version != "" {
			m["version"] = req.Version
		}
		if req.CorrelationID != "" {
			m["correlation_id"] = req.CorrelationID
		}
		if req.Op == opActivate {
			m["components_confirmed"] = req.Confirmed
		}
		if req.TransactionID != "" {
			m["transaction_id"] = req.TransactionID
		}
		canon, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		again, err := decodeRequest(canon)
		if err != nil || again != req {
			t.Fatalf("canonical form %q of an accepted request is not accepted again (%v)", canon, err)
		}
	})
}

// ---- the per-op decoder (G6.5) --------------------------------------------

func TestDecodeRequestPerOp(t *testing.T) {
	const cid = `"0123456789abcdef"`
	good := map[string]request{
		`{"v":1,"op":"status"}`: {V: 1, Op: "status"},
		`{"op":"status","v":1}`: {V: 1, Op: "status"},
		`{"v":1,"op":"stage","version":"1.2.3","correlation_id":` + cid + `}`:                                 {V: 1, Op: "stage", Version: "1.2.3", CorrelationID: "0123456789abcdef"},
		`{"correlation_id":` + cid + `,"version":"0.1.0-alpha.8","op":"stage","v":1}`:                         {V: 1, Op: "stage", Version: "0.1.0-alpha.8", CorrelationID: "0123456789abcdef"},
		`{"v":1,"op":"discard","version":"10.20.30-rc.1"}`:                                                    {V: 1, Op: "discard", Version: "10.20.30-rc.1"},
		`{"v":1,"op":"activate","version":"1.2.3","correlation_id":` + cid + `,"components_confirmed":true}`:  {V: 1, Op: "activate", Version: "1.2.3", CorrelationID: "0123456789abcdef", Confirmed: true},
		`{"components_confirmed":false,"correlation_id":` + cid + `,"version":"1.2.3","op":"activate","v":1}`: {V: 1, Op: "activate", Version: "1.2.3", CorrelationID: "0123456789abcdef"},
		`{"v":1,"op":"ack","transaction_id":` + cid + `}`:                                                     {V: 1, Op: "ack", TransactionID: "0123456789abcdef"},
	}
	for in, want := range good {
		got, err := decodeRequest([]byte(in))
		if err != nil || got != want {
			t.Errorf("%s: got %+v, %v; want %+v", in, got, err, want)
		}
	}

	bad := []string{
		// status accepts exactly v and op
		`{"v":1,"op":"status","version":"1.2.3"}`,
		`{"v":1,"op":"status","correlation_id":` + cid + `}`,
		// stage accepts exactly v, op, version, correlation_id
		`{"v":1,"op":"stage"}`,
		`{"v":1,"op":"stage","version":"1.2.3"}`,
		`{"v":1,"op":"stage","correlation_id":` + cid + `}`,
		`{"v":1,"op":"stage","version":"1.2.3","correlation_id":` + cid + `,"components_confirmed":true}`,
		`{"v":1,"op":"stage","version":"1.2.3","correlation_id":` + cid + `,"transaction_id":` + cid + `}`,
		// discard accepts exactly v, op, version
		`{"v":1,"op":"discard"}`,
		`{"v":1,"op":"discard","version":"1.2.3","correlation_id":` + cid + `}`,
		// rules of the stub decoder, for the new members too
		`{"v":1,"op":"stage","version":"1.2.3","version":"1.2.4","correlation_id":` + cid + `}`,
		`{"v":1,"op":"stage","Version":"1.2.3","correlation_id":` + cid + `}`,
		`{"v":1,"op":"stage","version":"1.2.3","CORRELATION_ID":` + cid + `}`,
		`{"v":1,"op":"stage","version":null,"correlation_id":` + cid + `}`,
		`{"v":1,"op":"stage","version":"1.2.3","correlation_id":null}`,
		`{"v":1,"op":"stage","version":1,"correlation_id":` + cid + `}`,
		`{"v":1,"op":"stage","version":["1.2.3"],"correlation_id":` + cid + `}`,
		`{"v":1,"op":"stage","version":"1.2.3","correlation_id":16}`,
		`{"v":1,"op":"stage","version":"1.2.3","correlation_id":` + cid + `} {}`,
		`{"v":1,"op":"discard","version":"1.2.3"}x`,
		// version rules of U2
		`{"v":1,"op":"discard","version":""}`,
		`{"v":1,"op":"discard","version":"v1.2.3"}`,
		`{"v":1,"op":"discard","version":"1.2"}`,
		`{"v":1,"op":"discard","version":"01.2.3"}`,
		`{"v":1,"op":"discard","version":"1.2.3+build"}`,
		`{"v":1,"op":"discard","version":"1.2.3-a..b"}`,
		`{"v":1,"op":"discard","version":"../1.2.3"}`,
		`{"v":1,"op":"discard","version":"1.2.3/../../x"}`,
		`{"v":1,"op":"discard","version":"1.2.3/x"}`,
		`{"v":1,"op":"discard","version":"1.2.3-a/b"}`,
		`{"v":1,"op":"discard","version":"1.2.3-/"}`,
		`{"v":1,"op":"discard","version":"1.2.3-a\\b"}`,
		`{"v":1,"op":"discard","version":"1.2.3-a b"}`,
		`{"v":1,"op":"discard","version":"1.2.3-a:b"}`,
		`{"v":1,"op":"discard","version":"1.2.3-\u00e9"}`,
		`{"v":1,"op":"discard","version":"1.2.3-a\u0000b"}`,
		`{"v":1,"op":"discard","version":"1.2.3\n"}`,
		`{"v":1,"op":"discard","version":"1.2.3 "}`,
		`{"v":1,"op":"discard","version":"1.2.3-` + strings.Repeat("a", 59) + `"}`, // 65 characters
		// correlation_id: exactly 16 lowercase hexadecimal digits
		`{"v":1,"op":"stage","version":"1.2.3","correlation_id":""}`,
		`{"v":1,"op":"stage","version":"1.2.3","correlation_id":"0123456789abcde"}`,
		`{"v":1,"op":"stage","version":"1.2.3","correlation_id":"0123456789abcdef0"}`,
		`{"v":1,"op":"stage","version":"1.2.3","correlation_id":"0123456789ABCDEF"}`,
		`{"v":1,"op":"stage","version":"1.2.3","correlation_id":"0123456789abcdeg"}`,
		`{"v":1,"op":"stage","version":"1.2.3","correlation_id":"0123456789abcde\n"}`,
		// activate and ack: exact member sets and types
		`{"v":1,"op":"activate","version":"1.2.3","correlation_id":"0123456789abcdef"}`,
		`{"v":1,"op":"activate","version":"1.2.3","correlation_id":"0123456789abcdef","components_confirmed":"true"}`,
		`{"v":1,"op":"activate","version":"1.2.3","correlation_id":"0123456789abcdef","components_confirmed":1}`,
		`{"v":1,"op":"activate","version":"1.2.3","correlation_id":"0123456789abcdef","components_confirmed":null}`,
		`{"v":1,"op":"activate","version":"1.2.3","correlation_id":"0123456789abcdef","components_confirmed":true,"x":1}`,
		`{"v":1,"op":"activate","version":"1.2.3","components_confirmed":true}`,
		`{"v":1,"op":"activate","correlation_id":"0123456789abcdef","components_confirmed":true}`,
		`{"v":1,"op":"activate","version":"1.2.3","correlation_id":"0123456789abcdef","components_confirmed":true,"transaction_id":"0123456789abcdef"}`,
		`{"v":1,"op":"ack"}`,
		`{"v":1,"op":"ack","transaction_id":null}`,
		`{"v":1,"op":"ack","transaction_id":"0123456789ABCDEF"}`,
		`{"v":1,"op":"ack","transaction_id":"0123456789abcde"}`,
		`{"v":1,"op":"ack","transaction_id":"0123456789abcdef","version":"1.2.3"}`,
		`{"v":1,"op":"stage","version":"1.2.3","correlation_id":"0123456789abcdef","components_confirmed":true}`,
		`{"v":1,"op":"discard","version":"1.2.3","transaction_id":"0123456789abcdef"}`,
	}
	for _, in := range bad {
		if got, err := decodeRequest([]byte(in)); err == nil {
			t.Errorf("accepted %s as %+v", in, got)
		}
	}
	// 64 characters is the longest version.
	long := "1.2.3-" + strings.Repeat("a", 58)
	if _, err := decodeRequest([]byte(`{"v":1,"op":"discard","version":"` + long + `"}`)); err != nil {
		t.Errorf("64-character version refused: %v", err)
	}
}

// The members of a malformed activate or ack are bad_request on the wire, and a
// refused request writes nothing.
func TestMalformedActivateAndAckAreBadRequest(t *testing.T) {
	v := newTenv(t)
	for _, line := range []string{
		`{"v":1,"op":"activate","version":"1.1.0","correlation_id":"0123456789abcdef"}`,
		`{"v":1,"op":"activate","version":"1.1.0","correlation_id":"0123456789abcdef","components_confirmed":"no"}`,
		`{"v":1,"op":"ack"}`,
		`{"v":1,"op":"ack","transaction_id":"nothex"}`,
	} {
		wantErrCode(t, v.request([]byte(line+"\n")), codeBadRequest)
	}
	if _, ok := v.readState(); ok {
		t.Fatal("a refused request wrote the state file")
	}
}

func TestErrorAnswersDoNotEchoTheRequest(t *testing.T) {
	v := newTenv(t)
	for _, line := range []string{
		`{"v":1,"op":"stage","version":"` + marker + `","correlation_id":"0123456789abcdef"}`,
		`{"v":1,"op":"` + marker + `"}`,
		`{"v":1,"op":"stage","version":"1.2.3","correlation_id":"` + marker + `"}`,
	} {
		raw := v.request([]byte(line + "\n"))
		wantErrCode(t, raw, codeBadRequest)
		if strings.Contains(string(raw), marker) {
			t.Fatalf("echoed: %s", raw)
		}
	}
}

// installer_required may carry one extra member, the differing names, taken
// from the closed list.
func TestInstallerRequiredBodyCarriesOnlyDiffers(t *testing.T) {
	raw := differsBody([]string{"oarfish-update-helper", "oarfish.service"})
	want := `{"v":1,"ok":false,"error":"installer_required","message":"This release must be installed with the installer.","differs":["oarfish-update-helper","oarfish.service"]}` + "\n"
	if string(raw) != want {
		t.Fatalf("got %s", raw)
	}
	if got := string(errorBody(codeBusy)); strings.Contains(got, "differs") {
		t.Fatalf("other errors carry no differs: %s", got)
	}
}

// validVersion is the version pattern of U2; the prerelease part may hold
// letters, digits, dots and hyphens and nothing else (no separator, no space).
func TestValidVersion(t *testing.T) {
	for _, v := range []string{"0.0.0", "1.2.3", "10.20.30", "1.2.3-a", "1.2.3-a.b-c", "0.1.0-alpha.6.1", "1.2.3-0", "1.2.3-A-z.9"} {
		if !validVersion(v) {
			t.Errorf("%q refused", v)
		}
	}
	for _, v := range []string{"", "1", "1.2", "1.2.3.4", "01.2.3", "1.02.3", "1.2.03", "v1.2.3", "1.2.3-", "1.2.3+b", "1.2.3-a+b", "1.2.3-a..b",
		"1.2.3-a/b", "1.2.3-/", "1.2.3/", "/1.2.3", "1.2.3-a\\b", "1.2.3-a b", "1.2.3 ", " 1.2.3", "1.2.3\n", "1.2.3-a:b", "1.2.3-a;b", "1.2.3-a_b",
		"1.2.3-\u00e9", "1.2.3-a\x00b", "../1.2.3", "..", "1.2.3-" + strings.Repeat("a", 59)} {
		if validVersion(v) {
			t.Errorf("%q accepted", v)
		}
	}
}

// ---- fuzzing the decoders of activate and ack (G6.6) -----------------------

func FuzzDecodeActivate(f *testing.F) {
	for _, s := range []string{
		`{"v":1,"op":"activate","version":"1.2.3","correlation_id":"0123456789abcdef","components_confirmed":false}`,
		`{"v":1,"op":"activate","version":"1.2.3","correlation_id":"0123456789abcdef","components_confirmed":true}`,
		`{"components_confirmed":true,"op":"activate","correlation_id":"0123456789abcdef","version":"0.1.0-alpha.8","v":1}`,
		`{"v":1,"op":"activate","version":"1.2.3","correlation_id":"0123456789abcdef","components_confirmed":null}`,
		`{"v":1,"op":"activate","version":"1.2.3","correlation_id":"0123456789abcdef","components_confirmed":"true"}`,
		`{"v":1,"op":"activate","version":"1.2.3","correlation_id":"0123456789abcdef","components_confirmed":true,"components_confirmed":false}`,
		`{"v":1,"op":"activate","version":"../1.2.3","correlation_id":"0123456789abcdef","components_confirmed":true}`,
		`{"v":1,"op":"activate"}`, `{"v":1,"op":"ack","transaction_id":"0123456789abcdef"}`, `{"V":1,"OP":"activate"}`, `[]`, `null`, ``,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		req, err := decodeRequest(data)
		if err != nil {
			if err != errMalformed {
				t.Fatalf("error %v", err)
			}
			return
		}
		if req.Op != opActivate {
			return
		}
		if req.V != 1 || !validVersion(req.Version) || !isHex(16)(req.CorrelationID) || req.TransactionID != "" {
			t.Fatalf("accepted %q as %+v", data, req)
		}
		// Exactly the five members, once each, in any order: the canonical form
		// is accepted again and means the same.
		canon := []byte(`{"v":1,"op":"activate","version":` + mustJSON(t, req.Version) + `,"correlation_id":` + mustJSON(t, req.CorrelationID) +
			`,"components_confirmed":` + mustJSON(t, req.Confirmed) + `}`)
		again, err := decodeRequest(canon)
		if err != nil || again != req {
			t.Fatalf("canonical form %q not accepted again (%v)", canon, err)
		}
	})
}

func FuzzDecodeAck(f *testing.F) {
	for _, s := range []string{
		`{"v":1,"op":"ack","transaction_id":"0123456789abcdef"}`,
		`{"transaction_id":"fedcba9876543210","op":"ack","v":1}`,
		`{"v":1,"op":"ack","transaction_id":"0123456789ABCDEF"}`,
		`{"v":1,"op":"ack","transaction_id":null}`,
		`{"v":1,"op":"ack","transaction_id":"0123456789abcdef","transaction_id":"0123456789abcdef"}`,
		`{"v":1,"op":"ack","transaction_id":"0123456789abcdef","x":1}`,
		`{"v":1,"op":"ack"}`, `{"v":1,"op":"ack","transaction_id":"0123456789abcdef\u0000"}`, `{"v":1,"op":"status"}`, ``, `{`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		req, err := decodeRequest(data)
		if err != nil {
			if err != errMalformed {
				t.Fatalf("error %v", err)
			}
			return
		}
		if req.Op != opAck {
			return
		}
		if req.V != 1 || !isHex(16)(req.TransactionID) || req.Version != "" || req.CorrelationID != "" || req.Confirmed {
			t.Fatalf("accepted %q as %+v", data, req)
		}
		canon := []byte(`{"v":1,"op":"ack","transaction_id":` + mustJSON(t, req.TransactionID) + `}`)
		again, err := decodeRequest(canon)
		if err != nil || again != req {
			t.Fatalf("canonical form %q not accepted again (%v)", canon, err)
		}
	})
}
