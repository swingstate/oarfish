package parse

import "strings"

// Stat is the aggregate CPU time from /proc/stat, in clock ticks, and the
// number of logical cores.
type Stat struct {
	Total uint64 // user+nice+system+idle+iowait+irq+softirq+steal
	Idle  uint64 // idle+iowait
	Cores int
}

// maxCores bounds the core count a parser reports.
const maxCores = 1024

// ParseStat reads /proc/stat. Guest time is already part of user and nice and
// is left out so it is not counted twice. Kernels before 2.6.33 have fewer
// columns; missing ones count as zero, but the first four are required.
func ParseStat(data []byte) (Stat, bool) {
	var st Stat
	found := false
	for _, line := range lines(data) {
		f := fields(line)
		switch {
		case len(f) == 0:
		case f[0] == "cpu":
			if found {
				continue
			}
			if len(f) < 5 {
				return Stat{}, false
			}
			var v [8]uint64
			for i := 0; i < len(v) && i+1 < len(f); i++ {
				n, ok := counter(f[i+1])
				if !ok {
					return Stat{}, false
				}
				v[i] = n
			}
			for _, n := range v {
				st.Total += n
			}
			st.Idle = v[3] + v[4]
			found = true
		case isCoreLabel(f[0]):
			if st.Cores < maxCores {
				st.Cores++
			}
		}
	}
	// Cores is 0 when there are no per-core lines; callers treat that as unknown.
	return st, found
}

// isCoreLabel reports whether s is "cpu" followed by 1 to 4 digits.
func isCoreLabel(s string) bool {
	digits, ok := strings.CutPrefix(s, "cpu")
	return ok && digits != "" && len(digits) <= 4 && strings.Trim(digits, "0123456789") == ""
}

// Load holds the 1, 5 and 15 minute load averages.
type Load struct{ One, Five, Fifteen float64 }

// ParseLoadavg reads /proc/loadavg.
func ParseLoadavg(data []byte) (Load, bool) {
	f := fields(firstLine(data))
	if len(f) < 3 {
		return Load{}, false
	}
	var v [3]float64
	for i := range v {
		n, ok := finite(f[i], 1e6)
		if !ok {
			return Load{}, false
		}
		v[i] = n
	}
	return Load{v[0], v[1], v[2]}, true
}

// maxUptimeSeconds is about 317 years; anything larger is garbage.
const maxUptimeSeconds = 1e10

// ParseUptime reads the seconds since boot from /proc/uptime.
func ParseUptime(data []byte) (float64, bool) {
	f := fields(firstLine(data))
	if len(f) < 1 {
		return 0, false
	}
	return finite(f[0], maxUptimeSeconds)
}

func firstLine(data []byte) []byte {
	for i, c := range data {
		if c == '\n' {
			return data[:i]
		}
	}
	return data
}

// Meminfo holds the values of /proc/meminfo that the dashboard needs, in
// bytes. A Has flag tells a missing value from a zero.
type Meminfo struct {
	Total, Available, Free, Buffers, Cached uint64
	SwapTotal, SwapFree                     uint64
	HasAvailable, HasSwap                   bool
}

// maxKB is the largest value in kB accepted: 1 EiB.
const maxKB = 1 << 40

// ParseMeminfo reads /proc/meminfo. MemTotal is required. Without MemAvailable
// (kernels before 3.14) the caller may estimate from Free, Buffers and Cached.
func ParseMeminfo(data []byte) (Meminfo, bool) {
	var m Meminfo
	var haveTotal, haveSwapTotal, haveSwapFree bool
	for _, line := range lines(data) {
		key, rest, ok := strings.Cut(string(line), ":")
		if !ok {
			continue
		}
		var dst *uint64
		switch key {
		case "MemTotal":
			dst = &m.Total
		case "MemAvailable":
			dst = &m.Available
		case "MemFree":
			dst = &m.Free
		case "Buffers":
			dst = &m.Buffers
		case "Cached":
			dst = &m.Cached
		case "SwapTotal":
			dst = &m.SwapTotal
		case "SwapFree":
			dst = &m.SwapFree
		default:
			continue
		}
		f := fields([]byte(rest))
		if len(f) == 0 || (len(f) > 1 && f[1] != "kB") || len(f) > 2 {
			continue
		}
		n, ok := counter(f[0])
		if !ok || n > maxKB {
			continue
		}
		*dst = n * 1024
		switch key {
		case "MemTotal":
			haveTotal = true
		case "MemAvailable":
			m.HasAvailable = true
		case "SwapTotal":
			haveSwapTotal = true
		case "SwapFree":
			haveSwapFree = true
		}
	}
	m.HasSwap = haveSwapTotal && haveSwapFree && m.SwapFree <= m.SwapTotal
	if !haveTotal || m.Total == 0 {
		return Meminfo{}, false
	}
	if m.HasAvailable && m.Available > m.Total {
		m.Available, m.HasAvailable = m.Total, true
	}
	return m, true
}
