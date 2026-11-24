package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// The fake root for the helper's lock and state (B9).

// helperState is a state.json in the form the helper writes it (ADR 0010 U8).
func helperState(phase string) string {
	return `{
  "schema_version": 1,
  "helper_version": "protocol-1",
  "previous_version": "",
  "needs_attention": false,
  "components_previous": "",
  "transaction": {
    "id": "",
    "phase": "` + phase + `",
    "to": "",
    "differs": [],
    "socket_restart_pending": false
  },
  "cooldown": {},
  "last_result": null
}
`
}

func makeUpdateDir(t *testing.T, dir, state string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if state != "" {
		writeFile(t, filepath.Join(dir, "state.json"), state, 0o600)
	}
}

// holdExclusive takes the helper's LOCK_EX on dir, as the helper and the
// installer do, until the test ends.
func holdExclusive(t *testing.T, dir string) func() {
	t.Helper()
	fd, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	release := func() { syscall.Close(fd) }
	t.Cleanup(release)
	return release
}

// tryExclusive reports whether LOCK_EX|LOCK_NB on dir succeeds now, as the
// helper's next request would try it.
func tryExclusive(t *testing.T, dir string) bool {
	t.Helper()
	fd, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	return syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) == nil
}

// No helper installed: a missing directory is not an update (B6).
func TestUpdateNoHelper(t *testing.T) {
	tb := newTestBroker(t, &fakeSys{})
	wantCode(t, tb.ask(t, power(opSystemReboot)), codeAccepted, "")
}

// BF24 (class C part): the helper holds its lock: update_running, and
// systemctl does not run. The lock is the helper's; the broker does not wait.
func TestUpdateLockHeld(t *testing.T) {
	for _, op := range []string{opSystemReboot, opSystemPoweroff} {
		tb := newTestBroker(t, &fakeSys{})
		makeUpdateDir(t, tb.dir, helperState("idle"))
		release := holdExclusive(t, tb.dir)
		wantCode(t, tb.ask(t, power(op)), codeUpdateRunning, "")
		wantCalls(t, tb)
		release()
		wantCode(t, tb.ask(t, power(op)), codeAccepted, "")
	}
}

// BF25 and B6: the phases. Only finished phases allow a power operation; the
// component phases and every unknown phase are update_running, also when no
// process holds the lock (the hand-over gap, c_switched).
func TestUpdatePhases(t *testing.T) {
	for phase, ok := range map[string]bool{
		"idle": true, "staged": true, "committed": true, "rolled_back": true, "rollback_failed": true,
		"staging": false, "switching": false, "verifying": false, "rolling_back": false,
		"c_switching": false, "c_switched": false, "c_verified": false, "c_committed": false, "c_rolling_back": false,
		"": false, "IDLE": false, "unknown": false, "idle ": false,
	} {
		tb := newTestBroker(t, &fakeSys{})
		makeUpdateDir(t, tb.dir, helperState(phase))
		want := codeUpdateRunning
		if ok {
			want = codeAccepted
		}
		if a := tb.ask(t, power(opSystemReboot)); a.Code != want {
			t.Errorf("phase %q: %s, want %s", phase, a.Code, want)
		}
	}
}

// Fail closed on every state file that is not one the helper writes.
func TestUpdateStateFailsClosed(t *testing.T) {
	cases := map[string]string{
		"missing file":      "",
		"empty":             " ",
		"not json":          "{",
		"schema 2":          strings.Replace(helperState("idle"), `"schema_version": 1`, `"schema_version": 2`, 1),
		"schema string":     strings.Replace(helperState("idle"), `"schema_version": 1`, `"schema_version": "1"`, 1),
		"schema 1.0":        strings.Replace(helperState("idle"), `"schema_version": 1`, `"schema_version": 1.0`, 1),
		"no schema":         strings.Replace(helperState("idle"), `"schema_version": 1,`, ``, 1),
		"no transaction":    `{"schema_version":1}`,
		"transaction null":  `{"schema_version":1,"transaction":null}`,
		"no phase":          `{"schema_version":1,"transaction":{}}`,
		"phase null":        `{"schema_version":1,"transaction":{"phase":null}}`,
		"phase number":      `{"schema_version":1,"transaction":{"phase":1}}`,
		"two phases":        `{"schema_version":1,"transaction":{"phase":"idle","phase":"switching"}}`,
		"Phase":             `{"schema_version":1,"transaction":{"phase":"idle","Phase":"switching"}}`,
		"two transactions":  `{"schema_version":1,"transaction":{"phase":"switching"},"transaction":{"phase":"idle"}}`,
		"Transaction":       `{"schema_version":1,"Transaction":{"phase":"switching"},"transaction":{"phase":"idle"}}`,
		"data after":        `{"schema_version":1,"transaction":{"phase":"idle"}} {}`,
		"too large":         `{"schema_version":1,"transaction":{"phase":"idle"},"x":"` + strings.Repeat("a", maxHelperState) + `"}`,
		"array":             `[{"schema_version":1,"transaction":{"phase":"idle"}}]`,
		"transaction array": `{"schema_version":1,"transaction":[{"phase":"idle"}]}`,
	}
	for name, content := range cases {
		tb := newTestBroker(t, &fakeSys{})
		makeUpdateDir(t, tb.dir, content)
		if a := tb.ask(t, power(opSystemReboot)); a.Code != codeUpdateRunning {
			t.Errorf("%s: %s", name, a.Code)
		}
		if len(tb.sys.callList()) != 0 {
			t.Errorf("%s: systemctl ran", name)
		}
	}
	// Unknown members are ignored (the helper adds members additively).
	tb := newTestBroker(t, &fakeSys{})
	makeUpdateDir(t, tb.dir, `{"schema_version":1,"new_member":{"x":[1,2]},"transaction":{"phase":"idle","new":true}}`)
	wantCode(t, tb.ask(t, power(opSystemReboot)), codeAccepted, "")
}

// The directory and the file are not followed through links, and a file that
// is not a regular file is refused.
func TestUpdateLinks(t *testing.T) {
	tb := newTestBroker(t, &fakeSys{})
	real := filepath.Join(t.TempDir(), "real")
	makeUpdateDir(t, real, helperState("idle"))
	if err := os.Symlink(real, tb.dir); err != nil {
		t.Fatal(err)
	}
	wantCode(t, tb.ask(t, power(opSystemReboot)), codeUpdateRunning, "")

	tb = newTestBroker(t, &fakeSys{})
	makeUpdateDir(t, tb.dir, "")
	other := filepath.Join(t.TempDir(), "state.json")
	writeFile(t, other, helperState("idle"), 0o600)
	if err := os.Symlink(other, filepath.Join(tb.dir, "state.json")); err != nil {
		t.Fatal(err)
	}
	wantCode(t, tb.ask(t, power(opSystemReboot)), codeUpdateRunning, "")

	tb = newTestBroker(t, &fakeSys{})
	makeUpdateDir(t, tb.dir, "")
	if err := os.Mkdir(filepath.Join(tb.dir, "state.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	wantCode(t, tb.ask(t, power(opSystemReboot)), codeUpdateRunning, "")

	// The path is a file, not a directory.
	tb = newTestBroker(t, &fakeSys{})
	writeFile(t, tb.dir, "x", 0o600)
	wantCode(t, tb.ask(t, power(opSystemReboot)), codeUpdateRunning, "")
}

// B6: the shared lock is held while systemctl runs, so the helper's
// LOCK_EX|LOCK_NB fails then; it is released afterwards. Nothing is written.
func TestUpdateLockHeldDuringCall(t *testing.T) {
	for _, exit := range []int{0, 1} {
		sys := &fakeSys{job: runResult{Exit: exit}}
		tb := newTestBroker(t, sys)
		makeUpdateDir(t, tb.dir, helperState("committed"))
		before := dirListing(t, tb.dir)
		var during bool
		sys.during = func() { during = tryExclusive(t, tb.dir) }
		tb.ask(t, power(opSystemPoweroff))
		if during {
			t.Error("the helper could take its lock while systemctl ran")
		}
		if !tryExclusive(t, tb.dir) {
			t.Error("the shared lock was not released")
		}
		if after := dirListing(t, tb.dir); after != before {
			t.Errorf("the directory changed: %s -> %s", before, after)
		}
	}
	// Another shared holder does not block the broker.
	tb := newTestBroker(t, &fakeSys{})
	makeUpdateDir(t, tb.dir, helperState("idle"))
	fd, _ := syscall.Open(tb.dir, syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
	defer syscall.Close(fd)
	syscall.Flock(fd, syscall.LOCK_SH|syscall.LOCK_NB)
	wantCode(t, tb.ask(t, power(opSystemReboot)), codeAccepted, "")
}

func dirListing(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var s []string
	for _, e := range entries {
		fi, _ := e.Info()
		s = append(s, e.Name()+":"+fi.ModTime().String()+":"+fi.Mode().String())
	}
	return strings.Join(s, ",")
}

// The order of B2 for power: busy before the update check, the update check
// before the once-per-lifetime rule.
func TestPowerOrder(t *testing.T) {
	tb := newTestBroker(t, &fakeSys{})
	makeUpdateDir(t, tb.dir, helperState("switching"))
	tb.power = true
	wantCode(t, tb.ask(t, power(opSystemReboot)), codeUpdateRunning, "")
	tb.running = true
	wantCode(t, tb.ask(t, power(opSystemReboot)), codeBusy, "")
}

// FuzzHelperPhase: the state decoder never panics, and a phase it returns
// came from a document whose schema_version is 1.
func FuzzHelperPhase(f *testing.F) {
	f.Add([]byte(helperState("idle")))
	f.Add([]byte(`{"schema_version":1,"transaction":{"phase":"c_switched"}}`))
	f.Add([]byte(`{"schema_version":1,"transaction":{"phase":"idle","phase":"x"}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		phase, err := decodeHelperPhase(data)
		if err != nil {
			return
		}
		top, err := jsonObject(data, "schema_version", "transaction")
		if err != nil || strings.TrimSpace(string(top["schema_version"])) != "1" {
			t.Fatalf("accepted %q (phase %q)", data, phase)
		}
	})
}
