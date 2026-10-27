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
