package metrics

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"
	"unicode/utf8"

	"github.com/swingstate/oarfish/backend/internal/metrics/parse"
	"github.com/swingstate/oarfish/backend/internal/platform/source"
)

var t0 = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

func fixture(t testing.TB, name string) fs.FS {
	t.Helper()
	dir := filepath.Join("testdata", name)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("fixture %q not found", name)
	}
	return os.DirFS(dir)
}

// cloneFixture copies a fixture into memory so a test can change it.
func cloneFixture(t testing.TB, name string) fstest.MapFS {
	t.Helper()
	src := fixture(t, name)
	m := fstest.MapFS{}
	err := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(src, p)
		m[p] = &fstest.MapFile{Data: data}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// fakeStatfs answers for fixed mount points and fails for all others.
func fakeStatfs(sizes map[string]Statfs) StatfsFunc {
	return func(path string) (Statfs, error) {
		if s, ok := sizes[path]; ok {
			return s, nil
		}
		return Statfs{}, errors.New("no such mount")
	}
}

func ptr[T any](v T) *T { return &v }

func mustJSON(t testing.TB, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ORB-DASH-001, ORB-DASH-002, ORB-DASH-003: a Raspberry Pi Zero 2 W style
// device shows host name, uptime, CPU, memory with swap, the SoC temperature,
// storage and network.
func TestCollectPiZero2W(t *testing.T) {
	statfs := fakeStatfs(map[string]Statfs{
		"/":              {BlockSize: 4096, Blocks: 1000000, Free: 700000, Available: 650000},
		"/boot/firmware": {BlockSize: 512, Blocks: 1000000, Free: 900000, Available: 900000},
	})
	c := NewCollector(fixture(t, "pi-zero-2w"), statfs)
	s := c.Collect(t0)

	if s.SchemaVersion != SchemaVersion || !s.CollectedAt.Equal(t0) {
		t.Errorf("header: %+v", s)
	}
	if s.Hostname == nil || *s.Hostname != "oarfish-test" {
		t.Errorf("hostname = %v", s.Hostname)
	}
	if s.UptimeSeconds == nil || *s.UptimeSeconds < 86412.3 || *s.UptimeSeconds > 86412.4 {
		t.Errorf("uptime = %v", s.UptimeSeconds)
	}

	cpu := s.CPU
	if cpu.Cores == nil || *cpu.Cores != 4 {
		t.Errorf("cores = %v", cpu.Cores)
	}
	if cpu.UsagePercent != nil {
		t.Errorf("the first sample has no CPU usage, got %v", *cpu.UsagePercent)
	}
	if cpu.Load == nil || cpu.Load.One != 0.18 || cpu.Load.Five != 0.12 || cpu.Load.Fifteen != 0.09 {
		t.Errorf("load = %+v", cpu.Load)
	}
	if cpu.Frequency.CurrentMHz == nil || *cpu.Frequency.CurrentMHz != 600 || cpu.Frequency.MaxMHz == nil || *cpu.Frequency.MaxMHz != 1000 {
		t.Errorf("frequency = %+v", cpu.Frequency)
	}
	if cpu.Governor == nil || *cpu.Governor != "ondemand" {
		t.Errorf("governor = %v", cpu.Governor)
	}

	m := s.Memory
	if *m.TotalBytes != 415408*1024 || *m.AvailableBytes != 262144*1024 || *m.UsedBytes != (415408-262144)*1024 {
		t.Errorf("memory = %s", mustJSON(t, m))
	}
	if *m.SwapTotalBytes != 524272*1024 || *m.SwapUsedBytes != 0 {
		t.Errorf("swap = %s", mustJSON(t, m))
	}

	// The SoC sensor appears once: the same zone as hwmon is not repeated and
	// the hwmon device without a temperature input is not a sensor.
	if len(s.Temperatures) != 1 {
		t.Fatalf("temperatures = %s", mustJSON(t, s.Temperatures))
	}
	if temp := s.Temperatures[0]; temp.ID != "thermal_zone0" || temp.Name != "cpu-thermal" || !temp.CPU || temp.Source != "thermal_zone" || *temp.Celsius != 43.3 {
		t.Errorf("temperature = %s", mustJSON(t, temp))
	}

	// Only real storage; pseudo and tmpfs mounts are skipped.
	if len(s.Storage) != 2 {
		t.Fatalf("storage = %s", mustJSON(t, s.Storage))
	}
	root, boot := s.Storage[0], s.Storage[1]
	if root.MountPoint != "/" || root.FSType != "ext4" || root.TotalBytes != 4096000000 || root.UsedBytes != 300000*4096 || root.AvailableBytes != 650000*4096 {
		t.Errorf("root = %+v", root)
	}
	// Like df: used / (used + available), so blocks reserved for root count as neither.
	if root.UsedPercent != 31.6 { // 300000 / 950000
		t.Errorf("root used = %v", root.UsedPercent)
	}
	if boot.MountPoint != "/boot/firmware" || boot.FSType != "vfat" || boot.UsedPercent != 10 {
		t.Errorf("boot = %+v", boot)
	}

	// Loopback is skipped; the link-local IPv6 address (which holds the MAC) is not shown.
	if len(s.Network) != 1 {
		t.Fatalf("network = %s", mustJSON(t, s.Network))
	}
	n := s.Network[0]
	if n.Name != "wlan0" || n.Kind != KindWiFi || n.State != "up" || !n.LinkUp || n.SpeedMbps != nil {
		t.Errorf("interface = %+v", n)
	}
	if *n.RxBytes != 123456789 || *n.TxBytes != 23456789 {
		t.Errorf("counters = %d %d", *n.RxBytes, *n.TxBytes)
	}
	if strings.Join(n.IPv4, ",") != "192.0.2.15" || strings.Join(n.IPv6, ",") != "2001:db8::15" {
		t.Errorf("addresses = %v %v", n.IPv4, n.IPv6)
	}
}

// ORB-DASH-006 relies on a delta: usage needs two samples.
func TestCPUUsageIsComputedFromDeltas(t *testing.T) {
	fsys := cloneFixture(t, "pi-zero-2w")
	c := NewCollector(fsys, fakeStatfs(nil))
	c.Collect(t0)

	// 1000 ticks pass; 250 of them are busy.
	fsys["proc/stat"] = &fstest.MapFile{Data: []byte("cpu  41300 310 15330 2290800 1210 0 2210 0 0 0\ncpu0 1 0 0 1 0 0 0\ncpu1 1 0 0 1 0 0 0\n")}
	s := c.Collect(t0.Add(2 * time.Second))
	if s.CPU.UsagePercent == nil || *s.CPU.UsagePercent != 20 { // busy 200 of 1000 ticks
		t.Errorf("usage = %s", mustJSON(t, s.CPU.UsagePercent))
	}
	if s.CPU.Cores == nil || *s.CPU.Cores != 2 {
		t.Errorf("cores = %v", s.CPU.Cores)
	}

	// No time passed: no value instead of a division by zero.
	if s = c.Collect(t0.Add(4 * time.Second)); s.CPU.UsagePercent != nil {
		t.Errorf("usage with identical counters = %v", *s.CPU.UsagePercent)
	}

	// A counter that goes backwards (reboot, wrap) is no value either, and the
	// sample after it works again.
	fsys["proc/stat"] = &fstest.MapFile{Data: []byte("cpu 10 0 10 100 0\n")}
	if s = c.Collect(t0.Add(6 * time.Second)); s.CPU.UsagePercent != nil {
		t.Errorf("usage after a counter reset = %v", *s.CPU.UsagePercent)
	}
	fsys["proc/stat"] = &fstest.MapFile{Data: []byte("cpu 60 0 10 150 0\n")}
	if s = c.Collect(t0.Add(8 * time.Second)); s.CPU.UsagePercent == nil || *s.CPU.UsagePercent != 50 {
		t.Errorf("usage after the reset = %s", mustJSON(t, s.CPU.UsagePercent))
	}

	// An unreadable /proc/stat forgets the baseline.
	delete(fsys, "proc/stat")
	if s = c.Collect(t0.Add(10 * time.Second)); s.CPU.UsagePercent != nil || s.CPU.Cores != nil {
		t.Errorf("cpu without /proc/stat = %+v", s.CPU)
	}
}

// ORB-DASH-002: every temperature source is shown with a readable name.
func TestCollectGenericARM64(t *testing.T) {
	statfs := fakeStatfs(map[string]Statfs{
		"/":                            {BlockSize: 4096, Blocks: 100000000, Free: 20000000, Available: 20000000},
		"/home":                        {BlockSize: 4096, Blocks: 100000000, Free: 20000000, Available: 20000000},
		"/boot/efi":                    {BlockSize: 1024, Blocks: 500000, Free: 400000, Available: 400000},
		"/var/log":                     {BlockSize: 4096, Blocks: 100000000, Free: 20000000, Available: 20000000},
		"/run/media/someone/DATA DISK": {BlockSize: 4096, Blocks: 1000, Free: 10, Available: 10},
		// /mnt/gone cannot be read.
	})
	s := NewCollector(fixture(t, "generic-arm64"), statfs).Collect(t0)

	if *s.CPU.Cores != 8 || s.CPU.Governor == nil || *s.CPU.Governor != "schedutil" {
		t.Errorf("cpu = %s", mustJSON(t, s.CPU))
	}
	// No cpuinfo_max_freq here: the scaling maximum is used instead.
	if *s.CPU.Frequency.CurrentMHz != 1800 || *s.CPU.Frequency.MaxMHz != 2400 {
		t.Errorf("frequency = %+v", s.CPU.Frequency)
	}
	if *s.Memory.SwapTotalBytes != 0 || *s.Memory.SwapUsedBytes != 0 {
		t.Errorf("a system without swap reports 0, got %s", mustJSON(t, s.Memory))
	}

	// hwmon only: two labelled NVMe inputs, then the processor.
	var names []string
	cpuCount := 0
	for _, temp := range s.Temperatures {
		names = append(names, temp.ID+"="+temp.Name)
		if temp.CPU {
			cpuCount++
		}
	}
	if got := strings.Join(names, ","); got != "hwmon0_temp1=Composite,hwmon0_temp2=Sensor 1,hwmon1_temp1=cpu" {
		t.Errorf("sensors = %s", got)
	}
	if cpuCount != 1 || !s.Temperatures[2].CPU || *s.Temperatures[2].Celsius != 52.1 {
		t.Errorf("cpu sensor = %s", mustJSON(t, s.Temperatures))
	}

	// One entry per device (btrfs subvolumes and bind mounts are the same
	// storage), no overlay, no unreadable mount, and no user name in a path.
	var mounts []string
	for _, st := range s.Storage {
		mounts = append(mounts, st.MountPoint+":"+st.FSType)
	}
	if got := strings.Join(mounts, ","); got != "/:btrfs,/boot/efi:vfat,/media/DATA DISK:exfat" {
		t.Errorf("storage = %s", got)
	}
	if s.Storage[2].UsedPercent != 99 { // (1000-10) / ((1000-10)+10)
		t.Errorf("stick used = %v", s.Storage[2].UsedPercent)
	}

	// Container interfaces come after the real ones; addresses are assigned by
	// the connected network; derived and tentative addresses are dropped.
	var nets []string
	for _, n := range s.Network {
		nets = append(nets, n.Name)
	}
	if got := strings.Join(nets, ","); got != "eth0,wlan0,docker0" {
		t.Fatalf("interfaces = %s", got)
	}
	eth, wlan, docker := s.Network[0], s.Network[1], s.Network[2]
	if eth.Kind != KindEthernet || *eth.SpeedMbps != 1000 || !eth.LinkUp || strings.Join(eth.IPv4, ",") != "198.51.100.40" || strings.Join(eth.IPv6, ",") != "2001:db8:0:1::40" {
		t.Errorf("eth0 = %s", mustJSON(t, eth))
	}
	if wlan.Kind != KindWiFi || wlan.SpeedMbps != nil || wlan.LinkUp || wlan.State != "down" || strings.Join(wlan.IPv4, ",") != "203.0.113.9" || len(wlan.IPv6) != 0 {
		t.Errorf("wlan0 = %s", mustJSON(t, wlan))
	}
	if docker.Kind != KindEthernet || len(docker.IPv4) != 0 || docker.IPv4 == nil || docker.IPv6 == nil {
		t.Errorf("docker0 = %s", mustJSON(t, docker))
	}
}

// ORB-NFR-015, ORB-DASH-003: missing cpufreq and missing sensors are not errors.
func TestCollectMinimalSystem(t *testing.T) {
	s := NewCollector(fixture(t, "minimal"), fakeStatfs(nil)).Collect(t0)
	if s.CPU.Frequency.CurrentMHz != nil || s.CPU.Frequency.MaxMHz != nil || s.CPU.Governor != nil {
		t.Errorf("cpufreq = %+v %v", s.CPU.Frequency, s.CPU.Governor)
	}
	if s.Temperatures == nil || len(s.Temperatures) != 0 || s.Storage == nil || len(s.Storage) != 0 || s.Network == nil || len(s.Network) != 0 {
		t.Errorf("lists must be empty, not null: %s", mustJSON(t, s))
	}
	if *s.Memory.AvailableBytes != (40960+5120+10240)*1024 || s.Memory.SwapTotalBytes != nil || s.Memory.SwapUsedBytes != nil {
		t.Errorf("memory = %s", mustJSON(t, s.Memory))
	}
	if s.Hostname == nil || s.UptimeSeconds == nil || s.CPU.Load == nil {
		t.Errorf("the core values must still be there: %s", mustJSON(t, s))
	}

	// A device with nothing at all gives an empty snapshot, not an error or a panic.
	empty := NewCollector(fstest.MapFS{}, fakeStatfs(nil)).Collect(t0)
	if empty.Hostname != nil || empty.UptimeSeconds != nil || empty.CPU.Load != nil || empty.Memory.TotalBytes != nil || len(empty.Temperatures) != 0 {
		t.Errorf("empty = %s", mustJSON(t, empty))
	}
	if !strings.Contains(mustJSON(t, empty), `"temperatures":[]`) || !strings.Contains(mustJSON(t, empty), `"network":[]`) {
		t.Errorf("lists are null in %s", mustJSON(t, empty))
	}
}

// assertClean fails if any string in the value is not clean text.
func assertClean(t testing.TB, v any) {
	t.Helper()
	var generic any
	if err := json.Unmarshal([]byte(mustJSON(t, v)), &generic); err != nil {
		t.Fatal(err)
	}
	var walk func(x any)
	walk = func(x any) {
		switch x := x.(type) {
		case string:
			if !utf8.ValidString(x) {
				t.Errorf("invalid UTF-8 in %q", x)
			}
			for _, r := range x {
				if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
					t.Errorf("control character %U in %q", r, x)
				}
			}
			if len(x) > 300 {
				t.Errorf("a string of %d bytes", len(x))
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(generic)
}

// Hostile content: every value is null, cleaned or skipped.
func TestCollectHostileFixture(t *testing.T) {
	s := NewCollector(fixture(t, "hostile"), func(string) (Statfs, error) {
		return Statfs{BlockSize: 4096, Blocks: 10, Free: 5, Available: 5}, nil
	}).Collect(t0)
	assertClean(t, s)

	if s.UptimeSeconds != nil || s.CPU.Load != nil || s.CPU.Cores != nil || s.CPU.UsagePercent != nil {
		t.Errorf("garbage numbers were accepted: %s", mustJSON(t, s.CPU))
	}
	if s.Memory.TotalBytes != nil {
		t.Errorf("memory = %s", mustJSON(t, s.Memory))
	}
	if s.CPU.Frequency.CurrentMHz != nil || s.CPU.Frequency.MaxMHz != nil {
		t.Errorf("frequency = %+v", s.CPU.Frequency)
	}
	if s.Hostname == nil || !strings.HasPrefix(*s.Hostname, "oarfish") {
		t.Errorf("hostname = %v", s.Hostname)
	}
	if len(s.Storage) != 0 || len(s.Network) != 1 {
		t.Errorf("storage/network = %s / %s", mustJSON(t, s.Storage), mustJSON(t, s.Network))
	}
	n := s.Network[0]
	if n.Name != "wlan0" || n.State != "unknown" || n.LinkUp || n.SpeedMbps != nil || n.RxBytes != nil || n.TxBytes != nil || len(n.IPv4) != 0 || len(n.IPv6) != 0 {
		t.Errorf("wlan0 = %s", mustJSON(t, n))
	}
	// Only the two zones with a plausible reading are sources; the NaN, the
	// implausible and the negative one are not.
	if len(s.Temperatures) != 2 {
		t.Fatalf("temperatures = %s", mustJSON(t, s.Temperatures))
	}
	if *s.Temperatures[0].Celsius != 47 || *s.Temperatures[1].Celsius != 45 {
		t.Errorf("temperatures = %s", mustJSON(t, s.Temperatures))
	}
	if s.Temperatures[1].Name != "thermal_zone1" {
		t.Errorf("a name without printable characters falls back to the directory name: %q", s.Temperatures[1].Name)
	}
	if !s.Temperatures[0].CPU || s.Temperatures[1].CPU {
		t.Error("the first sensor is the CPU sensor when none has a CPU name")
	}
	if s.CPU.Governor == nil || strings.ContainsAny(*s.CPU.Governor, "\x00\x1b") {
		t.Errorf("governor = %v", s.CPU.Governor)
	}
}

// Oversized files, thousands of mounts and thousands of sensors stay bounded.
func TestCollectBoundsOversizedAndNumerousInputs(t *testing.T) {
	fsys := cloneFixture(t, "pi-zero-2w")
	huge := make([]byte, 2<<20) // above every read limit (the largest is 1 MiB)
	for i := range huge {
		huge[i] = 'x'
	}
	fsys["proc/meminfo"] = &fstest.MapFile{Data: huge}
	fsys["proc/stat"] = &fstest.MapFile{Data: huge}
	fsys["proc/loadavg"] = &fstest.MapFile{Data: huge}
	fsys["proc/self/mountinfo"] = &fstest.MapFile{Data: huge}
	fsys["proc/net/fib_trie"] = &fstest.MapFile{Data: huge}
	fsys["proc/sys/kernel/hostname"] = &fstest.MapFile{Data: huge}

	s := NewCollector(fsys, fakeStatfs(nil)).Collect(t0)
	if s.Memory.TotalBytes != nil || s.CPU.Cores != nil || s.CPU.Load != nil || s.Hostname != nil || len(s.Storage) != 0 {
		t.Errorf("an oversized file was read: %s", mustJSON(t, s))
	}
	if len(s.Network) != 1 || len(s.Network[0].IPv4) != 0 {
		t.Errorf("network = %s", mustJSON(t, s.Network))
	}

	// Thousands of real-looking mounts: bounded entries and bounded system calls.
	var mi strings.Builder
	for i := 0; i < 5000; i++ {
		mi.WriteString("22 1 8:" + itoa(i) + " / /mnt/d" + itoa(i) + " rw - ext4 /dev/sd" + itoa(i) + " rw\n")
	}
	fsys = cloneFixture(t, "pi-zero-2w")
	fsys["proc/self/mountinfo"] = &fstest.MapFile{Data: []byte(mi.String())}
	calls := 0
	s = NewCollector(fsys, func(string) (Statfs, error) {
		calls++
		return Statfs{BlockSize: 4096, Blocks: 100, Free: 50, Available: 50}, nil
	}).Collect(t0)
	if len(s.Storage) != MaxMounts || calls > maxMountCandidates {
		t.Errorf("%d mounts shown after %d statfs calls", len(s.Storage), calls)
	}

	// Thousands of thermal zones, hwmon devices and interfaces.
	fsys = cloneFixture(t, "pi-zero-2w")
	for i := 0; i < 3000; i++ {
		fsys["sys/class/thermal/thermal_zone"+itoa(i+1)+"/temp"] = &fstest.MapFile{Data: []byte("40000\n")}
		fsys["sys/class/hwmon/hwmon"+itoa(i+2)+"/temp1_input"] = &fstest.MapFile{Data: []byte("40000\n")}
		fsys["sys/class/net/veth"+itoa(i)+"/type"] = &fstest.MapFile{Data: []byte("1\n")}
	}
	s = NewCollector(fsys, fakeStatfs(nil)).Collect(t0)
	if len(s.Temperatures) != MaxSensors || len(s.Network) != MaxInterfaces {
		t.Errorf("%d sensors and %d interfaces", len(s.Temperatures), len(s.Network))
	}

	// Container interfaces cannot push a real interface out of the cap.
	fsys = cloneFixture(t, "pi-zero-2w")
	for i := 0; i < 20; i++ {
		fsys["sys/class/net/veth"+itoa(i)+"/type"] = &fstest.MapFile{Data: []byte("1\n")}
	}
	s = NewCollector(fsys, fakeStatfs(nil)).Collect(t0)
	if len(s.Network) != MaxInterfaces || s.Network[0].Name != "wlan0" {
		t.Errorf("interfaces = %s", mustJSON(t, s.Network))
	}
}

func itoa(i int) string {
	var b [20]byte
	n := len(b)
	if i == 0 {
		return "0"
	}
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	return string(b[n:])
}

// The slow tier is read at the start and then every SlowInterval, not on every sample.
func TestSlowTierIsReadOnlyWhenDue(t *testing.T) {
	fsys := cloneFixture(t, "pi-zero-2w")
	calls := 0
	c := NewCollector(fsys, func(p string) (Statfs, error) {
		calls++
		return Statfs{BlockSize: 4096, Blocks: 100, Free: 50, Available: 50}, nil
	})
	c.Collect(t0)
	first := calls
	if first == 0 {
		t.Fatal("storage was not read at the start")
	}
	c.Collect(t0.Add(2 * time.Second))
	c.Collect(t0.Add(SlowInterval - time.Second))
	if calls != first {
		t.Errorf("storage was read again after %d calls, before it was due", calls-first)
	}
	// A change in a slow value is only seen when the slow tier is due.
	fsys["sys/devices/system/cpu/cpu0/cpufreq/scaling_governor"] = &fstest.MapFile{Data: []byte("performance\n")}
	if s := c.Collect(t0.Add(SlowInterval - time.Second + time.Millisecond)); *s.CPU.Governor != "ondemand" {
		t.Errorf("governor changed early: %v", *s.CPU.Governor)
	}
	if s := c.Collect(t0.Add(SlowInterval)); calls == first || *s.CPU.Governor != "performance" {
		t.Errorf("slow tier not refreshed when due (calls %d, governor %v)", calls, *s.CPU.Governor)
	}
	// A clock that jumps back refreshes too instead of waiting.
	before := calls
	c.Collect(t0)
	if calls == before {
		t.Error("a clock that went backwards did not refresh the slow tier")
	}
}

// Fast values (uptime, memory, temperature, counters, link state) follow every sample.
func TestFastTierFollowsEverySample(t *testing.T) {
	fsys := cloneFixture(t, "pi-zero-2w")
	c := NewCollector(fsys, fakeStatfs(nil))
	c.Collect(t0)
	fsys["sys/class/thermal/thermal_zone0/temp"] = &fstest.MapFile{Data: []byte("71500\n")}
	fsys["sys/class/net/wlan0/operstate"] = &fstest.MapFile{Data: []byte("down\n")}
	fsys["sys/class/net/wlan0/statistics/rx_bytes"] = &fstest.MapFile{Data: []byte("200000000\n")}
	fsys["proc/uptime"] = &fstest.MapFile{Data: []byte("86414.00 1\n")}
	s := c.Collect(t0.Add(2 * time.Second))
	if *s.Temperatures[0].Celsius != 71.5 || s.Network[0].LinkUp || *s.Network[0].RxBytes != 200000000 || *s.UptimeSeconds != 86414 {
		t.Errorf("snapshot did not follow: %s", mustJSON(t, s))
	}
}

func TestDisplayMountPointHidesUserNames(t *testing.T) {
	for in, want := range map[string]string{
		"/":                           "/",
		"/boot/firmware":              "/boot/firmware",
		"/mnt/data":                   "/mnt/data",
		"/home":                       "/home",
		"/home/alice":                 "/home/…",
		"/home/alice/Disk":            "/home/…",
		"/media/alice/STICK":          "/media/STICK",
		"/media/STICK":                "/media/STICK",
		"/run/media/alice/STICK":      "/media/STICK",
		"/run/media/alice/with space": "/media/with space",
		"/run/user/1000/gvfs":         "/run/user/1000/gvfs",
	} {
		if got := displayMountPoint(in); got != want {
			t.Errorf("displayMountPoint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStorageFromRejectsImpossibleValues(t *testing.T) {
	m := mountForTest("/data")
	for name, st := range map[string]Statfs{
		"no block size":     {BlockSize: 0, Blocks: 10, Free: 5, Available: 5},
		"no blocks":         {BlockSize: 4096, Blocks: 0},
		"free above total":  {BlockSize: 4096, Blocks: 10, Free: 11, Available: 5},
		"avail above total": {BlockSize: 4096, Blocks: 10, Free: 5, Available: 11},
		"overflow":          {BlockSize: 1 << 40, Blocks: 1 << 40, Free: 0, Available: 0},
		"all reserved":      {BlockSize: 4096, Blocks: 10, Free: 10, Available: 0},
	} {
		if s, ok := storageFrom(m, st); ok {
			t.Errorf("%s accepted: %+v", name, s)
		}
	}
	// Available above free (some file systems) is bounded by free.
	if s, ok := storageFrom(m, Statfs{BlockSize: 1000, Blocks: 100, Free: 40, Available: 90}); !ok || s.AvailableBytes != 40000 || s.UsedPercent != 60 {
		t.Errorf("%+v %v", s, ok)
	}
}

func mountForTest(point string) parse.Mount {
	return parse.Mount{MountPoint: point, FSType: "ext4", Root: "/"}
}

// Link-local and loopback IPv6 addresses never reach the output: a link-local
// address is usually built from the MAC address. The same holds for a global
// address built from it (EUI-64). Neither is stored as a fixture file, so the
// lines are added in memory.
func TestLinkLocalAndDerivedIPv6AreNeverShown(t *testing.T) {
	fsys := cloneFixture(t, "pi-zero-2w")
	inet6 := string(fsys["proc/net/if_inet6"].Data)
	inet6 += "fe80000000000000021122fffe334455 03 40 20 80 wlan0\n" + // link-local, EUI-64
		"fe80000000000000a1b2c3d4e5f60718 03 40 20 80 wlan0\n" + // link-local, other identifier
		"00000000000000000000000000000001 01 80 10 80 lo\n" + // loopback
		"20010db8000000000211 22fffe334455 03 40 00 80 wlan0\n" // global, EUI-64
	inet6 = strings.Replace(inet6, "0211 22", "021122", 1)
	fsys["proc/net/if_inet6"] = &fstest.MapFile{Data: []byte(inet6)}

	s := NewCollector(fsys, fakeStatfs(nil)).Collect(t0)
	if got := strings.Join(s.Network[0].IPv6, ","); got != "2001:db8::15" {
		t.Errorf("IPv6 addresses = %q", got)
	}
	out := mustJSON(t, s)
	for _, leaked := range []string{"fe80", "fffe", "ff:fe", "0211", "21122"} {
		if strings.Contains(out, leaked) {
			t.Errorf("%q found in the output", leaked)
		}
	}
}

// Runs the collector on the real /proc and /sys of the machine that runs the
// tests (skipped when it is not Linux), so real files are parsed in CI on a
// generic runner. It asserts completeness and cleanliness only, nothing about
// what the machine is, and it prints no value (host name and addresses are
// private).
func TestCollectOnTheRealHost(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs a real /proc and /sys")
	}
	c := NewCollector(source.Host(), HostStatfs)
	c.Collect(time.Now())
	time.Sleep(50 * time.Millisecond)
	s := c.Collect(time.Now())

	assertClean(t, s)
	if s.UptimeSeconds == nil || *s.UptimeSeconds <= 0 {
		t.Error("no uptime")
	}
	if s.CPU.Cores == nil || *s.CPU.Cores < 1 || s.CPU.Load == nil {
		t.Error("no CPU cores or load")
	}
	if s.Memory.TotalBytes == nil || s.Memory.AvailableBytes == nil || *s.Memory.AvailableBytes > *s.Memory.TotalBytes {
		t.Error("memory is missing or inconsistent")
	}
	if s.Hostname == nil || *s.Hostname == "" {
		t.Error("no host name")
	}
	for _, st := range s.Storage {
		if st.UsedPercent < 0 || st.UsedPercent > 100 || st.UsedBytes > st.TotalBytes {
			t.Errorf("a file system has impossible numbers (%s)", st.FSType)
		}
	}
	for _, n := range s.Network {
		if n.Name == "lo" {
			t.Error("the loopback interface is listed")
		}
	}
	// The output of a real machine has no MAC address in it.
	if mac := regexp.MustCompile(`(?i)\b[0-9a-f]{2}(:[0-9a-f]{2}){5}\b`).FindString(mustJSON(t, s)); mac != "" {
		t.Error("a MAC address is in the output")
	}
}

// BenchmarkCollect is the cost of one sample on the Raspberry Pi Zero 2 W
// fixture (about 15 small file reads). It runs on the developer's machine, so
// the numbers say how much work a sample is, not how long it takes on the
// device (docs/DASHBOARD.md).
func BenchmarkCollect(b *testing.B) {
	c := NewCollector(fixture(b, "pi-zero-2w"), fakeStatfs(map[string]Statfs{
		"/": {BlockSize: 4096, Blocks: 100, Free: 50, Available: 50}, "/boot/firmware": {BlockSize: 4096, Blocks: 100, Free: 50, Available: 50},
	}))
	now := t0
	c.Collect(now)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		now = now.Add(2 * time.Second) // never the slow tier: that runs every 30 s
		c.Collect(now)
	}
}

// Files in /sys/class/net that are not interfaces (bonding_masters exists when
// the bonding module is loaded) are not listed.
func TestEntriesThatAreNotInterfacesAreSkipped(t *testing.T) {
	fsys := cloneFixture(t, "pi-zero-2w")
	fsys["sys/class/net/bonding_masters"] = &fstest.MapFile{Data: []byte("\n")}
	fsys["sys/class/net/eth9/operstate"] = &fstest.MapFile{Data: []byte("up\n")} // no type file
	s := NewCollector(fsys, fakeStatfs(nil)).Collect(t0)
	if len(s.Network) != 1 || s.Network[0].Name != "wlan0" {
		t.Errorf("network = %s", mustJSON(t, s.Network))
	}
}

// A thermal zone that cannot be read does not become a permanent "unavailable" row.
func TestUnreadableThermalZonesAreNotSources(t *testing.T) {
	fsys := cloneFixture(t, "pi-zero-2w")
	fsys["sys/class/thermal/thermal_zone1/type"] = &fstest.MapFile{Data: []byte("gpu-thermal\n")}
	fsys["sys/class/thermal/thermal_zone2/type"] = &fstest.MapFile{Data: []byte("battery\n")}
	fsys["sys/class/thermal/thermal_zone2/temp"] = &fstest.MapFile{Data: []byte("30000\n")}
	s := NewCollector(fsys, fakeStatfs(nil)).Collect(t0)
	var ids []string
	for _, temp := range s.Temperatures {
		ids = append(ids, temp.ID)
	}
	if strings.Join(ids, ",") != "thermal_zone0,thermal_zone2" {
		t.Errorf("sensors = %v", ids)
	}
}
