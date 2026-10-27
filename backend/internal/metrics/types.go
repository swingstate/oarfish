package metrics

import "time"

// SchemaVersion is the version of the JSON layout of Snapshot, Alert and
// History. Adding a field does not change it; changing or removing one does.
const SchemaVersion = 1

// Snapshot is one reading of the device. It is the wire format of the
// "metrics" event and of GET /api/v1/metrics. A value that could not be read
// is null; lists are never null. It carries nothing that identifies a person:
// no MAC address, serial number, machine id, user name, environment or process.
// The host name and the IP addresses are shown on the live page only.
type Snapshot struct {
	SchemaVersion int                `json:"schema_version"`
	CollectedAt   time.Time          `json:"collected_at"`
	Hostname      *string            `json:"hostname"`
	UptimeSeconds *float64           `json:"uptime_seconds"`
	CPU           CPU                `json:"cpu"`
	Memory        Memory             `json:"memory"`
	Temperatures  []Temperature      `json:"temperatures"`
	Storage       []Storage          `json:"storage"`
	Network       []NetworkInterface `json:"network"`
}

// CPU describes load, usage and frequency.
type CPU struct {
	UsagePercent *float64  `json:"usage_percent"` // since the previous sample; null for the first
	Cores        *int      `json:"cores"`
	Load         *Load     `json:"load"`
	Frequency    Frequency `json:"frequency"`
	Governor     *string   `json:"governor"`
}

// Load is the load average over 1, 5 and 15 minutes.
type Load struct {
	One     float64 `json:"one"`
	Five    float64 `json:"five"`
	Fifteen float64 `json:"fifteen"`
}

// Frequency is the CPU frequency in MHz, null when the kernel exposes no cpufreq.
type Frequency struct {
	CurrentMHz *float64 `json:"current_mhz"`
	MaxMHz     *float64 `json:"max_mhz"`
}

// Memory is in bytes. UsedBytes is total minus available, the figure that
// matters for "is there memory left", not the raw "used" of older tools.
type Memory struct {
	TotalBytes     *uint64 `json:"total_bytes"`
	AvailableBytes *uint64 `json:"available_bytes"`
	UsedBytes      *uint64 `json:"used_bytes"`
	SwapTotalBytes *uint64 `json:"swap_total_bytes"`
	SwapUsedBytes  *uint64 `json:"swap_used_bytes"`
}

// Temperature is one sensor. ID is stable and built from the sensor's
// directory name; Name is the cleaned type, name or label the driver reports.
// CPU marks the sensor that stands for the processor (the one the alerts use).
type Temperature struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Source  string   `json:"source"` // "thermal_zone" or "hwmon"
	CPU     bool     `json:"cpu"`
	Celsius *float64 `json:"celsius"`
}

// Storage is a mounted file system of a real storage type.
type Storage struct {
	MountPoint     string  `json:"mount_point"`
	FSType         string  `json:"fs_type"`
	TotalBytes     uint64  `json:"total_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedPercent    float64 `json:"used_percent"`
}

// Network interface kinds.
const (
	KindEthernet = "ethernet"
	KindWiFi     = "wifi"
	KindOther    = "other"
)

// NetworkInterface is a network interface other than loopback. The hardware
// address is never read.
type NetworkInterface struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	State     string   `json:"state"` // the kernel's operstate: up, down, unknown, dormant, ...
	LinkUp    bool     `json:"link_up"`
	SpeedMbps *int     `json:"speed_mbps"`
	RxBytes   *uint64  `json:"rx_bytes"`
	TxBytes   *uint64  `json:"tx_bytes"`
	IPv4      []string `json:"ipv4"`
	IPv6      []string `json:"ipv6"`
}

// Limits that keep a snapshot, and the work to build it, small.
const (
	MaxSensors    = 16
	MaxMounts     = 8
	MaxInterfaces = 8
	MaxAddresses  = 4 // per interface and family
)
