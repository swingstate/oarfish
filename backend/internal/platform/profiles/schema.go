// Package profiles holds the SoC family and board profiles (Pflichtenheft 3.7,
// 8.4). Profiles are data: JSON files embedded in the binary, decoded strictly
// and checked by an explicit validator. A profile cannot contain a command. It
// can only name versioned handlers that exist in the core, from a list that is
// compiled in (ORB-HW-008).
package profiles

// SchemaVersion is the only profile schema version this build understands.
const SchemaVersion = 1

// Profile kinds.
const (
	KindSoCFamily = "soc_family"
	KindBoard     = "board"
	KindGeneric   = "generic"
)

// Support tiers a profile can declare. "unsupported" is never declared by a
// profile: it is computed when the minimum requirements are missing.
const (
	TierVerified     = "verified"
	TierCompatible   = "compatible"
	TierCommunity    = "community"
	TierExperimental = "experimental"
	TierGeneric      = "generic"
)

// GenericProfileID is the id of the built-in generic ARM64 profile.
const GenericProfileID = "generic-arm64"

// Profile is the on-disk schema. Unknown fields are an error. Which fields are
// required or forbidden depends on Kind (see validate.go).
type Profile struct {
	SchemaVersion      int                 `json:"schema_version"`
	Kind               string              `json:"kind"`
	ProfileID          string              `json:"profile_id"`
	Version            string              `json:"version"`
	Vendor             string              `json:"vendor"`
	Model              string              `json:"model,omitempty"`
	SoCFamily          string              `json:"soc_family,omitempty"`
	CompatiblePatterns []string            `json:"compatible_patterns"`
	OSConstraints      *OSConstraints      `json:"os_constraints,omitempty"`
	KernelConstraints  *KernelConstraints  `json:"kernel_constraints,omitempty"`
	SupportTier        string              `json:"support_tier,omitempty"`
	Maintainers        []string            `json:"maintainers"`
	TestedCombinations []TestedCombination `json:"tested_combinations,omitempty"`
	Capabilities       []string            `json:"capabilities,omitempty"`
	SensorBindings     []SensorBinding     `json:"sensor_bindings,omitempty"`
	StorageHints       *StorageHints       `json:"storage_hints,omitempty"`
	KnownLimitations   []string            `json:"known_limitations,omitempty"`
	DocumentationLinks []string            `json:"documentation_links,omitempty"`
}

// OSConstraints lists the OS adapters the profile applies to. On another
// adapter the board is still recognised, but runs in Generic Mode.
type OSConstraints struct {
	Adapters []string `json:"adapters"`
}

// KernelConstraints states the oldest kernel the profile applies to,
// as "major.minor". Empty means no constraint.
type KernelConstraints struct {
	MinRelease string `json:"min_release,omitempty"`
}

// TestedCombination records a physical test of a board and OS combination.
// A profile may only declare tier "verified" if a combination lists every core
// function and carries an evidence reference.
type TestedCombination struct {
	OSAdapter       string   `json:"os_adapter"`
	OSVersionID     string   `json:"os_version_id,omitempty"`
	OarfishVersion  string   `json:"oarfish_version"`
	FunctionsTested []string `json:"functions_tested"`
	EvidenceRef     string   `json:"evidence_ref"`
}

// SensorBinding connects a sensor to a named core handler.
type SensorBinding struct {
	Sensor  string `json:"sensor"`
	Handler string `json:"handler"`
}

// StorageHints describes the usual storage of a board.
type StorageHints struct {
	BootMedia []string `json:"boot_media,omitempty"`
}

// CoreFunctions are the functions that must all have been tested on the
// hardware before a combination can be Verified (Pflichtenheft 3.6, 3.8).
var CoreFunctions = []string{"install", "dashboard", "system_functions", "self_update", "rollback", "plugins"}

// KnownHandlers is the compiled-in allow-list of handler names a profile may
// reference. Handler names are versioned. G4 ships no handler that writes.
var KnownHandlers = map[string]bool{
	"sensor.thermal_zone.v1": true,
	"sensor.hwmon.v1":        true,
}

// KnownSensors are the sensor names a binding may use.
var KnownSensors = map[string]bool{
	"cpu_temperature": true,
}
