package parse

import (
	"encoding/binary"
	"encoding/hex"
	"net/netip"
	"strconv"
	"strings"
)

const (
	maxAddrs  = 64
	maxRoutes = 512
)

// ParseFibTrieLocal returns the IPv4 addresses that /proc/net/fib_trie marks as
// LOCAL: an address line ("|-- 192.0.2.5") followed by "/32 host LOCAL". The
// trie does not say which interface an address belongs to; use ParseRoutes for
// that. Loopback, unspecified and multicast addresses are left out. The result
// has no duplicates and at most 64 entries.
func ParseFibTrieLocal(data []byte) []netip.Addr {
	var out []netip.Addr
	seen := map[netip.Addr]bool{}
	var last netip.Addr
	for _, line := range lines(data) {
		t := strings.TrimSpace(string(line))
		if rest, ok := strings.CutPrefix(t, "|--"); ok {
			a, err := netip.ParseAddr(strings.TrimSpace(rest))
			if err != nil || !a.Is4() {
				a = netip.Addr{}
			}
			last = a
			continue
		}
		if t == "/32 host LOCAL" && last.IsValid() {
			if usableV4(last) && !seen[last] && len(out) < maxAddrs {
				seen[last] = true
				out = append(out, last)
			}
			last = netip.Addr{}
		}
	}
	return out
}

func usableV4(a netip.Addr) bool {
	return a.Is4() && !a.IsLoopback() && !a.IsUnspecified() && !a.IsMulticast()
}

// Route is a connected IPv4 network of one interface, from /proc/net/route.
type Route struct {
	Iface  string
	Prefix netip.Prefix
}

// ParseRoutes reads /proc/net/route and returns the directly connected
// networks: routes with a destination, without a gateway, that are up. They
// let a caller assign an address from the fib_trie to an interface. The file
// prints addresses as the host's native 32-bit integers, here decoded as
// little-endian, which holds for ARM64 and x86-64 (the supported targets).
func ParseRoutes(data []byte) []Route {
	var out []Route
	for i, line := range lines(data) {
		if i == 0 && strings.HasPrefix(string(line), "Iface") {
			continue
		}
		f := fields(line)
		// Iface Destination Gateway Flags RefCnt Use Metric Mask ...
		if len(f) < 8 || len(f[0]) == 0 || len(f[0]) > 15 || len(out) >= maxRoutes {
			continue
		}
		dest, ok1 := hexIPv4(f[1])
		mask, ok2 := hexIPv4(f[7])
		gw, ok3 := hexIPv4(f[2])
		flags, err := strconv.ParseUint(f[3], 16, 32)
		if !ok1 || !ok2 || !ok3 || err != nil {
			continue
		}
		const flagUp, flagGateway = 0x1, 0x2
		if flags&flagUp == 0 || flags&flagGateway != 0 || !gw.IsUnspecified() || dest.IsUnspecified() {
			continue
		}
		bits, ok := maskBits(mask)
		if !ok || bits == 0 {
			continue
		}
		p, err := dest.Prefix(bits)
		if err != nil {
			continue
		}
		out = append(out, Route{Iface: f[0], Prefix: p})
	}
	return out
}

func hexIPv4(s string) (netip.Addr, bool) {
	if len(s) != 8 {
		return netip.Addr{}, false
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return netip.Addr{}, false
	}
	v := binary.LittleEndian.Uint32(b)
	var a4 [4]byte
	binary.BigEndian.PutUint32(a4[:], v)
	return netip.AddrFrom4(a4), true
}

// maskBits returns the prefix length of a contiguous netmask.
func maskBits(m netip.Addr) (int, bool) {
	a := m.As4()
	v := binary.BigEndian.Uint32(a[:])
	ones := 0
	for v&0x80000000 != 0 {
		ones++
		v <<= 1
	}
	return ones, v == 0
}

// Inet6 is one line of /proc/net/if_inet6.
type Inet6 struct {
	Iface  string
	Addr   netip.Addr
	Prefix int
	Scope  int // 0 global, 0x20 link-local, 0x10 host
	Flags  int
}

// Flags and scope values from the kernel (include/uapi/linux/if_addr.h and ipv6.h).
const (
	Inet6ScopeGlobal   = 0x00
	Inet6FlagDADFail   = 0x08
	Inet6FlagTentative = 0x40
)

// ParseInet6 reads /proc/net/if_inet6:
// "<32 hex digits> <ifindex> <prefix> <scope> <flags> <ifname>".
func ParseInet6(data []byte) []Inet6 {
	var out []Inet6
	for _, line := range lines(data) {
		f := fields(line)
		if len(f) != 6 || len(f[0]) != 32 || len(f[5]) == 0 || len(f[5]) > 15 || len(out) >= maxAddrs {
			continue
		}
		raw, err := hex.DecodeString(f[0])
		if err != nil {
			continue
		}
		prefix, err1 := strconv.ParseUint(f[2], 16, 8)
		scope, err2 := strconv.ParseUint(f[3], 16, 8)
		flags, err3 := strconv.ParseUint(f[4], 16, 16)
		if err1 != nil || err2 != nil || err3 != nil || prefix > 128 {
			continue
		}
		out = append(out, Inet6{
			Iface: f[5], Addr: netip.AddrFrom16([16]byte(raw)),
			Prefix: int(prefix), Scope: int(scope), Flags: int(flags),
		})
	}
	return out
}

// DerivedFromHardwareAddress reports whether the interface identifier of an
// IPv6 address has the EUI-64 marker (ff:fe in the middle). Such an address
// contains the MAC address of the interface, which Oarfish never shows.
func DerivedFromHardwareAddress(a netip.Addr) bool {
	b := a.As16()
	return b[11] == 0xff && b[12] == 0xfe
}
