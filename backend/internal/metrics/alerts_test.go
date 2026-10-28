package metrics

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// healthy is a snapshot that raises no alert (with a link up and a known tier of "").
func healthy() Snapshot {
	total, avail, used := uint64(1000<<20), uint64(600<<20), uint64(400<<20)
	swapTotal, swapUsed := uint64(100<<20), uint64(0)
	cores := 4
	temp := 50.0
	return Snapshot{
		SchemaVersion: SchemaVersion,
		CPU:           CPU{Cores: &cores, Load: &Load{One: 0.5}},
		Memory:        Memory{TotalBytes: &total, AvailableBytes: &avail, UsedBytes: &used, SwapTotalBytes: &swapTotal, SwapUsedBytes: &swapUsed},
		Temperatures:  []Temperature{{ID: "thermal_zone0", Name: "cpu-thermal", CPU: true, Celsius: &temp}},
		Storage:       []Storage{{MountPoint: "/", FSType: "ext4", UsedPercent: 40}},
		Network:       []NetworkInterface{{Name: "wlan0", State: "up", LinkUp: true}},
	}
}

func ids(as []Alert) string {
	var out []string
	for _, a := range as {
		out = append(out, a.ID+"@"+a.Level)
	}
	return strings.Join(out, " ")
}

func TestHealthySystemRaisesNoAlert(t *testing.T) {
	got := Evaluate(healthy(), AlertContext{SupportTier: "verified"})
	if got == nil || len(got) != 0 {
		t.Errorf("alerts = %s (nil: %v)", ids(got), got == nil)
	}
}

// ORB-DASH-005: every rule and every threshold edge. "From" is >=, "below" is <,
// "above" is >; the edge value belongs to the side the table says.
func TestAlertThresholds(t *testing.T) {
	setTemp := func(c float64) func(*Snapshot) { return func(s *Snapshot) { s.Temperatures[0].Celsius = &c } }
	setAvail := func(percent float64) func(*Snapshot) {
		return func(s *Snapshot) {
			a := uint64(percent * float64(*s.Memory.TotalBytes) / 100)
			s.Memory.AvailableBytes = &a
		}
	}
	setSwap := func(used, total uint64) func(*Snapshot) {
		return func(s *Snapshot) { s.Memory.SwapUsedBytes, s.Memory.SwapTotalBytes = &used, &total }
	}
	const mib = uint64(1 << 20)
	setStorage := func(p float64) func(*Snapshot) { return func(s *Snapshot) { s.Storage[0].UsedPercent = p } }
	setLoad := func(l float64) func(*Snapshot) { return func(s *Snapshot) { s.CPU.Load.One = l } }

	tests := []struct {
		name   string
		change func(*Snapshot)
		want   string
	}{
		{"temperature 69.9", setTemp(69.9), ""},
		{"temperature 70 is relevant", setTemp(70), "cpu_temperature@relevant"},
		{"temperature 79.9", setTemp(79.9), "cpu_temperature@relevant"},
		{"temperature 80 is critical", setTemp(80), "cpu_temperature@critical"},
		{"temperature 110", setTemp(110), "cpu_temperature@critical"},
		{"temperature unknown", func(s *Snapshot) { s.Temperatures[0].Celsius = nil }, ""},
		{"no sensors", func(s *Snapshot) { s.Temperatures = []Temperature{} }, ""},

		{"memory 15.5% available", setAvail(15.5), ""},
		{"memory exactly 15% is not below", setAvail(15), ""},
		{"memory 14.9% is relevant", setAvail(14.9), "memory_available@relevant"},
		{"memory exactly 8% is not below", setAvail(8), "memory_available@relevant"},
		{"memory 7.9% is critical", setAvail(7.9), "memory_available@critical"},
		{"memory 0% is critical", setAvail(0), "memory_available@critical"},
		{"memory unknown", func(s *Snapshot) { s.Memory = Memory{} }, ""},
		{"memory total zero", func(s *Snapshot) { z := uint64(0); s.Memory.TotalBytes = &z }, ""},

		{"swap unused", setSwap(0, 400*mib), ""},
		{"swap just below 25%", setSwap(100*mib-1, 400*mib), ""},
		{"swap exactly 25% is relevant", setSwap(100*mib, 400*mib), "swap_in_use@relevant"},
		{"swap above 25%", setSwap(100*mib+1, 400*mib), "swap_in_use@relevant"},
		{"swap full", setSwap(400*mib, 400*mib), "swap_in_use@relevant"},
		{"swap total zero", setSwap(0, 0), ""},
		{"swap total zero with a used value", setSwap(5*mib, 0), ""},
		{"swap total unknown", func(s *Snapshot) { s.Memory.SwapTotalBytes = nil; u := 400 * mib; s.Memory.SwapUsedBytes = &u }, ""},
		{"swap used unknown", func(s *Snapshot) { s.Memory.SwapUsedBytes = nil }, ""},
		{"swap 1.5 MiB of 415 MiB, as parked on the real device", setSwap(mib+mib/2, 415*mib), ""},

		{"storage 85%", setStorage(85), ""},
		{"storage 85.1% is relevant", setStorage(85.1), "storage_usage:/@relevant"},
		{"storage 95%", setStorage(95), "storage_usage:/@relevant"},
		{"storage 95.1% is critical", setStorage(95.1), "storage_usage:/@critical"},
		{"storage 100% is critical", setStorage(100), "storage_usage:/@critical"},
		{"no storage", func(s *Snapshot) { s.Storage = []Storage{} }, ""},

		{"link up", func(s *Snapshot) {}, ""},
		{"all interfaces down", func(s *Snapshot) { s.Network[0].LinkUp = false; s.Network[0].State = "down" }, "network_no_link@relevant"},
		{"no interface at all is unknown, not an alert", func(s *Snapshot) { s.Network = []NetworkInterface{} }, ""},
		{"one of two is up", func(s *Snapshot) { s.Network = append(s.Network, NetworkInterface{Name: "eth0", State: "down"}) }, ""},

		{"load equals cores", setLoad(4), ""},
		{"load above cores", setLoad(4.01), "cpu_load@relevant"},
		{"load unknown", func(s *Snapshot) { s.CPU.Load = nil }, ""},
		{"cores unknown", func(s *Snapshot) { s.CPU.Cores = nil; s.CPU.Load.One = 99 }, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := healthy()
			tc.change(&s)
			if got := ids(Evaluate(s, AlertContext{SupportTier: "verified"})); got != tc.want {
				t.Errorf("alerts = %q, want %q", got, tc.want)
			}
		})
	}
}

// Only Generic Mode raises an informational alert; the tier itself is shown in
// the header and on the Device page (D-C44).
func TestInformationalAlertsFollowTheSupportTier(t *testing.T) {
	for tier, want := range map[string]string{
		"generic":    "support_generic@informational",
		"compatible": "", "verified": "", "community": "", "experimental": "", "unsupported": "", "": "",
	} {
		if got := ids(Evaluate(healthy(), AlertContext{SupportTier: tier})); got != want {
			t.Errorf("tier %q: alerts = %q, want %q", tier, got, want)
		}
	}
}

// The CPU temperature rule uses the sensors that stand for the processor, and
// the hottest of them; other sensors (storage, GPU) never raise it.
func TestCPUTemperatureRuleUsesTheCPUSensors(t *testing.T) {
	hot, warm := 85.0, 72.0
	s := healthy()
	s.Temperatures = []Temperature{
		{ID: "hwmon0_temp1", Name: "nvme", Celsius: &hot},
		{ID: "hwmon1_temp1", Name: "cpu", CPU: true, Celsius: &warm},
	}
	if got := ids(Evaluate(s, AlertContext{})); got != "cpu_temperature@relevant" {
		t.Errorf("alerts = %q", got)
	}
	s.Temperatures[0].CPU = true
	if got := ids(Evaluate(s, AlertContext{})); got != "cpu_temperature@critical" {
		t.Errorf("alerts = %q", got)
	}
}

// Alerts are sorted by level, then in the fixed rule order; storage alerts are
// per file system with an ID that names the mount.
func TestAlertOrderIsByLevelThenRule(t *testing.T) {
	s := healthy()
	t90, load, swap := 90.0, 9.0, uint64(30<<20) // 30% of the 100 MiB swap
	avail := uint64(10 << 20)                    // 1%
	s.Temperatures[0].Celsius = &t90
	s.CPU.Load.One = load
	s.Memory.SwapUsedBytes = &swap
	s.Memory.AvailableBytes = &avail
	s.Storage = []Storage{
		{MountPoint: "/", UsedPercent: 90},
		{MountPoint: "/data", UsedPercent: 99},
		{MountPoint: "/boot", UsedPercent: 10},
	}
	s.Network[0].LinkUp = false
	got := ids(Evaluate(s, AlertContext{SupportTier: "generic"}))
	want := "cpu_temperature@critical memory_available@critical storage_usage:/data@critical " +
		"swap_in_use@relevant storage_usage:/@relevant network_no_link@relevant cpu_load@relevant " +
		"support_generic@informational"
	if got != want {
		t.Errorf("order\n got: %s\nwant: %s", got, want)
	}
}

// ORB-DASH-005: every alert names a cause, an impact and a next step as
// translation keys with parameters; Go builds no free text.
func TestEveryAlertHasCauseImpactAndNextStepAsKeys(t *testing.T) {
	s := healthy()
	hot, swap := 90.0, uint64(30<<20)
	avail := uint64(10 << 20)
	s.Temperatures[0].Celsius = &hot
	s.Memory.SwapUsedBytes = &swap
	s.Memory.AvailableBytes = &avail
	s.Storage[0].UsedPercent = 99.5
	s.CPU.Load.One = 8
	s.Network[0].LinkUp = false
	alerts := Evaluate(s, AlertContext{SupportTier: "generic"})
	if len(alerts) != 7 {
		t.Fatalf("expected 7 alerts, got %s", ids(alerts))
	}
	known := map[string]bool{}
	for _, k := range AlertKeys() {
		known[k] = true
	}
	for _, a := range alerts {
		if a.ID == "" || a.Level == "" {
			t.Errorf("incomplete alert %+v", a)
		}
		for part, m := range map[string]Message{"cause": a.Cause, "impact": a.Impact, "next": a.NextStep} {
			if !strings.HasPrefix(m.Key, "alert.") || !strings.HasSuffix(m.Key, "."+part) || !known[m.Key] {
				t.Errorf("%s: %s key %q is not a known translation key", a.ID, part, m.Key)
			}
			for k, v := range m.Params {
				switch v.(type) {
				case float64, int, string:
				default:
					t.Errorf("%s: parameter %s has type %T", a.ID, k, v)
				}
			}
		}
	}
	// The parameters carry the numbers the text needs.
	temp := alerts[0]
	if temp.ID != "cpu_temperature" || temp.Cause.Params["celsius"] != 90.0 || temp.Cause.Params["threshold"] != CPUTemperatureCriticalC {
		t.Errorf("temperature alert = %+v", temp)
	}
}

func TestAlertKeysAreUniqueAndComplete(t *testing.T) {
	keys := AlertKeys()
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k] {
			t.Errorf("duplicate key %s", k)
		}
		seen[k] = true
	}
	// 3 leveled rules x 2 levels x 3 parts + 4 simple rules x 3 parts.
	if want := 3*2*3 + 4*3; len(keys) != want {
		t.Errorf("%d keys, want %d", len(keys), want)
	}
}

func TestSameAlertsComparesParameters(t *testing.T) {
	a := Evaluate(withTemp(75), AlertContext{})
	b := Evaluate(withTemp(75), AlertContext{})
	c := Evaluate(withTemp(76), AlertContext{})
	if !sameAlerts(a, b) || sameAlerts(a, c) || sameAlerts(a, nil) || !sameAlerts(nil, []Alert{}) {
		t.Error("sameAlerts is wrong")
	}
	if !reflect.DeepEqual(a, b) {
		t.Error("equal inputs gave different alerts")
	}
}

func withTemp(c float64) Snapshot {
	s := healthy()
	s.Temperatures[0].Celsius = &c
	return s
}

// Thresholds are the documented values (docs/DASHBOARD.md).
func TestThresholdConstants(t *testing.T) {
	got := fmt.Sprint(CPUTemperatureRelevantC, CPUTemperatureCriticalC, MemoryAvailableRelevantPercent, MemoryAvailableCriticalPercent, StorageRelevantPercent, StorageCriticalPercent, SwapUsedRelevantPercent)
	if got != "70 80 15 8 85 95 25" {
		t.Errorf("thresholds changed: %s", got)
	}
}
