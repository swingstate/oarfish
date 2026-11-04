// Command oarfish-update-helper is the privileged update helper of ADR 0010.
//
// This is the helper core of step G6.5: the strict request decoder, the peer
// check, `status`, `stage` and `discard`, the state file and the shared lock.
// `activate` and `ack` are known names that are answered with bad_request
// until G6.6, and `recover` still touches nothing (G6.6). Nothing here has
// been run on a device.
//
// The helper has exactly two subcommands. `serve` runs only when started by
// socket activation (U1, U2); `recover` is the boot-time recovery (U7).
//
// It is standard library plus this repository's relverify, trustkey and version
// packages. It must not import net/http, os/exec, a third-party module or the
// service's web, auth, audit, clock, securefile or server packages; TestImports
// checks this against `go list -deps`.
package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"runtime"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/swingstate/oarfish/backend/internal/trustkey"
	"github.com/swingstate/oarfish/backend/internal/version"
)

const (
	// serviceUser is the only user whose connections are served (U2).
	serviceUser = "oarfish"
	// releaseRoot is the tree the helper manages (U5).
	releaseRoot = "/opt/oarfish"

	// softMemoryLimit is the Go runtime's soft limit. The unit's MemoryMax=64M
	// is not enforced where the memory controller is absent (A4), so the
	// program limits itself; U12 expects a peak below 16 MB. The limit is
	// set by the program and not by an Environment= line (A4).
	softMemoryLimit = 16 << 20

	idleExit        = 30 * time.Second // U1: exit after 30 s without a connection
	requestDeadline = 5 * time.Second  // U2: time to send the request
	maxConnections  = 4                // U2
)

const usage = "usage: oarfish-update-helper serve|recover"

func main() {
	configureRuntime()
	os.Exit(run(os.Args[1:], os.Stderr, productionOptions()))
}

// configureRuntime sets the soft memory limit before anything allocates.
func configureRuntime() { debug.SetMemoryLimit(softMemoryLimit) }

// options holds everything the tests replace; productionOptions is what runs.
type options struct {
	getenv          func(string) string
	pid             int
	lookupUID       func(name string) (uint32, error)
	peerUID         func(net.Conn) (uint32, error)
	root            string // /opt/oarfish
	stateDir        string // /var/lib/oarfish-update
	cacheDir        string // /var/cache/oarfish
	arch            string
	journal         io.Writer
	helperVersion   string
	idle            time.Duration
	requestDeadline time.Duration
	maxConnections  int
	maxResponse     int
}

func productionOptions() options {
	return options{
		getenv:          os.Getenv,
		pid:             os.Getpid(),
		lookupUID:       lookupUID,
		peerUID:         peerUID,
		root:            releaseRoot,
		stateDir:        stateDirPath,
		cacheDir:        cacheDirPath,
		arch:            runtime.GOARCH,
		journal:         os.Stderr,
		helperVersion:   version.Version,
		idle:            idleExit,
		requestDeadline: requestDeadline,
		maxConnections:  maxConnections,
		maxResponse:     maxResponseBytes,
	}
}

// run dispatches the subcommand and returns the process exit status: 0 on
// success, 1 when serve cannot run, 2 for a usage error.
func run(args []string, stderr io.Writer, o options) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	switch args[0] {
	case "recover":
		// The stub touches nothing. Recovery is G6.6.
		return 0
	case "serve":
		return serve(stderr, o)
	default:
		fmt.Fprintln(stderr, usage)
		return 2
	}
}

// serve returns 1 with a fixed message for every start-up failure. The
// messages carry no environment value and no operating-system error text.
func serve(stderr io.Writer, o options) int {
	ln, err := activatedListener(o.getenv, o.pid)
	if err != nil {
		fmt.Fprintln(stderr, "oarfish-update-helper: "+err.Error())
		return 1
	}
	defer ln.Close()
	uid, err := o.lookupUID(serviceUser)
	if err != nil {
		fmt.Fprintln(stderr, "oarfish-update-helper: cannot determine the uid of the service user")
		return 1
	}
	key, err := trustkey.Release()
	if err != nil {
		fmt.Fprintln(stderr, "oarfish-update-helper: the embedded release key is unusable")
		return 1
	}
	s := newServer(o, uid, newHandler(o, key))
	if err := s.serve(ln); err != nil {
		fmt.Fprintln(stderr, "oarfish-update-helper: the listening socket failed")
		return 1
	}
	return 0
}

// lookupUID looks the user up once at start (U2). With CGO_ENABLED=0 this
// reads /etc/passwd.
func lookupUID(name string) (uint32, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, err
	}
	id, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(id), nil
}
