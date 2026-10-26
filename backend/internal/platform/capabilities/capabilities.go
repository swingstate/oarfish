// Package capabilities finds out what the running system can do by probing
// standard Linux interfaces (procfs, sysfs, hwmon, systemd, journald, cgroups).
// It never looks at the distribution name and never runs a program. What the
// GUI may offer follows from these results (ORB-GEN-004, ORB-HW-004).
package capabilities

import (
	"io/fs"

	"github.com/swingstate/oarfish/backend/internal/platform/source"
)

// Capability ids. They are part of the API and of the GUI translation files.
const (
	Procfs         = "procfs"
	Sysfs          = "sysfs"
	Systemd        = "systemd"
	Journald       = "journald"
	CgroupV2       = "cgroup_v2"
	CPUTemperature = "cpu_temperature"
	BootMediumID   = "boot_medium"
)

// All lists every capability id in the order Detect reports them.
var All = []string{Procfs, Sysfs, Systemd, Journald, CgroupV2, CPUTemperature, BootMediumID}

// Minimum are the capabilities without which Oarfish refuses partial
// operation (support tier "unsupported", Pflichtenheft 3.6). systemd is
// included because the installer and the service model need it (spec 3.3).
var Minimum = []string{Procfs, Sysfs, Systemd}

// Reasons for an unavailable capability.
const (
	ReasonNotFound         = "not_found"
	ReasonImplausible      = "implausible_value"
	ReasonUnrecognised     = "unrecognised_device"
	ReasonNoRootMount      = "no_root_mount"
	ReasonNoSensor         = "no_sensor"
	ReasonSensorUnreadable = "sensor_unreadable"
)

// Capability is one probed ability. Source says what was probed (a path or a
// path pattern, never data read from the system); Reason is a machine-readable
// code, set only when the capability is unavailable.
type Capability struct {
	ID        string
	Available bool
	Source    string
	Reason    string
}

// Result is the complete outcome of capability detection.
type Result struct {
	List        []Capability
	Temperature *Sensor // nil if no usable sensor was found
	BootMedium  BootMedium
}

// Get returns the capability with the given id; ok is false if it is unknown.
func (r Result) Get(id string) (Capability, bool) {
	for _, c := range r.List {
		if c.ID == id {
			return c, true
		}
	}
	return Capability{}, false
}

// Available reports whether the capability exists and is available.
func (r Result) Available(id string) bool {
	c, ok := r.Get(id)
	return ok && c.Available
}

// Detect probes fsys. It never fails and never panics.
func Detect(fsys fs.FS) Result {
	var r Result
	r.List = append(r.List,
		probeFile(fsys, Procfs, "proc/meminfo"),
		probeDir(fsys, Sysfs, "sys/class"),
		// Same test as sd_booted(3): systemd is the running init system.
		probeDir(fsys, Systemd, "run/systemd/system"),
		probeFile(fsys, Journald, "run/systemd/journal/socket"),
		probeFile(fsys, CgroupV2, "sys/fs/cgroup/cgroup.controllers"),
	)

	r.Temperature, r.List = detectTemperature(fsys, r.List)
	r.BootMedium, r.List = detectBootMedium(fsys, r.List)
	return r
}

func probeFile(fsys fs.FS, id, path string) Capability {
	if source.Exists(fsys, path) {
		return Capability{ID: id, Available: true, Source: "/" + path}
	}
	return Capability{ID: id, Source: "/" + path, Reason: ReasonNotFound}
}

func probeDir(fsys fs.FS, id, path string) Capability {
	if source.IsDir(fsys, path) {
		return Capability{ID: id, Available: true, Source: "/" + path}
	}
	return Capability{ID: id, Source: "/" + path, Reason: ReasonNotFound}
}
