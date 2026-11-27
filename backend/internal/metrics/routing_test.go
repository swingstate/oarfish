package metrics

import (
	"strings"
	"testing"
	"testing/fstest"
)

// Task N0 (ORB-NET-001): the default routes and the name servers in the snapshot.

func routingOf(t *testing.T, fsys fstest.MapFS) Routing {
	t.Helper()
	return NewCollector(fsys, fakeStatfs(nil)).Collect(t0).Routing
}

func str(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func TestRoutingPiZero2W(t *testing.T) {
	r := NewCollector(fixture(t, "pi-zero-2w"), fakeStatfs(nil)).Collect(t0).Routing
	if r.IPv4.Status != RoutingFound || str(r.IPv4.Interface) != "wlan0" || str(r.IPv4.Gateway) != "192.0.2.1" || r.IPv4.GatewayHidden {
		t.Errorf("ipv4 = %s", mustJSON(t, r.IPv4))
	}
	if r.IPv6.Status != RoutingFound || str(r.IPv6.Interface) != "wlan0" || str(r.IPv6.Gateway) != "2001:db8::1" {
		t.Errorf("ipv6 = %s", mustJSON(t, r.IPv6))
	}
	if got := mustJSON(t, r.DNS); got != `{"status":"found","servers":[{"address":"192.0.2.53","local":false},{"address":"2001:db8::53","local":false}],"hidden":0,"via_local_resolver":false}` {
		t.Errorf("dns = %s", got)
	}
}

func TestRoutingGenericARM64(t *testing.T) {
	r := NewCollector(fixture(t, "generic-arm64"), fakeStatfs(nil)).Collect(t0).Routing
	// Two default rows: eth0 with metric 100 wins over wlan0 with 600.
	if r.IPv4.Status != RoutingFound || str(r.IPv4.Interface) != "eth0" || str(r.IPv4.Gateway) != "198.51.100.1" {
		t.Errorf("ipv4 = %s", mustJSON(t, r.IPv4))
	}
	// Only a connected network and the kernel's unreachable route: no IPv6 default route.
	if r.IPv6.Status != RoutingNone || r.IPv6.Interface != nil || r.IPv6.Gateway != nil {
		t.Errorf("ipv6 = %s", mustJSON(t, r.IPv6))
	}
	// Comments, domain, search, options, sortlist and an unparsable line give nothing.
	got := mustJSON(t, r.DNS)
	if got != `{"status":"found","servers":[{"address":"198.51.100.53","local":false},{"address":"2001:db8:1::53","local":false}],"hidden":0,"via_local_resolver":false}` {
		t.Errorf("dns = %s", got)
	}
	for _, leak := range []string{"home.example", "lab.example", "edns0", "router", "managed", "198.51.100.99"} {
		if strings.Contains(mustJSON(t, r), leak) {
			t.Errorf("the routing part echoes %q from the file", leak)
		}
	}
}

// A missing source empties its own part and says so; the other parts and the
// rest of the snapshot are unaffected.
func TestRoutingFailsSoftPerSource(t *testing.T) {
	s := NewCollector(fixture(t, "minimal"), fakeStatfs(nil)).Collect(t0)
	r := s.Routing
	for name, d := range map[string]DefaultRoute{"ipv4": r.IPv4, "ipv6": r.IPv6} {
		if d.Status != RoutingUnreadable || d.Interface != nil || d.Gateway != nil {
			t.Errorf("%s = %s", name, mustJSON(t, d))
		}
	}
	if r.DNS.Status != RoutingUnreadable || r.DNS.Servers == nil || len(r.DNS.Servers) != 0 {
		t.Errorf("dns = %s", mustJSON(t, r.DNS))
	}
	if s.Memory.TotalBytes == nil {
		t.Error("the rest of the snapshot must still be read")
	}

	// Only resolv.conf missing: the routes stay.
	fsys := cloneFixture(t, "pi-zero-2w")
	delete(fsys, "etc/resolv.conf")
	r = routingOf(t, fsys)
	if r.DNS.Status != RoutingUnreadable || r.IPv4.Status != RoutingFound || r.IPv6.Status != RoutingFound {
		t.Errorf("routing = %s", mustJSON(t, r))
	}
	// Only ipv6_route missing.
	fsys = cloneFixture(t, "pi-zero-2w")
	delete(fsys, "proc/net/ipv6_route")
	r = routingOf(t, fsys)
	if r.IPv6.Status != RoutingUnreadable || r.IPv4.Status != RoutingFound || r.DNS.Status != RoutingFound {
		t.Errorf("routing = %s", mustJSON(t, r))
	}
}

func TestRoutingHostileFixture(t *testing.T) {
	r := NewCollector(fixture(t, "hostile"), fakeStatfs(nil)).Collect(t0).Routing
	if r.IPv4.Status != RoutingNone || r.IPv6.Status != RoutingNone {
		t.Errorf("routes = %s / %s", mustJSON(t, r.IPv4), mustJSON(t, r.IPv6))
	}
	if got := mustJSON(t, r.DNS); got != `{"status":"found","servers":[{"address":"203.0.113.5","local":false}],"hidden":0,"via_local_resolver":false}` {
		t.Errorf("dns = %s", got)
	}
	assertClean(t, r)
}

// Every file has a read limit; above it the part is unreadable, not cut.
func TestRoutingOversizedFiles(t *testing.T) {
	fsys := cloneFixture(t, "pi-zero-2w")
	fsys["etc/resolv.conf"] = &fstest.MapFile{Data: []byte("nameserver 192.0.2.53\n" + strings.Repeat("#", limitResolv))}
	fsys["proc/net/ipv6_route"] = &fstest.MapFile{Data: append([]byte(string(fsys["proc/net/ipv6_route"].Data)), make([]byte, limitNetTable)...)}
	r := routingOf(t, fsys)
	if r.DNS.Status != RoutingUnreadable || len(r.DNS.Servers) != 0 {
		t.Errorf("dns = %s", mustJSON(t, r.DNS))
	}
	if r.IPv6.Status != RoutingUnreadable {
		t.Errorf("ipv6 = %s", mustJSON(t, r.IPv6))
	}
	// Exactly at the limit is still read.
	fsys["etc/resolv.conf"] = &fstest.MapFile{Data: []byte("nameserver 192.0.2.53\n" + strings.Repeat("#", limitResolv-len("nameserver 192.0.2.53\n")))}
	if r := routingOf(t, fsys); r.DNS.Status != RoutingFound {
		t.Errorf("dns at the limit = %s", mustJSON(t, r.DNS))
	}
}

// A local resolver stub is shown as it is and marked local when the servers
// it forwards to cannot be read (this fixture has no
// /run/systemd/resolve/resolv.conf; the fallback of task N1 is tested in
// network_test.go).
func TestRoutingLocalStub(t *testing.T) {
	fsys := cloneFixture(t, "pi-zero-2w")
	fsys["etc/resolv.conf"] = &fstest.MapFile{Data: []byte("# stub\nnameserver 127.0.0.53\noptions edns0 trust-ad\nsearch .\n")}
	if got := mustJSON(t, routingOf(t, fsys).DNS); got != `{"status":"found","servers":[{"address":"127.0.0.53","local":true}],"hidden":0,"via_local_resolver":false}` {
		t.Errorf("dns = %s", got)
	}
	fsys["etc/resolv.conf"] = &fstest.MapFile{Data: []byte("nameserver ::1\nnameserver 192.0.2.53\n")}
	if got := mustJSON(t, routingOf(t, fsys).DNS); got != `{"status":"found","servers":[{"address":"::1","local":true},{"address":"192.0.2.53","local":false}],"hidden":0,"via_local_resolver":false}` {
		t.Errorf("dns = %s", got)
	}
	fsys["etc/resolv.conf"] = &fstest.MapFile{Data: []byte("# nothing configured\nsearch home.example\n")}
	if got := mustJSON(t, routingOf(t, fsys).DNS); got != `{"status":"none","servers":[],"hidden":0,"via_local_resolver":false}` {
		t.Errorf("dns = %s", got)
	}
}

// A link-local next hop is shown with its interface. A next hop or a name
// server whose address is built from a hardware address (EUI-64) contains the
// router's MAC address: its interface is shown, the address never.
func TestRoutingLinkLocalAndDerivedAddresses(t *testing.T) {
	z := "00000000000000000000000000000000"
	fsys := cloneFixture(t, "pi-zero-2w")
	fsys["proc/net/ipv6_route"] = &fstest.MapFile{Data: []byte(z + " 00 " + z + " 00 fe80000000000000000000000000abcd 00000400 00000001 00000000 00450003 wlan0\n")}
	r := routingOf(t, fsys)
	if r.IPv6.Status != RoutingFound || str(r.IPv6.Gateway) != "fe80::abcd%wlan0" || str(r.IPv6.Interface) != "wlan0" || r.IPv6.GatewayHidden {
		t.Errorf("ipv6 = %s", mustJSON(t, r.IPv6))
	}

	fsys["proc/net/ipv6_route"] = &fstest.MapFile{Data: []byte(z + " 00 " + z + " 00 fe80000000000000021122fffe334455 00000400 00000001 00000000 00450003 wlan0\n")}
	fsys["etc/resolv.conf"] = &fstest.MapFile{Data: []byte("nameserver fe80::211:22ff:fe33:4455%wlan0\nnameserver 2001:db8::211:22ff:fe33:4455\nnameserver fe80::53%wlan0\n")}
	r = routingOf(t, fsys)
	if r.IPv6.Status != RoutingFound || r.IPv6.Gateway != nil || !r.IPv6.GatewayHidden || str(r.IPv6.Interface) != "wlan0" {
		t.Errorf("ipv6 = %s", mustJSON(t, r.IPv6))
	}
	if got := mustJSON(t, r.DNS); got != `{"status":"found","servers":[{"address":"fe80::53%wlan0","local":false}],"hidden":2,"via_local_resolver":false}` {
		t.Errorf("dns = %s", got)
	}
	if out := mustJSON(t, r); strings.Contains(out, "fe33") || strings.Contains(out, "4455") {
		t.Errorf("a hardware-derived address reached the output: %s", out)
	}
}

// The routing part is read with the slow tier and a snapshot shares no memory
// with the cache.
func TestRoutingIsSlowTierAndCopied(t *testing.T) {
	fsys := cloneFixture(t, "pi-zero-2w")
	c := NewCollector(fsys, fakeStatfs(nil))
	s1 := c.Collect(t0)
	*s1.Routing.IPv4.Interface = "changed"
	s1.Routing.DNS.Servers[0].Address = "changed"
	fsys["etc/resolv.conf"] = &fstest.MapFile{Data: []byte("nameserver 198.51.100.1\n")}
	s2 := c.Collect(t0.Add(2e9))
	if str(s2.Routing.IPv4.Interface) != "wlan0" || s2.Routing.DNS.Servers[0].Address != "192.0.2.53" {
		t.Errorf("the cache was changed through a snapshot, or read before the slow tier was due: %s", mustJSON(t, s2.Routing))
	}
	s3 := c.Collect(t0.Add(SlowInterval))
	if s3.Routing.DNS.Servers[0].Address != "198.51.100.1" {
		t.Errorf("the slow tier did not read resolv.conf again: %s", mustJSON(t, s3.Routing))
	}
}
