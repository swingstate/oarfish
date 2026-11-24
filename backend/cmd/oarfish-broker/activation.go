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

// activatedListener turns the one socket that systemd passed into a listener
// (sd_listen_fds(3)): LISTEN_PID must equal the own pid, LISTEN_FDS must be 1,
// and the socket is descriptor 3. The broker never binds or creates a socket.
// The variables are unset first, so that no child inherits them (the
// environment of systemctl is cleared anyway).
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
	if f == nil {
		return nil, errNotListener
	}
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
	// The socket file belongs to systemd; closing the listener never removes it.
	ul.SetUnlinkOnClose(false)
	return ul, nil
}
