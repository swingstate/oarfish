package hardware

import (
	"bytes"
	"io/fs"
	"strconv"
	"strings"

	"github.com/swingstate/oarfish/backend/internal/platform/source"
)

type cpuInfo struct {
	Hardware string
	Revision string
	Model    string
}

func readCPUInfo(fsys fs.FS) cpuInfo {
	data, err := source.ReadFile(fsys, "proc/cpuinfo", maxCPUInfo)
	if err != nil {
		return cpuInfo{}
	}
	return parseCPUInfo(data)
}

// parseCPUInfo extracts the three fields Oarfish uses from /proc/cpuinfo:
// Hardware, Revision and Model. Every other line is skipped and not kept; in
// particular the Serial line is never looked at (privacy rule).
func parseCPUInfo(data []byte) cpuInfo {
	var ci cpuInfo
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		key, value, ok := strings.Cut(string(line), ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Hardware":
			ci.Hardware = source.Clean(value, 32)
		case "Revision":
			ci.Revision = revisionCode(value)
		case "Model":
			ci.Model = source.Clean(value, maxTextRunes)
		}
	}
	return ci
}

// revisionCode accepts a Raspberry Pi style revision: 1 to 8 hex digits.
func revisionCode(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) == 0 || len(s) > 8 {
		return ""
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return ""
		}
	}
	return s
}

// ParseMemTotal returns MemTotal from /proc/meminfo in bytes, or 0 if absent
// or implausible.
func ParseMemTotal(data []byte) uint64 {
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		rest, ok := strings.CutPrefix(string(line), "MemTotal:")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) != 2 || fields[1] != "kB" {
			return 0
		}
		kb, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil || kb == 0 || kb > 1<<40 { // up to 1 PiB
			return 0
		}
		return kb * 1024
	}
	return 0
}
