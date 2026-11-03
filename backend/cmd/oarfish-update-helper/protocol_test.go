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

		// The four known requests that the stub does not implement.
		{name: "stage (not implemented)", in: []byte(`{"v":1,"op":"stage"}` + "\n"), want: codeBadRequest},
		{name: "stage with arguments (not implemented)", in: []byte(`{"v":1,"op":"stage","version":"1.2.3","correlation_id":"0123456789abcdef"}` + "\n"), want: codeBadRequest},
		{name: "activate (not implemented)", in: []byte(`{"v":1,"op":"activate","version":"1.2.3","correlation_id":"0123456789abcdef","components_confirmed":false}` + "\n"), want: codeBadRequest},
		{name: "discard (not implemented)", in: []byte(`{"v":1,"op":"discard","version":"1.2.3"}` + "\n"), want: codeBadRequest},
		{name: "ack (not implemented)", in: []byte(`{"v":1,"op":"ack","transaction_id":"0123456789abcdef"}` + "\n"), want: codeBadRequest},

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
	// version the pattern allows.
	long := "1.2." + strings.Repeat("9", 60) // 64 characters
	src := statusSource{helperVersion: long, keyID: testKeyID, root: testRoot(t)}
	body, code := src.body()
	if code != "" || len(body) > maxResponseBytes/4 {
		t.Fatalf("status answer: code %q, %d bytes", code, len(body))
	}
	for c := range errorTexts {
		if n := len(errorBody(c)); n > 256 {
			t.Errorf("error answer %s is %d bytes", c, n)
		}
	}

	// A status answer above the bound is replaced by an error, not truncated.
	ts := startServer(t, func(o *options) { o.maxResponse = 120 })
	wantError(t, ts.exchange(t, []byte(okStatus), false), codeInternal)

	if maxResponseBytes != 8192 || maxRequestBytes != 4096 {
		t.Fatalf("limits changed: request %d, response %d", maxRequestBytes, maxResponseBytes)
	}
}

func TestStatusAnswer(t *testing.T) {
	root := testRoot(t)
	src := statusSource{helperVersion: "test-1", keyID: testKeyID, root: root}
	body, code := src.body()
	if code != "" {
		t.Fatal(code)
	}
	free, err := freeBytes(root)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	// Exactly these members, in this order.
	want := `{"v":1,"ok":true,"protocol_min":1,"protocol_max":1,"helper_version":"test-1",` +
		`"key_id":"11f1c52777090bfc","installed_version":"0.1.0-alpha.8","phase":"idle","free_bytes":`
	if got := string(body); !strings.HasPrefix(got, want) || !regexp.MustCompile(`^\d+\}\n$`).MatchString(got[len(want):]) {
		t.Fatalf("status answer:\n got %s\nwant %s<digits>}", got, want)
	}
	if len(m) != 9 {
		t.Fatalf("%d members, want 9", len(m))
	}
	if m["free_bytes"].(float64) <= 0 || free == 0 {
		t.Fatalf("free_bytes %v", m["free_bytes"])
	}
}

func TestStatusWithoutInstalledVersionAndWithUnreadableRoot(t *testing.T) {
	empty := shortDir(t)
	body, code := statusSource{helperVersion: "x", keyID: testKeyID, root: empty}.body()
	if code != "" || !strings.Contains(string(body), `"installed_version":""`) {
		t.Fatalf("code %q, body %q", code, body)
	}
	if _, code := (statusSource{root: empty + "/does-not-exist"}).body(); code != codeIOError {
		t.Fatalf("code %q, want io_error", code)
	}
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
		if req.V == nil || req.Op == nil || *req.V != 1 {
			t.Fatalf("accepted a request without a valid v and op: %q", data)
		}
		// An accepted request is one plain object: its canonical form is
		// accepted again and means the same.
		canon, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		again, err := decodeRequest(canon)
		if err != nil || *again.V != *req.V || *again.Op != *req.Op {
			t.Fatalf("canonical form %q of an accepted request is not accepted again (%v)", canon, err)
		}
	})
}
