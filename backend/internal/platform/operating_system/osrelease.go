package operating_system

import (
	"bytes"
	"strings"

	"github.com/swingstate/oarfish/backend/internal/platform/source"
)

// Release holds the os-release(5) fields Oarfish uses. Empty means absent or
// rejected as malformed.
type Release struct {
	ID         string
	IDLike     []string
	VersionID  string
	PrettyName string
	Name       string
}

const (
	maxOSReleaseFile = 64 << 10
	maxOSReleaseLine = 4096
	maxOSReleaseVal  = 1024
	maxIDLike        = 16
)

// ParseOSRelease parses the content of /etc/os-release. It is strict about
// quoting and tolerant about unknown keys: a line that is not a well-formed
// KEY=value assignment is ignored as a whole, unknown keys are skipped, and a
// value that is not plain printable UTF-8 is rejected rather than repaired.
// The size of data is not limited here; Detect limits what it reads.
func ParseOSRelease(data []byte) Release {
	vars := parseAssignments(data)
	rel := Release{
		ID:         validID(vars["ID"]),
		VersionID:  validVersionID(vars["VERSION_ID"]),
		PrettyName: source.Clean(vars["PRETTY_NAME"], 128),
		Name:       source.Clean(vars["NAME"], 64),
	}
	for _, f := range strings.Fields(vars["ID_LIKE"]) {
		if id := validID(f); id != "" && len(rel.IDLike) < maxIDLike {
			rel.IDLike = append(rel.IDLike, id)
		}
	}
	return rel
}

func parseAssignments(data []byte) map[string]string {
	vars := map[string]string{}
	for _, raw := range bytes.Split(data, []byte{'\n'}) {
		if len(raw) > maxOSReleaseLine {
			continue
		}
		line := strings.TrimLeft(string(raw), " \t")
		if line == "" || line[0] == '#' {
			continue
		}
		key, rest, ok := strings.Cut(line, "=")
		if !ok || !validKey(key) {
			continue
		}
		value, ok := parseValue(rest)
		if !ok || len(value) > maxOSReleaseVal || !source.IsPlain(value) {
			continue
		}
		vars[key] = value
	}
	return vars
}

func validKey(k string) bool {
	if k == "" || len(k) > 64 {
		return false
	}
	for i, c := range k {
		switch {
		case c >= 'A' && c <= 'Z', c == '_':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// parseValue reads one shell-style value: double-quoted (with the escapes
// \\ \" \$ \`), single-quoted (no escapes) or bare (no space, quote,
// backslash, "$" or backtick). Anything after the closing quote other than
// trailing blanks makes the value malformed.
func parseValue(s string) (string, bool) {
	if s == "" {
		return "", true
	}
	switch s[0] {
	case '"':
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			switch c := s[i]; c {
			case '"':
				return b.String(), onlyBlanks(s[i+1:])
			case '$', '`':
				return "", false
			case '\\':
				i++
				if i >= len(s) || !strings.ContainsRune(`\"$`+"`", rune(s[i])) {
					return "", false
				}
				b.WriteByte(s[i])
			default:
				b.WriteByte(c)
			}
		}
		return "", false // unterminated
	case '\'':
		end := strings.IndexByte(s[1:], '\'')
		if end < 0 {
			return "", false
		}
		return s[1 : 1+end], onlyBlanks(s[2+end:])
	default:
		v := strings.TrimRight(s, " \t")
		if strings.ContainsAny(v, " \t\"'\\$`") {
			return "", false
		}
		return v, true
	}
}

func onlyBlanks(s string) bool { return strings.Trim(s, " \t") == "" }

// validID accepts the lowercase identifier form os-release(5) defines for ID
// and ID_LIKE entries.
func validID(s string) string {
	if s == "" || len(s) > 64 {
		return ""
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
		default:
			return ""
		}
	}
	return s
}

func validVersionID(s string) string {
	if s == "" || len(s) > 64 {
		return ""
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '-', c == '+', c == '~':
		default:
			return ""
		}
	}
	return s
}
