// Package hardware identifies the board and the CPU architecture. It reads the
// device tree, /proc/cpuinfo, /proc/meminfo and DMI through an fs.FS and never
// looks at the operating system: a Raspberry Pi is the same Raspberry Pi under
// Raspberry Pi OS and under Ubuntu (ORB-HW-002). It does not import
// operating_system and runs no program (ORB-HW-006).
package hardware

import (
	"io/fs"
	"runtime"

	"github.com/swingstate/oarfish/backend/internal/platform/source"
)

// Class is the kind of hardware knowledge that produced an Info.
type Class string

const (
	ClassRaspberryPi Class = "raspberry_pi"
	ClassGenericARM  Class = "generic_arm64"
	// ClassOther is any system that is not ARM64; it is never given a board.
	ClassOther Class = "other"
)

// Env is the platform the process runs on. Production uses HostEnv; tests
// inject others to exercise non-ARM and non-Linux systems.
type Env struct {
	GOOS   string
	GOARCH string
}

// HostEnv returns the platform this binary was built for.
func HostEnv() Env { return Env{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH} }

// Info is everything hardware detection knows. Empty strings and zero values
// mean "not detected". Nothing here identifies an individual device: there is
// no serial number, MAC address or machine id (docs/DETECTION.md, privacy).
type Info struct {
	Class            Class
	GOOS             string
	Arch             string
	Vendor           string
	Model            string
	SoCFamily        string
	Compatible       []string
	RevisionCode     string
	MemoryTotalBytes uint64
}

// Size limits for the files read here. They are generous for real systems and
// small enough that a hostile file cannot cost real memory.
const (
	maxDeviceTreeFile = 8 << 10
	maxCPUInfo        = 1 << 20
	maxMemInfo        = 64 << 10
	maxDMIFile        = 512
	maxCompatible     = 32
	maxTextRunes      = 128
)

// Detect reads the hardware identity from fsys. It never fails and never
// panics: whatever cannot be read stays empty.
func Detect(fsys fs.FS, env Env) Info {
	info := Info{Class: ClassOther, GOOS: env.GOOS, Arch: env.GOARCH}
	info.MemoryTotalBytes = readMemTotal(fsys)

	if env.GOARCH != "arm64" {
		// Board knowledge is only applied to ARM64; other architectures get
		// the architecture and memory and are classified Unsupported later.
		return info
	}

	dt := readDeviceTree(fsys)
	cpu := readCPUInfo(fsys)
	if !detectRaspberryPi(&info, dt, cpu) {
		detectGenericARM64(&info, fsys, dt)
	}
	return info
}

func readMemTotal(fsys fs.FS) uint64 {
	data, err := source.ReadFile(fsys, "proc/meminfo", maxMemInfo)
	if err != nil {
		return 0
	}
	return ParseMemTotal(data)
}
