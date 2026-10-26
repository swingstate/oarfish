package hardware

import "strings"

// detectRaspberryPi fills info when the device tree or cpuinfo says this is a
// Raspberry Pi. All Raspberry Pi knowledge comes from /proc/device-tree and
// /proc/cpuinfo; no Raspberry Pi tool is run (ORB-HW-006). It reports false
// for anything else.
func detectRaspberryPi(info *Info, dt deviceTree, cpu cpuInfo) bool {
	if !isRaspberryPi(dt, cpu) {
		return false
	}
	info.Class = ClassRaspberryPi
	info.Vendor = "Raspberry Pi"
	info.Model = dt.Model
	if info.Model == "" {
		info.Model = cpu.Model
	}
	info.Compatible = dt.Compatible
	info.SoCFamily = broadcomSoC(dt.Compatible)
	info.RevisionCode = cpu.Revision
	return true
}

func isRaspberryPi(dt deviceTree, cpu cpuInfo) bool {
	for _, c := range dt.Compatible {
		if strings.HasPrefix(c, "raspberrypi,") {
			return true
		}
	}
	return strings.HasPrefix(dt.Model, "Raspberry Pi ") || strings.HasPrefix(cpu.Model, "Raspberry Pi ")
}

// broadcomSoC turns the "brcm,bcm2837" style compatible string into "BCM2837".
// The device tree is the source of truth; /proc/cpuinfo "Hardware" is not
// used because it reports BCM2835 on most Raspberry Pi models.
func broadcomSoC(compatible []string) string {
	for _, c := range compatible {
		rest, ok := strings.CutPrefix(c, "brcm,bcm")
		if !ok || len(rest) < 4 {
			continue
		}
		digits := rest[:4]
		if strings.Trim(digits, "0123456789") != "" {
			continue
		}
		return "BCM" + strings.ToUpper(rest)
	}
	return ""
}
