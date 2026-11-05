package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// The health check of ADR 0010 U6. The helper decides, nobody else.

const (
	defaultListen = "127.0.0.1:8484"
	healthPath    = "/healthz"

	healthDeadline  = 45 * time.Second // after the restart returned
	healthStability = 10 * time.Second // conditions hold at least this long
	healthPolls     = 3                // and for this many polls in a row
	precheckLimit   = 10 * time.Second // the pre-check of a transaction
	precheckPolls   = 2
	pollEvery       = time.Second

	httpTimeout  = 2 * time.Second
	maxHTTPBytes = 8 << 10
)

// unitProps is what `systemctl show` reports.
type unitProps struct {
	Active, Sub string
	MainPID     int64
	NRestarts   int64
	ExecStart   string
}

var errShow = errors.New("unreadable systemctl show output")

// parseShow reads the Key=Value lines of `systemctl show`. All five properties
// must be present; a number must be a non-negative integer.
func parseShow(out []byte) (unitProps, error) {
	var p unitProps
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return p, errShow
		}
		switch k {
		case "ActiveState":
			p.Active = v
		case "SubState":
			p.Sub = v
		case "MainPID", "NRestarts":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				return p, errShow
			}
			if k == "MainPID" {
				p.MainPID = n
			} else {
				p.NRestarts = n
			}
		case "ExecStart":
			p.ExecStart = v
		default:
			continue
		}
		seen[k] = true
	}
	if len(seen) != 5 {
		return p, errShow
	}
	return p, nil
}

// show reads the properties of oarfish.service.
func (h *handler) show() (unitProps, error) {
	out, err := h.systemctl(sysShow...)
	if err != nil {
		return unitProps{}, err
	}
	return parseShow(out)
}

// listenAddress is the address /healthz is asked on: the argument of -listen in
// the ExecStart property, accepted only when it is a literal loopback IP
// address with a port, else the default (U6).
func listenAddress(execStart string) string {
	argv := execStart
	if i := strings.Index(argv, "argv[]="); i >= 0 {
		argv = argv[i+len("argv[]="):]
		if j := strings.Index(argv, " ; "); j >= 0 {
			argv = argv[:j]
		}
	} else {
		return defaultListen
	}
	fields := strings.Fields(argv)
	for i, f := range fields {
		var val string
		switch {
		case (f == "-listen" || f == "--listen") && i+1 < len(fields):
			val = fields[i+1]
		case strings.HasPrefix(f, "-listen="):
			val = strings.TrimPrefix(f, "-listen=")
		case strings.HasPrefix(f, "--listen="):
			val = strings.TrimPrefix(f, "--listen=")
		default:
			continue
		}
		if ap, err := netip.ParseAddrPort(val); err == nil && ap.Addr().IsLoopback() && ap.Port() != 0 {
			return ap.String()
		}
		return defaultListen
	}
	return defaultListen
}

// httpGet is the production health client: one HTTP/1.0 GET to a literal
// loopback address, no redirects, no proxy, a bounded answer. It is written
// out because net/http is not allowed in the helper (U1).
func httpGet(addr, path string) (int, []byte, error) {
	conn, err := net.DialTimeout("tcp", addr, httpTimeout)
	if err != nil {
		return 0, nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(httpTimeout))
	if _, err := conn.Write([]byte("GET " + path + " HTTP/1.0\r\nHost: " + addr + "\r\nConnection: close\r\n\r\n")); err != nil {
		return 0, nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(conn, maxHTTPBytes))
	if err != nil && len(raw) == 0 {
		return 0, nil, err
	}
	return parseHTTP(raw)
}

var errHTTP = errors.New("unreadable HTTP answer")

func parseHTTP(raw []byte) (int, []byte, error) {
	head, body, ok := bytes.Cut(raw, []byte("\r\n\r\n"))
	if !ok {
		return 0, nil, errHTTP
	}
	line, _, _ := bytes.Cut(head, []byte("\r\n"))
	f := strings.Fields(string(line))
	if len(f) < 2 || !strings.HasPrefix(f[0], "HTTP/1.") {
		return 0, nil, errHTTP
	}
	code, err := strconv.Atoi(f[1])
	if err != nil {
		return 0, nil, errHTTP
	}
	return code, body, nil
}

// healthzOK is criteria 3 and 4: 200, "status":"ok", and the version that was
// installed.
func (h *handler) healthzOK(addr, want string) bool {
	get := h.healthGet
	if get == nil {
		get = httpGet
	}
	code, body, err := get(addr, healthPath)
	if err != nil || code != 200 {
		return false
	}
	var r struct {
		Status  *string `json:"status"`
		Version *string `json:"version"`
	}
	if json.Unmarshal(body, &r) != nil || r.Status == nil || r.Version == nil {
		return false
	}
	return *r.Status == "ok" && *r.Version == want
}

type healthResult int

const (
	healthOK healthResult = iota
	healthFailFast
	healthTimeout
)

// healthParams selects the variant of the check: the pre-check of a
// transaction and the check after a restart differ in these values only.
type healthParams struct {
	version      string
	prevPID      int64 // MainPID must differ from this; -1 does not check it
	baseRestarts int64 // NRestarts must not exceed this; -1 does not check it
	deadline     time.Duration
	polls        int
	window       time.Duration
}

// waitHealthy polls every second (U6): ActiveState active, SubState running, a
// MainPID that is not the old one, NRestarts not increased, /healthz answering
// with the version, all of it for polls consecutive polls and at least window.
// ActiveState failed or an increased NRestarts ends the wait at once; so does
// the deadline. It returns the properties of the last poll that could be read.
func (h *handler) waitHealthy(p healthParams) (healthResult, unitProps) {
	start := h.monotonic()
	var last unitProps
	good := 0
	var firstGood time.Duration
	for {
		props, err := h.show()
		now := h.monotonic()
		if err == nil {
			last = props
			if props.Active == "failed" || (p.baseRestarts >= 0 && props.NRestarts > p.baseRestarts) {
				return healthFailFast, last
			}
			ok := props.Active == "active" && props.Sub == "running" && props.MainPID > 0 &&
				(p.prevPID < 0 || props.MainPID != p.prevPID) &&
				h.healthzOK(listenAddress(props.ExecStart), p.version)
			if ok {
				if good == 0 {
					firstGood = now
				}
				good++
				if good >= p.polls && now-firstGood >= p.window {
					return healthOK, last
				}
			} else {
				good = 0
			}
		} else {
			good = 0
		}
		if now-start >= p.deadline {
			return healthTimeout, last
		}
		h.pause(pollEvery)
	}
}

// pause waits; the tests replace it with a fake clock.
func (h *handler) pause(d time.Duration) {
	if h.sleep != nil {
		h.sleep(d)
		return
	}
	time.Sleep(d)
}
