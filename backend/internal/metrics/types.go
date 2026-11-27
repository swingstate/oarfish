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
	Routing       Routing            `json:"routing"`
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
	// IPv4CIDR and IPv6CIDR are the same addresses in the same order with
	// their prefix length ("192.0.2.15/24"); added beside the plain lists
	// (task N1). The IPv4 prefix is that of the connected route the address
	// was assigned by, the IPv6 prefix that of /proc/net/if_inet6.
	IPv4CIDR []string `json:"ipv4_cidr"`
	IPv6CIDR []string `json:"ipv6_cidr"`
	// Signal is the Wi-Fi signal from /proc/net/wireless, read with every
	// sample; null for an interface that is not Wi-Fi or that the file does
	// not list (for example not associated). Task N1.
	Signal *WiFiSignal `json:"signal"`
}

// WiFiSignal is the signal of a Wi-Fi interface. LevelDBm is null unless the
// driver reports the level in dBm; LinkQuality is the driver's link quality
// on the driver's own scale (the file names no maximum), null when unreadable.
type WiFiSignal struct {
	LevelDBm    *int `json:"level_dbm"`
	LinkQuality *int `json:"link_quality"`
}

// Routing is the default route of each address family and the DNS servers
// (ORB-NET-001, ADR 0008 amendment "N0 as implemented"). Each part fails on
// its own: a source that cannot be read gives that part the status
// "unreadable" and leaves the others as they are.
type Routing struct {
	IPv4 DefaultRoute `json:"ipv4"`
	IPv6 DefaultRoute `json:"ipv6"`
	DNS  DNS          `json:"dns"`
}

// Status values of a routing part.
const (
	// RoutingFound means the source was read and holds a value.
	RoutingFound = "found"
	// RoutingNone means the source was read and holds no value (no default
	// route of this family, no usable nameserver line).
	RoutingNone = "none"
	// RoutingUnreadable means the source is missing, cannot be read or is
	// larger than its read limit.
	RoutingUnreadable = "unreadable"
)

// DefaultRoute is the default route of one family. Interface and Gateway are
// null unless Status is "found"; Gateway is also null for a route without a
// gateway, and when GatewayHidden is true. A gateway built from a hardware
// address (EUI-64) contains the MAC address of the router and is never shown.
// An IPv6 link-local gateway carries its interface as the zone ("fe80::1%wlan0").
type DefaultRoute struct {
	Status        string  `json:"status"`
	Interface     *string `json:"interface"`
	Gateway       *string `json:"gateway"`
	GatewayHidden bool    `json:"gateway_hidden"`
}

// DNS is the list of name servers from /etc/resolv.conf, at most
// parse.MaxNameservers, in file order. Hidden counts the servers among them
// whose address is built from a hardware address and is therefore not shown.
// ViaLocalResolver is true when every server of /etc/resolv.conf is a
// loopback address (a resolver stub on the device) and the servers come from
// /run/systemd/resolve/resolv.conf instead: the servers that stub forwards to,
// used through it (task N1, docs/UBUNTU_PROBE.md U3).
type DNS struct {
	Status           string      `json:"status"`
	Servers          []DNSServer `json:"servers"`
	Hidden           int         `json:"hidden"`
	ViaLocalResolver bool        `json:"via_local_resolver"`
}

// DNSServer is one name server. Local is true for a loopback address: a
// resolver on the device itself (for example a stub at 127.0.0.53), which
// hides the servers it forwards to.
type DNSServer struct {
	Address string `json:"address"`
	Local   bool   `json:"local"`
}

// Limits that keep a snapshot, and the work to build it, small.
const (
	MaxSensors    = 16
	MaxMounts     = 8
	MaxInterfaces = 8
	MaxAddresses  = 4 // per interface and family
)
