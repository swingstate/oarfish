//go:build faulthooks

package main

import (
	"errors"
	"os"
	"strconv"
	"sync/atomic"
	"syscall"
)

// Fault hooks for the test build of the helper (ADR 0010 U13, class T). The
// tag is off by default and no release build carries it. The hooks count every
// write, sync, rename, link, remove, directory sync and systemctl call; the
// call with the number OARFISH_FAULT_AT does what OARFISH_FAULT_MODE says:
//
//	error  the call fails and does nothing, every other call runs
//	stop   the process kills itself with SIGKILL before the call, which is
//	       what a power loss looks like to the files
//
// Without OARFISH_FAULT_AT nothing happens.

const faultsCompiled = true

var errFaultInjected = errors.New("injected fault")

var faultCount atomic.Int64

func faultTarget() (int64, string) {
	at, err := strconv.ParseInt(os.Getenv("OARFISH_FAULT_AT"), 10, 64)
	if err != nil || at <= 0 {
		return 0, ""
	}
	return at, os.Getenv("OARFISH_FAULT_MODE")
}

func faultPoint(string) error {
	at, mode := faultTarget()
	if at == 0 || faultCount.Add(1) != at {
		return nil
	}
	if mode == "stop" {
		syscall.Kill(os.Getpid(), syscall.SIGKILL)
		select {}
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
