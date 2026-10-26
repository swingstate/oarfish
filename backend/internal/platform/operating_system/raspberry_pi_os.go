package operating_system

import (
	"io/fs"
	"strings"

	"github.com/swingstate/oarfish/backend/internal/platform/source"
)

// raspberryPiOS is the initial target OS (ORB-HW-003, AT-002).
//
// os-release alone cannot tell Raspberry Pi OS from Debian: the 64-bit images
// report ID=debian (older 32-bit images report ID=raspbian). The image marker
// /etc/rpi-issue, written by pi-gen, is what separates them. This is an OS
// fact; it says nothing about the board, and a Raspberry Pi running Ubuntu or
// Debian has no such file.
type raspberryPiOS struct{}

func (raspberryPiOS) Name() string   { return "raspberry_pi_os" }
func (raspberryPiOS) Status() string { return StatusInitialTarget }
func (raspberryPiOS) Matches(rel Release, fsys fs.FS) bool {
	if rel.ID == "raspbian" {
		return true
	}
	if rel.ID != "debian" {
		return false
	}
	data, err := source.ReadFile(fsys, "etc/rpi-issue", 1024)
	return err == nil && strings.HasPrefix(string(data), "Raspberry Pi ")
}
