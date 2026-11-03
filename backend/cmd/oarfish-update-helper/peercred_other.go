//go:build !linux

package main

import (
	"errors"
	"net"
)

// The helper is a Linux program (U10). Other systems only compile it so that
// the repository's tests and vet run on a development machine; every
// connection fails the peer check there.
func peerUID(net.Conn) (uint32, error) {
	return 0, errors.New("peer credentials are only available on Linux")
}
