package main

import (
	"os"
	"testing"
)

// The kernel's SO_PEERCRED value is the uid of this process.
func TestPeerUIDFromTheKernel(t *testing.T) {
	ts := startServer(t, nil)
	c := ts.dial(t)
	// The server's side of the connection is the one that is checked; here
	// the client reads the credentials of the listener's process, which is
	// the same process and so has the same uid.
	uid, err := peerUID(c)
	if err != nil {
		t.Fatal(err)
	}
	if uid != uint32(os.Getuid()) {
		t.Fatalf("peerUID = %d, want %d", uid, os.Getuid())
	}
}

// The whole path with the real peer check: the expected uid is injected, the
// uid of the peer is read by the kernel.
func TestRealPeerCheck(t *testing.T) {
	self := uint32(os.Getuid())
	for _, tt := range []struct {
		name   string
		expect uint32
		served bool
	}{{"own uid", self, true}, {"another uid", self + 1, false}} {
		t.Run(tt.name, func(t *testing.T) {
			ts := startServer(t, func(o *options) {
				o.peerUID = peerUID
				o.lookupUID = func(string) (uint32, error) { return tt.expect, nil }
			})
			raw := ts.exchange(t, []byte(okStatus), false)
			if tt.served {
				if r := parseReply(t, raw); !r.OK {
					t.Fatalf("not served: %q", raw)
				}
			} else if len(raw) != 0 {
				t.Fatalf("answered a peer with another uid: %q", raw)
			}
		})
	}
}
