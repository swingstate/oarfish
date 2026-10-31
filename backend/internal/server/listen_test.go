package server

import (
	"strings"
	"testing"
)

// D-C29, D-C48: the binary refuses a listen address that is not a literal
// loopback IP address with a port, also after Gate G5.1, until LAN access and
// HTTPS are designed (D-D10).
func TestValidateListen(t *testing.T) {
	accepted := []struct{ in, want string }{
		{"127.0.0.1:8484", "127.0.0.1:8484"},
		{"127.0.0.1:1", "127.0.0.1:1"},
		{"127.0.0.1:65535", "127.0.0.1:65535"},
		{"127.1.2.3:9000", "127.1.2.3:9000"},
		{"127.255.255.254:80", "127.255.255.254:80"},
		{"[::1]:8484", "[::1]:8484"},
		{"[0:0:0:0:0:0:0:1]:8484", "[::1]:8484"},
	}
	for _, tc := range accepted {
		got, err := ValidateListen(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ValidateListen(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}

	refused := []struct {
		name, in string
		mention  string // text the message must contain
	}{
		{"empty host", ":8484", "127.0.0.1:8484"},
		{"empty host and port", ":", "127.0.0.1"},
		{"IPv4 unspecified", "0.0.0.0:8484", "loopback"},
		{"IPv6 unspecified", "[::]:8484", "loopback"},
		{"private IPv4", "192.168.1.5:8484", "loopback"},
		{"documentation IPv4", "192.0.2.1:8484", "loopback"},
		{"global IPv6", "[2001:db8::1]:8484", "loopback"},
		{"link-local IPv6", "[fe80::1]:8484", "loopback"},
		{"localhost", "localhost:8484", "127.0.0.1"},
		{"localhost upper case", "LOCALHOST:8484", "127.0.0.1"},
		{"host name", "oarfish.example:8484", "127.0.0.1"},
		{"host name with trailing dot", "localhost.:8484", "127.0.0.1"},
		{"IPv4 shorthand", "127.1:8484", "127.0.0.1"},
		{"IPv4 leading zero", "0127.0.0.1:8484", "127.0.0.1"},
		{"IPv4 decimal form", "2130706433:8484", "127.0.0.1"},
		{"IPv4 hex form", "0x7f000001:8484", "127.0.0.1"},
		{"IPv4-mapped IPv6 loopback", "[::ffff:127.0.0.1]:8484", "127.0.0.1"},
		{"IPv6 zone", "[::1%lo]:8484", "127.0.0.1"},
		{"IPv6 without brackets", "::1:8484", "IP:port"},
		{"no port", "127.0.0.1", "IP:port"},
		{"empty", "", "IP:port"},
		{"port zero", "127.0.0.1:0", "port"},
		{"port too large", "127.0.0.1:65536", "port"},
		{"port name", "127.0.0.1:http", "port"},
		{"empty port", "127.0.0.1:", "port"},
		{"negative port", "127.0.0.1:-1", "port"},
		{"whitespace", " 127.0.0.1:8484", "127.0.0.1"},
		{"trailing whitespace", "127.0.0.1:8484 ", "port"},
		{"URL", "http://127.0.0.1:8484", "IP:port"},
		{"unix socket", "unix:/run/oarfish.sock", "127.0.0.1"},
		{"two hosts", "127.0.0.1,0.0.0.0:8484", "127.0.0.1"},
		{"very long", strings.Repeat("a", 500) + ":8484", "too long"},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateListen(tc.in)
			if err == nil {
				t.Fatalf("ValidateListen(%q) = %q, want an error", tc.in, got)
			}
			if got != "" {
				t.Errorf("a refused address must return no address, got %q", got)
			}
			if !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("error %q should mention %q", err, tc.mention)
			}
		})
	}
}

// The error for a refused address must not echo an unbounded input.
func TestValidateListenErrorIsBounded(t *testing.T) {
	_, err := ValidateListen(strings.Repeat("x", 100000) + ":1")
	if err == nil || len(err.Error()) > 300 {
		t.Fatalf("error = %v", err)
	}
}

// D-C48: the refusal names the reason that still holds after Gate G5.1 (LAN
// access and HTTPS are not designed), not the old "until authentication
// exists" reason.
func TestValidateListenMessageNamesTheOpenDesign(t *testing.T) {
	_, err := ValidateListen("192.0.2.1:8484")
	if err == nil {
		t.Fatal("a non-loopback address was accepted")
	}
	msg := err.Error()
	for _, want := range []string{"LAN access and HTTPS", "D-D10", "127.0.0.1:8484"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not mention %q", msg, want)
		}
	}
	for _, stale := range []string{"G5.1", "until authentication exists"} {
		if strings.Contains(msg, stale) {
			t.Errorf("message %q still says %q", msg, stale)
		}
	}
}
