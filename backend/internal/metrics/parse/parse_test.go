package parse

import (
	"bytes"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// The inputs below are synthetic. Addresses come from the documentation
// ranges (192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24, 2001:db8::/32).

func TestParseStat(t *testing.T) {
	const good = "cpu  100 20 30 400 50 6 7 8 9 10\ncpu0 25 5 7 100 12 1 2 2 0 0\ncpu1 25 5 7 100 12 1 2 2 0 0\ncpu2 25 5 8 100 13 2 1 2 0 0\ncpu3 25 5 8 100 13 2 2 2 0 0\nintr 1 2 3\nctxt 5\n"
	tests := []struct {
		name string
		in   string
		want Stat
		ok   bool
	}{
		{"four cores", good, Stat{Total: 100 + 20 + 30 + 400 + 50 + 6 + 7 + 8, Idle: 450, Cores: 4}, true},
		{"old kernel with four columns", "cpu 1 2 3 4\ncpu0 1 2 3 4\n", Stat{Total: 10, Idle: 4, Cores: 1}, true},
		{"no per-core lines", "cpu 1 2 3 4 5\n", Stat{Total: 15, Idle: 9}, true},
		{"missing", "intr 1 2\n", Stat{}, false},
		{"empty", "", Stat{}, false},
		{"too few columns", "cpu 1 2 3\n", Stat{}, false},
		{"not a number", "cpu 1 x 3 4 5\n", Stat{}, false},
		{"negative", "cpu 1 -2 3 4 5\n", Stat{}, false},
		{"absurd value", "cpu 1 2 3 99999999999999999999 5\n", Stat{}, false},
		{"value above the counter limit", "cpu 1 2 3 72057594037927937 5\n", Stat{}, false},
		{"cpu label only counts with digits", "cpu 1 2 3 4\ncpufreq 1\ncpu 1 2 3 4\ncpu0 1\ncpuX 1\ncpu12345 1\n", Stat{Total: 10, Idle: 4, Cores: 1}, true},
		{"second aggregate line is ignored", "cpu 1 1 1 1\ncpu 9 9 9 9\n", Stat{Total: 4, Idle: 1}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseStat([]byte(tc.in))
			if ok != tc.ok || (ok && got != tc.want) {
				t.Errorf("got %+v, %v; want %+v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestParseStatCapsTheCoreCount(t *testing.T) {
	var b strings.Builder
	b.WriteString("cpu 1 1 1 1\n")
	for i := 0; i < 5000; i++ {
		b.WriteString("cpu1 1 1 1 1\n")
	}
	st, ok := ParseStat([]byte(b.String()))
	if !ok || st.Cores != maxCores {
		t.Fatalf("cores = %d, ok = %v; want %d", st.Cores, ok, maxCores)
	}
}

func TestParseLoadavgAndUptime(t *testing.T) {
	if l, ok := ParseLoadavg([]byte("0.52 0.40 0.31 1/234 5678\n")); !ok || l != (Load{0.52, 0.40, 0.31}) {
		t.Errorf("loadavg = %+v, %v", l, ok)
	}
	for _, bad := range []string{"", "0.5 0.4", "a b c", "NaN 1 1", "Inf 1 1", "-1 0 0", "1e9 0 0", "0.1 0.1 0.1e400"} {
		if _, ok := ParseLoadavg([]byte(bad)); ok {
			t.Errorf("loadavg %q accepted", bad)
		}
	}
	if u, ok := ParseUptime([]byte("12345.67 23456.78\n")); !ok || u != 12345.67 {
		t.Errorf("uptime = %v, %v", u, ok)
	}
	for _, bad := range []string{"", "x", "-5 1", "NaN 1", "1e12 1", "+Inf 1"} {
		if _, ok := ParseUptime([]byte(bad)); ok {
			t.Errorf("uptime %q accepted", bad)
		}
	}
}

func TestParseMeminfo(t *testing.T) {
	const good = "MemTotal:         425372 kB\nMemFree:          100000 kB\nMemAvailable:     250000 kB\nBuffers:           10000 kB\nCached:            90000 kB\nSwapTotal:        102396 kB\nSwapFree:          92396 kB\nHugePages_Total:       0\n"
	m, ok := ParseMeminfo([]byte(good))
	if !ok || m.Total != 425372*1024 || m.Available != 250000*1024 || !m.HasAvailable || m.SwapTotal != 102396*1024 || m.SwapFree != 92396*1024 || !m.HasSwap {
		t.Fatalf("got %+v, %v", m, ok)
	}

	// No MemAvailable (old kernel) and no swap lines.
	m, ok = ParseMeminfo([]byte("MemTotal: 1000 kB\nMemFree: 500 kB\nBuffers: 100 kB\nCached: 200 kB\n"))
	if !ok || m.HasAvailable || m.HasSwap || m.Free != 500*1024 {
		t.Fatalf("old kernel: %+v, %v", m, ok)
	}

	// Available above total is clamped; SwapFree above SwapTotal is no swap data.
	m, _ = ParseMeminfo([]byte("MemTotal: 1000 kB\nMemAvailable: 5000 kB\nSwapTotal: 10 kB\nSwapFree: 20 kB\n"))
	if m.Available != 1000*1024 || m.HasSwap {
		t.Fatalf("clamp: %+v", m)
	}

	for name, bad := range map[string]string{
		"empty":           "",
		"no total":        "MemFree: 5 kB\n",
		"zero total":      "MemTotal: 0 kB\n",
		"garbage total":   "MemTotal: lots kB\n",
		"negative total":  "MemTotal: -5 kB\n",
		"absurd total":    "MemTotal: 99999999999999999999 kB\n",
		"huge total":      "MemTotal: 18446744073709551615 kB\n",
		"wrong unit":      "MemTotal: 5 MB\n",
		"trailing tokens": "MemTotal: 5 kB extra\n",
	} {
		if m, ok := ParseMeminfo([]byte(bad)); ok {
			t.Errorf("%s: accepted as %+v", name, m)
		}
	}
}

func TestParseMountinfo(t *testing.T) {
	const data = `22 1 179:2 / / rw,noatime shared:1 - ext4 /dev/root rw,errors=remount-ro
23 22 179:1 / /boot/firmware rw,relatime shared:2 - vfat /dev/mmcblk0p1 rw,fmask=0022
24 22 0:21 / /proc rw,nosuid shared:3 - proc proc rw
25 22 0:30 / /run/user/1000 rw - tmpfs tmpfs rw,size=40000k
26 22 8:17 / /mnt/with\040space rw - xfs /dev/sdb1 rw
27 22 8:18 / /mnt/tab\011x rw - ext4 /dev/sdb2 rw
28 22 8:19 / /mnt/nul\000x rw - ext4 /dev/sdb3 rw
29 22 8:20 / relative rw - ext4 /dev/sdb4 rw
30 22 8:21 / /a/../b rw - ext4 /dev/sdb5 rw
31 22 8:22 / /mnt/bad\9 rw - ext4 /dev/sdb6 rw
32 22 8:23 / /mnt/trunc\04 rw - ext4 /dev/sdb7 rw
33 22 8:24 / /ok rw - ex/t4 /dev/sdb8 rw
34 22 xx / /nodev rw - ext4 /dev/sdb9 rw
35 22 8:25 / /no-separator rw ext4 /dev/sdb9 rw
36 22 8:26 / /no\134slash rw - btrfs /dev/sdb10 rw
`
	got := ParseMountinfo([]byte(data), 100)
	var points []string
	for _, m := range got {
		points = append(points, m.MountPoint+" "+m.FSType)
	}
	// Control characters (tab, NUL) are dropped; "ex t4" is not a file system type.
	want := []string{"/ ext4", "/boot/firmware vfat", "/proc proc", "/run/user/1000 tmpfs", "/mnt/with space xfs", `/no\slash btrfs`}
	if !reflect.DeepEqual(points, want) {
		t.Errorf("mounts = %q\nwant      %q", points, want)
	}
	if got[0].Major != 179 || got[0].Minor != 2 || got[0].Root != "/" {
		t.Errorf("first mount = %+v", got[0])
	}

	if n := len(ParseMountinfo([]byte(data), 2)); n != 2 {
		t.Errorf("max not applied: %d", n)
	}
}

func TestParseMountinfoBoundsHostileInput(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 20000; i++ {
		b.WriteString("22 1 8:1 / /m ext4 x - ext4 /dev/x rw\n")
	}
	if n := len(ParseMountinfo([]byte(b.String()), 1<<20)); n > maxMountLines {
		t.Errorf("considered %d lines", n)
	}
	long := "22 1 8:1 / /" + strings.Repeat("a", 300) + " rw - ext4 /dev/x rw\n"
	if n := len(ParseMountinfo([]byte(long), 10)); n != 0 {
		t.Errorf("an over-long mount point was accepted")
	}
}

func TestParseFibTrieLocal(t *testing.T) {
	const data = `Main:
  +-- 0.0.0.0/0 3 0 5
     |-- 0.0.0.0
        /0 universe UNICAST
     +-- 127.0.0.0/8 2 0 2
        +-- 127.0.0.0/31 1 0 0
           |-- 127.0.0.0
              /32 link BROADCAST
              /8 host LOCAL
           |-- 127.0.0.1
              /32 host LOCAL
        |-- 127.255.255.255
           /32 link BROADCAST
     +-- 192.0.2.0/24 2 0 2
        |-- 192.0.2.0
           /24 link UNICAST
        |-- 192.0.2.15
           /32 host LOCAL
        |-- 192.0.2.255
           /32 link BROADCAST
     |-- 198.51.100.7
        /32 host LOCAL
Local:
  +-- 0.0.0.0/0 3 0 5
        |-- 192.0.2.15
           /32 host LOCAL
        |-- 224.0.0.1
           /32 host LOCAL
        |-- not-an-address
           /32 host LOCAL
        |-- 2001:db8::1
           /32 host LOCAL
`
	got := ParseFibTrieLocal([]byte(data))
	want := []netip.Addr{netip.MustParseAddr("192.0.2.15"), netip.MustParseAddr("198.51.100.7")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("addresses = %v, want %v", got, want)
	}
	if len(ParseFibTrieLocal(nil)) != 0 || len(ParseFibTrieLocal([]byte("/32 host LOCAL\n"))) != 0 {
		t.Error("input without an address line produced addresses")
	}
}

func TestParseFibTrieLocalIsBounded(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 3000; i++ {
		b.WriteString("  |-- 203.0.113." + itoa(i%250+1) + "\n   /32 host LOCAL\n")
		b.WriteString("  |-- 198.51." + itoa(i%250) + "." + itoa(i/250+1) + "\n   /32 host LOCAL\n")
	}
	if n := len(ParseFibTrieLocal([]byte(b.String()))); n > maxAddrs {
		t.Errorf("%d addresses", n)
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

func TestParseRoutes(t *testing.T) {
	// The file prints each address as a native little-endian integer:
	// 192.0.2.0 is 000200C0, 198.51.100.0 is 006433C6, the mask 255.255.255.0 is 00FFFFFF.
	const data = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
		"wlan0\t00000000\t010200C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n" +
		"wlan0\t000200C0\t00000000\t0001\t0\t0\t600\t00FFFFFF\t0\t0\t0\n" +
		"eth0\t006433C6\t00000000\t0001\t0\t0\t100\t00FFFFFF\t0\t0\t0\n" +
		"eth0\t006433C6\t00000000\t0000\t0\t0\t100\t00FFFFFF\t0\t0\t0\n" +
		"eth0\t006433C6\t00000000\t0001\t0\t0\t100\t00FF00FF\t0\t0\t0\n" +
		"eth0\tZZZZZZZZ\t00000000\t0001\t0\t0\t100\t00FFFFFF\t0\t0\t0\n" +
		"usb0\t006433C6\t010200C0\t0003\t0\t0\t100\t00FFFFFF\t0\t0\t0\n"
	got := ParseRoutes([]byte(data))
	want := []Route{
		{"wlan0", netip.MustParsePrefix("192.0.2.0/24")},
		{"eth0", netip.MustParsePrefix("198.51.100.0/24")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("routes = %v\nwant     %v", got, want)
	}
}

func TestParseInet6(t *testing.T) {
	const data = "20010db8000000000000000000000001 02 40 00 80 wlan0\n" +
		"fe80000000000000021122fffe334455 02 40 20 80 wlan0\n" +
		"00000000000000000000000000000001 01 80 10 80 lo\n" +
		"short 02 40 00 80 wlan0\n" +
		"20010db8000000000000000000000002 02 99 00 80 wlan0\n" +
		"20010db8000000000000000000000003 02 40 00 80\n" +
		"20010db8000000000000000000000004 02 40 00 zz wlan0\n" +
		"2001ZZb8000000000000000000000005 02 40 00 80 wlan0\n"
	got := ParseInet6([]byte(data))
	if len(got) != 3 {
		t.Fatalf("got %d entries: %+v", len(got), got)
	}
	if got[0].Iface != "wlan0" || got[0].Addr != netip.MustParseAddr("2001:db8::1") || got[0].Prefix != 64 || got[0].Scope != Inet6ScopeGlobal {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Scope != 0x20 {
		t.Errorf("link-local scope = %x", got[1].Scope)
	}
}

func TestDerivedFromHardwareAddress(t *testing.T) {
	if !DerivedFromHardwareAddress(netip.MustParseAddr("2001:db8::0211:22ff:fe33:4455")) {
		t.Error("an EUI-64 address was not recognised")
	}
	if DerivedFromHardwareAddress(netip.MustParseAddr("2001:db8::1")) || DerivedFromHardwareAddress(netip.MustParseAddr("2001:db8::a1b2:c3d4:e5f6:1234")) {
		t.Error("a non-EUI-64 address was flagged")
	}
}

// Every parser survives hostile input: NUL bytes, invalid UTF-8, huge lines.
func TestParsersSurviveHostileInput(t *testing.T) {
	inputs := [][]byte{
		nil,
		bytes.Repeat([]byte{0}, 4096),
		bytes.Repeat([]byte{0xff, 0xfe, 0x80}, 3000),
		bytes.Repeat([]byte("A"), 1<<20),
		bytes.Repeat([]byte("cpu 1 2 3 4\n"), 100000),
		bytes.Repeat([]byte("\n"), 1<<20),
		[]byte("cpu\x00 1 2 3 4\nMemTotal:\x00 5 kB\n|-- \x00\n/32 host LOCAL\n"),
	}
	for _, in := range inputs {
		ParseStat(in)
		ParseLoadavg(in)
		ParseUptime(in)
		ParseMeminfo(in)
		ParseMountinfo(in, 32)
		ParseFibTrieLocal(in)
		ParseRoutes(in)
		ParseInet6(in)
	}
}
