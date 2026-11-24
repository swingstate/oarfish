//go:build !linux

package main

import (
	"errors"
	"net"
)

// The broker is a Linux program. Other systems only compile it so that vet and
// the tests run on a development machine; every connection fails the peer
// check there.
func peerUID(net.Conn) (uint32, error) {
	return 0, errors.New("peer credentials are only available on Linux")
}
