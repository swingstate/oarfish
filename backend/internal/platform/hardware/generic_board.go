package hardware

import (
	"io/fs"

	"github.com/swingstate/oarfish/backend/internal/platform/source"
)

// detectGenericARM64 describes an ARM64 system that no board-specific code
// recognises. It uses only what standard Linux interfaces offer: the device
// tree model and, on ACPI systems, the DMI vendor and product name. It reads
// no DMI serial number or UUID.
func detectGenericARM64(info *Info, fsys fs.FS, dt deviceTree) {
	info.Class = ClassGenericARM
	info.Model = dt.Model
	info.Compatible = dt.Compatible
	if v, err := source.ReadFile(fsys, "sys/class/dmi/id/sys_vendor", maxDMIFile); err == nil {
		info.Vendor = source.Clean(string(v), 64)
	}
	if info.Model == "" {
		if p, err := source.ReadFile(fsys, "sys/class/dmi/id/product_name", maxDMIFile); err == nil {
			info.Model = source.Clean(string(p), maxTextRunes)
		}
	}
}
