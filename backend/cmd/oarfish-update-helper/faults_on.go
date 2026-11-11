//go:build faulthooks

package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"
)

// Fault hooks for the test build of the helper (ADR 0010 U13, class T). The
// tag is off by default and no release build carries it. The hooks count every
// write, sync, rename, link, remove, directory sync and systemctl call; the
// call with the number OARFISH_FAULT_AT does what OARFISH_FAULT_MODE says:
//
//	error  the call fails and does nothing, every other call runs
//	stop   the process kills itself with SIGKILL before the call, which is
//	       what a power loss looks like to the files
//	hold   the call never returns: the goroutine that makes it waits forever,
//	       after one line on standard error that names the call, so that
//	       power can be removed at a known point (U13, F8 and F9); the rest
//	       of the process keeps running
//
// Any other mode is error. Without OARFISH_FAULT_AT nothing happens.
//
// A component transaction runs in two processes of the helper (the one that
// hands over and the one systemd starts next), and each process counts its own
// calls from 1. OARFISH_FAULT_PROC names the process the fault applies to, as
// <subcommand>:<phase>, where the phase is the one state.json named when the
// process started: serve:staged is the helper that receives activate after an
// idle exit, serve:c_switched the new helper at its first start,
// recover:c_switched the recover unit that finds that phase. Other values of
// the phase part: every phase name, "invalid" (state.json unusable), "locked"
// (the lock was held at the start of serve) and "unknown". Without the variable
// the fault applies to every process, as before.

const faultsCompiled = true

var errFaultInjected = errors.New("injected fault")

var faultCount atomic.Int64

// faultProc is what faultProcess recorded: nil until the process has looked at
// its state. Only the first call counts: it names the process.
var faultProc atomic.Pointer[string]

func faultProcess(sub, phase string) {
	name := sub + ":" + phase
	faultProc.CompareAndSwap(nil, &name)
}

// faultSelected: the fault applies to this process.
func faultSelected() bool {
	want := os.Getenv("OARFISH_FAULT_PROC")
	if want == "" {
		return true
	}
	got := faultProc.Load()
	return got != nil && *got == want
}

func faultTarget() (int64, string) {
	at, err := strconv.ParseInt(os.Getenv("OARFISH_FAULT_AT"), 10, 64)
	if err != nil || at <= 0 {
		return 0, ""
	}
	return at, os.Getenv("OARFISH_FAULT_MODE")
}

func faultPoint(kind string) error {
	at, mode := faultTarget()
	if at == 0 || faultCount.Add(1) != at || !faultSelected() {
		return nil
	}
	switch mode {
	case "stop":
		syscall.Kill(os.Getpid(), syscall.SIGKILL)
		select {}
	case "hold":
		fmt.Fprintf(os.Stderr, "oarfish-update-helper: fault hook: holding at call %d (%s)\n", at, kind)
		// A sleeping goroutine, not an empty select: with a pending timer the
		// runtime never reports a deadlock, even if nothing else is running.
		for {
			time.Sleep(time.Hour)
		}
	}
	return errFaultInjected
}

// faultFS puts faultPoint in front of every mutating call.
type faultFS struct{ fsops }

func wrapFaults(f fsops) fsops { return faultFS{f} }

func (f faultFS) Mkdir(t tree, n string, p os.FileMode) error {
	if err := faultPoint("mkdir"); err != nil {
		return err
	}
	return f.fsops.Mkdir(t, n, p)
}

func (f faultFS) Chmod(t tree, n string, p os.FileMode) error {
	if err := faultPoint("chmod"); err != nil {
		return err
	}
	return f.fsops.Chmod(t, n, p)
}

func (f faultFS) Create(t tree, n string, p os.FileMode) (wfile, error) {
	if err := faultPoint("create"); err != nil {
		return nil, err
	}
	w, err := f.fsops.Create(t, n, p)
	if err != nil {
		return nil, err
	}
	return faultFile{w}, nil
}

func (f faultFS) Symlink(t tree, target, n string) error {
	if err := faultPoint("symlink"); err != nil {
		return err
	}
	return f.fsops.Symlink(t, target, n)
}

func (f faultFS) Rename(t tree, a, b string) error {
	if err := faultPoint("rename"); err != nil {
		return err
	}
	return f.fsops.Rename(t, a, b)
}

func (f faultFS) RemoveAll(t tree, n string) error {
	if err := faultPoint("removeall"); err != nil {
		return err
	}
	return f.fsops.RemoveAll(t, n)
}

func (f faultFS) SyncDir(t tree, n string) error {
	if err := faultPoint("syncdir"); err != nil {
		return err
	}
	return f.fsops.SyncDir(t, n)
}

type faultFile struct{ wfile }

func (f faultFile) Write(p []byte) (int, error) {
	if err := faultPoint("write"); err != nil {
		return 0, err
	}
	return f.wfile.Write(p)
}

func (f faultFile) Sync() error {
	if err := faultPoint("sync"); err != nil {
		return err
	}
	return f.wfile.Sync()
}
