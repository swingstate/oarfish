package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fullState() *state {
	return &state{
		HelperVersion: helperID, Previous: "0.9.0",
		Txn: txn{ID: "00000000000000aa", Phase: "verifying", To: "1.1.0", From: "1.0.0",
			FromBinarySHA256: strings.Repeat("ab", 32), CorrelationID: testCID, StagedAtUptimeS: 1234,
			ComponentsFrom: "0.9.0", ComponentsTo: "1.0.0", KnownGoodSHA256: strings.Repeat("cd", 32), SocketRestartPending: true},
		Cool: cooldown{RefuseVersion: "1.1.0", RefuseUntilUptimeS: 2000, RefuseSetAtUptimeS: 1100, LastActivateUptimeS: 1100, LastActivateSetAtUptimeS: 1100},
		Last: &lastResult{TransactionID: "00000000000000bb", To: "1.0.0", From: "0.9.0", Outcome: "success", ReasonCode: "",
			CorrelationID: "fedcba9876543210", FinishedAt: "2023-11-14T22:13:20Z", Acknowledged: true},
	}
}

func TestStateRoundTrip(t *testing.T) {
	for _, s := range []*state{freshState("x"), fullState()} {
		got, err := decodeState(encodeState(s))
		if err != nil {
			t.Fatalf("%v\n%s", err, encodeState(s))
		}
		if !reflect.DeepEqual(got, s) {
			t.Fatalf("round trip:\n got %+v\nwant %+v", got, s)
		}
	}
}

// The file holds the full field set of U8 and nothing that identifies a host.
func TestStateFieldSet(t *testing.T) {
	var m map[string]any
	if err := jsonUnmarshal(encodeState(fullState()), &m); err != nil {
		t.Fatal(err)
	}
	keys := func(v any) string {
		var ks []string
		for k := range v.(map[string]any) {
			ks = append(ks, k)
		}
		return strings.Join(sortStrings(ks), ",")
	}
	if got, want := keys(m), "cooldown,helper_version,last_result,needs_attention,previous_version,schema_version,transaction"; got != want {
		t.Fatalf("top-level members %s, want %s", got, want)
	}
	if got, want := keys(m["transaction"]), "components_from,components_to,correlation_id,from,from_binary_sha256,id,known_good_sha256,phase,socket_restart_pending,staged_at_uptime_s,to"; got != want {
		t.Fatalf("transaction members %s", got)
	}
	if got, want := keys(m["cooldown"]), "last_activate_set_at_uptime_s,last_activate_uptime_s,refuse_set_at_uptime_s,refuse_until_uptime_s,refuse_version"; got != want {
		t.Fatalf("cooldown members %s", got)
	}
	if got, want := keys(m["last_result"]), "acknowledged,correlation_id,finished_at,from,outcome,reason_code,to,transaction_id"; got != want {
		t.Fatalf("last_result members %s", got)
	}
	host, _ := os.Hostname()
	enc := string(encodeState(fullState()))
	for _, bad := range []string{host, "http", "://", "/home", "/opt", "/var", "oarfish.local", "@"} {
		if bad != "" && strings.Contains(enc, bad) {
			t.Errorf("state file contains %q", bad)
		}
	}
}

func TestDecodeStateStrictness(t *testing.T) {
	ok := func(body string) string {
		return `{"schema_version":1,` + body + `}`
	}
	valid := []struct{ name, in string }{
		{"minimal", `{"schema_version":1}`},
		{"empty members", ok(`"transaction":{},"cooldown":{},"last_result":null`)},
		{"unknown top-level member", ok(`"future":{"a":[1,2]},"helper_version":"9.9.9"`)},
		{"unknown member in transaction", ok(`"transaction":{"phase":"idle","future_field":true}`)},
		{"unknown member in cooldown", ok(`"cooldown":{"x":1}`)},
		{"unknown member in last_result", ok(`"last_result":{"outcome":"success","extra":"x"}`)},
		{"every phase name of U7 and U15", ok(`"transaction":{"phase":"c_rolling_back"}`)},
		{"uptime zero", ok(`"transaction":{"staged_at_uptime_s":0}`)},
		{"version with prerelease", ok(`"previous_version":"0.1.0-alpha.6.1"`)},
		{"whitespace around", " \n" + ok(`"helper_version":"x"`) + "\n"},
	}
	for _, tt := range valid {
		if _, err := decodeState([]byte(tt.in)); err != nil {
			t.Errorf("%s: %v", tt.name, err)
		}
	}
	invalid := []struct{ name, in string }{
		{"empty", ``},
		{"not json", `x`},
		{"array", `[]`},
		{"null", `null`},
		{"no schema_version", `{}`},
		{"schema_version 2", `{"schema_version":2}`},
		{"schema_version 0", `{"schema_version":0}`},
		{"schema_version string", `{"schema_version":"1"}`},
		{"schema_version fraction", `{"schema_version":1.5}`},
		{"trailing data", `{"schema_version":1} {}`},
		{"trailing garbage", `{"schema_version":1}x`},
		{"duplicate top-level key", `{"schema_version":1,"schema_version":1}`},
		{"duplicate key in transaction", ok(`"transaction":{"phase":"idle","phase":"idle"}`)},
		{"case variant of a known key", ok(`"Transaction":{}`)},
		{"case variant in transaction", ok(`"transaction":{"Phase":"idle"}`)},
		{"transaction is a string", ok(`"transaction":"idle"`)},
		{"transaction null", ok(`"transaction":null`)},
		{"unknown phase", ok(`"transaction":{"phase":"working"}`)},
		{"phase null", ok(`"transaction":{"phase":null}`)},
		{"phase number", ok(`"transaction":{"phase":1}`)},
		{"version with slash", ok(`"transaction":{"to":"1.2.3/x"}`)},
		{"version with dotdot", ok(`"transaction":{"to":"1.2.3-a..b"}`)},
		{"version too long", ok(`"previous_version":"1.2.3-` + strings.Repeat("a", 59) + `"`)},
		{"version with v", ok(`"previous_version":"v1.2.3"`)},
		{"id too short", ok(`"transaction":{"id":"abc"}`)},
		{"id upper case", ok(`"transaction":{"id":"0123456789ABCDEF"}`)},
		{"correlation id not hex", ok(`"transaction":{"correlation_id":"0123456789abcdeg"}`)},
		{"sha256 too short", ok(`"transaction":{"from_binary_sha256":"ab"}`)},
		{"sha256 upper case", ok(`"transaction":{"known_good_sha256":"` + strings.Repeat("AB", 32) + `"}`)},
		{"negative uptime", ok(`"transaction":{"staged_at_uptime_s":-1}`)},
		{"uptime fraction", ok(`"transaction":{"staged_at_uptime_s":1.5}`)},
		{"uptime string", ok(`"transaction":{"staged_at_uptime_s":"5"}`)},
		{"uptime huge", ok(`"transaction":{"staged_at_uptime_s":9999999999999999999}`)},
		{"uptime above the bound", ok(`"cooldown":{"refuse_until_uptime_s":1099511627777}`)},
		{"negative marker", ok(`"cooldown":{"refuse_set_at_uptime_s":-5}`)},
		{"flag as string", ok(`"transaction":{"socket_restart_pending":"true"}`)},
		{"flag null", ok(`"transaction":{"socket_restart_pending":null}`)},
		{"unknown outcome", ok(`"last_result":{"outcome":"ok"}`)},
		{"last_result without outcome", ok(`"last_result":{"to":"1.2.3"}`)},
		{"reason code with upper case", ok(`"last_result":{"outcome":"aborted","reason_code":"Interrupted"}`)},
		{"reason code too long", ok(`"last_result":{"outcome":"aborted","reason_code":"a` + strings.Repeat("b", 32) + `"}`)},
		{"finished_at not a time", ok(`"last_result":{"outcome":"success","finished_at":"yesterday"}`)},
		{"acknowledged number", ok(`"last_result":{"outcome":"success","acknowledged":1}`)},
		{"helper_version with control character", ok(`"helper_version":"a\u0000b"`)},
		{"helper_version too long", ok(`"helper_version":"` + strings.Repeat("a", 65) + `"`)},
		{"larger than 64 KiB", ok(`"future":"` + strings.Repeat("a", maxStateBytes) + `"`)},
	}
	for _, tt := range invalid {
		if _, err := decodeState([]byte(tt.in)); err == nil {
			t.Errorf("%s: accepted", tt.name)
		}
	}
}

// A newer helper's file, with members this helper does not know, is read; the
// members it knows are kept.
func TestDecodeStateReadsANewerFile(t *testing.T) {
	s := fullState()
	enc := string(encodeState(s))
	newer := strings.Replace(enc, `"schema_version": 1,`, `"schema_version": 1, "brand_new": {"x": [1, 2, 3]},`, 1)
	newer = strings.Replace(newer, `"phase": "verifying",`, `"phase": "verifying", "later_field": 7,`, 1)
	got, err := decodeState([]byte(newer))
	if err != nil || !reflect.DeepEqual(got, s) {
		t.Fatalf("newer file: %v\n got %+v\nwant %+v", err, got, s)
	}
}

func TestMarkerActive(t *testing.T) {
	m := cooldownMarker{SetAtUptimeS: 100, UntilUptimeS: 1000}
	tests := []struct {
		now  int64
		want bool
	}{
		{0, false},  // reboot: uptime below set_at
		{99, false}, // reboot
		{100, true}, // the instant it was set
		{101, true},
		{999, true},   // the last second
		{1000, false}, // until is exclusive
		{1001, false},
		{1 << 40, false},
	}
	for _, tt := range tests {
		if got := m.active(tt.now); got != tt.want {
			t.Errorf("active(%d) = %v, want %v", tt.now, got, tt.want)
		}
	}
	if (cooldownMarker{}).active(0) || (cooldownMarker{}).active(5) {
		t.Error("the zero marker is active")
	}
	// A marker that expired at a smaller uptime than it was set is never active.
	if (cooldownMarker{SetAtUptimeS: 500, UntilUptimeS: 400}).active(450) {
		t.Error("a marker that ends before it starts is active")
	}
	if got := fullState().Cool.refuse(); got != (cooldownMarker{SetAtUptimeS: 1100, UntilUptimeS: 2000}) {
		t.Errorf("refuse marker %+v", got)
	}
}

func TestStageExpired(t *testing.T) {
	tests := []struct {
		staged, now int64
		want        bool
	}{
		{1000, 1000, false},
		{1000, 1001, false},
		{1000, 2799, false}, // 1799 s
		{1000, 2800, true},  // exactly 1800 s
		{1000, 2801, true},
		{1000, 999, true}, // uptime below the recorded one: a reboot
		{1000, 0, true},
		{0, 1799, false},
		{0, 1800, true},
	}
	for _, tt := range tests {
		if got := stageExpired(tt.staged, tt.now); got != tt.want {
			t.Errorf("stageExpired(%d, %d) = %v, want %v", tt.staged, tt.now, got, tt.want)
		}
	}
}

func TestLoadStateMissingCorruptAndUnusual(t *testing.T) {
	dir := shortDir(t)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	s, err := loadState(root, "h")
	if err != nil || s.Txn.Phase != phaseIdle || s.Last != nil || s.HelperVersion != "h" {
		t.Fatalf("missing file: %+v %v", s, err)
	}
	path := filepath.Join(dir, stateFile)
	for name, setup := range map[string]func(){
		"garbage":   func() { os.WriteFile(path, []byte("{{{"), 0o600) },
		"empty":     func() { os.WriteFile(path, nil, 0o600) },
		"directory": func() { os.Mkdir(path, 0o700) },
		"symlink": func() {
			os.WriteFile(filepath.Join(dir, "other"), encodeState(freshState("x")), 0o600)
			os.Symlink("other", path)
		},
		"too large": func() { os.WriteFile(path, make([]byte, maxStateBytes+1), 0o600) },
	} {
		os.RemoveAll(path)
		setup()
		if _, err := loadState(root, "h"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A FIFO must not block the read.
	os.RemoveAll(path)
	if err := syscallMkfifo(path); err != nil {
		t.Skip("no FIFO")
	}
	done := make(chan error, 1)
	go func() { _, err := loadState(root, "h"); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a FIFO was accepted")
		}
	case <-after2s():
		t.Fatal("loadState blocked on a FIFO")
	}
}

// A state file that exists and is not valid is reported by status as idle with
// needs_attention, and stage and discard answer io_error; nothing is repaired.
func TestCorruptStateFile(t *testing.T) {
	v := newTenv(t)
	v.stageable("1.1.0")
	path := filepath.Join(v.stateDir, stateFile)
	bad := []byte(`{"schema_version":1,"transaction":{"phase":"working"}}`)
	os.WriteFile(path, bad, 0o600)

	r := v.status()
	if !r.OK || r.Phase != phaseIdle || !r.NeedsAttention || r.LastResult != nil {
		t.Fatalf("status: %+v", r)
	}
	wantErrCode(t, v.stage("1.1.0"), codeIOError)
	wantErrCode(t, v.request(discardLine("1.1.0")), codeIOError)
	if got, _ := os.ReadFile(path); string(got) != string(bad) {
		t.Fatalf("the corrupt file was changed: %s", got)
	}
	v.wantClean()
	if entries, _ := os.ReadDir(v.stateDir); len(entries) != 1 {
		t.Fatalf("state dir holds %d entries", len(entries))
	}
}

// The write recipe: a temporary file (O_EXCL, 0600), write, fsync, rename,
// fsync of the directory. A leftover temporary file is never read.
func TestSaveStateRecipe(t *testing.T) {
	v := newTenv(t)
	e, code := v.h.open(true)
	if code != "" {
		t.Fatal(code)
	}
	defer e.close()
	st := fullState()
	if err := e.saveState(st); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, op := range v.fs.Ops() {
		kinds = append(kinds, op.Kind)
		if op.Tree != treeState {
			t.Errorf("save touched %s", op)
		}
	}
	if got := strings.Join(kinds, " "); got != "create write sync rename syncdir" {
		t.Fatalf("recipe: %s", got)
	}
	ops := v.fs.Ops()
	if !strings.HasPrefix(ops[0].Path, stateTempPrefix) || ops[3].Path != ops[0].Path || ops[3].Path2 != stateFile || ops[4].Path != "." {
		t.Fatalf("ops: %v", ops)
	}
	fi, err := os.Stat(filepath.Join(v.stateDir, stateFile))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("state.json: %v %v", fi, err)
	}
	if entries, _ := os.ReadDir(v.stateDir); len(entries) != 1 {
		t.Fatalf("temporary file left behind: %v", entries)
	}

	// A leftover temporary file is ignored: it is never read as the state.
	os.WriteFile(filepath.Join(v.stateDir, stateTempPrefix+"deadbeefdeadbeef"), []byte(`{"schema_version":1,"transaction":{"phase":"staged","to":"9.9.9"}}`), 0o600)
	got, ok := v.readState()
	if !ok || got.Txn.Phase != "verifying" {
		t.Fatalf("read the leftover: %+v", got)
	}
	if r := v.status(); r.Phase != "verifying" || r.StagedVersion != "" {
		t.Fatalf("status %+v", r)
	}
}

// A failed save removes its temporary file and leaves the old state.
func TestSaveStateFailureLeavesTheOldFile(t *testing.T) {
	for k := 1; k <= 5; k++ {
		v := newTenv(t)
		v.writeState(freshState("old"))
		e, _ := v.h.open(true)
		v.fs.failAt = k
		if err := e.saveState(fullState()); err == nil && k < 5 {
			t.Errorf("k=%d: no error", k)
		}
		e.close()
		got, ok := v.readState()
		if !ok {
			t.Fatalf("k=%d: state file lost", k)
		}
		if got.Txn.Phase != phaseIdle && got.Txn.Phase != "verifying" {
			t.Fatalf("k=%d: state %+v is neither the old nor the new one", k, got)
		}
		if k < 4 && got.HelperVersion != "old" {
			t.Fatalf("k=%d: the old state was replaced before the rename", k)
		}
		for _, ent := range readNames(t, v.stateDir) {
			if strings.HasPrefix(ent, stateTempPrefix) && k < 5 && k != 4 {
				t.Errorf("k=%d: leftover %s", k, ent)
			}
		}
	}
}

func readNames(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

// Seeds run as a normal test; `go test -fuzz FuzzDecodeState` explores.
func FuzzDecodeState(f *testing.F) {
	seeds := [][]byte{
		encodeState(freshState("x")), encodeState(fullState()),
		[]byte(`{"schema_version":1}`), []byte(`{"schema_version":1,"future":{"a":[1]}}`),
		[]byte(`{"schema_version":1,"transaction":{"phase":"staged","to":"1.2.3","staged_at_uptime_s":5}}`),
		[]byte(`{"schema_version":1,"transaction":{"phase":"staging","phase":"idle"}}`),
		[]byte(`{"schema_version":1,"Transaction":{}}`), []byte(`{"schema_version":2}`),
		[]byte(`{"schema_version":1,"last_result":{"outcome":"aborted","reason_code":"interrupted","finished_at":"2023-11-14T22:13:20Z"}}`),
		[]byte(`{"schema_version":1,"cooldown":{"refuse_until_uptime_s":-1}}`),
		[]byte(`{"schema_version":1,"transaction":null}`), []byte(`[]`), []byte(`null`), []byte(``), []byte(`{`),
		[]byte(strings.Repeat(`{"schema_version":1,"a":`, 200)),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := decodeState(data)
		if err != nil {
			if err != errStateInvalid {
				t.Fatalf("error %v", err)
			}
			return
		}
		if !knownPhases[s.Txn.Phase] || s.Txn.StagedAtUptimeS < 0 || s.Cool.RefuseUntilUptimeS < 0 || s.Cool.RefuseSetAtUptimeS < 0 ||
			(s.Last != nil && !knownOutcomes[s.Last.Outcome]) {
			t.Fatalf("accepted an out-of-range state: %+v", s)
		}
		for _, v := range []string{s.Previous, s.Txn.To, s.Txn.From, s.Txn.ComponentsFrom, s.Txn.ComponentsTo, s.Cool.RefuseVersion} {
			if v != "" && !validVersion(v) {
				t.Fatalf("accepted version %q", v)
			}
		}
		// What was accepted is written back in a form that reads the same.
		enc := encodeState(s)
		if len(enc) > maxStateBytes {
			t.Fatalf("encoded size %d", len(enc))
		}
		again, err := decodeState(enc)
		if err != nil || !reflect.DeepEqual(again, s) {
			t.Fatalf("round trip: %v\n %+v\n %+v", err, again, s)
		}
	})
}
