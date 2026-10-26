// Package detector combines hardware, operating system, capabilities and the
// profile registry into one result and computes the support tier. It is the
// only package that knows about all of them; hardware and operating_system do
// not know about each other. Detection runs once; only the CPU temperature is
// read again on request.
package detector

import (
	"io/fs"

	"github.com/swingstate/oarfish/backend/internal/platform/capabilities"
	"github.com/swingstate/oarfish/backend/internal/platform/hardware"
	"github.com/swingstate/oarfish/backend/internal/platform/operating_system"
	"github.com/swingstate/oarfish/backend/internal/platform/profiles"
)

// ProfileRef names the profile that matched the hardware.
type ProfileRef struct {
	ID        string
	Version   string
	MatchedBy string
}

// Result is the cached outcome of detection.
type Result struct {
	Hardware     hardware.Info
	OS           operating_system.Info
	Capabilities []capabilities.Capability
	BootMedium   capabilities.BootMedium
	Profile      ProfileRef
	// SoCFamily is the SoC family from the matched SoC profile if there is
	// one, else what hardware detection found.
	SoCFamily string
	Support   Support
}

// Detector holds the detection result and the temperature sensor.
type Detector struct {
	result Result
	sensor *capabilities.Sensor
}

// New runs detection once. It never fails: whatever cannot be determined
// becomes an unavailable capability or a lower support tier with a reason.
func New(fsys fs.FS, env hardware.Env, reg *profiles.Registry) *Detector {
	hw := hardware.Detect(fsys, env)
	osInfo := operating_system.Detect(fsys)
	caps := capabilities.Detect(fsys)
	match := reg.Match(hw.Compatible, hw.Model)

	soc := hw.SoCFamily
	if match.SoC != nil {
		soc = match.SoC.SoCFamily
	}
	return &Detector{
		result: Result{
			Hardware:     hw,
			OS:           osInfo,
			Capabilities: caps.List,
			BootMedium:   caps.BootMedium,
			Profile: ProfileRef{
				ID:        match.Profile.ProfileID,
				Version:   match.Profile.Version,
				MatchedBy: match.MatchedBy,
			},
			SoCFamily: soc,
			Support:   computeSupport(hw, osInfo, caps, match),
		},
		sensor: caps.Temperature,
	}
}

// Result returns the cached detection result.
func (d *Detector) Result() Result { return d.result }

// CPUTemperature reads the CPU temperature now. ok is false if there is no
// usable sensor or the reading failed.
func (d *Detector) CPUTemperature() (celsius float64, ok bool) {
	return d.sensor.Celsius()
}
