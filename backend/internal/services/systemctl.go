package services

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"time"
)

// The only program the service starts (ADR 0011 B2 read path R1, D-C87 item 3,
// the recorded exception to AGENT_RULES 4.7): /usr/bin/systemctl by absolute
// path, directly and without a shell, with exactly one of the two read shapes
// below, the environment systemctlEnv and nothing else of the service's, no
// standard input, standard error discarded, the working directory /, a
// deadline of 5 s (45 s for list-unit-files, ADR 0011 A9) and at most 256 KiB
// of standard output kept. This file is
// the only one of the service that imports os/exec, and runProgram is the only
// place that starts a process (TestOnlySystemctlExecutes and the architecture
// test of internal/platform).
const SystemctlPath = "/usr/bin/systemctl"

// systemdDir exists when systemd is the running init system (the same probe
// as internal/platform/capabilities). Without it the routes answer
// services_unavailable and no process is started.
const systemdDir = "/run/systemd/system"

// The bounds of one call (B2, A3.2). list-unit-files has its own deadline
// (A3.2 P-B2-4 allows a raised bound for it only; A9): PID 1 computes it,
// 17.0 s on the probed Ubuntu image.
const (
	maxOutput         = 256 << 10
	callDeadline      = 5 * time.Second
	filesCallDeadline = 45 * time.Second
)

// systemctlEnv is the whole environment of a call: a fixed PATH and the C
// locale, so that the output is never translated.
var systemctlEnv = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}

// The two read shapes, character for character. There is no third shape and
// no `show` on the read path; --output=json is not used (A3.2).
var (
	shapeListUnits     = []string{"list-units", "--type=service", "--all", "--no-legend", "--no-pager", "--plain"}
	shapeListUnitFiles = []string{"list-unit-files", "--type=service", "--no-legend", "--no-pager"}
)

// allowedShape reports whether args is exactly one of the two shapes. It is
// the last check before a process starts.
func allowedShape(args []string) bool {
	return slices.Equal(args, shapeListUnits) || slices.Equal(args, shapeListUnitFiles)
}

// runResult is what one call produced. Exit is the exit status (-1 when a
// signal ended the process); TimedOut is set when the deadline ended it;
// Overflow is set when more than maxOutput bytes arrived (Out then holds the
// first maxOutput bytes).
type runResult struct {
	Out      []byte
	Overflow bool
	Exit     int
	TimedOut bool
}

var (
	errShape    = errors.New("argument shape not allowed")
	errNotStart = errors.New("systemctl could not be started")
)

// runner starts one process; tests replace the path, never the runner of
// production code.
type runner func(ctx context.Context, path string, deadline time.Duration, args []string) (runResult, error)

// runProgram is the production runner. At the deadline, or when ctx ends, the
// child is killed (SIGKILL); WaitDelay bounds the wait for its output after
// that, so the service never holds a child past the deadline. The error is
// errShape or errNotStart and never carries an operating-system text.
func runProgram(ctx context.Context, path string, deadline time.Duration, args []string) (runResult, error) {
	if !allowedShape(args) {
		return runResult{}, errShape
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, slices.Clone(args)...)
	cmd.Env = slices.Clone(systemctlEnv)
	cmd.Dir = "/"
	out := &limitedBuffer{max: maxOutput}
	cmd.Stdout = out
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	r := runResult{Out: out.b, Overflow: out.overflow}
	if ctx.Err() != nil {
		r.TimedOut, r.Exit = true, -1
		return r, nil
	}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		r.Exit = ee.ExitCode()
	default:
		return runResult{}, errNotStart
	}
	return r, nil
}

// limitedBuffer keeps at most max bytes and records that more arrived. It
// accepts the rest without keeping it, so the child never blocks on a full
// pipe before the deadline.
type limitedBuffer struct {
	b        []byte
	max      int
	overflow bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	room := l.max - len(l.b)
	if len(p) > room {
		l.overflow = true
		l.b = append(l.b, p[:max(room, 0)]...)
		return len(p), nil
	}
	l.b = append(l.b, p...)
	return len(p), nil
}
