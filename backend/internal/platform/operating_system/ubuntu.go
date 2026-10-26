package operating_system

import "io/fs"

// ubuntu is the adapter boundary for Ubuntu Server LTS ARM64 (ORB-HW-010). In
// G4 it only recognises Ubuntu and reports "not yet qualified". It carries no
// Ubuntu-specific behaviour: qualification belongs to Gate G12.
type ubuntu struct{}

func (ubuntu) Name() string   { return "ubuntu" }
func (ubuntu) Status() string { return StatusNotYetQualified }
func (ubuntu) Matches(rel Release, _ fs.FS) bool {
	return hasID(rel, "ubuntu")
}
