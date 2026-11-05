package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The recover subcommand as a process, started the way systemd starts it: the
// test binary re-executes itself (os.Args[0]) with the real run(), real flock,
// real symlink and rename calls, and only the root and the state directory
// replaced. On Linux this exercises the production file system layer (os.Root,
// fsync, rename over a link) that the in-process tests run through as well, but
// here without any test seam.
func TestLinuxRecoverSubcommandAsAProcess(t *testing.T) {
	v := newActiveEnv(t)
	v.sd.running = "1.1.0"
	v.crashedAt(phaseVerifying)
	v.pointCurrent("1.1.0")
	os.Mkdir(filepath.Join(v.root, "releases", ".trash.00000000000000ee"), 0o755)

	c := &child{dir: shortDir(t)}
	c.cmd = execSelf(c, "recover", v)
	if err := c.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if code := c.wait(t); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, c.stderr.String())
	}
	if got := v.current(); got != "1.0.0" {
		t.Fatalf("current %q", got)
	}
	v.wantIdleWith(outcomeRolledBack, reasonInterrupted)
	v.wantNoLeftovers()
	if s := c.stderr.String(); !strings.Contains(s, "phase=rolled_back to=1.1.0 from=1.0.0") || strings.Contains(s, v.root) || strings.Contains(s, v.stateDir) {
		t.Fatalf("stderr %q", s)
	}
}

func TestLinuxRecoverSubcommandRefusesWhileTheLockIsHeld(t *testing.T) {
	v := newActiveEnv(t)
	v.crashedAt(phaseVerifying)
	v.pointCurrent("1.1.0")
	lk, err := lockDir(v.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lk.release()
	c := &child{dir: shortDir(t)}
	c.cmd = execSelf(c, "recover", v)
	if err := c.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if code := c.wait(t); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if v.current() != "1.1.0" || v.phase() != phaseVerifying {
		t.Fatal("changed something without the lock")
	}
	if !strings.Contains(c.stderr.String(), "recovery did not run: The helper is busy") {
		t.Fatalf("stderr %q", c.stderr.String())
	}
}

// execSelf prepares the re-executed test binary for a subcommand against the
// directories of a tenv. The caller starts it and waits with c.wait.
func execSelf(c *child, mode string, v *tenv) *exec.Cmd {
	cmd := exec.Command(os.Args[0])
	cmd.Dir = c.dir
	cmd.Env = []string{"OARFISH_TEST_CHILD=" + mode, "OARFISH_TEST_ROOT=" + v.root, "OARFISH_TEST_STATE=" + v.stateDir}
	cmd.Stderr, cmd.Stdout = &c.stderr, &c.stdout
	return cmd
}
