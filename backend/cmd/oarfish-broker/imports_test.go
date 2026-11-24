package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The broker is a root process: it imports only the standard library (ADR
// 0011 B1). No package of this module (in particular not the service's
// server, auth, updater, relverify or trustkey packages, nor the update
// helper), no third-party module (no golang.org/x package: the copies inside
// the standard library are named vendor/golang.org/x and are part of it),
// and not net/http or plugin.
func TestImports(t *testing.T) {
	const self = "github.com/swingstate/oarfish/backend/cmd/oarfish-broker"
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goTool); err != nil {
		goTool = "go"
	}
	for _, target := range []struct{ goos, goarch, cgo string }{
		{"linux", "arm64", "0"}, {"linux", "amd64", "0"}, {runtime.GOOS, runtime.GOARCH, "0"}, {runtime.GOOS, runtime.GOARCH, "1"},
	} {
		t.Run(target.goos+"/"+target.goarch+"/cgo"+target.cgo, func(t *testing.T) {
			cmd := exec.Command(goTool, "list", "-deps", "-f", "{{.ImportPath}} {{.Standard}}", ".")
			cmd.Env = append(os.Environ(), "GOOS="+target.goos, "GOARCH="+target.goarch, "CGO_ENABLED="+target.cgo)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("go list: %v\n%s", err, out)
			}
			seen := 0
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				path, std, ok := strings.Cut(line, " ")
				if !ok {
					t.Fatalf("unexpected line %q", line)
				}
				seen++
				if path == self {
					continue
				}
				if std != "true" || strings.HasPrefix(path, "golang.org/x") || strings.HasPrefix(path, "github.com/") {
					t.Errorf("the broker depends on %s, which is not the standard library", path)
				}
				for _, f := range []string{"net/http", "plugin"} {
					if path == f || strings.HasPrefix(path, f+"/") {
						t.Errorf("the broker depends on %s", path)
					}
				}
			}
			if seen < 20 {
				t.Fatalf("only %d packages listed: %s", seen, out)
			}
		})
	}
}

// Only systemctl.go imports os/exec, it starts a process in exactly one
// place, and nothing else in the broker starts or replaces a process.
func TestOnlySystemctlExecutes(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
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
		// The broker writes no file (B8): no call that creates, writes,
		// renames or removes anything on the file system.
		for _, bad := range []string{"os.Create", "os.WriteFile", "os.Rename", "os.Remove", "os.Mkdir", "O_WRONLY", "O_RDWR", "O_CREATE", "O_CREAT", "os.Chmod", "os.Symlink", "os.Link("} {
			if strings.Contains(text, bad) {
				t.Errorf("%s uses %s", name, bad)
			}
		}
	}
	src, _ := os.ReadFile("systemctl.go")
	if n := strings.Count(string(src), "exec.CommandContext("); n != 1 {
		t.Errorf("systemctl.go starts a process %d times, want once", n)
	}
	if strings.Contains(string(src), `"/bin/sh"`) || strings.Contains(string(src), `"sh"`) {
		t.Error("systemctl.go names a shell")
	}
}

// No subcommand other than serve and no flag: the production binary reads
// nothing from its command line or environment except LISTEN_PID and
// LISTEN_FDS.
func TestNoEnvironmentSeams(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, _ := os.ReadFile(name)
		text := string(src)
		for _, bad := range []string{`"flag"`, "os.LookupEnv", "os.Environ", "os.Args[2", `getenv("OARFISH`, `Getenv("OARFISH`} {
			if strings.Contains(text, bad) {
				t.Errorf("%s uses %s", name, bad)
			}
		}
		if strings.Contains(text, "getenv(") && name != "activation.go" && name != "main.go" {
			t.Errorf("%s reads the environment", name)
		}
	}
}
