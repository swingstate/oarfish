package parse

import (
	"strconv"
	"strings"
	"testing"
)

const wirelessHeader = "Inter-| sta-|   Quality        |   Discarded packets               | Missed | WE\n" +
	" face | tus | link level noise |  nwid  crypt   frag  retry   misc | beacon | 22\n"

func intOf(p *int) string {
	if p == nil {
		return "<nil>"
	}
	return strconv.Itoa(*p)
}

// Task N1: the shape the kernel prints (wext-proc.c), as on the reference
// device in S1 (quality 61, level -49 dBm); several interfaces; the forms of
// the level; lines that are not interface lines.
func TestParseWireless(t *testing.T) {
	data := wirelessHeader +
		" wlan0: 0000   61.  -49.  -256        0      0      0      0      0        0\n" +
		"  wlp1s0: 0000   40   -71   -256        0      0      0      0      0        0\n" +
		" wlx0: 0000   70.  204.  0        0      0      0      0      0        0\n" + // a level without dBm
		" wly0: 0000   0    0    0        0      0      0      0      0        0\n"
	got := ParseWireless([]byte(data))
	want := []string{"wlan0 61 -49", "wlp1s0 40 -71", "wlx0 70 <nil>", "wly0 0 <nil>"}
	if len(got) != len(want) {
		t.Fatalf("%d entries: %+v", len(got), got)
	}
	for i, w := range got {
		if s := w.Iface + " " + intOf(w.Quality) + " " + intOf(w.LevelDBm); s != want[i] {
			t.Errorf("entry %d: %s, want %s", i, s, want[i])
		}
	}
}

func TestParseWirelessNoLineAndMalformed(t *testing.T) {
	for name, data := range map[string]string{
		"empty":        "",
		"header only":  wirelessHeader,
		"no colon":     wirelessHeader + " wlan0 0000 58. -52. -256 0 0 0 0 0 0\n",
		"short":        wirelessHeader + " wlan0: 0000 58.\n",
		"bad status":   wirelessHeader + " wlan0: zz00 58. -52. -256 0 0 0 0 0 0\n",
		"bad name":     wirelessHeader + " ../x: 0000 58. -52. -256 0 0 0 0 0 0\n",
		"long name":    wirelessHeader + " abcdefghijklmnop: 0000 58. -52. -256 0 0 0 0 0 0\n",
		"binary noise": "\x00\xff\x01: 0000 1 2 3\n",
	} {
		if got := ParseWireless([]byte(data)); len(got) != 0 {
			t.Errorf("%s: %+v", name, got)
		}
	}
	// Values out of range or not numbers leave that value empty, not the line.
	got := ParseWireless([]byte(" wlan0: 0000 999. -300. 0 0 0 0 0 0 0\n wlan1: 0000 x. +5 0 0 0 0 0 0 0\n wlan2: 0000 -1 -128 0 0 0 0 0 0 0\n"))
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	for _, w := range got {
		if w.Quality != nil || w.LevelDBm != nil {
			t.Errorf("%s: %s %s", w.Iface, intOf(w.Quality), intOf(w.LevelDBm))
		}
	}
}

// A name appears once; at most 16 interfaces; an oversize input is cut by
// the line bound and stays bounded.
func TestParseWirelessBounds(t *testing.T) {
	var b strings.Builder
	b.WriteString(" wlan0: 0000 50. -60. 0 0 0 0 0 0 0\n wlan0: 0000 10. -90. 0 0 0 0 0 0 0\n")
	for i := 0; i < 100; i++ {
		b.WriteString(" wl" + strconv.Itoa(i) + ": 0000 50. -60. 0 0 0 0 0 0 0\n")
	}
	got := ParseWireless([]byte(b.String()))
	if len(got) != maxWireless || got[0].Iface != "wlan0" || *got[0].LevelDBm != -60 {
		t.Errorf("%d entries, first %+v", len(got), got[0])
	}
	if got := ParseWireless([]byte(strings.Repeat("x\n", 100000) + " wlan0: 0000 50. -60. 0 0 0 0 0 0 0\n")); len(got) != 0 {
		t.Errorf("a line past the line bound was read: %+v", got)
	}
}
