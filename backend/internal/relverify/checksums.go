package relverify

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const (
	// ManifestName is the file name of the manifest in a release.
	ManifestName = "manifest.json"

	maxChecksumsFile = 64 << 10
	maxChecksumLines = 64
	maxFileNameLen   = 255
)

// Checksums is a parsed checksums.txt, the file whose signature
// VerifySignature checks.
type Checksums struct {
	names  []string
	hashes map[string]string
}

// ParseChecksums parses the format `bundlegen checksums` writes, which is that
// of sha256sum: "<64 lowercase hexadecimal digits>  <file name>\n", one line per
// file, two spaces between the fields. Strict rules: the file is at most 64 KiB
// and has at most 64 lines, every line is terminated by a line feed, there is no
// empty line and no carriage return, a name is 1 to 255 characters from
// [0-9A-Za-z._-] (so no path separator, no space and no control character) and
// is neither "." nor "..", every name appears at most once, and bundleFile and
// manifest.json each appear exactly once. Other names (install.sh, the SBOMs)
// are allowed and exposed by Names and Hash; nothing interprets them.
//
// Every failure is a *Error with CodeChecksumMismatch.
func ParseChecksums(file []byte, bundleFile string) (*Checksums, error) {
	if !validFileName(bundleFile) {
		return nil, fail(CodeBadRequest, "the expected bundle file name is not a plain file name")
	}
	if len(file) == 0 || len(file) > maxChecksumsFile {
		return nil, fail(CodeChecksumMismatch, "checksums.txt is empty or larger than the limit")
	}
	if file[len(file)-1] != '\n' {
		return nil, fail(CodeChecksumMismatch, "checksums.txt does not end in a line feed")
	}
	lines := strings.Split(string(file[:len(file)-1]), "\n")
	if len(lines) > maxChecksumLines {
		return nil, fail(CodeChecksumMismatch, "checksums.txt has too many lines")
	}
	c := &Checksums{hashes: make(map[string]string, len(lines))}
	for _, line := range lines {
		if len(line) < 64+2+1 {
			return nil, fail(CodeChecksumMismatch, "a line of checksums.txt is too short")
		}
		hash, sep, name := line[:64], line[64:66], line[66:]
		if !isLowerHex(hash) {
			return nil, fail(CodeChecksumMismatch, "a hash in checksums.txt is not 64 lowercase hexadecimal digits")
		}
		if sep != "  " {
			return nil, fail(CodeChecksumMismatch, "a line of checksums.txt does not separate hash and name by two spaces")
		}
		if !validFileName(name) {
			return nil, fail(CodeChecksumMismatch, "a file name in checksums.txt is not a plain file name")
		}
		if _, dup := c.hashes[name]; dup {
			return nil, fail(CodeChecksumMismatch, "checksums.txt lists a file name twice")
		}
		c.hashes[name] = hash
		c.names = append(c.names, name)
	}
	if _, ok := c.hashes[bundleFile]; !ok {
		return nil, fail(CodeChecksumMismatch, "checksums.txt does not list the bundle")
	}
	if _, ok := c.hashes[ManifestName]; !ok {
		return nil, fail(CodeChecksumMismatch, "checksums.txt does not list manifest.json")
	}
	return c, nil
}

// Names returns the listed file names in file order.
func (c *Checksums) Names() []string {
	if c == nil {
		return nil
	}
	return append([]string(nil), c.names...)
}

// Hash returns the listed SHA-256 (64 lowercase hexadecimal digits) of name.
func (c *Checksums) Hash(name string) (string, bool) {
	if c == nil {
		return "", false
	}
	h, ok := c.hashes[name]
	return h, ok
}

// Check requires name to be listed with exactly the digest sha256hex.
func (c *Checksums) Check(name, sha256hex string) error {
	if c == nil {
		return fail(CodeInternal, "no checksums")
	}
	want, ok := c.hashes[name]
	if !ok {
		return fail(CodeChecksumMismatch, "the file is not listed in checksums.txt")
	}
	if want != sha256hex {
		return fail(CodeChecksumMismatch, "the SHA-256 differs from the one in checksums.txt")
	}
	return nil
}

// SumHex returns the SHA-256 of b as 64 lowercase hexadecimal digits.
func SumHex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func validFileName(s string) bool {
	if s == "" || len(s) > maxFileNameLen || s == "." || s == ".." {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}
