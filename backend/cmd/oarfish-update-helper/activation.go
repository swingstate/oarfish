package main

import (
	"errors"
	"net"
	"os"
	"strconv"
)

// firstPassedFD is SD_LISTEN_FDS_START: systemd passes sockets from fd 3.
const firstPassedFD = 3

// Fixed messages. None of them contains an environment value.
var (
	errNotActivated = errors.New("not started by socket activation")
	errWrongFDs     = errors.New("expected exactly one passed socket")
	errNotListener  = errors.New("the passed descriptor is not a Unix stream listener")
)

// activatedListener turns the socket that systemd passed into a listener.
// It never binds or creates a socket itself (U2). The activation protocol is
// sd_listen_fds(3): LISTEN_PID must equal the own pid, LISTEN_FDS must be 1,
// and the socket is descriptor 3. The variables are unset afterwards so that
// nothing inherits them.
func activatedListener(getenv func(string) string, pid int) (*net.UnixListener, error) {
	listenPID, listenFDs := getenv("LISTEN_PID"), getenv("LISTEN_FDS")
	os.Unsetenv("LISTEN_PID")
	os.Unsetenv("LISTEN_FDS")
	os.Unsetenv("LISTEN_FDNAMES")
	if listenPID == "" || listenFDs == "" {
		return nil, errNotActivated
	}
	if p, err := strconv.Atoi(listenPID); err != nil || p != pid {
		return nil, errNotActivated
	}
	if n, err := strconv.Atoi(listenFDs); err != nil || n != 1 {
		return nil, errWrongFDs
	}
	f := os.NewFile(firstPassedFD, "passed-socket")
	defer f.Close()
	fl, err := net.FileListener(f) // duplicates the descriptor; f is closed above
	if err != nil {
		return nil, errNotListener
	}
	ul, ok := fl.(*net.UnixListener)
	if !ok {
		fl.Close()
		return nil, errNotListener
	}
	// The socket file belongs to systemd. Closing the listener must never
	// remove it (a FileListener does not, this makes it explicit).
	ul.SetUnlinkOnClose(false)
	return ul, nil
}
