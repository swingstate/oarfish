package metrics

import (
	"fmt"
	"sort"
)

// Alert levels, most severe first (Pflichtenheft ORB-DASH-005).
const (
	LevelCritical      = "critical"
	LevelRelevant      = "relevant"
	LevelInformational = "informational"
)

// Levels lists the levels from the most to the least severe.
var Levels = []string{LevelCritical, LevelRelevant, LevelInformational}

// The thresholds of the alert rules. They are named constants in this one
// table, documented in docs/DASHBOARD.md, and not configurable in Gate G5.
// "From" means >=, "below" means <, "above" means >.
const (
	CPUTemperatureRelevantC = 70.0 // CPU temperature from this value: relevant
	CPUTemperatureCriticalC = 80.0 // CPU temperature from this value: critical

	MemoryAvailableRelevantPercent = 15.0 // available memory below this share of the total: relevant
	MemoryAvailableCriticalPercent = 8.0  // below this share: critical

	// SwapUsedRelevantPercent is the share of the swap total that counts as a
	// lot. Pages that the kernel parks in swap stay there without any swap
	// activity, so a small occupied share is normal and raises nothing (D-C38).
	SwapUsedRelevantPercent = 25.0 // swap occupied from this share of the swap total: relevant

	StorageRelevantPercent = 85.0 // a real file system used above this share: relevant
	StorageCriticalPercent = 95.0 // above this share: critical
)

// The load rule has no constant: the one-minute load average above the number
// of logical cores is relevant.

// Message is a translation key with parameters. Go never builds user text.
// Parameters are numbers (rounded to one decimal) or short strings.
type Message struct {
	Key    string         `json:"key"`
	Params map[string]any `json:"params,omitempty"`
}

// Alert is one prioritised hint: what is wrong, what follows from it, and what
// to do next (ORB-DASH-005). ID is stable for the same rule and subject (for
// storage, the mount point); the level may change while the ID stays.
type Alert struct {
	ID       string  `json:"id"`
	Level    string  `json:"level"`
	Cause    Message `json:"cause"`
	Impact   Message `json:"impact"`
	NextStep Message `json:"next_step"`
}

// AlertContext carries what the rules need besides the snapshot.
type AlertContext struct {
	// SupportTier is the tier from device detection ("generic", "compatible", ...).
	SupportTier string
}

// Evaluate applies all rules to a snapshot. The result is never nil, sorted by
// level (critical first) and then in the fixed order of the rules below.
func Evaluate(s Snapshot, ctx AlertContext) []Alert {
	alerts := []Alert{}
	add := func(a Alert) { alerts = append(alerts, a) }

	// 1. CPU temperature: the hottest sensor that stands for the processor.
	if t, ok := hottestCPU(s.Temperatures); ok {
		switch {
		case t >= CPUTemperatureCriticalC:
			add(leveled("cpu_temperature", LevelCritical, map[string]any{"celsius": round(t, 1), "threshold": CPUTemperatureCriticalC}))
		case t >= CPUTemperatureRelevantC:
			add(leveled("cpu_temperature", LevelRelevant, map[string]any{"celsius": round(t, 1), "threshold": CPUTemperatureRelevantC}))
		}
	}

	// 2. Memory available (not "free": cache that can be dropped counts as available).
	if m := s.Memory; m.TotalBytes != nil && m.AvailableBytes != nil && *m.TotalBytes > 0 {
		pct := float64(*m.AvailableBytes) / float64(*m.TotalBytes) * 100
		switch {
		case pct < MemoryAvailableCriticalPercent:
			add(leveled("memory_available", LevelCritical, map[string]any{"percent": round(pct, 1), "threshold": MemoryAvailableCriticalPercent}))
		case pct < MemoryAvailableRelevantPercent:
			add(leveled("memory_available", LevelRelevant, map[string]any{"percent": round(pct, 1), "threshold": MemoryAvailableRelevantPercent}))
		}
	}

	// 3. Swap occupied: a share of the swap total. Occupied is not active, so
	// the texts do not claim that the system is swapping. An unknown or zero
	// swap total (no swap configured) raises nothing.
	if m := s.Memory; m.SwapTotalBytes != nil && m.SwapUsedBytes != nil && *m.SwapTotalBytes > 0 {
		pct := float64(*m.SwapUsedBytes) / float64(*m.SwapTotalBytes) * 100
		if pct >= SwapUsedRelevantPercent {
			add(simple("swap_in_use", LevelRelevant, map[string]any{"percent": round(pct, 1), "used_mib": round(float64(*m.SwapUsedBytes)/(1<<20), 1)}))
		}
	}

	// 4. Storage: every real file system on its own.
	for _, st := range s.Storage {
		params := func(threshold float64) map[string]any {
			return map[string]any{"mount": st.MountPoint, "percent": st.UsedPercent, "threshold": threshold}
		}
		var a Alert
		switch {
		case st.UsedPercent > StorageCriticalPercent:
			a = leveled("storage_usage", LevelCritical, params(StorageCriticalPercent))
		case st.UsedPercent > StorageRelevantPercent:
			a = leveled("storage_usage", LevelRelevant, params(StorageRelevantPercent))
		default:
			continue
		}
		a.ID += ":" + st.MountPoint
		add(a)
	}

	// 5. Network: interfaces exist (loopback excluded) but none has a link. A
	// system that lists no interface at all is not alerted: that is far more
	// often a sysfs that could not be read than a device without a network.
	linkUp := false
	for _, n := range s.Network {
		linkUp = linkUp || n.LinkUp
	}
	if len(s.Network) > 0 && !linkUp {
		add(simple("network_no_link", LevelRelevant, nil))
	}

	// 6. Load: the one-minute average above the number of logical cores.
	if s.CPU.Load != nil && s.CPU.Cores != nil && *s.CPU.Cores > 0 && s.CPU.Load.One > float64(*s.CPU.Cores) {
		add(simple("cpu_load", LevelRelevant, map[string]any{"load": s.CPU.Load.One, "cores": *s.CPU.Cores}))
	}

	// 7. Informational: Generic Mode from device detection. The support tier
	// itself ("Compatible", "Verified", ...) is shown in the Home header and on
	// the Device page, not as an alert (D-C44).
	if ctx.SupportTier == "generic" {
		add(simple("support_generic", LevelInformational, nil))
	}

	sort.SliceStable(alerts, func(i, j int) bool { return levelRank(alerts[i].Level) < levelRank(alerts[j].Level) })
	return alerts
}

func levelRank(l string) int {
	for i, x := range Levels {
		if x == l {
			return i
		}
	}
	return len(Levels)
}

func hottestCPU(ts []Temperature) (float64, bool) {
	best, ok := 0.0, false
	for _, t := range ts {
		if t.CPU && t.Celsius != nil && (!ok || *t.Celsius > best) {
			best, ok = *t.Celsius, true
		}
	}
	return best, ok
}

// leveled builds an alert whose texts differ by level:
// alert.<rule>.<level>.cause, .impact and .next.
func leveled(rule, level string, params map[string]any) Alert {
	return build(rule, level, fmt.Sprintf("alert.%s.%s", rule, level), params)
}

// simple builds an alert with one wording: alert.<rule>.cause, .impact and .next.
func simple(rule, level string, params map[string]any) Alert {
	return build(rule, level, "alert."+rule, params)
}

func build(id, level, prefix string, params map[string]any) Alert {
	return Alert{
		ID: id, Level: level,
		Cause:    Message{Key: prefix + ".cause", Params: params},
		Impact:   Message{Key: prefix + ".impact", Params: params},
		NextStep: Message{Key: prefix + ".next", Params: params},
	}
}

// AlertKeys lists every translation key the rules can produce. Tests check
// that both language files have all of them.
func AlertKeys() []string {
	var keys []string
	for _, r := range []struct {
		rule   string
		levels []string
	}{
		{"cpu_temperature", []string{LevelRelevant, LevelCritical}},
		{"memory_available", []string{LevelRelevant, LevelCritical}},
		{"storage_usage", []string{LevelRelevant, LevelCritical}},
		{"swap_in_use", nil}, {"network_no_link", nil}, {"cpu_load", nil},
		{"support_generic", nil},
	} {
		for _, part := range []string{"cause", "impact", "next"} {
			if r.levels == nil {
				keys = append(keys, fmt.Sprintf("alert.%s.%s", r.rule, part))
				continue
			}
			for _, l := range r.levels {
				keys = append(keys, fmt.Sprintf("alert.%s.%s.%s", r.rule, l, part))
			}
		}
	}
	return keys
}
