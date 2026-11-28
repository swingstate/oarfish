package parse

import "testing"

// The fuzz targets check that no input makes a parser panic, hang or return a
// value outside its stated bounds. The seeds also run as ordinary tests.

func FuzzParseStat(f *testing.F) {
	f.Add([]byte("cpu  100 20 30 400 50 6 7 8 9 10\ncpu0 25 5 7 100 12 1 2 2 0 0\n"))
	f.Add([]byte("cpu 1 2 3 4\n"))
	f.Add([]byte("cpu 99999999999999999999 1 1 1\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		st, ok := ParseStat(data)
		if ok && (st.Idle > st.Total || st.Cores < 0 || st.Cores > maxCores) {
			t.Fatalf("out of bounds: %+v", st)
		}
	})
}

func FuzzParseMeminfo(f *testing.F) {
	f.Add([]byte("MemTotal: 425372 kB\nMemAvailable: 250000 kB\nSwapTotal: 100 kB\nSwapFree: 50 kB\n"))
	f.Add([]byte("MemTotal: 18446744073709551615 kB\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, ok := ParseMeminfo(data)
		if !ok {
			return
		}
		if m.Total == 0 || (m.HasAvailable && m.Available > m.Total) || (m.HasSwap && m.SwapFree > m.SwapTotal) {
			t.Fatalf("out of bounds: %+v", m)
		}
	})
}

func FuzzParseFibTrie(f *testing.F) {
	f.Add([]byte("Main:\n  +-- 0.0.0.0/0 3 0 5\n     |-- 192.0.2.15\n        /32 host LOCAL\n"))
	f.Add([]byte("|-- 2001:db8::1\n/32 host LOCAL\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		got := ParseFibTrieLocal(data)
		if len(got) > maxAddrs {
			t.Fatalf("%d addresses", len(got))
		}
		for _, a := range got {
			if !a.Is4() || a.IsLoopback() {
				t.Fatalf("unexpected address %v", a)
			}
		}
	})
}

func FuzzParseInet6(f *testing.F) {
	f.Add([]byte("20010db8000000000000000000000001 02 40 00 80 wlan0\n"))
	f.Add([]byte("fe80000000000000021122fffe334455 02 40 20 80 wlan0\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		got := ParseInet6(data)
		if len(got) > maxAddrs {
			t.Fatalf("%d entries", len(got))
		}
		for _, e := range got {
			if !e.Addr.Is6() || e.Prefix > 128 || e.Iface == "" || len(e.Iface) > 15 {
				t.Fatalf("unexpected entry %+v", e)
			}
		}
	})
}

func FuzzParseMountinfo(f *testing.F) {
	f.Add([]byte("22 1 179:2 / / rw shared:1 - ext4 /dev/root rw\n23 22 179:1 / /mnt/a\\040b rw - vfat /dev/x rw\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, m := range ParseMountinfo(data, 16) {
			if m.MountPoint == "" || m.MountPoint[0] != '/' || len(m.MountPoint) > maxMountPath || !plainWord(m.FSType, maxFSTypeLen) {
				t.Fatalf("unexpected mount %+v", m)
			}
		}
	})
}

func FuzzParseRoutes(f *testing.F) {
	f.Add([]byte("Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\n" +
		"wlan0\t000200C0\t00000000\t0001\t0\t0\t600\t00FFFFFF\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, r := range ParseRoutes(data) {
			if !r.Prefix.IsValid() || r.Prefix.Bits() == 0 || r.Iface == "" {
				t.Fatalf("unexpected route %+v", r)
			}
		}
	})
}

func FuzzParseDefaultRoute4(f *testing.F) {
	f.Add([]byte("Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\n" +
		"wlan0\t00000000\t010200C0\t0003\t0\t0\t600\t00000000\n"))
	f.Add([]byte("ppp0\t00000000\t00000000\t0001\t0\t0\t0\t00000000\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		r, ok := ParseDefaultRoute4(data)
		if ok && (!ifaceName(r.Iface) || r.Iface == "lo" || (r.Gateway.IsValid() && (!r.Gateway.Is4() || r.Gateway.IsUnspecified()))) {
			t.Fatalf("unexpected route %+v", r)
		}
	})
}

func FuzzParseDefaultRoute6(f *testing.F) {
	z := "00000000000000000000000000000000"
	f.Add([]byte(z + " 00 " + z + " 00 fe80000000000000000000000000abcd 00000400 00000001 00000000 00450003 wlan0\n"))
	f.Add([]byte(z + " 00 " + z + " 00 " + z + " ffffffff 00000001 00000000 00200200 lo\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		r, ok := ParseDefaultRoute6(data)
		if ok && (!ifaceName(r.Iface) || r.Iface == "lo" || (r.Gateway.IsValid() && (!r.Gateway.Is6() || r.Gateway.IsUnspecified()))) {
			t.Fatalf("unexpected route %+v", r)
		}
	})
}

func FuzzParseResolvConf(f *testing.F) {
	f.Add([]byte("# comment\nsearch home.example\nnameserver 192.0.2.53\nnameserver fe80::1%wlan0\n"))
	f.Add([]byte("nameserver 127.0.0.53\noptions edns0\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		got := ParseResolvConf(data)
		if len(got) > MaxNameservers {
			t.Fatalf("%d servers", len(got))
		}
		for _, a := range got {
			if !a.IsValid() || a.IsUnspecified() || a.IsMulticast() || (a.Zone() != "" && !ifaceName(a.Zone())) {
				t.Fatalf("unexpected server %v", a)
			}
		}
	})
}

func FuzzParseWireless(f *testing.F) {
	f.Add([]byte("Inter-| sta-|   Quality        |   Discarded packets               | Missed | WE\n face | tus | link level noise |  nwid  crypt   frag  retry   misc | beacon | 22\n wlan0: 0000   58.  -52.  -256        0      0      0      0      0        0\n"))
	f.Add([]byte(" wlan0: 0000   70   204   0 0 0 0 0 0 0\n"))
	f.Add([]byte("wlan0: zzzz 1 2 3\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		got := ParseWireless(data)
		if len(got) > maxWireless {
			t.Fatalf("%d interfaces", len(got))
		}
		seen := map[string]bool{}
		for _, w := range got {
			if !ifaceName(w.Iface) || seen[w.Iface] {
				t.Fatalf("name %q", w.Iface)
			}
			seen[w.Iface] = true
			if (w.Quality != nil && (*w.Quality < 0 || *w.Quality > 255)) || (w.LevelDBm != nil && (*w.LevelDBm < -127 || *w.LevelDBm > -1)) {
				t.Fatalf("out of bounds: %+v", w)
			}
		}
	})
}
