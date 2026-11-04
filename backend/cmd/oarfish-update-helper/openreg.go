package main

import (
	"errors"
	"os"
	"syscall"
)

// errNotRegular is returned for a name that is not a regular file: a link, a
// directory, a FIFO, a socket or a device.
var errNotRegular = errors.New("not a regular file")

// hookAfterLstat is a test seam between the Lstat and the open of openRegular,
// so that a test can swap the name at exactly that instant. It is nil in
// production and set by tests only.
var hookAfterLstat func(root *os.Root, name string)

// openRegular opens a regular file below root for reading.
//
// os.Root.OpenFile does not honour O_NOFOLLOW for the last path element: it
// resolves the element itself and follows a link that stays inside the root
// (a link that leads out is refused). This was observed with the Go version of
// go.mod and is a difference to the assumption of ADR 0010 U3 (V5). The name
// is therefore checked with Lstat first, which does not follow the last
// element, then opened with O_NONBLOCK (a FIFO can not block the open) and
// O_NOFOLLOW (kept for the platforms and versions that honour it), and the
// opened descriptor must be the same regular file as the one Lstat saw. A name
// that is swapped for a link between the two calls is refused by the
// SameFile check; one swapped after it can only lead to another file inside
// the root, and its bytes are hashed against the signed values anyway.
func openRegular(root *os.Root, name string) (*os.File, os.FileInfo, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, nil, errNotRegular
	}
	if hookAfterLstat != nil {
		hookAfterLstat(root, name)
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	after, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		f.Close()
		return nil, nil, errNotRegular
	}
	return f, after, nil
}
