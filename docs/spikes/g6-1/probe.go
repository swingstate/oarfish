//go:build linux

// probe.go is the throwaway probe of the G6.1 device spike (ADR 0010, assumptions
// V1 to V13). It is not product code, it is not part of any Go module, it is
// built by build-probe.sh and never committed as a binary.
//
// Standard library only. Every subcommand has fixed behaviour and fixed
// arguments; no subcommand runs a command that the caller names. The only
// programs it starts are /usr/bin/systemctl with fixed argument lists (v1,
// recover, v13-serve) and /usr/bin/timedatectl with one fixed argument list
// (v8-boot). It never prints an IP address, a host name or a user name: network
// results are reduced to counts, error classes and durations.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	optDir    = "/opt/oarfish-spike"
	stateDir  = "/var/lib/oarfish-spike-update"
	h11Dir    = optDir + "/h11"
	systemctl = "/usr/bin/systemctl"

	clockMonotonic = 1
	clockBoottime  = 7
)

// ---- Linux-only calls, kept apart so that check-kit.sh can stub them for a local run ----
// BEGIN LINUX-ONLY

// clockGettime calls clock_gettime through the standard library's syscall package,
// the way ADR 0010 (U6, V12) plans to read CLOCK_BOOTTIME.
func clockGettime(id uintptr) (time.Duration, error) {
	var ts syscall.Timespec
	_, _, e := syscall.Syscall(syscall.SYS_CLOCK_GETTIME, id, uintptr(unsafe.Pointer(&ts)), 0)
	if e != 0 {
		return 0, e
	}
	return time.Duration(ts.Sec)*time.Second + time.Duration(ts.Nsec), nil
}

// peerCred reads SO_PEERCRED of a connected Unix socket (ADR 0010, U2).
func peerCred(c *net.UnixConn) (uid, gid, pid int, err error) {
	rc, err := c.SyscallConn()
	if err != nil {
		return 0, 0, 0, err
	}
	var cred *syscall.Ucred
	var serr error
	if err := rc.Control(func(fd uintptr) {
		cred, serr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, 0, 0, err
	}
	if serr != nil {
		return 0, 0, 0, serr
	}
	return int(cred.Uid), int(cred.Gid), int(cred.Pid), nil
}

// END LINUX-ONLY

// ---- output helpers ----

func say(format string, a ...any) { fmt.Printf(format+"\n", a...) }

var errnoNames = map[syscall.Errno]string{
	syscall.EPERM: "EPERM", syscall.EACCES: "EACCES", syscall.ENOENT: "ENOENT", syscall.EROFS: "EROFS",
	syscall.ECONNREFUSED: "ECONNREFUSED", syscall.ECONNRESET: "ECONNRESET", syscall.ETIMEDOUT: "ETIMEDOUT",
	syscall.ENETUNREACH: "ENETUNREACH", syscall.EHOSTUNREACH: "EHOSTUNREACH", syscall.EAGAIN: "EAGAIN",
	syscall.EINVAL: "EINVAL", syscall.ENOSYS: "ENOSYS", syscall.EPIPE: "EPIPE", syscall.EEXIST: "EEXIST",
	syscall.EISDIR: "EISDIR", syscall.ENOTDIR: "ENOTDIR", syscall.EXDEV: "EXDEV", syscall.ENOSPC: "ENOSPC",
}

// errTag reduces an error to a class that carries no address, host name or path.
func errTag(err error) string {
	if err == nil {
		return "ok"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "dns(" + dns.Err + ")"
	}
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var en syscall.Errno
	if errors.As(err, &en) {
		if n, ok := errnoNames[en]; ok {
			return n + " (" + en.Error() + ")"
		}
		return fmt.Sprintf("errno%d (%s)", int(en), en.Error())
	}
	if errors.Is(err, io.EOF) {
		return "EOF"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	return "other"
}

func procStatus(keys ...string) {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		say("proc_status=unreadable kind=%s", errTag(err))
		return
	}
	for _, ln := range strings.Split(string(b), "\n") {
		for _, k := range keys {
			if strings.HasPrefix(ln, k+":") {
				say("%s", strings.Join(strings.Fields(ln), " "))
			}
		}
	}
}

func bootSecs() string {
	d, err := clockGettime(clockBoottime)
	if err != nil {
		return "-1"
	}
	return fmt.Sprintf("%.3f", d.Seconds())
}

// logTo appends one event line (seconds since boot, event, process id, details)
// to a file in the spike state directory and prints it as well.
func logTo(file, event string, kvs ...string) {
	line := fmt.Sprintf("t=%s event=%s pid=%d %s", bootSecs(), event, os.Getpid(), strings.Join(kvs, " "))
	line = strings.TrimRight(line, " ")
	f, err := os.OpenFile(filepath.Join(stateDir, file), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err == nil {
		fmt.Fprintln(f, line)
		f.Close()
	} else {
		say("cannot write %s: %s", file, errTag(err))
	}
	fmt.Println(line)
}

type limitedBuf struct {
	b   []byte
	max int
}

func (l *limitedBuf) Write(p []byte) (int, error) {
	n := len(p)
	if room := l.max - len(l.b); room > 0 {
		if len(p) > room {
			p = p[:room]
		}
		l.b = append(l.b, p...)
	}
	return n, nil
}

// runFixed runs one fixed program with a fixed argument list, with an empty
// environment and no shell, and prints its output and exit code.
func runFixed(prog string, args ...string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, prog, args...)
	cmd.Env = []string{}
	buf := &limitedBuf{max: 2048}
	cmd.Stdout, cmd.Stderr = buf, buf
	start := time.Now()
	err := cmd.Run()
	rc := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			rc = ee.ExitCode()
		} else {
			rc = -1
		}
	}
	say("$ %s %s", filepath.Base(prog), strings.Join(args, " "))
	for _, ln := range strings.Split(strings.TrimRight(string(buf.b), "\n"), "\n") {
		if ln != "" {
			say("| %s", ln)
		}
	}
	if err != nil && rc == -1 {
		say("exec_error=%s", errTag(err))
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && rc == -1 {
		say("process_status=%s", ee.String())
	}
	say("exit=%d elapsed_ms=%d", rc, time.Since(start).Milliseconds())
	return rc
}

func runSystemctl(args ...string) int { return runFixed(systemctl, args...) }

// ---- socket activation ----

func activatedListener() (*net.UnixListener, error) {
	pid, _ := strconv.Atoi(os.Getenv("LISTEN_PID"))
	n, _ := strconv.Atoi(os.Getenv("LISTEN_FDS"))
	if pid != os.Getpid() || n < 1 {
		return nil, errors.New("not socket activated")
	}
	syscall.CloseOnExec(3)
	f := os.NewFile(3, "activated")
	ln, err := net.FileListener(f)
	f.Close()
	if err != nil {
		return nil, err
	}
	ul, ok := ln.(*net.UnixListener)
	if !ok {
		return nil, errors.New("activated socket is not a Unix socket")
	}
	return ul, nil
}

// ---- links ----

// switchLink replaces link by a link to target with a temporary link and a
// rename (never ln -sfn), the way the helper moves current/active.
func switchLink(link, target string) error {
	tmp := fmt.Sprintf("%s.new.%d", link, os.Getpid())
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func exeDir() (string, error) {
	p, err := os.Readlink("/proc/self/exe")
	return filepath.Dir(p), err
}

// ---- V1 ----

func cmdV1() int {
	say("## sandbox state seen by the probe")
	procStatus("Uid", "Gid", "CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb", "NoNewPrivs", "Seccomp", "Threads")

	say("## additional observation: file access of uid 0 without capabilities")
	for _, name := range []string{"pub.txt", "priv.txt"} {
		b, err := os.ReadFile(filepath.Join(optDir, "fixtures", "cache", name))
		if err != nil {
			say("read %s (owned by the service user): %s", name, errTag(err))
		} else {
			say("read %s (owned by the service user): ok, %d bytes", name, len(b))
		}
	}
	probeFile := filepath.Join(optDir, "fixtures", "chown-test")
	if err := os.WriteFile(probeFile, []byte("x"), 0o600); err != nil {
		say("create file in the writable directory: %s", errTag(err))
	} else {
		say("create file in the writable directory: ok")
		say("chown of own file to another uid: %s", errTag(os.Chown(probeFile, 65534, 65534)))
		_ = os.Remove(probeFile)
	}

	say("## the four calls of V1, in this order")
	runSystemctl("daemon-reload")
	runSystemctl("restart", "oarfish-spike-target.service")
	runSystemctl("restart", "oarfish-spike-sock.socket")
	runSystemctl("reset-failed", "oarfish-spike-failing.service")

	say("## sandbox state after the calls")
	procStatus("Threads")
	return 0
}

// ---- V2 ----

func cmdV2Serve() int {
	u, err := user.Lookup("oarfish")
	if err != nil {
		say("lookup of the service user: %s", errTag(err))
		return 1
	}
	want, _ := strconv.Atoi(u.Uid)
	ul, err := activatedListener()
	if err != nil {
		say("listener: %s", err)
		return 1
	}
	say("serve start: expected_uid=%d", want)
	for {
		_ = ul.SetDeadline(time.Now().Add(20 * time.Second))
		c, err := ul.AcceptUnix()
		if err != nil {
			say("serve end: %s", errTag(err))
			return 0
		}
		uid, gid, pid, perr := peerCred(c)
		verdict := "accepted"
		if perr != nil {
			verdict = "no-credentials(" + errTag(perr) + ")"
		} else if uid != want {
			verdict = "refused-by-uid-check"
		}
		line := fmt.Sprintf("peer_uid=%d peer_gid=%d peer_pid=%d verdict=%s", uid, gid, pid, verdict)
		say("%s", line)
		_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
		fmt.Fprintln(c, line)
		c.Close()
	}
}

func cmdV2Connect(path, label string) int {
	say("client label=%s uid=%d gid=%d", label, os.Getuid(), os.Getgid())
	c, err := net.DialTimeout("unix", path, 5*time.Second)
	if err != nil {
		say("label=%s connect=FAIL kind=%s", label, errTag(err))
		return 0
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	fmt.Fprintf(c, "hello %s\n", label)
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		say("label=%s connect=ok reply=none kind=%s", label, errTag(err))
		return 0
	}
	say("label=%s connect=ok reply=[%s]", label, strings.TrimSpace(line))
	return 0
}

func tcpLoop(label, network, addr string) {
	l, err := net.Listen(network, addr)
	if err != nil {
		say("%s: listen n/a: %s", label, errTag(err))
		return
	}
	defer l.Close()
	go func() {
		if c, err := l.Accept(); err == nil {
			c.Close()
		}
	}()
	c, err := net.DialTimeout(network, l.Addr().String(), 5*time.Second)
	if err != nil {
		say("%s: connect FAIL %s", label, errTag(err))
		return
	}
	c.Close()
	say("%s: connected", label)
}

func udpLoop(label, network, addr string) {
	l, err := net.ListenPacket(network, addr)
	if err != nil {
		say("%s: listen n/a: %s", label, errTag(err))
		return
	}
	defer l.Close()
	c, err := net.Dial(network, l.LocalAddr().String())
	if err != nil {
		say("%s: dial FAIL %s", label, errTag(err))
		return
	}
	defer c.Close()
	if _, err := c.Write([]byte("x")); err != nil {
		say("%s: write FAIL %s", label, errTag(err))
		return
	}
	_ = l.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 8)
	if _, _, err := l.ReadFrom(buf); err != nil {
		say("%s: written, not received: %s", label, errTag(err))
		return
	}
	say("%s: delivered", label)
}

func cmdV2IP(args []string) int {
	target := ""
	if len(args) > 0 {
		target = args[0]
	} else {
		b, err := os.ReadFile(filepath.Join(stateDir, "ip-target"))
		if err != nil {
			say("no target: %s", errTag(err))
			return 1
		}
		target = strings.TrimSpace(string(b))
	}
	procStatus("Uid", "CapEff", "Seccomp")
	tcpLoop("tcp loopback v4", "tcp4", "127.0.0.1:0")
	tcpLoop("tcp loopback v6", "tcp6", "[::1]:0")
	udpLoop("udp loopback v4", "udp4", "127.0.0.1:0")

	start := time.Now()
	c, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		say("tcp ADDRESS: connect FAIL %s after %d ms", errTag(err), time.Since(start).Milliseconds())
	} else {
		c.Close()
		say("tcp ADDRESS: connected after %d ms", time.Since(start).Milliseconds())
	}

	u, err := net.Dial("udp", target)
	if err != nil {
		say("udp ADDRESS: dial FAIL %s", errTag(err))
		return 0
	}
	defer u.Close()
	if _, err := u.Write([]byte("x")); err != nil {
		say("udp ADDRESS: write FAIL %s", errTag(err))
		return 0
	}
	say("udp ADDRESS: write ok")
	_ = u.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := u.Read(make([]byte, 8)); err != nil {
		say("udp ADDRESS: read %s", errTag(err))
	} else {
		say("udp ADDRESS: reply received")
	}
	return 0
}

// ---- V4 ----

func fileFact(label, path string) {
	st, err := os.Stat(path)
	if err != nil {
		say("%s: missing (%s)", label, errTag(err))
		return
	}
	say("%s: exists, mode %o, %d bytes", label, st.Mode().Perm(), st.Size())
}

func tryCreate(label, path string) {
	err := os.WriteFile(path, []byte("x"), 0o600)
	if err == nil {
		_ = os.Remove(path)
	}
	say("create in %s: %s", label, errTag(err))
}

func cmdV4() int {
	say("## sandbox state seen by the probe")
	procStatus("Uid", "Gid", "CapEff", "NoNewPrivs", "Seccomp")

	say("## files the Go client reads")
	fileFact("CA bundle /etc/ssl/certs/ca-certificates.crt", "/etc/ssl/certs/ca-certificates.crt")
	fileFact("directory /etc/ssl/certs", "/etc/ssl/certs")
	if b, err := os.ReadFile("/etc/resolv.conf"); err != nil {
		say("resolv.conf: %s", errTag(err))
	} else {
		n := 0
		for _, ln := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(ln), "nameserver") {
				n++
			}
		}
		say("resolv.conf: readable, %d nameserver lines", n)
	}
	if _, err := os.ReadFile("/etc/nsswitch.conf"); err != nil {
		say("nsswitch.conf: %s", errTag(err))
	} else {
		say("nsswitch.conf: readable")
	}

	say("## DNS lookup with the Go resolver (cgo off), counts only")
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, "api.github.com")
	cancel()
	if err != nil {
		say("lookup api.github.com: FAIL %s after %d ms", errTag(err), time.Since(start).Milliseconds())
	} else {
		v4, v6 := 0, 0
		for _, ip := range ips {
			if ip.IP.To4() != nil {
				v4++
			} else {
				v6++
			}
		}
		say("lookup api.github.com: ok, %d IPv4 and %d IPv6 answers, %d ms", v4, v6, time.Since(start).Milliseconds())
	}

	say("## HTTPS HEAD with net/http, TLS 1.2 or newer, system roots, no proxy")
	var dnsStart, connStart, tlsStart time.Time
	var dnsMs, connMs, tlsMs int64
	trace := &httptrace.ClientTrace{
		DNSStart:          func(httptrace.DNSStartInfo) { dnsStart = time.Now() },
		DNSDone:           func(httptrace.DNSDoneInfo) { dnsMs = time.Since(dnsStart).Milliseconds() },
		ConnectStart:      func(string, string) { connStart = time.Now() },
		ConnectDone:       func(string, string, error) { connMs = time.Since(connStart).Milliseconds() },
		TLSHandshakeStart: func() { tlsStart = time.Now() },
		TLSHandshakeDone:  func(tls.ConnectionState, error) { tlsMs = time.Since(tlsStart).Milliseconds() },
	}
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		DisableKeepAlives:     true,
		ForceAttemptHTTP2:     true,
	}
	cl := &http.Client{Transport: tr, Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace),
		http.MethodHead, "https://api.github.com/rate_limit", nil)
	req.Header.Set("User-Agent", "oarfish-spike/g6-1")
	start = time.Now()
	resp, err := cl.Do(req)
	if err != nil {
		say("HEAD api.github.com/rate_limit: FAIL %s after %d ms", errTag(err), time.Since(start).Milliseconds())
	} else {
		resp.Body.Close()
		say("HEAD api.github.com/rate_limit: status %d proto %s total %d ms (dns %d, connect %d, tls %d)",
			resp.StatusCode, resp.Proto, time.Since(start).Milliseconds(), dnsMs, connMs, tlsMs)
		if resp.TLS != nil {
			say("tls: %s, cipher %s, verified chains %d", tls.VersionName(resp.TLS.Version),
				tls.CipherSuiteName(resp.TLS.CipherSuite), len(resp.TLS.VerifiedChains))
		}
		for _, h := range []string{"X-Ratelimit-Limit", "X-Ratelimit-Remaining", "X-Ratelimit-Resource"} {
			say("header %s: %s", h, resp.Header.Get(h))
		}
	}

	say("## writes the update client would do (U3), and writes that must fail")
	if cache := os.Getenv("CACHE_DIRECTORY"); cache != "" {
		p := filepath.Join(cache, "probe-file.txt")
		err := os.WriteFile(p, []byte("x"), 0o600)
		if err == nil {
			err = os.Chmod(p, 0o644)
		}
		mode := "n/a"
		if st, serr := os.Stat(p); serr == nil {
			mode = fmt.Sprintf("%o", st.Mode().Perm())
		}
		say("cache directory (CacheDirectory=): write and chmod 0644: %s, mode now %s", errTag(err), mode)
		_ = os.Remove(p)
	} else {
		say("cache directory: CACHE_DIRECTORY not set")
	}
	tryCreate("/opt/oarfish-spike (not writable for the service)", filepath.Join(optDir, "probe-neg"))
	tryCreate("/etc (ProtectSystem=strict)", "/etc/oarfish-spike-neg")
	tryCreate("/var/lib/oarfish-spike-update (not the unit's directory)", filepath.Join(stateDir, "probe-neg"))
	return 0
}

// ---- V8, V10 ----

func cmdV8Boot() int {
	_ = os.MkdirAll(stateDir, 0o700)
	bt := bootSecs()
	up := "n/a"
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			up = f[0]
		}
	}
	now := time.Now().UTC()
	ntp := "unknown"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/timedatectl", "show", "-p", "NTPSynchronized", "--value")
	cmd.Env = []string{}
	if out, err := cmd.Output(); err == nil {
		ntp = strings.TrimSpace(string(out))
	}
	logTo("v8-boot.log", "boot-record",
		"boottime_s="+bt, "uptime_s="+up,
		"realtime_utc="+now.Format("2006-01-02T15:04:05Z"), "realtime_unix="+strconv.FormatInt(now.Unix(), 10),
		"ntp_synchronized="+ntp)
	return 0
}

func cmdV10Run(name, marker string) int {
	_ = os.MkdirAll(stateDir, 0o700)
	exe, _ := os.Readlink("/proc/self/exe")
	up := "n/a"
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			up = f[0]
		}
	}
	logTo("v10-"+name+".log", "unit-run", "name="+name, "release="+marker,
		"uptime_s="+up, "dropin_env="+os.Getenv("OARFISH_SPIKE_DROPIN"), "exe="+filepath.Base(exe))
	return 0
}

func cmdV10Helper() int {
	say("## the binary was reached through links")
	exe, _ := os.Readlink("/proc/self/exe")
	say("argv0 as started: %s", os.Args[0])
	say("resolved executable: %s", exe)
	procStatus("Uid", "CapEff", "CapBnd", "Seccomp")

	say("## writes that must work (ReadWritePaths=/opt/oarfish-spike and the state directory)")
	p := filepath.Join(optDir, "components", "helper-write-test")
	err := os.WriteFile(p, []byte("x"), 0o600)
	say("create file below /opt/oarfish-spike/components: %s", errTag(err))
	_ = os.Remove(p)
	link := filepath.Join(optDir, "components", "active-test")
	say("atomic link switch below /opt/oarfish-spike/components (temporary link + rename): %s", errTag(switchLink(link, "c1")))
	_ = os.Remove(link)
	say("create file in the state directory: %s", errTag(os.WriteFile(filepath.Join(stateDir, "helper-write-test"), []byte("x"), 0o600)))
	_ = os.Remove(filepath.Join(stateDir, "helper-write-test"))

	say("## writes that must fail (negative test)")
	say("create file in /usr/libexec: %s", errTag(os.WriteFile("/usr/libexec/oarfish-spike-neg", []byte("x"), 0o600)))
	say("create file in /etc/systemd/system: %s", errTag(os.WriteFile("/etc/systemd/system/oarfish-spike-neg.service", []byte("x"), 0o600)))
	say("replace the link /usr/libexec/oarfish-spike-helper (temporary link + rename): %s",
		errTag(switchLink("/usr/libexec/oarfish-spike-helper", filepath.Join(optDir, "components", "active", "oarfish-spike-helper"))))
	say("replace the linked unit /etc/systemd/system/oarfish-spike-v10a.service (temporary link + rename): %s",
		errTag(switchLink("/etc/systemd/system/oarfish-spike-v10a.service", filepath.Join(optDir, "current", "oarfish-spike-v10a.service"))))
	say("create file in /etc/systemd/system/oarfish-spike-v10a.service.d: %s",
		errTag(os.WriteFile("/etc/systemd/system/oarfish-spike-v10a.service.d/neg.conf", []byte("x"), 0o600)))

	say("## reads that must work")
	if _, err := os.ReadFile("/etc/systemd/system/oarfish-spike-v10a.service"); err != nil {
		say("read the linked unit: %s", errTag(err))
	} else {
		say("read the linked unit through its links: ok")
	}
	return 0
}

// ---- V11 ----

var validComp = func(s string) bool {
	if len(s) < 3 || len(s) > 16 || !strings.HasPrefix(s, "c-") {
		return false
	}
	for _, r := range s[2:] {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func readMode(dir string) []string {
	b, err := os.ReadFile(filepath.Join(dir, "mode"))
	if err != nil {
		return nil
	}
	return strings.Fields(string(b))
}

func cmdH11() int {
	dir, err := exeDir()
	if err != nil {
		say("exe: %s", errTag(err))
		return 3
	}
	comp := filepath.Base(dir)
	f := readMode(dir)
	mode := "none"
	if len(f) > 0 {
		mode = f[0]
	}
	logTo("h11.log", "start", "comp="+comp, "mode="+mode)
	switch mode {
	case "update75": // update75 <delay_s> <next component>
		if len(f) != 3 || !validComp(f[2]) {
			logTo("h11.log", "badmode", "comp="+comp)
			return 3
		}
		delay, err := strconv.Atoi(f[1])
		if err != nil || delay < 0 || delay > 30 {
			logTo("h11.log", "badmode", "comp="+comp)
			return 3
		}
		time.Sleep(time.Duration(delay) * time.Second)
		serr := switchLink(h11Dir+"/active", "components/"+f[2])
		logTo("h11.log", "exit75", "comp="+comp, "next="+f[2], "switch="+errTag(serr))
		return 75
	case "crash":
		logTo("h11.log", "exit1", "comp="+comp)
		return 1
	case "hang":
		logTo("h11.log", "hang", "comp="+comp)
		for {
			time.Sleep(time.Hour)
		}
	case "ok":
		time.Sleep(3 * time.Second)
		logTo("h11.log", "exit0", "comp="+comp)
		return 0
	}
	logTo("h11.log", "badmode", "comp="+comp)
	return 3
}

// cmdRecover does what the recover unit of ADR 0010 (U15 rule 4) does after a
// helper that does not start: switch active back to known-good, reload, reset
// the failed unit.
func cmdRecover() int {
	dir, _ := exeDir()
	logTo("h11.log", "recover-start", "comp="+filepath.Base(dir))
	t, err := os.Readlink(h11Dir + "/known-good")
	if err != nil {
		logTo("h11.log", "recover-fail", "readlink="+errTag(err))
		return 1
	}
	serr := switchLink(h11Dir+"/active", t)
	rc1 := runSystemctl("daemon-reload")
	rc2 := runSystemctl("reset-failed", "oarfish-spike-h11.service")
	logTo("h11.log", "recover-done", "switch="+errTag(serr), fmt.Sprintf("daemon_reload_rc=%d reset_failed_rc=%d", rc1, rc2))
	return 0
}

// ---- V12 ----

func cmdV12() int {
	say("probe started")
	procStatus("Uid", "CapEff", "NoNewPrivs", "Seccomp")
	var bt, mono time.Duration
	var btErr error
	for _, c := range []struct {
		name string
		id   uintptr
	}{{"CLOCK_MONOTONIC", clockMonotonic}, {"CLOCK_BOOTTIME", clockBoottime}} {
		d, err := clockGettime(c.id)
		if err != nil {
			say("clock_gettime(%s) via syscall.Syscall: FAIL %s", c.name, errTag(err))
		} else {
			say("clock_gettime(%s) via syscall.Syscall: ok, %.3f s", c.name, d.Seconds())
		}
		if c.id == clockBoottime {
			bt, btErr = d, err
		} else {
			mono = d
		}
	}
	_ = mono
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		say("read /proc/uptime: FAIL %s", errTag(err))
		return 0
	}
	f := strings.Fields(string(b))
	say("read /proc/uptime: ok, first field %s", f[0])
	if up, perr := strconv.ParseFloat(f[0], 64); perr == nil && btErr == nil {
		say("difference CLOCK_BOOTTIME minus /proc/uptime: %.3f s", bt.Seconds()-up)
	}
	return 0
}

// ---- V13 ----

func cmdV13Serve() int {
	ul, err := activatedListener()
	if err != nil {
		say("listener: %s", err)
		return 1
	}
	logTo("v13.log", "service-start")
	_ = ul.SetDeadline(time.Now().Add(10 * time.Second))
	c, err := ul.AcceptUnix()
	if err != nil {
		logTo("v13.log", "idle-exit", "kind="+errTag(err))
		return 0
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, _ := bufio.NewReader(c).ReadString('\n')
	label := strings.TrimSpace(strings.TrimPrefix(line, "hello "))
	logTo("v13.log", "accepted", "label="+label)
	time.Sleep(3 * time.Second)
	fmt.Fprintf(c, "done label=%s\n", label)
	logTo("v13.log", "replied", "label="+label)
	// The accepted connection stays open during the restart on purpose.
	rc := runSystemctl("restart", "oarfish-spike-v13.socket")
	logTo("v13.log", "socket-restarted", fmt.Sprintf("rc=%d", rc))
	c.Close()
	logTo("v13.log", "service-exit")
	return 0
}

func cmdV13Client(path, label string) int {
	start := time.Now()
	ms := func() int64 { return time.Since(start).Milliseconds() }
	c, err := net.DialTimeout("unix", path, 5*time.Second)
	if err != nil {
		say("client %s: connect FAIL %s at %d ms", label, errTag(err), ms())
		return 0
	}
	defer c.Close()
	say("client %s: connected at %d ms", label, ms())
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	fmt.Fprintf(c, "hello %s\n", label)
	r := bufio.NewReader(c)
	line, err := r.ReadString('\n')
	if err != nil {
		say("client %s: no reply, %s at %d ms", label, errTag(err), ms())
		return 0
	}
	say("client %s: reply [%s] at %d ms", label, strings.TrimSpace(line), ms())
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	_, err = r.ReadByte()
	say("client %s: after the reply the connection ends with %s at %d ms", label, errTag(err), ms())
	return 0
}

// ---- misc ----

func cmdIdle() int {
	for {
		time.Sleep(time.Hour)
	}
}

func cmdInfo() int {
	say("probe: oarfish-spike-probe for G6.1")
	say("go: %s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	say("cpus: %d", runtime.NumCPU())
	return 0
}

// cmdSelftest runs the parts that need no Linux feature; check-kit.sh runs it on
// the development machine with the Linux-only calls stubbed.
func cmdSelftest() int {
	fail := 0
	check := func(name string, ok bool) {
		if !ok {
			say("selftest FAIL: %s", name)
			fail++
		}
	}
	check("errTag nil", errTag(nil) == "ok")
	check("errTag errno", strings.HasPrefix(errTag(&os.PathError{Op: "open", Path: "/x", Err: syscall.EROFS}), "EROFS"))
	check("errTag hides path", !strings.Contains(errTag(&os.PathError{Op: "open", Path: "/secret", Err: syscall.EACCES}), "secret"))
	check("errTag dns hides name", !strings.Contains(errTag(&net.DNSError{Err: "no such host", Name: "example.invalid"}), "example"))
	check("errTag timeout", errTag(os.ErrDeadlineExceeded) == "timeout")
	check("validComp ok", validComp("c-a1") && validComp("c-good"))
	check("validComp rejects", !validComp("c-../x") && !validComp("x") && !validComp("c-") && !validComp("c-A"))
	lb := &limitedBuf{max: 4}
	n, _ := lb.Write([]byte("abcdefgh"))
	check("limitedBuf", n == 8 && bytes.Equal(lb.b, []byte("abcd")))
	dir, err := os.MkdirTemp("", "probe-selftest")
	if err == nil {
		defer os.RemoveAll(dir)
		_ = os.Mkdir(filepath.Join(dir, "c1"), 0o755)
		_ = os.Mkdir(filepath.Join(dir, "c2"), 0o755)
		link := filepath.Join(dir, "active")
		check("switchLink create", switchLink(link, "c1") == nil)
		check("switchLink replace", switchLink(link, "c2") == nil)
		t, _ := os.Readlink(link)
		check("switchLink target", t == "c2")
		_ = os.WriteFile(filepath.Join(dir, "c1", "mode"), []byte("update75 2 c-a2\n"), 0o600)
		m := readMode(filepath.Join(dir, "c1"))
		check("readMode", len(m) == 3 && m[0] == "update75" && m[2] == "c-a2")
		check("readMode missing", readMode(filepath.Join(dir, "none")) == nil)
	} else {
		check("tempdir", false)
	}
	if fail == 0 {
		say("selftest ok")
		return 0
	}
	return 1
}

func usage() int {
	fmt.Fprintln(os.Stderr, "usage: oarfish-spike-probe <info|selftest|idle|v1|v2-serve|v2-connect PATH LABEL|v2-ip [HOST:PORT]|v4|v8-boot|v10-run NAME RELEASE|v10-helper|h11|recover|v12|v13-serve|v13-client PATH LABEL>")
	return 2
}

func main() {
	if len(os.Args) < 2 {
		os.Exit(usage())
	}
	a := os.Args[2:]
	need := func(n int) bool { return len(a) == n }
	var rc int
	switch os.Args[1] {
	case "info":
		rc = cmdInfo()
	case "selftest":
		rc = cmdSelftest()
	case "idle":
		rc = cmdIdle()
	case "v1":
		rc = cmdV1()
	case "v2-serve":
		rc = cmdV2Serve()
	case "v2-connect":
		if !need(2) {
			os.Exit(usage())
		}
		rc = cmdV2Connect(a[0], a[1])
	case "v2-ip":
		rc = cmdV2IP(a)
	case "v4":
		rc = cmdV4()
	case "v8-boot":
		rc = cmdV8Boot()
	case "v10-run":
		if !need(2) {
			os.Exit(usage())
		}
		rc = cmdV10Run(a[0], a[1])
	case "v10-helper":
		rc = cmdV10Helper()
	case "h11":
		rc = cmdH11()
	case "recover":
		rc = cmdRecover()
	case "v12":
		rc = cmdV12()
	case "v13-serve":
		rc = cmdV13Serve()
	case "v13-client":
		if !need(2) {
			os.Exit(usage())
		}
		rc = cmdV13Client(a[0], a[1])
	default:
		rc = usage()
	}
	os.Exit(rc)
}
