package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

// phaseIdle is the only transaction phase the stub knows (U7).
const phaseIdle = "idle"

// versionPattern is the version pattern of U2 (the installer's valid_version).
var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$`)

func validVersion(s string) bool {
	return len(s) <= 64 && !strings.Contains(s, "..") && versionPattern.MatchString(s)
}

// statusSource builds the answer to `status`.
type statusSource struct {
	helperVersion string
	keyID         string
	root          string // /opt/oarfish; a temporary directory in tests
}

// body returns the answer to `status`, or the error code that replaces it.
func (s statusSource) body() ([]byte, errorCode) {
	free, err := freeBytes(s.root)
	if err != nil {
		return nil, codeIOError
	}
	return encode(statusResponse{
		V:                protocolMax,
		OK:               true,
		ProtocolMin:      protocolMin,
		ProtocolMax:      protocolMax,
		HelperVersion:    s.helperVersion,
		KeyID:            s.keyID,
		InstalledVersion: installedVersion(s.root),
		Phase:            phaseIdle,
		FreeBytes:        free,
	}), ""
}

// installedVersion is the version in the name of the target of
// <root>/current. The only accepted target is the relative link the installer
// writes, releases/<version> (U5), with a version that matches the pattern of
// U2. Anything else, and a missing link, gives "".
func installedVersion(root string) string {
	target, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil {
		return ""
	}
	dir, name := filepath.Split(target)
	if dir != "releases/" || !validVersion(name) {
		return ""
	}
	return name
}

// freeBytes is the space available on the file system that holds the
// releases (U5): blocks available to a process without CAP_SYS_RESOURCE
// times the block size.
func freeBytes(root string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(root, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}
