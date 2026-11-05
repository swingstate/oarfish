package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const modulePath = "github.com/swingstate/oarfish/backend/"

// The helper is a root process: nothing that is not needed may be compiled
// into it (ADR 0010 U1). Besides the standard library it may use this
// repository's version, trustkey and relverify packages and nothing else; in
// particular no net/http, no os/exec, no third-party module (no golang.org/x
// package: the copies vendored inside the standard library are named
// vendor/golang.org/x and are part of it) and nothing of the service's web,
// auth, audit, clock, securefile or server packages. os/exec is allowed for the
// one function that runs systemctl (G6.6); TestOnlySystemctlExecutes pins it to
// systemctl.go.
func TestImports(t *testing.T) {
	allowed := map[string]bool{
		modulePath + "cmd/oarfish-update-helper": true,
		modulePath + "internal/relverify":        true,
		modulePath + "internal/trustkey":         true,
		modulePath + "internal/version":          true,
	}
	forbidden := []string{"net/http", "plugin"}
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
				if strings.HasPrefix(path, "golang.org/x") {
					t.Errorf("the helper depends on %s", path)
				}
				for _, f := range forbidden {
					if path == f || strings.HasPrefix(path, f+"/") {
						t.Errorf("the helper depends on %s", path)
					}
				}
				if std != "true" && !allowed[path] {
					t.Errorf("the helper depends on %s, which is neither the standard library nor an allowed package of this repository", path)
				}
			}
			if seen < 20 {
				t.Fatalf("only %d packages listed: %s", seen, out)
			}
		})
	}
}

// Only systemctl.go imports os/exec, and nothing in the helper starts a process
// anywhere else (ADR 0010 U2: one program, one function).
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
			for _, bad := range []string{`"os/exec"`, "syscall.ForkExec", "syscall.Exec(", "os.StartProcess"} {
				if strings.Contains(text, bad) {
					t.Errorf("%s uses %s", name, bad)
				}
			}
		}
	}
	src, _ := os.ReadFile("systemctl.go")
	if n := strings.Count(string(src), "exec.CommandContext("); n != 1 {
		t.Errorf("systemctl.go starts a process %d times, want once", n)
	}
}
