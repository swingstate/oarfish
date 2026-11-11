//go:build !faulthooks

package main

// The release build has no fault hooks (ADR 0010 U13, class T): this file is
// compiled unless the build tag faulthooks is given, and everything in it is a
// no-op the compiler removes. The fault-injection code and the names of its
// environment variables (three since G6.10 step b) exist only in faults_on.go; scripts/check-helper-hooks.sh
// proves that the release binary contains none of them.

// faultsCompiled says whether the hooks are in this binary.
const faultsCompiled = false

// faultPoint is called before every write, rename, fsync and systemctl call.
func faultPoint(string) error { return nil }

// wrapFaults returns the file system unchanged.
func wrapFaults(f fsops) fsops { return f }

// faultProcess tells the hooks which process this is: the subcommand and the
// phase that state.json named when it started.
func faultProcess(string, string) {}
