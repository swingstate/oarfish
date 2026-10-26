package capabilities

import (
	"bytes"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/swingstate/oarfish/backend/internal/platform/source"
)

// Boot medium types.
const (
	MediumMMC     = "mmc"
	MediumNVMe    = "nvme"
	MediumUSB     = "usb"
	MediumUnknown = "unknown"
)

// BootMedium is the kind of device the root file system is on.
type BootMedium struct {
	Type string
}

const maxMountInfo = 1 << 20

// detectBootMedium works out the device behind "/" from /proc/self/mountinfo
// and /sys/dev/block. /proc/mounts alone is not enough: on Raspberry Pi OS the
// root device often shows up as /dev/root. The major:minor numbers in
// mountinfo identify it, and /sys/dev/block/<major>:<minor> names it. Paths
// are built from those two integers only, never from file content.
func detectBootMedium(fsys fs.FS, list []Capability) (BootMedium, []Capability) {
	const probe = "/proc/self/mountinfo, /sys/dev/block"
	unknown := BootMedium{Type: MediumUnknown}

	data, err := source.ReadFile(fsys, "proc/self/mountinfo", maxMountInfo)
	if err != nil {
		return unknown, append(list, Capability{ID: BootMediumID, Source: probe, Reason: ReasonNoRootMount})
	}
	root, ok := parseRootMount(data)
	if !ok {
		return unknown, append(list, Capability{ID: BootMediumID, Source: probe, Reason: ReasonNoRootMount})
	}

	name := ""
	if root.major > 0 {
		name = deviceName(fsys, root.major, root.minor)
	}
	if name == "" {
		name = deviceNameFromSource(root.source)
	}
	typ := classify(fsys, name, root)
	if typ == MediumUnknown {
		return unknown, append(list, Capability{ID: BootMediumID, Source: probe, Reason: ReasonUnrecognised})
	}
	return BootMedium{Type: typ}, append(list, Capability{ID: BootMediumID, Available: true, Source: probe})
}

type rootMount struct {
	major, minor int
	source       string
}

// parseRootMount returns the topmost mount at "/".
func parseRootMount(data []byte) (rootMount, bool) {
	var found rootMount
	ok := false
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		fields := strings.Fields(string(line))
		sep := -1
		for i := 6; i < len(fields); i++ {
			if fields[i] == "-" {
				sep = i
				break
			}
		}
		if sep < 0 || len(fields) < sep+3 || fields[4] != "/" {
			continue
		}
		major, minor, valid := parseMajorMinor(fields[2])
		if !valid {
			continue
		}
		found = rootMount{major: major, minor: minor, source: fields[sep+2]}
		ok = true
	}
	return found, ok
}

func parseMajorMinor(s string) (major, minor int, ok bool) {
	a, b, found := strings.Cut(s, ":")
	if !found || len(a) > 6 || len(b) > 8 {
		return 0, 0, false
	}
	major, err1 := strconv.Atoi(a)
	minor, err2 := strconv.Atoi(b)
	if err1 != nil || err2 != nil || major < 0 || minor < 0 {
		return 0, 0, false
	}
	return major, minor, true
}

// deviceName reads DEVNAME from /sys/dev/block/<major>:<minor>/uevent.
func deviceName(fsys fs.FS, major, minor int) string {
	data, err := source.ReadFile(fsys, fmt.Sprintf("sys/dev/block/%d:%d/uevent", major, minor), 1024)
	if err != nil {
		return ""
	}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if v, ok := strings.CutPrefix(string(line), "DEVNAME="); ok {
			return validDeviceName(v)
		}
	}
	return ""
}

// deviceNameFromSource handles file systems without a block major number
// (for example btrfs) whose mount source is /dev/<name>.
func deviceNameFromSource(src string) string {
	name, ok := strings.CutPrefix(src, "/dev/")
	if !ok || name == "root" {
		return ""
	}
	return validDeviceName(name)
}

func validDeviceName(s string) string {
	if s == "" || len(s) > 32 {
		return ""
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return ""
		}
	}
	return s
}

func classify(fsys fs.FS, name string, root rootMount) string {
	switch {
	case strings.HasPrefix(name, "mmcblk"):
		return MediumMMC
	case strings.HasPrefix(name, "nvme"):
		return MediumNVMe
	case strings.HasPrefix(name, "sd") && root.major > 0:
		// sdX is SCSI, SATA or USB; the sysfs link shows the bus it hangs off.
		target, err := fs.ReadLink(fsys, fmt.Sprintf("sys/dev/block/%d:%d", root.major, root.minor))
		if err == nil && onUSBBus(target) {
			return MediumUSB
		}
	}
	return MediumUnknown
}

// onUSBBus reports whether a sysfs device path has a "usbN" component.
func onUSBBus(link string) bool {
	for _, part := range strings.Split(link, "/") {
		digits, ok := strings.CutPrefix(part, "usb")
		if ok && digits != "" && strings.Trim(digits, "0123456789") == "" {
			return true
		}
	}
	return false
}
