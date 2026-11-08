//go:build faulthooks

package main

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Run with: go test -tags faulthooks -run TestFaultHooks ./cmd/oarfish-update-helper
func TestFaultHooksFailTheNthCall(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f := wrapFaults(realFS{roots: map[tree]*os.Root{treeOpt: root}})
	for _, at := range []string{"1", "2", "3"} {
		faultCount.Store(0)
		t.Setenv("OARFISH_FAULT_AT", at)
		t.Setenv("OARFISH_FAULT_MODE", "error")
		var failed []string
		names := []string{"a" + at, "b" + at, "c" + at}
		for i, n := range names {
			if err := f.Mkdir(treeOpt, n, 0o700); err != nil {
				failed = append(failed, names[i])
			}
		}
		if len(failed) != 1 || failed[0] != names[int(at[0]-'1')] {
			t.Fatalf("at %s: failed %v", at, failed)
		}
	}
	faultCount.Store(0)
	os.Unsetenv("OARFISH_FAULT_AT")
	if err := f.Mkdir(treeOpt, "d", 0o700); err != nil {
		t.Fatal(err)
	}
	// systemctl is a numbered call as well.
	faultCount.Store(0)
	t.Setenv("OARFISH_FAULT_AT", "1")
	h := &handler{sysrun: func([]string) ([]byte, error) { t.Fatal("ran"); return nil, nil }}
	if _, err := h.systemctl(sysRestart...); err == nil {
		t.Fatal("the hook did not fail systemctl")
	}
}

func TestFaultHooksStopKillsTheProcess(t *testing.T) {
	if os.Getenv("OARFISH_TEST_FAULT_CHILD") == "1" {
		faultPoint("x")
		os.Exit(0) // not reached
	}
	cmd := exec.Command(os.Args[0], "-test.run", "^TestFaultHooksStopKillsTheProcess$")
	cmd.Dir = filepath.Dir(os.Args[0])
	cmd.Env = append(os.Environ(), "OARFISH_TEST_FAULT_CHILD=1", "OARFISH_FAULT_AT=1", "OARFISH_FAULT_MODE=stop")
	err := cmd.Run()
	ee, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("child ended with %v", err)
	}
	if ws := ee.Sys().(syscall.WaitStatus); !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("child status %v", ws)
	}
}

func TestFaultHooksHoldBlocksAtTheChosenCall(t *testing.T) {
	if os.Getenv("OARFISH_TEST_FAULT_CHILD") == "1" {
		if faultPoint("first") != nil {
			os.Exit(3) // the call before the chosen one must pass
		}
		faultPoint("second")
		os.Exit(0) // not reached
	}
	cmd := exec.Command(os.Args[0], "-test.run", "^TestFaultHooksHoldBlocksAtTheChosenCall$")
	cmd.Dir = filepath.Dir(os.Args[0])
	cmd.Env = append(os.Environ(), "OARFISH_TEST_FAULT_CHILD=1", "OARFISH_FAULT_AT=2", "OARFISH_FAULT_MODE=hold")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if strings.Contains(sc.Text(), "fault hook") {
				line <- sc.Text()
				break
			}
		}
		io.Copy(io.Discard, stderr)
	}()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case l := <-line:
		if want := "oarfish-update-helper: fault hook: holding at call 2 (second)"; l != want {
			t.Errorf("hold line %q, want %q", l, want)
		}
	case err := <-done:
		t.Fatalf("child ended before the hold: %v", err)
	case <-time.After(30 * time.Second):
		cmd.Process.Kill()
		t.Fatal("no hold line within 30 s")
	}
	// The call does not return: the child is still there a while later.
	select {
	case err := <-done:
		t.Fatalf("child ended during the hold: %v", err)
	case <-time.After(1500 * time.Millisecond):
	}
	cmd.Process.Kill()
	err = <-done
	ee, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("child ended with %v", err)
	}
	if ws := ee.Sys().(syscall.WaitStatus); !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("child status %v, want killed by the test", ws)
	}
}

func TestFaultHooksUnknownModeIsError(t *testing.T) {
	faultCount.Store(0)
	t.Setenv("OARFISH_FAULT_AT", "1")
	t.Setenv("OARFISH_FAULT_MODE", "holdd")
	if err := faultPoint("x"); err != errFaultInjected {
		t.Fatalf("unknown mode: %v, want the injected error", err)
	}
	faultCount.Store(0)
}
