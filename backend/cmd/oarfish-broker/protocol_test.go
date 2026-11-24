package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// BF2: the strict decoder. Every one of these is bad_request and runs nothing.
func TestDecodeRejects(t *testing.T) {
	cid := `"correlation_id":"` + testCID + `"`
	bad := []string{
		``, ` `, `null`, `[]`, `"x"`, `{}`, `{"v":1}`, `{"op":"status"}`,
		`{"v":1,"op":"status"}`, // correlation_id missing
		`{"v":2,"op":"status",` + cid + `}`,
		`{"v":0,"op":"status",` + cid + `}`,
		`{"v":1.0,"op":"status",` + cid + `}`,
		`{"v":1e0,"op":"status",` + cid + `}`,
		`{"v":"1","op":"status",` + cid + `}`,
		`{"v":null,"op":"status",` + cid + `}`,
		`{"v":true,"op":"status",` + cid + `}`,
		`{"v":1,"op":"exec",` + cid + `}`,
		`{"v":1,"op":"service.reload","unit":"a.service",` + cid + `}`,
		`{"v":1,"op":"service.enable","unit":"a.service",` + cid + `}`,
		`{"v":1,"op":"system.halt",` + cid + `}`,
		`{"v":1,"op":"Status",` + cid + `}`,
		`{"v":1,"op":null,` + cid + `}`,
		`{"v":1,"op":1,` + cid + `}`,
		`{"v":1,"op":"status","unit":"a.service",` + cid + `}`,
		`{"v":1,"op":"system.reboot","unit":"a.service",` + cid + `}`,
		`{"v":1,"op":"service.start",` + cid + `}`, // unit missing
		`{"v":1,"op":"service.start","unit":null,` + cid + `}`,
		`{"v":1,"op":"service.start","unit":1,` + cid + `}`,
		`{"v":1,"op":"service.start","unit":["a.service"],` + cid + `}`,
		`{"v":1,"op":"status","correlation_id":"0123456789ABCDEF"}`,
		`{"v":1,"op":"status","correlation_id":"0123456789abcde"}`,
		`{"v":1,"op":"status","correlation_id":"0123456789abcdef0"}`,
		`{"v":1,"op":"status","correlation_id":"0123456789abcdeg"}`,
		`{"v":1,"op":"status","correlation_id":null}`,
		`{"v":1,"op":"status","correlation_id":1234567890123456}`,
		`{"v":1,"op":"status",` + cid + `,"extra":1}`,
		`{"v":1,"op":"status",` + cid + `,"V":1}`,
		`{"V":1,"op":"status",` + cid + `}`,
		`{"v":1,"OP":"status",` + cid + `}`,
		`{"v":1,"op":"status","op":"status",` + cid + `}`,
		`{"v":1,"v":1,"op":"status",` + cid + `}`,
		`{"v":1,"op":"status",` + cid + `}{}`,
		`{"v":1,"op":"status",` + cid + `} x`,
		`{"v":1,"op":"status",` + cid,
		`{"v":1,"op":"status",` + cid + `,"path":"/etc/shadow"}`,
		`{"v":1,"op":"service.start","unit":"a.service","property":"ExecStart",` + cid + `}`,
		`{"v":1,"op":"service.start","unit":"a.service","verb":"enable",` + cid + `}`,
		`{"v":1,"op":"service.start","unit":"a.service","args":["--now"],` + cid + `}`,
		`{"v":1,"op":"status","plugin":"x",` + cid + `}`,
		`{"v":1,"op":"status","caller":0,` + cid + `}`,
		`{"v":1,"op":"status",` + cid + `,"":1}`,
	}
	for _, line := range bad {
		if _, err := decodeRequest([]byte(line)); err == nil {
			t.Errorf("accepted %s", line)
		}
		tb := newTestBroker(t, &fakeSys{})
		if a := tb.ask(t, line); a.Code != codeBadRequest || a.State != "" || a.RetryAfterS != 0 {
			t.Errorf("%s: %+v", line, a)
		}
		if len(tb.sys.callList()) != 0 || len(tb.window) != 0 {
			t.Errorf("%s: something ran", line)
		}
	}
}

func TestDecodeAccepts(t *testing.T) {
	for line, want := range map[string]request{
		`{"v":1,"op":"status","correlation_id":"` + testCID + `"}`:                             {Op: opStatus, CorrelationID: testCID},
		`{"correlation_id":"` + testCID + `","op":"system.reboot","v":1}`:                      {Op: opSystemReboot, CorrelationID: testCID},
		` { "v" : 1 , "op" : "system.poweroff" , "correlation_id" : "` + testCID + `" } `:      {Op: opSystemPoweroff, CorrelationID: testCID},
		`{"v":1,"op":"service.start","unit":"a.service","correlation_id":"` + testCID + `"}`:   {Op: opServiceStart, Unit: "a.service", CorrelationID: testCID},
		`{"v":1,"op":"service.stop","unit":"bad name","correlation_id":"` + testCID + `"}`:     {Op: opServiceStop, Unit: "bad name", CorrelationID: testCID},
		`{"v":1,"op":"service.restart","unit":"","correlation_id":"` + testCID + `"}`:          {Op: opServiceRestart, Unit: "", CorrelationID: testCID},
		`{"v":1,"op":"service.restart","unit":"a.service","correlation_id":"` + testCID + `"}`: {Op: opServiceRestart, Unit: "a.service", CorrelationID: testCID},
	} {
		got, err := decodeRequest([]byte(line))
		if err != nil || got != want {
			t.Errorf("%s: %+v %v, want %+v", line, got, err, want)
		}
	}
}

// BF2: the line bound of 1 KiB including the newline, and trailing data.
func TestReadRequest(t *testing.T) {
	ok := strings.Repeat(" ", maxRequestBytes-1) + "\n"
	if line, err := readRequest(strings.NewReader(ok)); err != nil || len(line) != maxRequestBytes-1 {
		t.Fatalf("a request of exactly 1 KiB: %d %v", len(line), err)
	}
	for name, in := range map[string]string{
		"too long":       strings.Repeat(" ", maxRequestBytes) + "\n",
		"far too long":   strings.Repeat("x", 64<<10),
		"no newline":     `{"v":1}`,
		"empty":          "",
		"data after":     "{}\n{}\n",
		"byte after":     "{}\nx",
		"two newlines":   "{}\n\n",
		"newline inside": "{\n}",
	} {
		if _, err := readRequest(strings.NewReader(in)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A read error before the newline is malformed too.
	if _, err := readRequest(io.MultiReader(strings.NewReader("{"), errReader{})); !errors.Is(err, errMalformed) {
		t.Errorf("read error: %v", err)
	}
	// A request split over several reads.
	if line, err := readRequest(&slowReader{data: []byte(`{"v":1}` + "\n")}); err != nil || string(line) != `{"v":1}` {
		t.Errorf("slow reader: %q %v", line, err)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("x") }

type slowReader struct{ data []byte }

func (s *slowReader) Read(p []byte) (int, error) {
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	p[0] = s.data[0]
	s.data = s.data[1:]
	return 1, nil
}

// FuzzDecodeRequest: the decoder never panics, and what it accepts is one of
// the six operations with a valid correlation ID and a unit exactly for the
// service operations.
func FuzzDecodeRequest(f *testing.F) {
	for _, s := range []string{
		`{"v":1,"op":"status","correlation_id":"` + testCID + `"}`,
		`{"v":1,"op":"service.start","unit":"a.service","correlation_id":"` + testCID + `"}`,
		`{"v":1,"op":"system.reboot","correlation_id":"` + testCID + `"}`,
		`{"v":1,"op":"status","op":"status"}`, `{"v":1e0}`, `[`, `{"v":1,"op":"service.stop","unit":"\u0000"}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := decodeRequest(data)
		if err != nil {
			return
		}
		if _, ok := opKeys[r.Op]; !ok || !validCorrelationID(r.CorrelationID) {
			t.Fatalf("accepted %q as %+v", data, r)
		}
		if !isServiceOp(r.Op) && r.Unit != "" {
			t.Fatalf("unit on %s", r.Op)
		}
		if !bytes.Contains(data, []byte(`"v"`)) {
			t.Fatalf("accepted without v: %q", data)
		}
	})
}
