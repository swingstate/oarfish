package main

import (
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
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

func TestResolveStateDir(t *testing.T) {
	cases := []struct{ flag, env, want string }{
		{"", "", ""},
		{"", "/var/lib/oarfish", "/var/lib/oarfish"},
		{"", "/var/lib/oarfish:/var/lib/other", "/var/lib/oarfish"},
		{"/tmp/dev", "/var/lib/oarfish", "/tmp/dev"},
	}
	for _, c := range cases {
		if got := resolveStateDir(c.flag, c.env); got != c.want {
			t.Errorf("resolveStateDir(%q, %q) = %q, want %q", c.flag, c.env, got, c.want)
		}
	}
}

// freeAddr returns a loopback address with a port that was free a moment ago.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func helperCmd(addr string, env ...string) *exec.Cmd {
	cmd := exec.Command(os.Args[0])
	base := []string{}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "STATE_DIRECTORY=") {
			base = append(base, e)
		}
	}
	cmd.Env = append(append(base, helperEnv+"=1", "OARFISH_TEST_ARGS=-listen "+addr), env...)
	return cmd
}

// ORB-SEC-005: without a usable state directory the process stops before it
// listens, with a message that says what is missing.
func TestMainRefusesToStartWithoutAStateDirectory(t *testing.T) {
	for name, env := range map[string][]string{
		"unset":   nil,
		"missing": {"STATE_DIRECTORY=" + filepath.Join(t.TempDir(), "absent")},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := helperCmd(freeAddr(t), env...).CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				t.Fatalf("want exit status 1, got %v\n%s", err, out)
			}
			if !strings.Contains(string(out), "state directory") && !strings.Contains(string(out), "STATE_DIRECTORY") {
				t.Errorf("message does not name the state directory:\n%s", out)
			}
			if strings.Contains(string(out), "oarfish listening") {
				t.Errorf("the process listened:\n%s", out)
			}
		})
	}
}

// E4: a corrupt account file stops the start; there is no silent setup mode.
func TestMainRefusesACorruptAccountFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "auth"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth", "account.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := helperCmd(freeAddr(t), "STATE_DIRECTORY="+dir).CombinedOutput()
	if err == nil {
		t.Fatalf("started with a corrupt account file:\n%s", out)
	}
	if !strings.Contains(string(out), "account.json") || !strings.Contains(string(out), "remove") {
		t.Errorf("message does not name the file and the recovery:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "setup-token")); err == nil {
		t.Error("a setup token was written")
	}
}

// ORB-ONB-002, AT-023, E7: the real process starts in setup mode, writes the
// token file but never logs the token, accepts the setup, and on SIGTERM
// writes the audit buffer before it exits.
func TestMainSetupThenSIGTERMFlushesTheAudit(t *testing.T) {
	dir := t.TempDir()
	addr := freeAddr(t)
	var log strings.Builder
	cmd := helperCmd(addr, "STATE_DIRECTORY="+dir)
	cmd.Stdout, cmd.Stderr = &log, &log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	base := "http://" + addr
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(base + "/healthz")
		if err == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never became healthy:\n%s", log.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "setup-token"))
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(raw))
	const password = "Sentinel-Pw-3e9f1c7a"
	body := `{"setup_token":"` + token + `","username":"admin-test","password":"` + password + `"}`
	req, _ := http.NewRequest(http.MethodPost, base+"/api/v1/setup", strings.NewReader(body))
	req.Header.Set("Origin", base)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup = %d", resp.StatusCode)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("exit after SIGTERM: %v\n%s", err, log.String())
	}
	audit, err := os.ReadFile(filepath.Join(dir, "audit", "audit.jsonl"))
	if err != nil || !strings.Contains(string(audit), `"action":"setup_completed"`) {
		t.Fatalf("the audit was not written on SIGTERM: %v %q", err, audit)
	}
	out := log.String()
	if !strings.Contains(out, "setup-token") {
		t.Errorf("the log does not say where the token is:\n%s", out)
	}
	for name, secret := range map[string]string{"setup token": token, "password": password} {
		if strings.Contains(out, secret) || strings.Contains(string(audit), secret) {
			t.Errorf("the %s is in the log or the audit file", name)
		}
	}
}
