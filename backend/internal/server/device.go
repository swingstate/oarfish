package server

import (
	"math"
	"net/http"

	"github.com/swingstate/oarfish/backend/internal/platform/detector"
)

// DeviceSource is what GET /api/v1/device needs from device detection.
// *detector.Detector implements it.
type DeviceSource interface {
	Result() detector.Result
	CPUTemperature() (celsius float64, ok bool)
}

// DeviceSchemaVersion is the version of the /api/v1/device response layout.
// Adding fields does not change it; changing or removing one does.
const DeviceSchemaVersion = 1

// deviceResponse is the wire format of GET /api/v1/device. Values that are
// not known are null. It carries nothing that identifies a single device: no
// serial number, MAC address, machine id, host name, user name or IP address.
type deviceResponse struct {
	SchemaVersion int                `json:"schema_version"`
	Hardware      deviceHardware     `json:"hardware"`
	OS            deviceOS           `json:"os"`
	Kernel        deviceKernel       `json:"kernel"`
	Profile       deviceProfile      `json:"profile"`
	Support       deviceSupport      `json:"support"`
	Capabilities  []deviceCapability `json:"capabilities"`
	Sensors       deviceSensors      `json:"sensors"`
	BootMedium    deviceBootMedium   `json:"boot_medium"`
}

type deviceHardware struct {
	Vendor           *string `json:"vendor"`
	Model            *string `json:"model"`
	SoCFamily        *string `json:"soc_family"`
	Arch             string  `json:"arch"`
	RevisionCode     *string `json:"revision_code"`
	MemoryTotalBytes *uint64 `json:"memory_total_bytes"`
}

type deviceOS struct {
	ID            *string  `json:"id"`
	IDLike        []string `json:"id_like"`
	VersionID     *string  `json:"version_id"`
	PrettyName    *string  `json:"pretty_name"`
	Adapter       string   `json:"adapter"`
	AdapterStatus string   `json:"adapter_status"`
}

type deviceKernel struct {
	Release *string `json:"release"`
}

type deviceProfile struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	MatchedBy string `json:"matched_by"`
}

type deviceSupport struct {
	Tier                string   `json:"tier"`
	Reason              string   `json:"reason"`
	ExplanationKey      string   `json:"explanation_key"`
	Limitations         []string `json:"limitations"`
	BoardActionsAllowed bool     `json:"board_actions_allowed"`
}

type deviceCapability struct {
	ID        string  `json:"id"`
	Available bool    `json:"available"`
	Source    string  `json:"source"`
	Reason    *string `json:"reason,omitempty"`
}

type deviceSensors struct {
	CPUTemperatureCelsius *float64 `json:"cpu_temperature_celsius"`
}

type deviceBootMedium struct {
	Type string `json:"type"`
}

func handleDevice(w http.ResponseWriter, src DeviceSource) {
	writeJSON(w, http.StatusOK, buildDeviceResponse(src))
}

func buildDeviceResponse(src DeviceSource) deviceResponse {
	r := src.Result()
	resp := deviceResponse{
		SchemaVersion: DeviceSchemaVersion,
		Hardware: deviceHardware{
			Vendor:       nullable(r.Hardware.Vendor),
			Model:        nullable(r.Hardware.Model),
			SoCFamily:    nullable(r.SoCFamily),
			Arch:         r.Hardware.Arch,
			RevisionCode: nullable(r.Hardware.RevisionCode),
		},
		OS: deviceOS{
			ID:            nullable(r.OS.ID),
			IDLike:        nonNil(r.OS.IDLike),
			VersionID:     nullable(r.OS.VersionID),
			PrettyName:    nullable(r.OS.PrettyName),
			Adapter:       r.OS.Adapter,
			AdapterStatus: r.OS.AdapterStatus,
		},
		Kernel:  deviceKernel{Release: nullable(r.OS.Kernel)},
		Profile: deviceProfile{ID: r.Profile.ID, Version: r.Profile.Version, MatchedBy: r.Profile.MatchedBy},
		Support: deviceSupport{
			Tier:                r.Support.Tier,
			Reason:              r.Support.Reason,
			ExplanationKey:      r.Support.ExplanationKey,
			Limitations:         nonNil(r.Support.Limitations),
			BoardActionsAllowed: r.Support.BoardActionsAllowed,
		},
		Capabilities: make([]deviceCapability, 0, len(r.Capabilities)),
		BootMedium:   deviceBootMedium{Type: r.BootMedium.Type},
	}
	if r.Hardware.MemoryTotalBytes > 0 {
		m := r.Hardware.MemoryTotalBytes
		resp.Hardware.MemoryTotalBytes = &m
	}
	for _, c := range r.Capabilities {
		dc := deviceCapability{ID: c.ID, Available: c.Available, Source: c.Source}
		if c.Reason != "" {
			reason := c.Reason
			dc.Reason = &reason
		}
		resp.Capabilities = append(resp.Capabilities, dc)
	}
	if t, ok := src.CPUTemperature(); ok {
		t = math.Round(t*10) / 10
		resp.Sensors.CPUTemperatureCelsius = &t
	}
	return resp
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
