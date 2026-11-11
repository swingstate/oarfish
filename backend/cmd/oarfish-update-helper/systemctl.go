package main

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"slices"
	"time"
)

// The only program the helper executes (ADR 0010 U2, the closed list of
// programs): /usr/bin/systemctl by absolute path, with a cleared environment,
// no shell, no standard input, and exactly one of the argument shapes below.
// h.systemctl is the only function that starts a process; this file is the
// only one that may import os/exec (TestOnlySystemctlExecutes checks it).
const systemctlPath = "/usr/bin/systemctl"

// The five argument shapes of U2 (A2 removed the sixth). The release
// transaction calls the first three; the component transaction of G6.10 calls
// daemon-reload, and its recovery reset-failed of the helper service.
// TestSystemctlShapesMatchTheADR compares this list with the table in the ADR.
var (
	sysRestart           = []string{"restart", "oarfish.service"}
	sysResetFailed       = []string{"reset-failed", "oarfish.service"}
	sysShow              = []string{"show", "oarfish.service", "--property=ActiveState,SubState,MainPID,NRestarts,ExecStart", "--no-pager"}
	sysDaemonReload      = []string{"daemon-reload"}
	sysResetFailedHelper = []string{"reset-failed", "oarfish-update-helper.service"}

	systemctlShapes = [][]string{sysRestart, sysResetFailed, sysShow, sysDaemonReload, sysResetFailedHelper}
)

const (
	// systemctlTimeout bounds one call. A restart waits for the unit to start.
	systemctlTimeout = 60 * time.Second
	// maxSystemctlOutput bounds what is read from a call.
	maxSystemctlOutput = 16 << 10
)

// errSystemctl is the one failure of a call. The cause (the exit status, the
// text of the program) is not kept: no operating-system string reaches an
// answer or the journal.
var errSystemctl = errors.New("systemctl failed")

// systemctl runs one of the closed argument shapes and returns its standard
// output. Any other argument list is refused without running anything.
func (h *handler) systemctl(args ...string) ([]byte, error) {
	allowed := false
	for _, shape := range systemctlShapes {
		if slices.Equal(shape, args) {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, errSystemctl
	}
	if err := faultPoint("systemctl"); err != nil {
		return nil, errSystemctl
	}
	run := h.sysrun
	if run == nil {
		run = runSystemctl
	}
	out, err := run(slices.Clone(args))
	if err != nil {
		return nil, errSystemctl
	}
	return out, nil
}

// runSystemctl is the production runner.
func runSystemctl(args []string) ([]byte, error) { return runProgram(systemctlPath, args) }

// runProgram starts path with args, no shell, a cleared environment, no
// standard input, a timeout and at most maxSystemctlOutput bytes of output.
// Only h.systemctl reaches it, with the absolute path of systemctl; the tests
// call it with other programs to check the process setup.
func runProgram(path string, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), systemctlTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = []string{} // cleared, not inherited
	cmd.Dir = "/"
	var out limitedBuffer
	cmd.Stdout = &out
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.b.Bytes(), nil
}

// limitedBuffer keeps at most maxSystemctlOutput bytes and drops the rest.
type limitedBuffer struct{ b bytes.Buffer }

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := maxSystemctlOutput - l.b.Len(); room > 0 {
		l.b.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}
