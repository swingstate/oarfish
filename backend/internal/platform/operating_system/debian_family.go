package operating_system

import "io/fs"

// debianFamily covers Debian and its derivatives that have no adapter of their
// own. It is the "Debian ARM64 with systemd" row of the OS baseline
// (Pflichtenheft 3.3): basic functions, no full support statement.
type debianFamily struct{}

func (debianFamily) Name() string   { return "debian_family" }
func (debianFamily) Status() string { return StatusGeneric }
func (debianFamily) Matches(rel Release, _ fs.FS) bool {
	return hasID(rel, "debian")
}
