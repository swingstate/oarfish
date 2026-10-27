package parse

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Mount is one line of /proc/self/mountinfo.
type Mount struct {
	Major, Minor int
	Root         string // the mounted directory inside the file system
	MountPoint   string // decoded, absolute, printable
	FSType       string
	Source       string // mount source such as /dev/mmcblk0p2; only a de-duplication key, never a path to open
}

const (
	maxMountPath  = 255
	maxFSTypeLen  = 32
	maxMountLines = 4096
)

// ParseMountinfo reads /proc/self/mountinfo and returns at most max mounts, in
// file order. Lines it cannot read, mount points that are not absolute
// printable paths, and file system types that are not plain words are skipped.
// The mount point is decoded from the kernel's octal escapes (\040 is a space).
//
// Format: id parent major:minor root mountpoint options [optional...] - fstype source superoptions.
func ParseMountinfo(data []byte, max int) []Mount {
	var out []Mount
	for i, line := range lines(data) {
		if i >= maxMountLines || len(out) >= max {
			break
		}
		f := fields(line)
		sep := -1
		for j := 6; j < len(f); j++ {
			if f[j] == "-" {
				sep = j
				break
			}
		}
		if sep < 0 || len(f) < sep+3 {
			continue
		}
		major, minor, ok := majorMinor(f[2])
		if !ok {
			continue
		}
		root, ok := unescapeMountPath(f[3])
		if !ok {
			continue
		}
		point, ok := unescapeMountPath(f[4])
		if !ok {
			continue
		}
		fstype := f[sep+1]
		if !plainWord(fstype, maxFSTypeLen) {
			continue
		}
		src := f[sep+2]
		if len(src) > maxMountPath || !plainText(src) {
			src = ""
		}
		out = append(out, Mount{Major: major, Minor: minor, Root: root, MountPoint: point, FSType: fstype, Source: src})
	}
	return out
}

func majorMinor(s string) (major, minor int, ok bool) {
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

// unescapeMountPath decodes \ooo octal escapes and accepts only an absolute,
// valid UTF-8 path of printable characters, without "." or ".." elements.
func unescapeMountPath(s string) (string, bool) {
	if len(s) == 0 || len(s) > 4*maxMountPath || s[0] != '/' {
		return "", false
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		if i+3 >= len(s) {
			return "", false
		}
		d := s[i+1 : i+4]
		v, err := strconv.ParseUint(d, 8, 8)
		if err != nil || len(d) != 3 {
			return "", false
		}
		b.WriteByte(byte(v))
		i += 3
	}
	p := b.String()
	if len(p) > maxMountPath || !utf8.ValidString(p) {
		return "", false
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f || r == utf8.RuneError || (r >= 0x80 && r < 0xa0) {
			return "", false
		}
	}
	for _, el := range strings.Split(p, "/") {
		if el == "." || el == ".." {
			return "", false
		}
	}
	return p, true
}

func plainWord(s string, max int) bool {
	if s == "" || len(s) > max {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-', c == '.', c == '+':
		default:
			return false
		}
	}
	return true
}

// plainText reports whether s is printable ASCII without blanks.
func plainText(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] <= 0x20 || s[i] >= 0x7f {
			return false
		}
	}
	return true
}
