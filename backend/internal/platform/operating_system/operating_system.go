// Package operating_system identifies the operating system and kernel and
// selects an OS adapter. It reads /etc/os-release and a few marker files
// through an fs.FS; it knows nothing about boards and does not import
// hardware (ORB-HW-002, ORB-HW-010).
package operating_system

import (
	"io/fs"

	"github.com/swingstate/oarfish/backend/internal/platform/source"
)

// Adapter status values. Nothing is "verified" in G4: the status says how far
// qualification of that OS has come, not that it was tested.
const (
	StatusInitialTarget   = "initial-target"
	StatusNotYetQualified = "not-yet-qualified"
	StatusGeneric         = "generic"
	StatusUnrecognised    = "unrecognised"
)

// Adapter is the boundary for operating-system specific behaviour. In G4 an
// adapter only recognises its system; package, network and service adapters
// come with later gates. An adapter must not know about hardware.
type Adapter interface {
	// Name is the stable adapter id used in the API.
	Name() string
	// Status says how far qualification of this OS has come.
	Status() string
	// Matches reports whether the adapter handles this system.
	Matches(rel Release, fsys fs.FS) bool
}

// adapters in match order: the most specific first.
var adapters = []Adapter{raspberryPiOS{}, ubuntu{}, debianFamily{}}

// Info is the detected operating system and kernel.
type Info struct {
	ID            string
	IDLike        []string
	VersionID     string
	PrettyName    string
	Adapter       string
	AdapterStatus string
	Kernel        string
}

const maxKernelRelease = 256

// Detect reads the operating system identity from fsys. It never fails and
// never panics. /etc/os-release is primary; /usr/lib/os-release is used when
// the primary file is missing, too large or has no usable ID.
func Detect(fsys fs.FS) Info {
	rel := readRelease(fsys)
	info := Info{
		ID:         rel.ID,
		IDLike:     rel.IDLike,
		VersionID:  rel.VersionID,
		PrettyName: rel.PrettyName,
		Kernel:     readKernelRelease(fsys),
	}
	if info.PrettyName == "" {
		info.PrettyName = rel.Name
	}
	info.Adapter, info.AdapterStatus = selectAdapter(rel, fsys)
	return info
}

func selectAdapter(rel Release, fsys fs.FS) (name, status string) {
	for _, a := range adapters {
		if a.Matches(rel, fsys) {
			return a.Name(), a.Status()
		}
	}
	return "generic_linux", StatusUnrecognised
}

func readRelease(fsys fs.FS) Release {
	for _, name := range []string{"etc/os-release", "usr/lib/os-release"} {
		data, err := source.ReadFile(fsys, name, maxOSReleaseFile)
		if err != nil {
			continue
		}
		if rel := ParseOSRelease(data); rel.ID != "" {
			return rel
		}
	}
	return Release{}
}

func readKernelRelease(fsys fs.FS) string {
	data, err := source.ReadFile(fsys, "proc/sys/kernel/osrelease", maxKernelRelease)
	if err != nil {
		return ""
	}
	return validVersionID(source.Clean(string(data), 64))
}

func hasID(rel Release, id string) bool {
	if rel.ID == id {
		return true
	}
	for _, l := range rel.IDLike {
		if l == id {
			return true
		}
	}
	return false
}

// AdapterNames returns the ids of all adapters, including the generic fallback.
func AdapterNames() []string {
	names := make([]string, 0, len(adapters)+1)
	for _, a := range adapters {
		names = append(names, a.Name())
	}
	return append(names, "generic_linux")
}
