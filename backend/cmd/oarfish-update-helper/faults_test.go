package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// The release helper has no fault hooks (ADR 0010 U13: "a release build must
// not contain the hooks (CI checks the binary)"). The hooks live in
// faults_on.go behind the build tag faulthooks; this builds the helper without
// the tag and with it, and looks for the name of the hooks' environment
// variable in both binaries. The second half proves that the check can fail.
func TestReleaseBuildHasNoFaultHooks(t *testing.T) {
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goTool); err != nil {
		goTool = "go"
	}
	build := func(tags string) []byte {
		out := filepath.Join(t.TempDir(), "helper")
		args := []string{"build", "-trimpath", "-o", out}
		if tags != "" {
			args = append(args, "-tags", tags)
		}
		cmd := exec.Command(goTool, append(args, ".")...)
		cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go build: %v\n%s", err, b)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	markers := [][]byte{[]byte("OARFISH_FAULT_AT"), []byte("OARFISH_FAULT_MODE"), []byte("injected fault")}
	release := build("")
	for _, m := range markers {
		if bytes.Contains(release, m) {
			t.Errorf("the release build contains %q", m)
		}
	}
	hooked := build("faulthooks")
	for _, m := range markers {
		if !bytes.Contains(hooked, m) {
			t.Errorf("the check can not fail: the faulthooks build lacks %q", m)
		}
	}
}

// Without the tag, faultPoint does nothing and wrapFaults returns its argument.
func TestFaultStubsDoNothing(t *testing.T) {
	if faultsCompiled {
		t.Skip("built with the faulthooks tag")
	}
	t.Setenv("OARFISH_FAULT_AT", "1")
	if faultPoint("x") != nil {
		t.Fatal("faultPoint fired")
	}
	var f fsops = realFS{}
	if _, ok := wrapFaults(f).(realFS); !ok {
		t.Fatal("wrapFaults wrapped")
	}
}
