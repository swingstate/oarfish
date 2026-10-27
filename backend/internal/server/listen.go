package server

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
)

// ValidateListen enforces decision D-C29: until Gate G5.1 (authentication) is
// PASS, the listen address must be a literal loopback IP address with a port,
// that is 127.0.0.0/8 or [::1]. It returns the address in canonical form.
//
// An empty host (":8484"), an unspecified address (0.0.0.0, [::]), any other
// IP address and every host name, "localhost" included, are refused. There is
// no override flag and no environment variable. Host names are refused
// because resolving one is a decision of the resolver, not of the
// administrator who typed it. A zone ("[::1%lo]") and an IPv4-mapped IPv6
// address ("[::ffff:127.0.0.1]") are refused as well: they are not the
// literal forms this guard promises.
func ValidateListen(addr string) (string, error) {
	const maxLen = 64
	if len(addr) > maxLen {
		return "", errors.New("the listen address is too long; use a loopback address such as 127.0.0.1:8484")
	}
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("the listen address %q is not of the form IP:port; use a loopback address such as 127.0.0.1:8484", addr)
	}
	if host == "" {
		return "", fmt.Errorf("the listen address %q has no host and would listen on every interface; use 127.0.0.1:%s", addr, portHint(portText))
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.Zone() != "" || ip.Is4In6() {
		return "", fmt.Errorf("the listen host %q is not a literal loopback IP address; host names such as \"localhost\" are not accepted, use 127.0.0.1", host)
	}
	if !ip.IsLoopback() {
		return "", fmt.Errorf("the listen address %q is not a loopback address; until authentication exists (Gate G5.1) Oarfish listens on 127.0.0.0/8 or [::1] only; use 127.0.0.1:%s", addr, portHint(portText))
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return "", fmt.Errorf("the listen port %q is not a number from 1 to 65535", portText)
	}
	return net.JoinHostPort(ip.String(), strconv.FormatUint(port, 10)), nil
}

// portHint returns a usable port for an error message.
func portHint(p string) string {
	if n, err := strconv.ParseUint(p, 10, 16); err == nil && n > 0 {
		return p
	}
	return "8484"
}
