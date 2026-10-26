package capabilities

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/swingstate/oarfish/backend/internal/platform/platformtest"
)

func capByID(t *testing.T, r Result, id string) Capability {
	t.Helper()
	c, ok := r.Get(id)
	if !ok {
		t.Fatalf("capability %q is missing", id)
	}
	return c
}

// ORB-HW-004: every capability has an id, a source and, when unavailable, a reason.
func TestEveryCapabilityIsReportedWithSourceAndReason(t *testing.T) {
	for _, fixture := range []string{
		"pi-zero-2w-raspios", "pi4-raspios", "generic-arm64-debian", "generic-arm64-ubuntu",
		"unknown-arm64-minimal", "non-arm", "broken-hostile",
	} {
		r := Detect(platformtest.Fixture(t, fixture))
		if len(r.List) != len(All) {
			t.Fatalf("%s: %d capabilities, want %d", fixture, len(r.List), len(All))
		}
		for i, c := range r.List {
			if c.ID != All[i] {
				t.Errorf("%s: capability %d is %q, want %q", fixture, i, c.ID, All[i])
			}
			if c.Source == "" {
				t.Errorf("%s: %s has no source", fixture, c.ID)
			}
			if c.Available && c.Reason != "" {
				t.Errorf("%s: %s is available but has reason %q", fixture, c.ID, c.Reason)
			}
			if !c.Available && c.Reason == "" {
				t.Errorf("%s: %s is unavailable without a reason", fixture, c.ID)
			}
		}
	}
}

// AT-003: sensors and boot medium of the Raspberry Pi fixture.
func TestPiZero2WCapabilities(t *testing.T) {
	r := Detect(platformtest.Fixture(t, "pi-zero-2w-raspios"))
	for _, id := range All {
		if !r.Available(id) {
			t.Errorf("%s should be available: %+v", id, capByID(t, r, id))
		}
	}
	if r.BootMedium.Type != MediumMMC {
		t.Errorf("boot medium = %q, want mmc (root shown as /dev/root)", r.BootMedium.Type)
	}
	if c, ok := r.Temperature.Celsius(); !ok || c != 43.312 {
		t.Errorf("temperature = %v, %v", c, ok)
	}
	if got := capByID(t, r, CPUTemperature).Source; got != "/sys/class/thermal/thermal_zone0/temp" {
		t.Errorf("source = %q", got)
	}
}

func TestHwmonAndNVMe(t *testing.T) {
	r := Detect(platformtest.Fixture(t, "generic-arm64-debian"))
	if c, ok := r.Temperature.Celsius(); !ok || c != 45.25 {
		t.Errorf("hwmon temperature = %v, %v", c, ok)
	}
	if r.BootMedium.Type != MediumNVMe {
		t.Errorf("boot medium = %q", r.BootMedium.Type)
	}
	if r.Available(Journald) == false {
		t.Error("journald should be available in this fixture")
	}
}

func TestUnknownBootMediumIsAnUnavailableCapability(t *testing.T) {
	r := Detect(platformtest.Fixture(t, "generic-arm64-ubuntu"))
	c := capByID(t, r, BootMediumID)
	if c.Available || c.Reason != ReasonUnrecognised || r.BootMedium.Type != MediumUnknown {
		t.Errorf("sda without usb link: %+v, type %q", c, r.BootMedium.Type)
	}
}

func TestMinimalSystem(t *testing.T) {
	r := Detect(platformtest.Fixture(t, "unknown-arm64-minimal"))
	for _, id := range Minimum {
		if !r.Available(id) {
			t.Errorf("minimum capability %s should be available", id)
		}
	}
	for id, reason := range map[string]string{
		Journald: ReasonNotFound, CgroupV2: ReasonNotFound, CPUTemperature: ReasonNoSensor, BootMediumID: ReasonNoRootMount,
	} {
		if c := capByID(t, r, id); c.Available || c.Reason != reason {
			t.Errorf("%s = %+v, want unavailable with %q", id, c, reason)
		}
	}
	if r.Temperature != nil {
		t.Error("no sensor expected")
	}
	if _, ok := r.Temperature.Celsius(); ok {
		t.Error("a nil sensor must read as unavailable")
	}
}

// Detection of a system without anything must not fail or panic.
func TestEmptyFileSource(t *testing.T) {
	r := Detect(fstest.MapFS{})
	for _, c := range r.List {
		if c.Available {
			t.Errorf("%s available on an empty system", c.ID)
		}
	}
	if r.BootMedium.Type != MediumUnknown {
		t.Errorf("boot medium = %q", r.BootMedium.Type)
	}
}

func TestHostileFiles(t *testing.T) {
	r := Detect(platformtest.Fixture(t, "broken-hostile"))
	if c := capByID(t, r, CPUTemperature); c.Available {
		t.Errorf("implausible and unparsable temperatures must not count: %+v", c)
	}
	if c := capByID(t, r, BootMediumID); c.Available || c.Reason != ReasonNoRootMount {
		t.Errorf("boot medium = %+v", c)
	}
}

// The temperature is read from the file every time (the API reads it per request).
func TestTemperatureIsReadOnEveryCall(t *testing.T) {
	m := platformtest.Clone(t, "pi-zero-2w-raspios")
	r := Detect(m)
	m["sys/class/thermal/thermal_zone0/temp"] = &fstest.MapFile{Data: []byte("61500\n")}
	if c, ok := r.Temperature.Celsius(); !ok || c != 61.5 {
		t.Errorf("after change: %v, %v", c, ok)
	}
	m["sys/class/thermal/thermal_zone0/temp"] = &fstest.MapFile{Data: []byte("not a number")}
	if _, ok := r.Temperature.Celsius(); ok {
		t.Error("garbage must read as unavailable")
	}
	delete(m, "sys/class/thermal/thermal_zone0/temp")
	if _, ok := r.Temperature.Celsius(); ok {
		t.Error("removed sensor must read as unavailable")
	}
}

func TestParseMilliCelsius(t *testing.T) {
	good := map[string]float64{"43312": 43.312, "0\n": 0, "-5000": -5, "150000": 150, " 20000 ": 20}
	for in, want := range good {
		if got, ok := parseMilliCelsius(in); !ok || got != want {
			t.Errorf("%q = %v, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "abc", "1.5", "150001", "-40001", "99999999999999999999", "12 34"} {
		if _, ok := parseMilliCelsius(in); ok {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestSensorSelection(t *testing.T) {
	// A CPU sensor wins over a lower-numbered unrelated one, and zones are
	// ordered numerically (zone2 before zone10).
	m := fstest.MapFS{
		"sys/class/thermal/thermal_zone0/type":   {Data: []byte("battery\n")},
		"sys/class/thermal/thermal_zone0/temp":   {Data: []byte("30000\n")},
		"sys/class/thermal/thermal_zone10/type":  {Data: []byte("cpu-thermal\n")},
		"sys/class/thermal/thermal_zone10/temp":  {Data: []byte("50000\n")},
		"sys/class/thermal/thermal_zone2/type":   {Data: []byte("gpu\n")},
		"sys/class/thermal/thermal_zone2/temp":   {Data: []byte("40000\n")},
		"sys/class/thermal/cooling_device0/x":    {Data: []byte("ignored")},
		"sys/class/thermal/thermal_zonefoo/temp": {Data: []byte("1000")},
	}
	r := Detect(m)
	if c, _ := r.Temperature.Celsius(); c != 50 {
		t.Errorf("picked %v, want the cpu-thermal zone (50)", c)
	}
	delete(m, "sys/class/thermal/thermal_zone10/type")
	if c, _ := Detect(m).Temperature.Celsius(); c != 30 {
		t.Errorf("without a cpu sensor the first zone wins, got %v", c)
	}
}

func TestBootMediumClassification(t *testing.T) {
	const usbLink = "../../devices/platform/soc/3f980000.usb/usb1/1-1/1-1:1.0/host0/target0:0:0/0:0:0:0/block/sda/sda1"
	mk := func(mountinfo string, extra fstest.MapFS) Result {
		m := fstest.MapFS{"proc/self/mountinfo": {Data: []byte(mountinfo)}}
		for k, v := range extra {
			m[k] = v
		}
		return Detect(m)
	}
	uevent := func(name string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte("MAJOR=8\nMINOR=1\nDEVNAME=" + name + "\nDEVTYPE=partition\n")}
	}

	t.Run("usb stick on sd*", func(t *testing.T) {
		// As in real sysfs, /sys/dev/block/8:1 is a link and uevent lives behind it.
		r := mk("30 1 8:1 / / rw - ext4 /dev/root rw\n", fstest.MapFS{
			"sys/devices/platform/soc/3f980000.usb/usb1/1-1/1-1:1.0/host0/target0:0:0/0:0:0:0/block/sda/sda1/uevent": uevent("sda1"),
			"sys/dev/block/8:1": {Data: []byte(usbLink), Mode: fs.ModeSymlink},
		})
		if r.BootMedium.Type != MediumUSB || !r.Available(BootMediumID) {
			t.Errorf("type = %q", r.BootMedium.Type)
		}
	})
	t.Run("sata disk on sd* is unknown", func(t *testing.T) {
		r := mk("30 1 8:1 / / rw - ext4 /dev/sda1 rw\n", fstest.MapFS{
			"sys/devices/pci0000:00/0000:00:1f.2/ata1/host0/target0:0:0/0:0:0:0/block/sda/sda1/uevent": uevent("sda1"),
			"sys/dev/block/8:1": {Data: []byte("../../devices/pci0000:00/0000:00:1f.2/ata1/host0/target0:0:0/0:0:0:0/block/sda/sda1"), Mode: fs.ModeSymlink},
		})
		if r.BootMedium.Type != MediumUnknown {
			t.Errorf("type = %q", r.BootMedium.Type)
		}
	})
	t.Run("btrfs has no block major; the mount source names the device", func(t *testing.T) {
		r := mk("30 1 0:30 /@ / rw - btrfs /dev/mmcblk0p2 rw\n", nil)
		if r.BootMedium.Type != MediumMMC {
			t.Errorf("type = %q", r.BootMedium.Type)
		}
	})
	t.Run("the topmost mount at / wins", func(t *testing.T) {
		r := mk("1 0 179:2 / / rw - ext4 /dev/root rw\n2 1 0:50 / / rw - overlay overlay rw\n"+
			"3 1 259:1 / /data rw - ext4 /dev/nvme0n1p1 rw\n", fstest.MapFS{"sys/dev/block/179:2/uevent": uevent("mmcblk0p2")})
		if r.BootMedium.Type != MediumUnknown {
			t.Errorf("overlay on top: type = %q, want unknown", r.BootMedium.Type)
		}
	})
	t.Run("device names from files are validated", func(t *testing.T) {
		r := mk("30 1 8:1 / / rw - ext4 /dev/root rw\n", fstest.MapFS{"sys/dev/block/8:1/uevent": uevent("mmcblk0/../../x")})
		if r.BootMedium.Type != MediumUnknown {
			t.Errorf("type = %q", r.BootMedium.Type)
		}
	})
	t.Run("malformed lines are skipped", func(t *testing.T) {
		for _, bad := range []string{
			"", "garbage", "1 2 x:y / / rw - ext4 /dev/mmcblk0p1 rw", "1 2 8:1 / /mnt rw - ext4 /dev/mmcblk0p1 rw",
			"1 2 -1:0 / / rw - ext4 /dev/mmcblk0p1 rw", "1 2 8:1 / / rw ext4 /dev/mmcblk0p1 rw",
		} {
			if r := mk(bad+"\n", nil); r.BootMedium.Type != MediumUnknown || r.Available(BootMediumID) {
				t.Errorf("%q: %+v", bad, r.BootMedium)
			}
		}
	})
}

func TestOnUSBBus(t *testing.T) {
	for link, want := range map[string]bool{
		"../devices/pci/usb1/1-1/block/sda": true, "../devices/platform/xhci/usb12/x": true,
		"../devices/pci/ata1/block/sda": false, "../devices/usbfoo/x": false, "../devices/usb/x": false, "": false,
	} {
		if got := onUSBBus(link); got != want {
			t.Errorf("onUSBBus(%q) = %v, want %v", link, got, want)
		}
	}
}

func TestSourcesAreProbePathsNeverFileContent(t *testing.T) {
	const marker = "SECRET-MARKER-9137"
	m := platformtest.Clone(t, "pi-zero-2w-raspios")
	m["sys/class/thermal/thermal_zone0/type"] = &fstest.MapFile{Data: []byte(marker)}
	m["sys/dev/block/179:2/uevent"] = &fstest.MapFile{Data: []byte("DEVNAME=mmcblk0p2\nOTHER=" + marker + "\n")}
	for _, c := range Detect(m).List {
		if strings.Contains(c.Source, marker) || strings.Contains(c.Reason, marker) {
			t.Errorf("%s leaks file content: %+v", c.ID, c)
		}
	}
}
