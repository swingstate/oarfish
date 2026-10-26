// Package source is how detection code reads the host: through an fs.FS that is
// rooted at "/" in production and at a fixture directory in tests. All content
// that comes through it is untrusted: reads are bounded and text is cleaned
// before it is shown or stored.
package source

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrTooLarge is returned when a file is bigger than the limit given to ReadFile.
var ErrTooLarge = errors.New("file exceeds size limit")

// Host returns the production file source: the real root file system.
// Names passed to it are slash-separated and have no leading slash,
// for example "proc/cpuinfo". It uses no cgo.
func Host() fs.FS {
	return os.DirFS("/")
}

// ReadFile reads name from fsys and fails with ErrTooLarge when the file is
// longer than max bytes. It does not trust the size the file system reports
// (files in /proc report 0), it counts what it actually reads.
func ReadFile(fsys fs.FS, name string, max int64) ([]byte, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("%s: %w (limit %d bytes)", name, ErrTooLarge, max)
	}
	return data, nil
}

// Exists reports whether name can be stat-ed (any file type, for example a socket).
func Exists(fsys fs.FS, name string) bool {
	_, err := fs.Stat(fsys, name)
	return err == nil
}

// IsDir reports whether name is a directory.
func IsDir(fsys fs.FS, name string) bool {
	info, err := fs.Stat(fsys, name)
	return err == nil && info.IsDir()
}

// Clean makes untrusted text safe to display: invalid UTF-8 becomes U+FFFD,
// control, format and other non-graphic characters are removed, surrounding
// space is trimmed, and the result is cut to at most maxRunes characters.
func Clean(s string, maxRunes int) string {
	s = strings.ToValidUTF8(s, "�")
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= maxRunes {
			break
		}
		if r == '\t' {
			r = ' '
		}
		if r != ' ' && (!unicode.IsPrint(r) || unicode.IsControl(r)) {
			continue
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

// IsPlain reports whether s is valid UTF-8 without control or non-printable
// characters. Strict parsers use it to reject a value instead of repairing it.
func IsPlain(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r != ' ' && (!unicode.IsPrint(r) || unicode.IsControl(r)) {
			return false
		}
	}
	return true
}
