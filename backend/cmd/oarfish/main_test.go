package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// When this variable is set the test binary behaves like the oarfish command,
// so the exit status and the message of a refusal can be checked end to end
// without building a second binary.
const helperEnv = "OARFISH_TEST_RUN_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		os.Args = append([]string{"oarfish"}, strings.Fields(os.Getenv("OARFISH_TEST_ARGS"))...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// D-C29: a non-loopback listen address stops the process before it listens,
// with a non-zero status and a message that says what to use instead.
func TestMainRefusesANonLoopbackListenAddress(t *testing.T) {
	for _, addr := range []string{":8484", "0.0.0.0:8484", "[::]:8484", "192.0.2.10:8484", "localhost:8484", "198.51.100.7:1"} {
		t.Run(addr, func(t *testing.T) {
			cmd := exec.Command(os.Args[0])
			cmd.Env = append(os.Environ(), helperEnv+"=1", "OARFISH_TEST_ARGS=-listen "+addr)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				t.Fatalf("want exit status 1, got %v\n%s", err, out)
			}
			if !strings.Contains(string(out), "refuses to start") || !strings.Contains(string(out), "127.0.0.1") {
				t.Errorf("message does not explain the refusal:\n%s", out)
			}
			if strings.Contains(string(out), "oarfish listening") {
				t.Errorf("the process must not start listening:\n%s", out)
			}
		})
	}
}

// There is no way around the guard through the environment.
func TestListenGuardIgnoresTheEnvironment(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), helperEnv+"=1", "OARFISH_TEST_ARGS=-listen 0.0.0.0:8484",
		"OARFISH_LISTEN=0.0.0.0:8484", "OARFISH_ALLOW_PUBLIC=1", "OARFISH_ALLOW_NON_LOOPBACK=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the process started although the address is public:\n%s", out)
	}
}
