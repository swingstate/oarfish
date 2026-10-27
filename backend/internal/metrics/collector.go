package metrics

import (
	"io/fs"
	"math"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/swingstate/oarfish/backend/internal/metrics/parse"
	"github.com/swingstate/oarfish/backend/internal/platform/source"
)

// SlowInterval is how often the slowly changing data is read again: file
// systems, network addresses and link speed, the sensor list, the governor.
const SlowInterval = 30 * time.Second

// The files the collector reads. All names are relative to the file source
// root ("proc/stat" is /proc/stat). Nothing else is opened; the list is
// enforced by a test (privacy, docs/DASHBOARD.md). The hardware address of an
// interface (sys/class/net/<if>/address) is deliberately not on it.
const (
	pathStat      = "proc/stat"
	pathLoadavg   = "proc/loadavg"
	pathUptime    = "proc/uptime"
	pathMeminfo   = "proc/meminfo"
	pathHostname  = "proc/sys/kernel/hostname"
	pathMountinfo = "proc/self/mountinfo"
	pathFibTrie   = "proc/net/fib_trie"
	pathRoute     = "proc/net/route"
	pathInet6     = "proc/net/if_inet6"
	dirCPUFreq    = "sys/devices/system/cpu/cpu0/cpufreq"
	dirThermal    = "sys/class/thermal"
	dirHwmon      = "sys/class/hwmon"
	dirNet        = "sys/class/net"
)

// Read limits in bytes. A file above its limit counts as unreadable.
const (
	limitStat      = 64 << 10
	limitMeminfo   = 16 << 10
	limitMountinfo = 1 << 20
	limitNetTable  = 256 << 10
	limitSmall     = 128
	maxDirEntries  = 512
	// maxMountCandidates is how many distinct devices are asked for their size.
	maxMountCandidates = 64
)

// Collector reads the host through a file source and keeps the small amount of
// state that deltas and the slow tier need. It runs no program. It is safe for
// concurrent use, but calls are serialised.
type Collector struct {
	fsys   fs.FS
	statfs StatfsFunc

	mu       sync.Mutex
	prev     parse.Stat
	havePrev bool
	slowAt   time.Time
	slow     slowData
}

// NewCollector returns a Collector reading fsys (source.Host() in production,
// a fixture in tests) and asking statfs for file system sizes.
func NewCollector(fsys fs.FS, statfs StatfsFunc) *Collector {
	return &Collector{fsys: fsys, statfs: statfs}
}

type sensorRef struct {
	Temperature
	path string
}

type ifaceStatic struct {
	name      string
	kind      string
	speedMbps *int
	ipv4      []string
	ipv6      []string
}

type slowData struct {
	sensors  []sensorRef
	storage  []Storage
	ifaces   []ifaceStatic
	governor *string
	maxMHz   *float64
}

// Collect reads the device. now decides whether the slow tier is due.
// The first call has no CPU usage (there is no previous sample to compare with).
func (c *Collector) Collect(now time.Time) Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.slowAt.IsZero() || now.Sub(c.slowAt) >= SlowInterval || now.Before(c.slowAt) {
		c.refreshSlow()
		c.slowAt = now
	}

	snap := Snapshot{
		SchemaVersion: SchemaVersion,
		CollectedAt:   now.UTC().Truncate(time.Millisecond),
		Temperatures:  []Temperature{},
		Storage:       append([]Storage{}, c.slow.storage...),
		Network:       []NetworkInterface{},
	}
	snap.Hostname = c.hostname()
	if data, err := source.ReadFile(c.fsys, pathUptime, limitSmall); err == nil {
		if v, ok := parse.ParseUptime(data); ok {
			v = round(v, 1)
			snap.UptimeSeconds = &v
		}
	}
	snap.CPU = c.cpu()
	snap.Memory = c.memory()
	for _, s := range c.slow.sensors {
		t := s.Temperature
		t.Celsius = readCelsius(c.fsys, s.path)
		snap.Temperatures = append(snap.Temperatures, t)
	}
	snap.Network = c.network()
	return snap
}

func (c *Collector) hostname() *string {
	data, err := source.ReadFile(c.fsys, pathHostname, limitSmall)
	if err != nil {
		return nil
	}
	return nonEmpty(source.Clean(string(data), 64))
}

func (c *Collector) cpu() CPU {
	out := CPU{Governor: c.slow.governor, Frequency: Frequency{MaxMHz: c.slow.maxMHz}}
	if data, err := source.ReadFile(c.fsys, pathStat, limitStat); err == nil {
		if st, ok := parse.ParseStat(data); ok {
			if st.Cores > 0 {
				n := st.Cores
				out.Cores = &n
			}
			if c.havePrev && st.Total > c.prev.Total && st.Idle >= c.prev.Idle {
				dTotal := float64(st.Total - c.prev.Total)
				dIdle := float64(st.Idle - c.prev.Idle)
				if dIdle <= dTotal {
					v := round(clamp((dTotal-dIdle)/dTotal*100, 0, 100), 1)
					out.UsagePercent = &v
				}
			}
			c.prev, c.havePrev = st, true
		} else {
			c.havePrev = false
		}
	} else {
		c.havePrev = false
	}
	if data, err := source.ReadFile(c.fsys, pathLoadavg, limitSmall); err == nil {
		if l, ok := parse.ParseLoadavg(data); ok {
			out.Load = &Load{One: round(l.One, 2), Five: round(l.Five, 2), Fifteen: round(l.Fifteen, 2)}
		}
	}
	out.Frequency.CurrentMHz = readMHz(c.fsys, dirCPUFreq+"/scaling_cur_freq")
	return out
}

func (c *Collector) memory() Memory {
	var out Memory
	data, err := source.ReadFile(c.fsys, pathMeminfo, limitMeminfo)
	if err != nil {
		return out
	}
	m, ok := parse.ParseMeminfo(data)
	if !ok {
		return out
	}
	avail := m.Available
	if !m.HasAvailable {
		avail = min(m.Free+m.Buffers+m.Cached, m.Total)
	}
	used := m.Total - avail
	out.TotalBytes, out.AvailableBytes, out.UsedBytes = &m.Total, &avail, &used
	if m.HasSwap {
		swapUsed := m.SwapTotal - m.SwapFree
		out.SwapTotalBytes, out.SwapUsedBytes = &m.SwapTotal, &swapUsed
	}
	return out
}

// network combines the cached static part with a fresh read of state and counters.
func (c *Collector) network() []NetworkInterface {
	out := make([]NetworkInterface, 0, len(c.slow.ifaces))
	for _, s := range c.slow.ifaces {
		base := dirNet + "/" + s.name + "/"
		ni := NetworkInterface{
			Name: s.name, Kind: s.kind, SpeedMbps: s.speedMbps,
			State: "unknown", IPv4: s.ipv4, IPv6: s.ipv6,
			RxBytes: readCounter(c.fsys, base+"statistics/rx_bytes"),
			TxBytes: readCounter(c.fsys, base+"statistics/tx_bytes"),
		}
		if data, err := source.ReadFile(c.fsys, base+"operstate", limitSmall); err == nil {
			ni.State = operState(string(data))
		}
		ni.LinkUp = ni.State == "up"
		out = append(out, ni)
	}
	return out
}

// refreshSlow reads everything that changes rarely.
func (c *Collector) refreshSlow() {
	c.slow = slowData{
		sensors:  discoverSensors(c.fsys),
		storage:  c.discoverStorage(),
		ifaces:   discoverInterfaces(c.fsys),
		governor: nil,
	}
	if data, err := source.ReadFile(c.fsys, dirCPUFreq+"/scaling_governor", limitSmall); err == nil {
		c.slow.governor = nonEmpty(source.Clean(string(data), 32))
	}
	if v := readMHz(c.fsys, dirCPUFreq+"/cpuinfo_max_freq"); v != nil {
		c.slow.maxMHz = v
	} else {
		c.slow.maxMHz = readMHz(c.fsys, dirCPUFreq+"/scaling_max_freq")
	}
}

// ---- temperatures -------------------------------------------------------

// Plausible range of a temperature in millidegrees Celsius.
const (
	minMilliC = -60_000
	maxMilliC = 200_000
)

func readCelsius(fsys fs.FS, path string) *float64 {
	data, err := source.ReadFile(fsys, path, limitSmall)
	if err != nil {
		return nil
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || v < minMilliC || v > maxMilliC {
		return nil
	}
	c := round(float64(v)/1000, 1)
	return &c
}

// cpuSensorNames are the (normalised) names drivers give the processor sensor.
var cpuSensorNames = map[string]bool{
	"cpu_thermal": true, "soc_thermal": true, "x86_pkg_temp": true,
	"coretemp": true, "k10temp": true, "cpu": true,
}

func normaliseSensorName(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), "-", "_")
}

// discoverSensors lists the temperature sources: thermal zones first, then
// hwmon devices whose name is not already a thermal zone (many drivers show
// up in both). Directory names are used in paths only if they match a strict
// pattern; the display name is cleaned text from the driver.
func discoverSensors(fsys fs.FS) []sensorRef {
	var out []sensorRef
	seen := map[string]bool{}

	for _, dir := range numbered(fsys, dirThermal, "thermal_zone") {
		if len(out) >= MaxSensors {
			break
		}
		base := dirThermal + "/" + dir
		if readCelsius(fsys, base+"/temp") == nil {
			continue // a zone that cannot be read (disabled, implausible) is not a source
		}
		name := cleanName(fsys, base+"/type", dir)
		out = append(out, sensorRef{
			Temperature: Temperature{ID: dir, Name: name, Source: "thermal_zone"},
			path:        base + "/temp",
		})
		seen[normaliseSensorName(name)] = true
	}
	for _, dir := range numbered(fsys, dirHwmon, "hwmon") {
		base := dirHwmon + "/" + dir
		name := cleanName(fsys, base+"/name", dir)
		if seen[normaliseSensorName(name)] {
			continue
		}
		var inputs []int
		for i := 1; i <= 8; i++ {
			if readCelsius(fsys, base+"/temp"+strconv.Itoa(i)+"_input") != nil {
				inputs = append(inputs, i)
			}
		}
		for _, i := range inputs {
			if len(out) >= MaxSensors {
				break
			}
			n := strconv.Itoa(i)
			display := name
			if label := cleanName(fsys, base+"/temp"+n+"_label", ""); label != "" {
				display = label
			} else if len(inputs) > 1 {
				display = name + " " + n
			}
			out = append(out, sensorRef{
				Temperature: Temperature{ID: dir + "_temp" + n, Name: display, Source: "hwmon"},
				path:        base + "/temp" + n + "_input",
			})
		}
	}

	marked := false
	for i := range out {
		if cpuSensorNames[normaliseSensorName(out[i].Name)] {
			out[i].CPU, marked = true, true
		}
	}
	if !marked && len(out) > 0 {
		out[0].CPU = true // the same fallback the device detection uses: the first sensor
	}
	return out
}

func cleanName(fsys fs.FS, path, fallback string) string {
	data, err := source.ReadFile(fsys, path, limitSmall)
	if err != nil {
		return fallback
	}
	if s := source.Clean(string(data), 32); s != "" {
		return s
	}
	return fallback
}

// ---- frequency ----------------------------------------------------------

func readMHz(fsys fs.FS, path string) *float64 {
	data, err := source.ReadFile(fsys, path, limitSmall)
	if err != nil {
		return nil
	}
	khz, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || khz <= 0 || khz > 10_000_000 { // above 10 GHz is not a CPU
		return nil
	}
	v := round(float64(khz)/1000, 1)
	return &v
}

// ---- storage ------------------------------------------------------------

// realFS are the file system types of real storage. Pseudo and memory file
// systems (proc, sysfs, tmpfs, overlay, cgroup, ...) and network file systems
// are not listed on purpose.
var realFS = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "vfat": true, "exfat": true,
	"f2fs": true, "xfs": true, "btrfs": true, "ntfs3": true,
}

func (c *Collector) discoverStorage() []Storage {
	data, err := source.ReadFile(c.fsys, pathMountinfo, limitMountinfo)
	if err != nil {
		return nil
	}
	// One entry per device: bind mounts and btrfs subvolumes show the same
	// storage more than once. Prefer the mount whose root is "/", then the
	// shortest mount point, then the first.
	byDevice := map[string]parse.Mount{}
	var order []string
	for _, m := range parse.ParseMountinfo(data, 1024) {
		if !realFS[m.FSType] {
			continue
		}
		key := m.Source
		if key == "" || !strings.HasPrefix(key, "/") {
			key = strconv.Itoa(m.Major) + ":" + strconv.Itoa(m.Minor)
		}
		old, seen := byDevice[key]
		switch {
		case !seen:
			order = append(order, key)
			byDevice[key] = m
		case better(m, old):
			byDevice[key] = m
		}
	}
	// A hostile mount table must not turn into thousands of system calls.
	if len(order) > maxMountCandidates {
		order = order[:maxMountCandidates]
	}
	var out []Storage
	for _, key := range order {
		m := byDevice[key]
		st, err := c.statfs(m.MountPoint)
		if err != nil {
			continue // a mount that cannot be read is skipped
		}
		if s, ok := storageFrom(m, st); ok {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].MountPoint == "/") != (out[j].MountPoint == "/") {
			return out[i].MountPoint == "/"
		}
		return out[i].MountPoint < out[j].MountPoint
	})
	if len(out) > MaxMounts {
		out = out[:MaxMounts]
	}
	return out
}

func better(a, b parse.Mount) bool {
	if (a.Root == "/") != (b.Root == "/") {
		return a.Root == "/"
	}
	return len(a.MountPoint) < len(b.MountPoint)
}

func storageFrom(m parse.Mount, st Statfs) (Storage, bool) {
	if st.BlockSize == 0 || st.Blocks == 0 || st.Free > st.Blocks || st.Available > st.Blocks {
		return Storage{}, false
	}
	if st.Blocks > math.MaxUint64/st.BlockSize {
		return Storage{}, false
	}
	avail := min(st.Available, st.Free)
	total := st.Blocks * st.BlockSize
	used := (st.Blocks - st.Free) * st.BlockSize
	availBytes := avail * st.BlockSize
	denom := used + availBytes // like df: reserved blocks count as neither used nor available; cannot overflow
	if denom == 0 {
		return Storage{}, false
	}
	return Storage{
		MountPoint: displayMountPoint(m.MountPoint), FSType: m.FSType,
		TotalBytes: total, UsedBytes: used, AvailableBytes: availBytes,
		UsedPercent: round(float64(used)/float64(denom)*100, 1),
	}, true
}

// displayMountPoint keeps a user name out of the page. Removable media are
// mounted under /media/<user>/<label> or /run/media/<user>/<label>, and home
// directories under /home/<user>; those become /media/<label> and /home/…
func displayMountPoint(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	switch {
	case len(parts) >= 2 && parts[0] == "home":
		return "/home/…"
	case len(parts) >= 3 && parts[0] == "run" && parts[1] == "media":
		return "/media/" + parts[len(parts)-1]
	case len(parts) >= 2 && parts[0] == "media":
		return "/media/" + parts[len(parts)-1]
	}
	return p
}

// ---- network ------------------------------------------------------------

func discoverInterfaces(fsys fs.FS) []ifaceStatic {
	names := listNames(fsys, dirNet)
	var found []ifaceStatic
	for _, name := range names {
		if !validInterfaceName(name) || name == "lo" {
			continue
		}
		base := dirNet + "/" + name + "/"
		typ := -1
		if data, err := source.ReadFile(fsys, base+"type", limitSmall); err == nil {
			if v, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				typ = v
			}
		}
		const arphrdLoopback = 772
		if typ < 0 || typ == arphrdLoopback {
			continue // not an interface (for example the file bonding_masters), or loopback
		}
		s := ifaceStatic{name: name, kind: KindOther, ipv4: []string{}, ipv6: []string{}}
		switch {
		case source.Exists(fsys, base+"wireless") || source.Exists(fsys, base+"phy80211"):
			s.kind = KindWiFi
		case typ == 1:
			s.kind = KindEthernet
		}
		if data, err := source.ReadFile(fsys, base+"speed", limitSmall); err == nil {
			if v, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && v > 0 && v <= 1_000_000 {
				s.speedMbps = &v
			}
		}
		found = append(found, s)
	}
	// Physical-looking names first, so that a pile of container interfaces
	// cannot push the real ones out of the cap.
	sort.SliceStable(found, func(i, j int) bool { return rank(found[i].name) < rank(found[j].name) })
	if len(found) > MaxInterfaces {
		found = found[:MaxInterfaces]
	}
	assignAddresses(fsys, found)
	return found
}

func rank(name string) int {
	for _, p := range []string{"en", "eth", "wl", "ww", "usb"} {
		if strings.HasPrefix(name, p) {
			return 0
		}
	}
	return 1
}

// validInterfaceName accepts the characters real interface names use. The
// name becomes part of a path, so anything else is dropped. The kernel limit
// is 15 characters.
func validInterfaceName(s string) bool {
	if s == "" || len(s) > 15 || s == "." || s == ".." {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-', c == '.', c == ':', c == '@':
		default:
			return false
		}
	}
	return true
}

// OperStates are the values NetworkInterface.State can have (RFC 2863, as the
// kernel reports them). Anything else is reported as "unknown".
var OperStates = []string{"up", "down", "unknown", "dormant", "lowerlayerdown", "notpresent", "testing"}

func operState(s string) string {
	s = strings.TrimSpace(s)
	for _, known := range OperStates {
		if s == known {
			return s
		}
	}
	return "unknown"
}

// assignAddresses fills the addresses of the interfaces.
//
// IPv6 comes from /proc/net/if_inet6, which names the interface. IPv4 comes
// from the LOCAL entries of /proc/net/fib_trie, which do not; an address is
// assigned to the interface whose directly connected network (from
// /proc/net/route) contains it, the most specific network winning. An address
// without such a route is not shown.
//
// Link-local IPv6 addresses and addresses derived from the hardware address
// (EUI-64) are left out: both contain the MAC address, which Oarfish never shows.
func assignAddresses(fsys fs.FS, ifaces []ifaceStatic) {
	index := map[string]int{}
	for i, s := range ifaces {
		index[s.name] = i
	}

	if data, err := source.ReadFile(fsys, pathInet6, limitNetTable); err == nil {
		for _, e := range parse.ParseInet6(data) {
			i, ok := index[e.Iface]
			if !ok || e.Scope != parse.Inet6ScopeGlobal || e.Flags&(parse.Inet6FlagDADFail|parse.Inet6FlagTentative) != 0 ||
				parse.DerivedFromHardwareAddress(e.Addr) || len(ifaces[i].ipv6) >= MaxAddresses {
				continue
			}
			ifaces[i].ipv6 = append(ifaces[i].ipv6, e.Addr.String())
		}
	}

	trie, err1 := source.ReadFile(fsys, pathFibTrie, limitNetTable)
	routes, err2 := source.ReadFile(fsys, pathRoute, limitNetTable)
	if err1 != nil || err2 != nil {
		return
	}
	connected := parse.ParseRoutes(routes)
	for _, addr := range parse.ParseFibTrieLocal(trie) {
		best, bestBits := "", -1
		for _, r := range connected {
			if r.Prefix.Contains(addr) && r.Prefix.Bits() > bestBits {
				best, bestBits = r.Iface, r.Prefix.Bits()
			}
		}
		if i, ok := index[best]; ok && len(ifaces[i].ipv4) < MaxAddresses {
			ifaces[i].ipv4 = append(ifaces[i].ipv4, addr.String())
		}
	}
	for i := range ifaces {
		sort.Slice(ifaces[i].ipv4, func(a, b int) bool {
			return netip.MustParseAddr(ifaces[i].ipv4[a]).Less(netip.MustParseAddr(ifaces[i].ipv4[b]))
		})
	}
}

func readCounter(fsys fs.FS, path string) *uint64 {
	data, err := source.ReadFile(fsys, path, limitSmall)
	if err != nil {
		return nil
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || v > 1<<56 {
		return nil
	}
	return &v
}

// ---- helpers ------------------------------------------------------------

// listNames returns up to maxDirEntries names of a directory, sorted. The
// bound keeps a hostile directory from using memory.
func listNames(fsys fs.FS, dir string) []string {
	f, err := fsys.Open(dir)
	if err != nil {
		return nil
	}
	defer f.Close()
	d, ok := f.(fs.ReadDirFile)
	if !ok {
		return nil
	}
	entries, _ := d.ReadDir(maxDirEntries)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// numbered returns the names prefix+digits (1 to 4 digits) in numeric order.
// Only these strict names are ever used to build a path.
func numbered(fsys fs.FS, dir, prefix string) []string {
	type item struct {
		name string
		n    int
	}
	var items []item
	for _, name := range listNames(fsys, dir) {
		digits, ok := strings.CutPrefix(name, prefix)
		if !ok || digits == "" || len(digits) > 4 || strings.Trim(digits, "0123456789") != "" {
			continue
		}
		n, _ := strconv.Atoi(digits)
		items = append(items, item{name, n})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].n < items[j].n })
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.name
	}
	return out
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func round(v float64, decimals int) float64 {
	p := math.Pow(10, float64(decimals))
	return math.Round(v*p) / p
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
