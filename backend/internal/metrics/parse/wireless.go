package parse

import (
	"strconv"
	"strings"
)

// Wireless is the signal of one interface from /proc/net/wireless (task N1,
// ADR 0008 S1.2). LevelDBm is set only when the driver reports the level in
// dBm; Quality is the driver's link quality, on the driver's own scale (the
// file does not say its maximum).
type Wireless struct {
	Iface    string
	Quality  *int
	LevelDBm *int
}

const maxWireless = 16

// ParseWireless reads /proc/net/wireless:
//
//	Inter-| sta-|   Quality        |   Discarded packets               | Missed | WE
//	 face | tus | link level noise |  nwid  crypt   frag  retry   misc | beacon | 22
//	 wlan0: 0000   58.  -52.  -256        0      0      0      0      0        0
//
// The kernel (net/wireless/wext-proc.c) prints the level minus 256 when the
// driver reports dBm, so a dBm level is negative; a positive level is a value
// without a unit and is not shown as dBm. Each value may end in "." (updated
// since the last read). A line that does not have this shape is skipped; at
// most 16 interfaces, each name once.
func ParseWireless(data []byte) []Wireless {
	var out []Wireless
	seen := map[string]bool{}
	for _, line := range lines(data) {
		if len(out) >= maxWireless {
			break
		}
		f := fields(line)
		if len(f) < 5 {
			continue
		}
		name, ok := strings.CutSuffix(f[0], ":")
		if !ok || !ifaceName(name) || seen[name] {
			continue // also the two header lines
		}
		if _, err := strconv.ParseUint(f[1], 16, 16); err != nil || len(f[1]) != 4 {
			continue
		}
		w := Wireless{Iface: name}
		if q, ok := wirelessValue(f[2]); ok && q >= 0 && q <= 255 {
			w.Quality = &q
		}
		if l, ok := wirelessValue(f[3]); ok && l >= -127 && l <= -1 {
			w.LevelDBm = &l
		}
		seen[name] = true
		out = append(out, w)
	}
	return out
}

// wirelessValue parses a small signed integer with an optional "." suffix.
func wirelessValue(s string) (int, bool) {
	s = strings.TrimSuffix(s, ".")
	if s == "" || len(s) > 5 || s[0] == '+' {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return v, true
}
