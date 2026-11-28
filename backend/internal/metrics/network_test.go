package metrics

import (
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

// Task N1 (ORB-NET-001, ORB-NET-002 in part): prefixes, the Wi-Fi signal and
// the servers behind a local resolver stub.

// recorder notes every name opened.
type recorder struct {
	fs.FS
	mu    sync.Mutex
	names []string
}

func (r *recorder) Open(name string) (fs.File, error) {
	r.mu.Lock()
	r.names = append(r.names, name)
	r.mu.Unlock()
	return r.FS.Open(name)
}

// ReadLink and Lstat implement fs.ReadLinkFS, so that a link is recorded too.
func (r *recorder) ReadLink(name string) (string, error) {
	r.mu.Lock()
	r.names = append(r.names, name)
	r.mu.Unlock()
	return fs.ReadLink(r.FS, name)
}

func (r *recorder) Lstat(name string) (fs.FileInfo, error) {
	r.mu.Lock()
	r.names = append(r.names, name)
	r.mu.Unlock()
	return fs.Lstat(r.FS, name)
}

func (r *recorder) opened(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Contains(r.names, name)
}

func TestNetworkPrefixesAndSignal(t *testing.T) {
	s := NewCollector(fixture(t, "pi-zero-2w"), fakeStatfs(nil)).Collect(t0)
	if len(s.Network) != 1 {
		t.Fatalf("network = %s", mustJSON(t, s.Network))
	}
	n := s.Network[0]
	if strings.Join(n.IPv4CIDR, ",") != "192.0.2.15/24" || strings.Join(n.IPv6CIDR, ",") != "2001:db8::15/64" {
		t.Errorf("cidr = %v %v", n.IPv4CIDR, n.IPv6CIDR)
	}
	if n.Signal == nil || n.Signal.LevelDBm == nil || *n.Signal.LevelDBm != -52 || n.Signal.LinkQuality == nil || *n.Signal.LinkQuality != 58 {
		t.Errorf("signal = %s", mustJSON(t, n.Signal))
	}
}

// Several interfaces and families: the CIDR lists have the order and length
// of the plain lists; the IPv4 prefix is that of the most specific connected
// route; an interface that is not Wi-Fi has no signal.
func TestNetworkPrefixesGeneric(t *testing.T) {
	s := NewCollector(fixture(t, "generic-arm64"), fakeStatfs(nil)).Collect(t0)
	for _, n := range s.Network {
		if len(n.IPv4CIDR) != len(n.IPv4) || len(n.IPv6CIDR) != len(n.IPv6) {
			t.Fatalf("%s: %v %v / %v %v", n.Name, n.IPv4, n.IPv4CIDR, n.IPv6, n.IPv6CIDR)
		}
		for i := range n.IPv4 {
			if !strings.HasPrefix(n.IPv4CIDR[i], n.IPv4[i]+"/") {
				t.Errorf("%s: %s and %s", n.Name, n.IPv4[i], n.IPv4CIDR[i])
			}
		}
		for i := range n.IPv6 {
			if !strings.HasPrefix(n.IPv6CIDR[i], n.IPv6[i]+"/") {
				t.Errorf("%s: %s and %s", n.Name, n.IPv6[i], n.IPv6CIDR[i])
			}
		}
		if n.Kind != KindWiFi && n.Signal != nil {
			t.Errorf("%s (%s) has a signal", n.Name, n.Kind)
		}
	}
	if got := mustJSON(t, s.Network); !strings.Contains(got, `"ipv4_cidr":["198.51.100.40/24"],"ipv6_cidr":["2001:db8:0:1::40/64"]`) {
		t.Errorf("network = %s", got)
	}
}

// No wireless line, a missing or oversize file: no signal, nothing else
// changes. Without a Wi-Fi interface the file is not opened at all.
func TestNetworkSignalFailsSoft(t *testing.T) {
	for name, data := range map[string]*fstest.MapFile{
		"no line":  {Data: []byte("Inter-| sta-|\n face | tus |\n")},
		"missing":  nil,
		"oversize": {Data: []byte(" wlan0: 0000 58. -52. -256 0 0 0 0 0 0\n" + strings.Repeat("#", limitWireless))},
	} {
		fsys := cloneFixture(t, "pi-zero-2w")
		if data == nil {
			delete(fsys, pathWireless)
		} else {
			fsys[pathWireless] = data
		}
		s := NewCollector(fsys, fakeStatfs(nil)).Collect(t0)
		if len(s.Network) != 1 || s.Network[0].Signal != nil || s.Network[0].State != "up" {
			t.Errorf("%s: %s", name, mustJSON(t, s.Network))
		}
	}
	// Ethernet only.
	fsys := cloneFixture(t, "pi-zero-2w")
	delete(fsys, "sys/class/net/wlan0/phy80211")
	rec := &recorder{FS: fsys}
	s := NewCollector(rec, fakeStatfs(nil)).Collect(t0)
	if s.Network[0].Kind == KindWiFi || s.Network[0].Signal != nil || rec.opened(pathWireless) {
		t.Errorf("without Wi-Fi: %s, opened %v", mustJSON(t, s.Network), rec.opened(pathWireless))
	}
}

// The signal follows every sample (fast tier).
func TestNetworkSignalIsFastTier(t *testing.T) {
	fsys := cloneFixture(t, "pi-zero-2w")
	c := NewCollector(fsys, fakeStatfs(nil))
	c.Collect(t0)
	fsys[pathWireless] = &fstest.MapFile{Data: []byte(" wlan0: 0000   30.  -77.  -256 0 0 0 0 0 0\n")}
	s := c.Collect(t0.Add(2_000_000_000))
	if *s.Network[0].Signal.LevelDBm != -77 || *s.Network[0].Signal.LinkQuality != 30 {
		t.Errorf("signal = %s", mustJSON(t, s.Network[0].Signal))
	}
}

// The Ubuntu shape (docs/UBUNTU_PROBE.md U3): the stub in /etc/resolv.conf,
// two servers behind it in /run/systemd/resolve/resolv.conf.
func TestDNSBehindTheLocalResolver(t *testing.T) {
	r := NewCollector(fixture(t, "ubuntu-stub"), fakeStatfs(nil)).Collect(t0).Routing
	want := `{"status":"found","servers":[{"address":"198.51.100.53","local":false},{"address":"2001:db8:2::53","local":false}],"hidden":0,"via_local_resolver":true}`
	if got := mustJSON(t, r.DNS); got != want {
		t.Errorf("dns = %s", got)
	}
}

func TestDNSFallbackCases(t *testing.T) {
	const stub = "nameserver 127.0.0.53\noptions edns0 trust-ad\n"
	n0 := `{"status":"found","servers":[{"address":"127.0.0.53","local":true}],"hidden":0,"via_local_resolver":false}`
	for name, c := range map[string]struct {
		resolv   string
		upstream *fstest.MapFile // nil: missing
		want     string
		read     bool
	}{
		"stub with file":                      {stub, &fstest.MapFile{Data: []byte("nameserver 198.51.100.53\n")}, `{"status":"found","servers":[{"address":"198.51.100.53","local":false}],"hidden":0,"via_local_resolver":true}`, true},
		"stub without file":                   {stub, nil, n0, true},
		"stub, file unreadable (a directory)": {stub, &fstest.MapFile{Mode: fs.ModeDir}, n0, true},
		"stub, file over 64 KiB":              {stub, &fstest.MapFile{Data: []byte("nameserver 198.51.100.53\n" + strings.Repeat("#", limitResolv))}, n0, true},
		"stub, file without a server":         {stub, &fstest.MapFile{Data: []byte("# nothing\nsearch .\n")}, n0, true},
		"stub, file names only loopback":      {stub, &fstest.MapFile{Data: []byte("nameserver 127.0.0.1\nnameserver 198.51.100.53\n")}, n0, true},
		"two loopback servers": {"nameserver 127.0.0.53\nnameserver ::1\n", &fstest.MapFile{Data: []byte("nameserver 198.51.100.53\n")},
			`{"status":"found","servers":[{"address":"198.51.100.53","local":false}],"hidden":0,"via_local_resolver":true}`, true},
		"no stub": {"nameserver 192.0.2.53\n", &fstest.MapFile{Data: []byte("nameserver 198.51.100.53\n")},
			`{"status":"found","servers":[{"address":"192.0.2.53","local":false}],"hidden":0,"via_local_resolver":false}`, false},
		"stub and a real server": {"nameserver 127.0.0.53\nnameserver 192.0.2.53\n", &fstest.MapFile{Data: []byte("nameserver 198.51.100.53\n")},
			`{"status":"found","servers":[{"address":"127.0.0.53","local":true},{"address":"192.0.2.53","local":false}],"hidden":0,"via_local_resolver":false}`, false},
		"no server at all": {"search .\n", &fstest.MapFile{Data: []byte("nameserver 198.51.100.53\n")}, `{"status":"none","servers":[],"hidden":0,"via_local_resolver":false}`, false},
		// The rules of N0.2 apply to the fallback: EUI-64 hidden, at most three.
		"hidden EUI-64 behind the stub": {stub, &fstest.MapFile{Data: []byte("nameserver 2001:db8::211:22ff:fe33:4455\nnameserver 198.51.100.53\n")},
			`{"status":"found","servers":[{"address":"198.51.100.53","local":false}],"hidden":1,"via_local_resolver":true}`, true},
		"at most three": {stub, &fstest.MapFile{Data: []byte("nameserver 198.51.100.1\nnameserver 198.51.100.2\nnameserver 198.51.100.3\nnameserver 198.51.100.4\n")},
			`{"status":"found","servers":[{"address":"198.51.100.1","local":false},{"address":"198.51.100.2","local":false},{"address":"198.51.100.3","local":false}],"hidden":0,"via_local_resolver":true}`, true},
	} {
		fsys := cloneFixture(t, "pi-zero-2w")
		fsys[pathResolv] = &fstest.MapFile{Data: []byte(c.resolv)}
		if c.upstream != nil {
			fsys[pathResolvUpstream] = c.upstream
		}
		rec := &recorder{FS: fsys}
		r := NewCollector(rec, fakeStatfs(nil)).Collect(t0).Routing
		if got := mustJSON(t, r.DNS); got != c.want {
			t.Errorf("%s: dns = %s\nwant %s", name, got, c.want)
		}
		if rec.opened(pathResolvUpstream) != c.read {
			t.Errorf("%s: fallback file opened = %v", name, !c.read)
		}
		if strings.Contains(mustJSON(t, r), "edns0") || strings.Contains(mustJSON(t, r), "4455") {
			t.Errorf("%s: text of a file reached the output", name)
		}
	}
}

// Task SF1: the global IPv6 addresses left out because they contain the
// hardware address (EUI-64) are counted per interface; the address itself
// never reaches the output. Link-local, tentative and failed addresses are
// not counted (they are not addresses of the device on the network), and an
// IPv4 address is never counted.
func TestNetworkHiddenAddressCount(t *testing.T) {
	const (
		plain   = "20010db8000000000000000000000015 03 40 00 80 wlan0\n"
		eui1    = "20010db8000000000211aafffebb0001 03 40 00 00 wlan0\n"
		eui2    = "20010db8000000010211aafffebb0001 03 40 00 00 wlan0\n"
		eui3    = "20010db8000000020211aafffebb0001 03 40 00 00 wlan0\n"
		link    = "fe800000000000000211aafffebb0001 03 40 20 80 wlan0\n"
		tent    = "20010db8000000030211aafffebb0001 03 40 00 40 wlan0\n"
		dadFail = "20010db8000000040211aafffebb0001 03 40 00 08 wlan0\n"
	)
	for name, c := range map[string]struct {
		inet6 string
		want  int
		addrs string
	}{
		"none":    {plain, 0, "2001:db8::15/64"},
		"one":     {plain + eui1 + link, 1, "2001:db8::15/64"},
		"several": {eui1 + plain + eui2 + eui3 + tent + dadFail, 3, "2001:db8::15/64"},
		"only":    {eui1 + eui2, 2, ""},
	} {
		t.Run(name, func(t *testing.T) {
			fsys := cloneFixture(t, "pi-zero-2w")
			fsys["proc/net/if_inet6"] = &fstest.MapFile{Data: []byte(c.inet6)}
			s := NewCollector(fsys, fakeStatfs(nil)).Collect(t0)
			if len(s.Network) != 1 {
				t.Fatalf("network = %s", mustJSON(t, s.Network))
			}
			n := s.Network[0]
			if n.IPv6Hidden != c.want || strings.Join(n.IPv6CIDR, ",") != c.addrs {
				t.Errorf("hidden = %d, addresses %v; want %d, %s", n.IPv6Hidden, n.IPv6CIDR, c.want, c.addrs)
			}
			if strings.Join(n.IPv4, ",") != "192.0.2.15" {
				t.Errorf("ipv4 = %v", n.IPv4)
			}
			out := mustJSON(t, s)
			if !strings.Contains(out, `"ipv6_hidden":`+fmt.Sprint(c.want)) {
				t.Errorf("no ipv6_hidden in %s", out)
			}
			for _, leak := range []string{"fffe", "ff:fe", "211:aaff", "aaff:febb"} {
				if strings.Contains(out, leak) {
					t.Errorf("%q reached the output", leak)
				}
			}
		})
	}

	// IPv4 is never counted: no IPv6 table at all gives zero, the IPv4
	// address stays.
	fsys := cloneFixture(t, "pi-zero-2w")
	delete(fsys, "proc/net/if_inet6")
	s := NewCollector(fsys, fakeStatfs(nil)).Collect(t0)
	if s.Network[0].IPv6Hidden != 0 || len(s.Network[0].IPv4) != 1 {
		t.Errorf("without if_inet6: %s", mustJSON(t, s.Network[0]))
	}

	// The fixture generic-arm64 has one global EUI-64 address on eth0.
	s = NewCollector(fixture(t, "generic-arm64"), fakeStatfs(nil)).Collect(t0)
	for _, n := range s.Network {
		want := 0
		if n.Name == "eth0" {
			want = 1
		}
		if n.IPv6Hidden != want {
			t.Errorf("%s: hidden = %d, want %d", n.Name, n.IPv6Hidden, want)
		}
	}
}
