package hardware

import (
	"bytes"
	"io/fs"

	"github.com/swingstate/oarfish/backend/internal/platform/source"
)

type deviceTree struct {
	Model      string
	Compatible []string
}

func readDeviceTree(fsys fs.FS) deviceTree {
	var dt deviceTree
	if data, err := source.ReadFile(fsys, "proc/device-tree/model", maxDeviceTreeFile); err == nil {
		dt.Model = ParseModel(data)
	}
	if data, err := source.ReadFile(fsys, "proc/device-tree/compatible", maxDeviceTreeFile); err == nil {
		dt.Compatible = ParseCompatible(data)
	}
	return dt
}

// ParseModel returns the board model from /proc/device-tree/model: the text up
// to the first NUL, cleaned and shortened.
func ParseModel(data []byte) string {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		data = data[:i]
	}
	return source.Clean(string(data), maxTextRunes)
}

// ParseCompatible splits the NUL-separated list in /proc/device-tree/compatible.
// Entries that are not valid compatible strings are dropped, so the result only
// holds short ASCII strings of letters, digits and ",._+-". At most
// maxCompatible entries are kept, in file order (most specific first).
func ParseCompatible(data []byte) []string {
	var out []string
	for _, part := range bytes.Split(data, []byte{0}) {
		if len(out) >= maxCompatible {
			break
		}
		if validCompatible(part) {
			out = append(out, string(part))
		}
	}
	return out
}

func validCompatible(b []byte) bool {
	if len(b) == 0 || len(b) > 128 {
		return false
	}
	for _, c := range b {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == ',' || c == '.' || c == '_' || c == '+' || c == '-':
		default:
			return false
		}
	}
	return true
}
