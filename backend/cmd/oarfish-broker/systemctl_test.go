package main

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeSystemctl is the process-level fake (ADR 0011 B9): a small test program
// that records its arguments and environment and prints fixture output. Call
// n (from 1) prints dir/out.<n>, exits with dir/exit.<n> (default 0), or, when
// dir/sleep.<n> exists, replaces itself with sleep for that many seconds.
type fakeSystemctl struct {
	t    *testing.T
	dir  string
	path string
}

func newFakeSystemctl(t *testing.T) *fakeSystemctl {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
d='` + dir + `'
n=$(cat "$d/count" 2>/dev/null || echo 0)
n=$((n + 1))
echo "$n" > "$d/count"
for a in "$@"; do printf '%s\n' "$a"; done > "$d/args.$n"
env > "$d/env.$n"
if [ -f "$d/sleep.$n" ]; then exec sleep "$(cat "$d/sleep.$n")"; fi
if [ -f "$d/out.$n" ]; then cat "$d/out.$n"; fi
exit "$(cat "$d/exit.$n" 2>/dev/null || echo 0)"
`
	path := filepath.Join(dir, "systemctl")
	writeFile(t, path, script, 0o755)
	return &fakeSystemctl{t: t, dir: dir, path: path}
}

func (f *fakeSystemctl) set(n int, kind, value string) {
	writeFile(f.t, filepath.Join(f.dir, kind+"."+strconv.Itoa(n)), value, 0o644)
}

func (f *fakeSystemctl) count() int {
	b, err := os.ReadFile(filepath.Join(f.dir, "count"))
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

func (f *fakeSystemctl) args(n int) []string {
	b, err := os.ReadFile(filepath.Join(f.dir, "args."+strconv.Itoa(n)))
	if err != nil {
		f.t.Fatalf("call %d: %v", n, err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

func (f *fakeSystemctl) env(n int) string {
	b, _ := os.ReadFile(filepath.Join(f.dir, "env."+strconv.Itoa(n)))
	return string(b)
}

// The fixed shapes of B2, character for character.
func TestShapes(t *testing.T) {
	want := map[string][]string{
		"show":     {"show", "--no-pager", "--property=Id,Names,LoadState,ActiveState,SubState,Result,Type,Transient,RefuseManualStart,RefuseManualStop,UnitFileState,ExecMainStartTimestampMonotonic,MainPID", "--", "u.service"},
		"start":    {"start", "--no-ask-password", "--job-mode=fail", "--", "u.service"},
		"stop":     {"stop", "--no-ask-password", "--job-mode=fail", "--", "u.service"},
		"restart":  {"restart", "--no-ask-password", "--job-mode=fail", "--", "u.service"},
		"reboot":   {"reboot", "--no-ask-password"},
		"poweroff": {"poweroff", "--no-ask-password"},
	}
	got := map[string][]string{
		"show": shapeShow("u.service"), "start": shapeUnitJob(verbStart, "u.service"), "stop": shapeUnitJob(verbStop, "u.service"),
		"restart": shapeUnitJob(verbRestart, "u.service"), "reboot": shapePower(verbReboot), "poweroff": shapePower(verbPoweroff),
	}
	for k, w := range want {
		if !slices.Equal(got[k], w) {
			t.Errorf("%s: %q, want %q", k, got[k], w)
		}
		if !allowedShape(w) {
			t.Errorf("%s not allowed", k)
		}
	}
	if systemctlPath != "/usr/bin/systemctl" {
		t.Error(systemctlPath)
	}
}

// Nothing outside the shapes passes the last check before a process starts.
func TestAllowedShapeRefuses(t *testing.T) {
	for _, args := range [][]string{
		nil, {}, {"show"}, {"daemon-reload"}, {"enable", "--no-ask-password", "--job-mode=fail", "--", "a.service"},
		{"start", "--no-ask-password", "--job-mode=fail", "a.service"},
		{"start", "--no-ask-password", "--job-mode=fail", "--", "-a.service"},
		{"start", "--no-ask-password", "--job-mode=fail", "--", "a.socket"},
		{"start", "--no-ask-password", "--job-mode=replace", "--", "a.service"},
		{"start", "--no-ask-password", "--job-mode=fail", "--", "a.service", "b.service"},
		{"start", "--job-mode=fail", "--no-ask-password", "--", "a.service"},
		{"show", "--no-pager", "--property=ExecStart", "--", "a.service"},
		{"show", "--no-pager", showPropertyArg, "--", "../a.service"},
		{"reboot"}, {"reboot", "--no-ask-password", "--force"}, {"halt", "--no-ask-password"},
		{"kexec", "--no-ask-password"}, {"soft-reboot", "--no-ask-password"},
	} {
		if allowedShape(args) {
			t.Errorf("allowed %q", args)
		}
		tb := newTestBroker(t, &fakeSys{})
		if _, err := tb.systemctl(time.Second, args); err != errShape {
			t.Errorf("%q: %v", args, err)
		}
		if len(tb.sys.callList()) != 0 {
			t.Errorf("%q ran", args)
		}
	}
}

// The production runner, with the process-level fake: arguments as given,
// no shell interpretation, the cleared environment, the exit status.
func TestRunProgram(t *testing.T) {
	t.Setenv("OARFISH_BROKER_TEST_LEAK", "must-not-reach-the-child")
	f := newFakeSystemctl(t)
	f.set(1, "out", "Id=x\n")
	f.set(1, "exit", "3")
	args := []string{"show", "--no-pager", showPropertyArg, "--", `a@$(id)\x2d;x.service`}
	r, err := runProgram(f.path, 10*time.Second, args)
	if err != nil {
		t.Fatal(err)
	}
	if r.Exit != 3 || string(r.Out) != "Id=x\n" || r.TimedOut || r.Overflow {
		t.Fatalf("%+v", r)
	}
	if got := f.args(1); !slices.Equal(got, args) {
		t.Fatalf("args %q, want %q", got, args)
	}
	env := f.env(1)
	if strings.Contains(env, "OARFISH_BROKER_TEST_LEAK") || !strings.Contains(env, "PATH=/usr/sbin:/usr/bin:/sbin:/bin\n") || !strings.Contains(env, "LC_ALL=C\n") {
		t.Fatalf("environment of the child:\n%s", env)
	}
	for _, line := range strings.Split(strings.TrimSpace(env), "\n") {
		k, _, _ := strings.Cut(line, "=")
		switch k {
		case "PATH", "LC_ALL", "PWD", "SHLVL", "_", "OLDPWD":
			// PWD, SHLVL, _ and OLDPWD are set by the fake's own shell.
		default:
			t.Errorf("unexpected variable %s in the child", k)
		}
	}
	// Exit 0 with no output.
	r, err = runProgram(f.path, 10*time.Second, shapePower(verbReboot))
	if err != nil || r.Exit != 0 || len(r.Out) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	// A program that cannot be started is an error, never an exit status.
	if _, err := runProgram(filepath.Join(f.dir, "missing"), time.Second, nil); err == nil {
		t.Fatal("missing program accepted")
	}
}

// BF14 (runner part): at the deadline the child is ended.
func TestRunProgramDeadline(t *testing.T) {
	f := newFakeSystemctl(t)
	f.set(1, "sleep", "30")
	start := time.Now()
	r, err := runProgram(f.path, 300*time.Millisecond, shapeUnitJob(verbStart, "a.service"))
	if err != nil || !r.TimedOut {
		t.Fatalf("%+v %v", r, err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("the deadline took %v", d)
	}
}

// BF21 (runner part): 1 MiB of output is cut at 256 KiB and flagged.
func TestRunProgramBound(t *testing.T) {
	f := newFakeSystemctl(t)
	f.set(1, "out", strings.Repeat("x", 1<<20))
	r, err := runProgram(f.path, 10*time.Second, shapeShow("a.service"))
	if err != nil || !r.Overflow || len(r.Out) != maxSystemctlOutput {
		t.Fatalf("overflow %v, %d bytes, %v", r.Overflow, len(r.Out), err)
	}
	f.set(2, "out", strings.Repeat("x", maxSystemctlOutput))
	r, _ = runProgram(f.path, 10*time.Second, shapeShow("a.service"))
	if r.Overflow || len(r.Out) != maxSystemctlOutput {
		t.Fatalf("exactly the bound: overflow %v, %d", r.Overflow, len(r.Out))
	}
}

// A whole operation through the process-level fake: the calls and their order.
func TestServiceThroughProcessFake(t *testing.T) {
	f := newFakeSystemctl(t)
	f.set(1, "out", showOut(demo))
	f.set(3, "out", showOut(demo, "ExecMainStartTimestampMonotonic", "5000000"))
	tb := newTestBroker(t, &fakeSys{})
	tb.o.runner = nil
	tb.o.systemctlPath = f.path
	wantCode(t, tb.ask(t, svc(opServiceRestart, demo)), codeSuccess, "active")
	if f.count() != 3 {
		t.Fatalf("%d calls", f.count())
	}
	for i, want := range [][]string{shapeShow(demo), shapeUnitJob(verbRestart, demo), shapeShow(demo)} {
		if got := f.args(i + 1); !slices.Equal(got, want) {
			t.Fatalf("call %d: %q", i+1, got)
		}
	}
	// A hanging job ends at the deadline with the state read afterwards.
	f2 := newFakeSystemctl(t)
	f2.set(1, "out", showOut(demo, "ActiveState", "inactive"))
	f2.set(2, "sleep", "30")
	f2.set(3, "out", showOut(demo, "ActiveState", "activating"))
	tb = newTestBroker(t, &fakeSys{})
	tb.o.runner = nil
	tb.o.systemctlPath = f2.path
	tb.o.opDeadline = 300 * time.Millisecond
	wantCode(t, tb.ask(t, svc(opServiceStart, demo)), codeTimeout, "activating")
}

func TestLimitedBuffer(t *testing.T) {
	l := &limitedBuffer{max: 4}
	l.Write([]byte("ab"))
	l.Write([]byte("cdef"))
	l.Write([]byte("g"))
	if string(l.b) != "abcd" || !l.overflow {
		t.Fatalf("%q %v", l.b, l.overflow)
	}
}
