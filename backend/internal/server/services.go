package server

import (
	"context"
	"net/http"
	"time"

	"github.com/swingstate/oarfish/backend/internal/auth"
	"github.com/swingstate/oarfish/backend/internal/services"
	"github.com/swingstate/oarfish/backend/internal/unitname"
)

// ServicesSchemaVersion is the version of the answers of the services routes.
const ServicesSchemaVersion = 1

// ServiceControl is what the services routes need (ADR 0011 B4, step G7.4).
// *services.Manager implements it.
type ServiceControl interface {
	List(ctx context.Context) (services.List, *services.Error)
	BrokerAvailable() bool
	Submit(req services.Request) string
	Last() *services.ActionView
}

var _ ServiceControl = (*services.Manager)(nil)

// WithServices enables the routes of the Activity page: GET /api/v1/services,
// POST /api/v1/services/action and GET /api/v1/services/action. Without it
// they answer 503.
func WithServices(s ServiceControl) Option { return func(c *config) { c.services = s } }

type servicesAPI struct {
	authAPI
	svc ServiceControl
}

var errServicesUnavailable = apiError{
	Code:     string(services.CodeUnavailable),
	Message:  "Services cannot be listed on this system.",
	Recovery: "Oarfish lists services only where systemd runs.",
}

func (a *servicesAPI) unavailable(w http.ResponseWriter, r *http.Request) bool {
	if a.svc != nil {
		return false
	}
	writeError(w, r, http.StatusServiceUnavailable, errServicesUnavailable)
	return true
}

// serviceRow is one row of GET /api/v1/services.
type serviceRow struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	LoadState   string   `json:"load_state"`
	ActiveState string   `json:"active_state"`
	SubState    string   `json:"sub_state"`
	StartMode   string   `json:"start_mode"`
	Denied      bool     `json:"denied"`
	RestartOnly bool     `json:"restart_only"`
	Critical    string   `json:"critical"`
	Actions     []string `json:"actions"`
}

type serviceCounts struct {
	Total    int `json:"total"`
	Active   int `json:"active"`
	Failed   int `json:"failed"`
	Inactive int `json:"inactive"`
}

type servicesResponse struct {
	SchemaVersion   int    `json:"schema_version"`
	ReadAt          string `json:"read_at"`
	BrokerAvailable bool   `json:"broker_available"`
	// StartModes is known, pending or unavailable (ADR 0011 A9); while it is
	// not known, every start_mode is "" and the units that only the unit files
	// name are missing. StartModesReadAt is the time of the unit-file read the
	// start modes come from, null unless known. Both are added fields.
	StartModes       string        `json:"start_modes"`
	StartModesReadAt *string       `json:"start_modes_read_at"`
	Counts           serviceCounts `json:"counts"`
	Services         []serviceRow  `json:"services"`
}

// list answers GET /api/v1/services: the cached or a fresh read, and whether
// the broker's socket exists (a stat only; opening the page never starts the
// root process).
func (a *servicesAPI) list(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if a.unavailable(w, r) {
		return
	}
	l, e := a.svc.List(r.Context())
	if e != nil {
		if e.Code == services.CodeUnavailable {
			writeError(w, r, http.StatusServiceUnavailable, errServicesUnavailable)
			return
		}
		writeError(w, r, http.StatusServiceUnavailable, apiError{
			Code:     string(services.CodeReadFailed),
			Message:  "The service list could not be read.",
			Recovery: "Try again in a few seconds.",
		})
		return
	}
	rows := make([]serviceRow, len(l.Rows))
	for i, s := range l.Rows {
		rows[i] = serviceRow{Name: s.Name, Description: s.Description, LoadState: s.LoadState, ActiveState: s.ActiveState,
			SubState: s.SubState, StartMode: s.StartMode, Denied: s.Denied, RestartOnly: s.RestartOnly,
			Critical: s.Critical, Actions: s.Actions}
	}
	var modesAt *string
	if l.StartModes == services.StartModesKnown && !l.StartModesReadAt.IsZero() {
		s := l.StartModesReadAt.UTC().Format(time.RFC3339)
		modesAt = &s
	}
	writeJSON(w, http.StatusOK, servicesResponse{
		SchemaVersion: ServicesSchemaVersion, ReadAt: l.ReadAt.UTC().Format(time.RFC3339),
		BrokerAvailable: a.svc.BrokerAvailable(),
		StartModes:      l.StartModes, StartModesReadAt: modesAt,
		Counts:   serviceCounts{Total: l.Counts.Total, Active: l.Counts.Active, Failed: l.Counts.Failed, Inactive: l.Counts.Inactive},
		Services: rows,
	})
}

var errInvalidRequest = apiError{Code: "invalid_request", Message: "The request could not be read.", Recovery: "Reload the page and try again."}

// action answers POST /api/v1/services/action. The order: the body (at most
// 1 KiB, JSON, exactly the members unit and action), rule 1 of B3 for the
// unit and the closed set of the action (both 400 invalid_request, not
// audited), then the job's own checks: the broker's socket exists (503
// unavailable) and no action runs (409 busy), both audited; else 202 and the
// job runs on its own. No password (D-C87 item 7): the click is the
// confirmation.
func (a *servicesAPI) action(w http.ResponseWriter, r *http.Request, s *auth.Session) {
	if a.unavailable(w, r) {
		return
	}
	var body struct {
		Unit   string `json:"unit"`
		Action string `json:"action"`
	}
	if !decodeUpdateBody(w, r, &body) {
		return
	}
	if !unitname.Valid(body.Unit) || !services.ValidAction(body.Action) {
		writeError(w, r, http.StatusBadRequest, errInvalidRequest)
		return
	}
	switch a.svc.Submit(services.Request{Unit: body.Unit, Action: body.Action, User: s.Username, CorrelationID: correlationID(r)}) {
	case services.SubmitAccepted:
		writeJSON(w, http.StatusAccepted, map[string]any{"schema_version": ServicesSchemaVersion, "accepted": true})
	case services.SubmitBusy:
		writeError(w, r, http.StatusConflict, apiError{Code: "busy", Message: "Another service action is still running.", Recovery: "Wait until it has finished."})
	case services.SubmitUnavailable:
		writeError(w, r, http.StatusServiceUnavailable, apiError{Code: "unavailable", Message: "The privilege broker cannot be reached.", Recovery: "The list stays readable. Check the installation of Oarfish on the device."})
	default:
		writeError(w, r, http.StatusBadRequest, errInvalidRequest)
	}
}

// actionView is the last action in GET /api/v1/services/action.
type actionView struct {
	Unit        string `json:"unit"`
	Action      string `json:"action"`
	Step        string `json:"step"`
	StartedAt   string `json:"started_at"`
	FinishedAt  string `json:"finished_at,omitempty"`
	Code        string `json:"code,omitempty"`
	State       string `json:"state,omitempty"`
	RetryAfterS int    `json:"retry_after_s,omitempty"`
}

// lastAction answers GET /api/v1/services/action from memory: no systemctl,
// no broker.
func (a *servicesAPI) lastAction(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if a.unavailable(w, r) {
		return
	}
	resp := struct {
		SchemaVersion int         `json:"schema_version"`
		Action        *actionView `json:"action"`
	}{SchemaVersion: ServicesSchemaVersion}
	if v := a.svc.Last(); v != nil {
		av := &actionView{Unit: v.Unit, Action: v.Action, Step: v.Step, StartedAt: v.StartedAt.UTC().Format(time.RFC3339)}
		if v.Step == services.StepDone {
			av.FinishedAt = v.FinishedAt.UTC().Format(time.RFC3339)
			av.Code, av.State, av.RetryAfterS = v.Code, v.State, v.RetryAfterS
		}
		resp.Action = av
	}
	writeJSON(w, http.StatusOK, resp)
}
