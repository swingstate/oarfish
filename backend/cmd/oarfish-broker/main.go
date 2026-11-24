// Command oarfish-broker is the privilege broker of ADR 0011 (step G7.2, with
// amendments A1, A3 and A4 and decisions D-C87 to D-C89).
//
// It is a root process started by socket activation. It serves exactly six
// requests: status, service.start, service.stop, service.restart,
// system.reboot and system.poweroff. Every argument is checked against the
// closed rules of B2 and B3 before anything runs, and the only program it
// starts is /usr/bin/systemctl with one of the fixed argument shapes of B2.
// There is no generic command, path, verb, option or property argument.
//
// The broker writes no file. It reads only the system user database (once, at
// start) and, for a power operation, the update helper's lock and the member
// `phase` of its state file in /var/lib/oarfish-update (B6, ADR 0010 A19).
//
// It has one subcommand, serve, and no flag. Everything the tests replace is in
// options, which only tests construct; main always runs productionOptions, so
// no test seam is reachable from the command line or the environment.
//
// It is standard library only: it imports no package of this module and no
// third-party module (TestImports).
package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"runtime/debug"
	"strconv"
	"sync"
	"time"
)

const (
	// serviceUser is the only user whose connections are served (B4).
	serviceUser = "oarfish"

	// buildID is build_id in status. It is a constant compiled into the
	// binary, changed by hand with the code, and never the release version or
	// the commit: the broker's bytes must not depend on either (D-C80 applied
	// to the broker, D-C89).
	buildID = "broker-1"

	// softMemoryLimit is the Go runtime's soft limit (B8). MemoryMax= of the
	// unit is not enforced where the memory controller is absent (ADR 0010
	// A4), so the program limits itself.
	softMemoryLimit = 16 << 20

	idleExit        = 30 * time.Second  // B8: exit 30 s after the last connection
	requestDeadline = 5 * time.Second   // time to send the request line
	maxConnections  = 4                 // connections served at once; more wait in the backlog
	opDeadline      = 100 * time.Second // B2, A3.3: deadline of each mutating systemctl call
	showDeadline    = 5 * time.Second   // deadline of each show call (A4)
	verifyDelay     = 1 * time.Second   // A3.3: the verification read after start and restart
	rateWindow      = 60 * time.Second  // B4: the window of the rate limit
	rateMax         = 10                // B4: mutating service operations per window
)

const usage = "usage: oarfish-broker serve"

func main() {
	configureRuntime()
	os.Exit(run(os.Args[1:], os.Stderr, productionOptions()))
}

// configureRuntime sets the soft memory limit before anything allocates.
func configureRuntime() { debug.SetMemoryLimit(softMemoryLimit) }

// options holds everything the tests replace; productionOptions is what runs.
type options struct {
	getenv    func(string) string
	pid       int
	lookupUID func(name string) (uint32, error)
	peerUID   func(net.Conn) (uint32, error)
	journal   io.Writer
	now       func() time.Time

	systemctlPath string
	// runner starts systemctl; nil means runProgram. Only tests set it.
	runner    func(path string, deadline time.Duration, args []string) (runResult, error)
	updateDir string
	policy    func(coreUID uint32) Policy

	idle            time.Duration
	requestDeadline time.Duration
	maxConnections  int
	opDeadline      time.Duration
	showDeadline    time.Duration
	verifyDelay     time.Duration
	rateWindow      time.Duration
	rateMax         int
}

func productionOptions() options {
	return options{
		getenv:          os.Getenv,
		pid:             os.Getpid(),
		lookupUID:       lookupUID,
		peerUID:         peerUID,
		journal:         os.Stderr,
		now:             time.Now,
		systemctlPath:   systemctlPath,
		updateDir:       updateDir,
		policy:          newCorePolicy,
		idle:            idleExit,
		requestDeadline: requestDeadline,
		maxConnections:  maxConnections,
		opDeadline:      opDeadline,
		showDeadline:    showDeadline,
		verifyDelay:     verifyDelay,
		rateWindow:      rateWindow,
		rateMax:         rateMax,
	}
}

// run dispatches the subcommand and returns the exit status: 0 after an idle
// exit, 1 when serve cannot run, 2 for a usage error.
func run(args []string, stderr io.Writer, o options) int {
	if len(args) != 1 || args[0] != "serve" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	return serve(stderr, o)
}

// serve runs the broker. Every start-up message is fixed and carries no
// environment value and no operating-system error text.
func serve(stderr io.Writer, o options) int {
	ln, err := activatedListener(o.getenv, o.pid)
	if err != nil {
		fmt.Fprintln(stderr, "oarfish-broker: "+err.Error())
		return 1
	}
	defer ln.Close()
	j := &journal{w: o.journal}
	uid, err := o.lookupUID(serviceUser)
	refuseAll := err != nil
	if refuseAll {
		// B4: without the user every connection is closed without an answer.
		j.line("oarfish-broker: the user " + serviceUser + " does not exist; every connection is refused")
	}
	b := newBroker(o, uid, j)
	s := newServer(o, uid, refuseAll, b)
	if err := s.serve(ln); err != nil {
		fmt.Fprintln(stderr, "oarfish-broker: the listening socket failed")
		return 1
	}
	return 0
}

// lookupUID looks the user up once at start (B4). With CGO_ENABLED=0 the
// standard library reads /etc/passwd.
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

// journal writes whole lines to standard error, which systemd puts into the
// journal. Lines of concurrent connections never interleave.
type journal struct {
	mu sync.Mutex
	w  io.Writer
}

func (j *journal) line(s string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	io.WriteString(j.w, s+"\n")
}
