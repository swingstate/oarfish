package main

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// The only program the broker starts (ADR 0011 B2, A3): /usr/bin/systemctl by
// absolute path, directly and without a shell, with a cleared environment
// that holds only systemctlEnv, no standard input, standard error discarded,
// the working directory /, a deadline, at most maxSystemctlOutput bytes of
// standard output kept, and exactly one of the argument shapes below. The
// unit name always follows `--`. This file is the only one that imports
// os/exec, and runProgram is the only place that starts a process
// (TestOnlySystemctlExecutes).
const systemctlPath = "/usr/bin/systemctl"

// maxSystemctlOutput bounds the standard output read from one call (B2).
const maxSystemctlOutput = 256 << 10

// systemctlEnv is the whole environment of a systemctl call (A4): a fixed
// PATH and the C locale, so that the output is never translated. Nothing of
// the broker's own environment is passed on.
var systemctlEnv = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}

// The verbs of the fixed shapes. No other verb is ever passed.
const (
	verbShow     = "show"
	verbStart    = "start"
	verbStop     = "stop"
	verbRestart  = "restart"
	verbReboot   = "reboot"
	verbPoweroff = "poweroff"
)

// showPropertyArg is the --property= option of the show shape of B2.
var showPropertyArg = "--property=" + strings.Join(showProperties, ",")

// shapeShow is `systemctl show --no-pager --property=… -- <unit>`.
func shapeShow(unit string) []string {
	return []string{verbShow, "--no-pager", showPropertyArg, "--", unit}
}

// shapeUnitJob is `systemctl start|stop|restart --no-ask-password --job-mode=fail -- <unit>`.
func shapeUnitJob(verb, unit string) []string {
	return []string{verb, "--no-ask-password", "--job-mode=fail", "--", unit}
}

// shapePower is `systemctl reboot|poweroff --no-ask-password`.
func shapePower(verb string) []string { return []string{verb, "--no-ask-password"} }

// allowedShape reports whether args is exactly one of the shapes of B2 with a
// unit name that passes rule 1 of B3. The callers build args only with the
// shape functions; this is the last check before a process starts.
func allowedShape(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case verbShow:
		return len(args) == 5 && validUnitName(args[4]) && slices.Equal(args, shapeShow(args[4]))
	case verbStart, verbStop, verbRestart:
		return len(args) == 5 && validUnitName(args[4]) && slices.Equal(args, shapeUnitJob(args[0], args[4]))
	case verbReboot, verbPoweroff:
		return slices.Equal(args, shapePower(args[0]))
	}
	return false
}

// runResult is what one call produced. Exit is the exit status (-1 when the
// process was ended by a signal); TimedOut is set when the deadline ended it;
// Overflow is set when standard output exceeded maxSystemctlOutput (Out then
// holds the first maxSystemctlOutput bytes).
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

// systemctl runs one allowed shape with the given deadline. The error is
// errShape or errNotStart and never carries an operating-system text.
func (b *broker) systemctl(deadline time.Duration, args []string) (runResult, error) {
	if !allowedShape(args) {
		return runResult{}, errShape
	}
	run := b.o.runner
	if run == nil {
		run = runProgram
	}
	r, err := run(b.o.systemctlPath, deadline, slices.Clone(args))
	if err != nil {
		return runResult{}, errNotStart
	}
	return r, nil
}

// runProgram is the production runner. At the deadline the child is killed
// (SIGKILL); WaitDelay bounds the wait for its output afterwards.
func runProgram(path string, deadline time.Duration, args []string) (runResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = slices.Clone(systemctlEnv)
	cmd.Dir = "/"
	out := &limitedBuffer{max: maxSystemctlOutput}
	cmd.Stdout = out
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	r := runResult{Out: out.b, Overflow: out.overflow}
	if ctx.Err() == context.DeadlineExceeded {
		r.TimedOut, r.Exit = true, -1
		return r, nil
	}
	var ee *exec.ExitError
	switch {
	case err == nil:
		r.Exit = 0
	case errors.As(err, &ee):
		r.Exit = ee.ExitCode()
	default:
		return runResult{}, err
	}
	return r, nil
}

// limitedBuffer keeps at most max bytes and records that more arrived.
type limitedBuffer struct {
	b        []byte
	max      int
	overflow bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	room := l.max - len(l.b)
	if len(p) > room {
		l.overflow = true
		p2 := p[:max(room, 0)]
		l.b = append(l.b, p2...)
		return len(p), nil
	}
	l.b = append(l.b, p...)
	return len(p), nil
}
