package main

import (
	"errors"
	"syscall"
)

// The helper side of the shared lock (ADR 0010 U7, U8, A9, T14). The installer
// holds an exclusive flock on the directory /var/lib/oarfish-update through
// descriptor 9 (`flock -x -w`); the helper locks the same object, the
// directory, with LOCK_EX|LOCK_NB on a descriptor of its own. flock locks
// belong to the open file description, so two requests of the helper conflict
// with each other exactly like the helper and the installer do.

var errLockBusy = errors.New("lock held")

// dirLock is a held lock. release is idempotent.
type dirLock struct{ fd int }

// lockDir opens dir (O_RDONLY, O_DIRECTORY, O_NOFOLLOW, O_CLOEXEC) and takes
// the exclusive lock without waiting. errLockBusy means somebody else holds
// it; any other error means the directory could not be opened or locked.
func lockDir(dir string) (*dirLock, error) {
	var fd int
	var err error
	for {
		fd, err = syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		syscall.Close(fd)
		if err == syscall.EWOULDBLOCK {
			return nil, errLockBusy
		}
		return nil, err
	}
	return &dirLock{fd: fd}, nil
}

// release drops the lock by closing the descriptor, which also releases the
// flock. Callers use defer so that every path, errors and the deadline
// included, releases it.
func (l *dirLock) release() {
	if l == nil || l.fd < 0 {
		return
	}
	syscall.Close(l.fd)
	l.fd = -1
}
