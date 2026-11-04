package main

import (
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// lockFree reports whether the shared lock can be taken right now, and drops
// it again.
func lockFree(t *testing.T, dir string) bool {
	t.Helper()
	l, err := lockDir(dir)
	if err == errLockBusy {
		return false
	}
	if err != nil {
		t.Fatalf("lockDir: %v", err)
	}
	l.release()
	return true
}

// (a) Another descriptor holds the lock (the installer, or another request):
// stage and discard answer busy and change nothing, status still answers.
func TestLockHeldElsewhere(t *testing.T) {
	v := newTenv(t)
	v.stageable("1.1.0")
	held, err := lockDir(v.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer held.release()

	wantErrCode(t, v.stage("1.1.0"), codeBusy)
	wantErrCode(t, v.request(discardLine("1.1.0")), codeBusy)
	if r := v.status(); !r.OK || r.Phase != phaseIdle {
		t.Fatalf("status while locked: %+v", r)
	}
	v.wantClean()
	if _, ok := v.readState(); ok {
		t.Fatal("a refused request wrote the state file")
	}
	if ops := v.fs.Ops(); len(ops) != 0 {
		t.Fatalf("a refused request mutated: %v", ops)
	}

	// Status with expiry work does not wait and does not fail either.
	held.release()
	r := wantOK(t, v.stage("1.1.0"))
	_ = r
	v.uptime.Store(1000 + stageLifetimeS)
	held, err = lockDir(v.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer held.release()
	start := time.Now()
	s := v.status()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("status waited %v for the lock", elapsed)
	}
	if !s.OK || s.Phase != phaseStaged || s.StagedVersion != "1.1.0" {
		t.Fatalf("status with the lock held: %+v", s)
	}
	if _, err := os.Stat(v.root + "/releases/1.1.0"); err != nil {
		t.Fatal("the staged release was removed while the lock was held")
	}
}

// (b) While stage is paused in the middle of a copy, the lock is held: LOCK_NB
// on another descriptor fails, and so does flock(1) when it exists.
func TestLockHeldWhileStageCopies(t *testing.T) {
	v := newTenv(t)
	v.stageable("1.1.0")
	paused, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	v.h.onChunk = func(file string, copied int64) {
		if file == nameManifest {
			once.Do(func() { close(paused) })
			<-resume
		}
	}
	done := make(chan []byte, 1)
	go func() { done <- v.stage("1.1.0") }()
	<-paused

	if lockFree(t, v.stateDir) {
		t.Fatal("the lock is free in the middle of a copy")
	}
	if path, err := exec.LookPath("flock"); err == nil {
		out, err := exec.Command(path, "-x", "-n", v.stateDir, "true").CombinedOutput()
		if err == nil {
			t.Fatalf("flock -x -n succeeded on the state directory: %s", out)
		}
	} else {
		t.Log("flock(1) not found: skipping the command part")
	}
	// A concurrent request is busy and changes nothing.
	wantErrCode(t, v.request(discardLine("1.1.0")), codeBusy)

	close(resume)
	wantOK(t, <-done)
	if !lockFree(t, v.stateDir) {
		t.Fatal("the lock is still held after stage")
	}
}

// (c) The lock is free again after success, after every error class and after
// the deadline.
func TestLockReleasedOnEveryPath(t *testing.T) {
	type scenario struct {
		name string
		run  func(v *tenv) []byte
		want errorCode // empty: ok
	}
	scenarios := []scenario{
		{"success", func(v *tenv) []byte { v.stageable("1.1.0"); return v.stage("1.1.0") }, ""},
		{"bad request", func(v *tenv) []byte { return v.request([]byte(`{"v":1,"op":"stage"}` + "\n")) }, codeBadRequest},
		{"not found", func(v *tenv) []byte { return v.stage("1.1.0") }, codeNotFound},
		{"current unhealthy", func(v *tenv) []byte { v.stageable("1.1.0"); os.Remove(v.root + "/current"); return v.stage("1.1.0") }, codeCurrentUnhealthy},
		{"signature invalid", func(v *tenv) []byte {
			r := v.release("1.1.0")
			r.key = newTestKey(t, "other")
			v.place("1.1.0", r.build())
			return v.stage("1.1.0")
		}, codeSignatureInvalid},
		{"no space", func(v *tenv) []byte {
			v.stageable("1.1.0")
			v.h.freeBytes = func(*os.File) (uint64, error) { return 1 << 20, nil }
			return v.stage("1.1.0")
		}, codeNoSpace},
		{"installer required", func(v *tenv) []byte {
			r := v.release("1.1.0")
			r.setFile("oarfish-update-helper", elfStub("another helper"))
			v.place("1.1.0", r.build())
			return v.stage("1.1.0")
		}, codeInstallerRequired},
		{"io error from the file system", func(v *tenv) []byte {
			v.stageable("1.1.0")
			v.h.wrapFS = func(inner fsops) fsops { f := newTestFS(inner); f.failAt = 3; return f }
			return v.stage("1.1.0")
		}, codeIOError},
		{"deadline", func(v *tenv) []byte {
			v.stageable("1.1.0")
			v.h.onChunk = func(string, int64) { v.mono.Add(int64(stageDeadline)) }
			return v.stage("1.1.0")
		}, codeIOError},
		{"state file corrupt", func(v *tenv) []byte {
			v.stageable("1.1.0")
			os.WriteFile(v.stateDir+"/"+stateFile, []byte("x"), 0o600)
			return v.stage("1.1.0")
		}, codeIOError},
		{"discard not found", func(v *tenv) []byte { return v.request(discardLine("1.1.0")) }, codeNotFound},
		{"discard success", func(v *tenv) []byte {
			v.stageable("1.1.0")
			v.stage("1.1.0")
			return v.request(discardLine("1.1.0"))
		}, ""},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			v := newTenv(t)
			raw := sc.run(v)
			if sc.want == "" {
				wantOK(t, raw)
			} else {
				wantErrCode(t, raw, sc.want)
			}
			if !lockFree(t, v.stateDir) {
				t.Fatal("the lock is still held")
			}
		})
	}
}

// (d) Two concurrent stage requests: exactly one proceeds, the other answers
// busy.
func TestConcurrentStageOneWins(t *testing.T) {
	v := newTenv(t)
	v.stageable("1.1.0")
	paused, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	v.h.onChunk = func(file string, copied int64) {
		once.Do(func() { close(paused) })
		<-resume
	}
	first := make(chan []byte, 1)
	go func() { first <- v.stage("1.1.0") }()
	<-paused
	second := v.stage("1.1.0")
	wantErrCode(t, second, codeBusy)
	close(resume)
	wantOK(t, <-first)
	if st, _ := v.readState(); st == nil || st.Txn.Phase != phaseStaged {
		t.Fatalf("state %+v", st)
	}
	// A third request now finds a staged release: busy as well.
	wantErrCode(t, v.stage("1.1.0"), codeBusy)
}

func TestLockOnAMissingOrWrongDirectory(t *testing.T) {
	if _, err := lockDir("/does/not/exist"); err == nil || err == errLockBusy {
		t.Fatalf("missing directory: %v", err)
	}
	dir := shortDir(t)
	os.WriteFile(dir+"/f", nil, 0o600)
	if _, err := lockDir(dir + "/f"); err == nil {
		t.Fatal("a regular file was locked")
	}
	os.Symlink(dir, dir+"/l")
	if _, err := lockDir(dir + "/l"); err == nil {
		t.Fatal("a symbolic link was followed")
	}
	if !strings.HasPrefix(dir, "/") {
		t.Fatal("setup")
	}
}
