package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

// versionPattern is the version pattern of U2 (the installer's valid_version).
var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$`)

func validVersion(s string) bool {
	return len(s) <= 64 && !strings.Contains(s, "..") && versionPattern.MatchString(s)
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
