package services

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// The two read shapes, character for character (D-C87 item 3, A3.2).
func TestShapes(t *testing.T) {
	if !slices.Equal(shapeListUnits, []string{"list-units", "--type=service", "--all", "--no-legend", "--no-pager", "--plain"}) {
		t.Error(shapeListUnits)
	}
	if !slices.Equal(shapeListUnitFiles, []string{"list-unit-files", "--type=service", "--no-legend", "--no-pager"}) {
		t.Error(shapeListUnitFiles)
	}
	if SystemctlPath != "/usr/bin/systemctl" || systemdDir != "/run/systemd/system" {
		t.Error(SystemctlPath, systemdDir)
	}
	if !slices.Equal(systemctlEnv, []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}) {
		t.Error(systemctlEnv)
	}
	if maxOutput != 256<<10 || callDeadline != 5*time.Second || maxUnits != 1000 || cacheFor != 5*time.Second || maxDescription != 200 {
		t.Error("bounds")
	}
	for _, args := range [][]string{
		nil, {}, {"show", "--no-pager", "--", "a.service"}, {"list-units"}, {"list-units", "--type=service", "--all", "--no-legend", "--no-pager", "--plain", "--output=json"},
		{"list-unit-files", "--type=service", "--no-legend", "--no-pager", "--", "a.service"}, {"start", "a.service"}, {"daemon-reload"},
	} {
		if allowedShape(args) {
			t.Errorf("allowed %q", args)
		}
		f := newFakeSystemctl(t)
		if _, err := runProgram(context.Background(), f.path, time.Second, args); err != errShape {
			t.Errorf("%q: %v", args, err)
		}
		if f.count() != 0 {
			t.Errorf("%q ran", args)
		}
	}
}

// The production runner with the process fake: the arguments as given (no
// shell interpretation), exactly the two environment variables, the working
// directory /, standard input empty, the exit status kept.
func TestRunProgram(t *testing.T) {
	f := newFakeSystemctl(t)
	f.set(1, "out", "line one\n")
	r, err := runProgram(context.Background(), f.path, 5*time.Second, shapeListUnits)
	if err != nil || r.Exit != 0 || r.TimedOut || r.Overflow || string(r.Out) != "line one\n" {
		t.Fatalf("%+v %v", r, err)
	}
	if !slices.Equal(f.args(1), shapeListUnits) {
		t.Errorf("args %q", f.args(1))
	}
	env := strings.Split(strings.TrimSpace(f.file("env", 1)), "\n")
	var kept []string
	for _, kv := range env {
		// The shell itself adds a few of its own (PWD, SHLVL, _); nothing else may come from the service.
		if k, _, _ := strings.Cut(kv, "="); k != "PWD" && k != "SHLVL" && k != "_" && k != "OLDPWD" {
			kept = append(kept, kv)
		}
	}
	slices.Sort(kept)
	if !slices.Equal(kept, []string{"LC_ALL=C", "PATH=/usr/sbin:/usr/bin:/sbin:/bin"}) {
		t.Errorf("environment %q", kept)
	}
	if got := strings.TrimSpace(f.file("pwd", 1)); got != "/" {
		t.Errorf("working directory %q", got)
	}
	f.set(2, "exit", "3")
	if r, err := runProgram(context.Background(), f.path, 5*time.Second, shapeListUnitFiles); err != nil || r.Exit != 3 {
		t.Errorf("%+v %v", r, err)
	}
	if _, err := runProgram(context.Background(), filepath.Join(t.TempDir(), "missing"), time.Second, shapeListUnits); err != errNotStart {
		t.Errorf("missing program: %v", err)
	}
}

// The deadline ends the child; the runner returns soon after it.
func TestRunProgramDeadline(t *testing.T) {
	f := newFakeSystemctl(t)
	f.set(1, "sleep", "5")
	start := time.Now()
	r, err := runProgram(context.Background(), f.path, 200*time.Millisecond, shapeListUnits)
	if err != nil || !r.TimedOut {
		t.Fatalf("%+v %v", r, err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("returned after %v", d)
	}
}

// Output over 256 KiB is cut and marked; memory stays bounded at the limit.
func TestRunProgramBound(t *testing.T) {
	f := newFakeSystemctl(t)
	f.set(1, "out", strings.Repeat("x", 1<<20))
	r, err := runProgram(context.Background(), f.path, 5*time.Second, shapeListUnits)
	if err != nil || !r.Overflow || len(r.Out) != maxOutput {
		t.Fatalf("overflow %v, %d bytes, %v", r.Overflow, len(r.Out), err)
	}
}

// Only systemctl.go of this package imports os/exec, it starts a process in
// exactly one place, names no shell, and nothing else starts or replaces a
// process. The architecture test of internal/platform pins the same for the
// whole service.
func TestOnlySystemctlExecutes(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := string(src)
		if name != "systemctl.go" {
			for _, bad := range []string{`"os/exec"`, "syscall.ForkExec", "syscall.Exec(", "os.StartProcess", "syscall.StartProcess"} {
				if strings.Contains(text, bad) {
					t.Errorf("%s uses %s", name, bad)
				}
			}
		}
		// The read path writes nothing.
		for _, bad := range []string{"os.Create", "os.WriteFile", "os.Rename", "os.Remove", "os.Mkdir", "O_WRONLY", "O_RDWR", "O_CREATE"} {
			if strings.Contains(text, bad) {
				t.Errorf("%s uses %s", name, bad)
			}
		}
	}
	if checked < 5 {
		t.Fatalf("only %d files", checked)
	}
	src, _ := os.ReadFile("systemctl.go")
	if n := strings.Count(string(src), "exec.CommandContext("); n != 1 {
		t.Errorf("systemctl.go starts a process %d times, want once", n)
	}
	if strings.Contains(string(src), `"/bin/sh"`) || strings.Contains(string(src), `"sh"`) || strings.Contains(string(src), "bash") {
		t.Error("systemctl.go names a shell")
	}
	// The production runner is the only one: no field or variable replaces it
	// outside the tests.
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, _ := os.ReadFile(name)
		if n := strings.Count(string(src), "run:"); name == "reader.go" && n != 1 {
			t.Errorf("reader.go sets the runner %d times", n)
		}
	}
}

// Standard library only, besides the module's own audit, clock, broker client
// and rule-1 packages (no new dependency, AGENT_RULES 8.1).
func TestImports(t *testing.T) {
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goTool); err != nil {
		goTool = "go"
	}
	for _, pkg := range []string{".", "../brokerclient", "../unitname"} {
		out, err := exec.Command(goTool, "list", "-deps", "-f", "{{.ImportPath}} {{.Standard}}", pkg).CombinedOutput()
		if err != nil {
			t.Fatalf("go list %s: %v\n%s", pkg, err, out)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			path, std, _ := strings.Cut(line, " ")
			if std == "true" {
				continue
			}
			const mod = "github.com/swingstate/oarfish/backend/internal/"
			switch strings.TrimPrefix(path, mod) {
			case "services", "brokerclient", "unitname", "audit", "clock", "securefile":
				if strings.HasPrefix(path, mod) {
					continue
				}
			}
			t.Errorf("%s depends on %s", pkg, path)
		}
	}
}
