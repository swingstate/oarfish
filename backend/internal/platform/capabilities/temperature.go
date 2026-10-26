package capabilities

import (
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/swingstate/oarfish/backend/internal/platform/source"
)

// Sensor reads one temperature file. It is found once at startup and read on
// every API request; reading is cheap and has no side effects.
type Sensor struct {
	fsys fs.FS
	path string // built from directory names that matched a strict pattern
}

// Plausible range of a CPU temperature in millidegrees Celsius.
const (
	minMilliC = -40_000
	maxMilliC = 150_000
)

// Celsius returns the current temperature. ok is false if the file can no
// longer be read or holds an implausible value.
func (s *Sensor) Celsius() (float64, bool) {
	if s == nil {
		return 0, false
	}
	data, err := source.ReadFile(s.fsys, s.path, 32)
	if err != nil {
		return 0, false
	}
	return parseMilliCelsius(string(data))
}

func parseMilliCelsius(s string) (float64, bool) {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || v < minMilliC || v > maxMilliC {
		return 0, false
	}
	return float64(v) / 1000, true
}

// preferred sensor names, in the order the kernel drivers commonly use them.
var cpuSensorNames = map[string]bool{
	"cpu-thermal": true, "cpu_thermal": true, "soc-thermal": true, "soc_thermal": true,
	"x86_pkg_temp": true, "coretemp": true, "k10temp": true, "cpu": true,
}

func detectTemperature(fsys fs.FS, list []Capability) (*Sensor, []Capability) {
	const pattern = "/sys/class/thermal/thermal_zone*/temp, /sys/class/hwmon/hwmon*/temp1_input"

	var fallback *Sensor
	for _, c := range []struct{ dir, prefix, valueFile, nameFile string }{
		{"sys/class/thermal", "thermal_zone", "temp", "type"},
		{"sys/class/hwmon", "hwmon", "temp1_input", "name"},
	} {
		for _, entry := range numberedEntries(fsys, c.dir, c.prefix) {
			base := c.dir + "/" + entry
			s := &Sensor{fsys: fsys, path: base + "/" + c.valueFile}
			if _, ok := s.Celsius(); !ok {
				continue
			}
			name, _ := source.ReadFile(fsys, base+"/"+c.nameFile, 64)
			if cpuSensorNames[source.Clean(string(name), 64)] {
				return s, append(list, Capability{ID: CPUTemperature, Available: true, Source: "/" + s.path})
			}
			if fallback == nil {
				fallback = s
			}
		}
	}
	if fallback != nil {
		return fallback, append(list, Capability{ID: CPUTemperature, Available: true, Source: "/" + fallback.path})
	}
	return nil, append(list, Capability{ID: CPUTemperature, Source: pattern, Reason: ReasonNoSensor})
}

// numberedEntries lists directory entries named prefix+digits in numeric order.
// Only names that match this strict pattern are ever used to build a path.
func numberedEntries(fsys fs.FS, dir, prefix string) []string {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil
	}
	type numbered struct {
		name string
		n    int
	}
	var found []numbered
	for _, e := range entries {
		digits, ok := strings.CutPrefix(e.Name(), prefix)
		if !ok || len(digits) == 0 || len(digits) > 4 {
			continue
		}
		n, err := strconv.Atoi(digits)
		if err != nil || n < 0 {
			continue
		}
		found = append(found, numbered{e.Name(), n})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].n < found[j].n })
	out := make([]string, len(found))
	for i, f := range found {
		out[i] = f.name
	}
	return out
}
