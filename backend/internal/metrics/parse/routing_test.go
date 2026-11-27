package parse

import (
	"net/netip"
	"strings"
	"testing"
)

// Task N0 (ORB-NET-001): the default route of each family and the name
// servers. The inputs are written from the kernel and resolver formats; the
// addresses are documentation addresses, except where a test is about a
// loopback stub or a link-local next hop, which have no documentation range.

const route4Header = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n"

func TestParseDefaultRoute4(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		ok         bool
		iface, gw  string
		metric     uint32
	}{
		{"one default row", route4Header +
			"wlan0\t00000000\t010200C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n" +
			"wlan0\t000200C0\t00000000\t0001\t0\t0\t600\t00FFFFFF\t0\t0\t0\n", true, "wlan0", "192.0.2.1", 600},
		{"no default route", route4Header +
			"wlan0\t000200C0\t00000000\t0001\t0\t0\t600\t00FFFFFF\t0\t0\t0\n", false, "", "", 0},
		{"several default rows: the lowest metric wins", route4Header +
			"wlan0\t00000000\t010200C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n" +
			"eth0\t00000000\t016433C6\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
			"usb0\t00000000\t017100CB\t0003\t0\t0\t100\t00000000\t0\t0\t0\n", true, "eth0", "198.51.100.1", 100},
		{"equal metrics: the first row of the file wins", route4Header +
			"wlan0\t00000000\t010200C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
			"eth0\t00000000\t016433C6\t0003\t0\t0\t100\t00000000\t0\t0\t0\n", true, "wlan0", "192.0.2.1", 100},
		{"a default route without a gateway (point-to-point)", route4Header +
			"ppp0\t00000000\t00000000\t0001\t0\t0\t0\t00000000\t0\t0\t0\n", true, "ppp0", "", 0},
		{"a down row and a reject row are not the default route", route4Header +
			"wlan0\t00000000\t010200C0\t0002\t0\t0\t1\t00000000\t0\t0\t0\n" +
			"eth0\t00000000\t016433C6\t0203\t0\t0\t1\t00000000\t0\t0\t0\n" +
			"eth1\t00000000\t017100CB\t0003\t0\t0\t900\t00000000\t0\t0\t0\n", true, "eth1", "203.0.113.1", 900},
		{"a gateway flag without a gateway, and a gateway without the flag", route4Header +
			"wlan0\t00000000\t00000000\t0003\t0\t0\t1\t00000000\t0\t0\t0\n" +
			"eth0\t00000000\t016433C6\t0001\t0\t0\t1\t00000000\t0\t0\t0\n", false, "", "", 0},
		{"a destination 0.0.0.0 with a mask is not the default route", route4Header +
			"wlan0\t00000000\t010200C0\t0003\t0\t0\t1\t000000FF\t0\t0\t0\n", false, "", "", 0},
		{"malformed lines are skipped", route4Header +
			"wlan0\tZZZZZZZZ\t010200C0\t0003\t0\t0\t1\t00000000\n" +
			"wlan0\t00000000\t010200C0\tXYZ\t0\t0\t1\t00000000\n" +
			"wlan0\t00000000\t010200C0\t0003\t0\t0\t-1\t00000000\n" +
			"wlan0\t00000000\t0102C0\t0003\t0\t0\t1\t00000000\n" +
			"bad/name\t00000000\t010200C0\t0003\t0\t0\t1\t00000000\n" +
			"averyveryverylongname0\t00000000\t010200C0\t0003\t0\t0\t1\t00000000\n" +
			"lo\t00000000\t010200C0\t0003\t0\t0\t1\t00000000\n" +
			"wlan0 00000000\n\n\x00\xff\n", false, "", "", 0},
		{"no header line", "eth0\t00000000\t016433C6\t0003\t0\t0\t100\t00000000\t0\t0\t0\n", true, "eth0", "198.51.100.1", 100},
		{"empty", "", false, "", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, ok := ParseDefaultRoute4([]byte(tc.data))
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (%+v)", ok, tc.ok, r)
			}
			if !ok {
				return
			}
			gw := ""
			if r.Gateway.IsValid() {
				gw = r.Gateway.String()
			}
			if r.Iface != tc.iface || gw != tc.gw || r.Metric != tc.metric {
				t.Errorf("got %s via %q metric %d, want %s via %q metric %d", r.Iface, gw, r.Metric, tc.iface, tc.gw, tc.metric)
			}
		})
	}
}

const zero6 = "00000000000000000000000000000000"

func route6(dest, destLen, src, srcLen, hop, metric, flags, dev string) string {
	return strings.Join([]string{dest, destLen, src, srcLen, hop, metric, "00000001", "00000000", flags, dev}, " ") + "\n"
}

func TestParseDefaultRoute6(t *testing.T) {
	connected := route6("20010db8000000000000000000000000", "40", zero6, "00", zero6, "00000100", "00000001", "wlan0")
	unreachable := route6(zero6, "00", zero6, "00", zero6, "ffffffff", "00200200", "lo")
	for _, tc := range []struct {
		name, data string
		ok         bool
		iface, gw  string
	}{
		{"a default route learnt from a router advertisement", connected +
			route6(zero6, "00", zero6, "00", "20010db8000000000000000000000001", "00000400", "00450003", "wlan0") + unreachable,
			true, "wlan0", "2001:db8::1"},
		{"a link-local next hop", connected +
			route6(zero6, "00", zero6, "00", "fe80000000000000000000000000abcd", "00000400", "00450003", "wlan0"),
			true, "wlan0", "fe80::abcd"},
		{"only the unreachable route of the kernel: no default route", connected + unreachable, false, "", ""},
		{"several default rows: the lowest metric wins", connected +
			route6(zero6, "00", zero6, "00", "20010db8000000000000000000000001", "00000400", "00450003", "wlan0") +
			route6(zero6, "00", zero6, "00", "20010db8000000020000000000000001", "00000064", "00000003", "eth0"),
			true, "eth0", "2001:db8:0:2::1"},
		{"a source-specific default route is not the default route",
			route6(zero6, "00", "20010db8000000000000000000000000", "40", "20010db8000000000000000000000001", "00000001", "00000003", "wlan0"),
			false, "", ""},
		{"a down row is not the default route",
			route6(zero6, "00", zero6, "00", "20010db8000000000000000000000001", "00000001", "00000002", "wlan0"), false, "", ""},
		{"a default route without a next hop (a tunnel)",
			route6(zero6, "00", zero6, "00", zero6, "00000400", "00000001", "wg0"), true, "wg0", ""},
		{"malformed lines are skipped",
			route6(zero6, "00", zero6, "00", "zz0db8000000000000000000000001", "00000400", "00450003", "wlan0") +
				route6(zero6, "00", zero6, "00", "20010db8000000000000000000000001", "zz", "00000003", "wlan0") +
				route6(zero6, "00", zero6, "00", "20010db8000000000000000000000001", "00000400", "00000003", "bad/name") +
				route6("10000000000000000000000000000000", "00", zero6, "00", "20010db8000000000000000000000001", "00000400", "00000003", "wlan0") +
				zero6 + " 00 " + zero6 + "\n\x00\n", false, "", ""},
		{"empty", "", false, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, ok := ParseDefaultRoute6([]byte(tc.data))
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (%+v)", ok, tc.ok, r)
			}
			if !ok {
				return
			}
			gw := ""
			if r.Gateway.IsValid() {
				gw = r.Gateway.String()
			}
			if r.Iface != tc.iface || gw != tc.gw {
				t.Errorf("got %s via %q, want %s via %q", r.Iface, gw, tc.iface, tc.gw)
			}
		})
	}
}

func addrs(list []netip.Addr) string {
	s := make([]string, len(list))
	for i, a := range list {
		s[i] = a.String()
	}
	return strings.Join(s, ",")
}

func TestParseResolvConf(t *testing.T) {
	for _, tc := range []struct{ name, data, want string }{
		{"two servers, IPv4 and IPv6", "# Generated by NetworkManager\nnameserver 192.0.2.53\nnameserver 2001:db8::53\n", "192.0.2.53,2001:db8::53"},
		{"comments, search, domain, options and sortlist are ignored",
			"# nameserver 198.51.100.1\n; nameserver 198.51.100.2\n  # indented comment\ndomain home.example\nsearch home.example lab.example\n" +
				"options edns0 trust-ad\nsortlist 198.51.100.0\nnameserver 198.51.100.53 # the router\n", "198.51.100.53"},
		{"a local stub only", "nameserver 127.0.0.53\noptions edns0 trust-ad\nsearch .\n", "127.0.0.53"},
		{"an IPv6 loopback resolver", "nameserver ::1\n", "::1"},
		{"a link-local server with its zone", "nameserver fe80::53%wlan0\n", "fe80::53%wlan0"},
		{"an IPv4-mapped address is shown as IPv4", "nameserver ::ffff:192.0.2.53\n", "192.0.2.53"},
		{"more than the bound: only the first three, which the resolver uses",
			"nameserver 192.0.2.1\nnameserver 192.0.2.2\nnameserver 192.0.2.3\nnameserver 192.0.2.4\nnameserver 192.0.2.5\n", "192.0.2.1,192.0.2.2,192.0.2.3"},
		{"unusable lines do not count against the bound",
			"nameserver\nnameserver not-an-address\nnameserver 0.0.0.0\nnameserver ::\nnameserver 224.0.0.1\nnameserver 2001:db8::1%bad/zone\n" +
				"nameserver 999.1.1.1\nnameservers 192.0.2.9\nNAMESERVER 192.0.2.8\n\x00nameserver 192.0.2.7\nnameserver \x1b[31m192.0.2.6\n" +
				"nameserver 192.0.2.1\nnameserver 192.0.2.2\nnameserver 192.0.2.3\n", "192.0.2.1,192.0.2.2,192.0.2.3"},
		{"tabs and CRLF", "nameserver\t192.0.2.53\r\n", "192.0.2.53"},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := addrs(ParseResolvConf([]byte(tc.data))); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Nothing but the parsed addresses comes out: no comment, search domain or
// option, whatever the file holds.
func TestParseResolvConfNeverEchoesOtherText(t *testing.T) {
	data := "# secret-comment\nsearch secret-domain.example\noptions secret-option\nnameserver 192.0.2.53 secret-trailer\n"
	got := addrs(ParseResolvConf([]byte(data)))
	if strings.Contains(got, "secret") || got != "192.0.2.53" {
		t.Errorf("got %q", got)
	}
}

func TestParseResolvConfLineBound(t *testing.T) {
	// The servers after the line bound are never seen.
	data := strings.Repeat("# filler\n", maxLines) + "nameserver 192.0.2.53\n"
	if got := ParseResolvConf([]byte(data)); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}
