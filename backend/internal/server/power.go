package server

import (
	"context"
	"net/http"
	"time"

	"github.com/swingstate/oarfish/backend/internal/audit"
	"github.com/swingstate/oarfish/backend/internal/auth"
	"github.com/swingstate/oarfish/backend/internal/brokerclient"
	"github.com/swingstate/oarfish/backend/internal/updater"
)

// PowerSchemaVersion is the version of the answers of the power routes.
const PowerSchemaVersion = 1

// UpdateActivity is the update state a reboot or shutdown needs (ADR 0011 B6).
// *updater.Updater implements it.
type UpdateActivity interface {
	Activity(ctx context.Context) updater.Activity
}

// ServiceActivity tells whether a service action runs. *services.Manager
// implements it.
type ServiceActivity interface {
	ActionRunning() bool
}

var _ UpdateActivity = (*updater.Updater)(nil)

// PowerConfig is what the power routes need (ADR 0011 B4, B5, B6; step G7.5).
type PowerConfig struct {
	// Broker is the client of the broker's socket. Required.
	Broker brokerclient.PowerAPI
	// Updates gives the update job and the helper's status; nil: no update.
	Updates UpdateActivity
	// Services tells whether a service action runs; nil: none.
	Services ServiceActivity
	// Views is the number of open live views (the event streams of the
	// Dashboard); nil: none.
	Views func() int
}

// WithPower enables GET /api/v1/power and POST /api/v1/power: reboot and
// shutdown from the Activity page. Without it they answer 503.
func WithPower(p PowerConfig) Option { return func(c *config) { c.power = &p } }

// updateCheckTimeout bounds the read of the helper's status in a power request.
// With the password hash (about 0.5 s on the reference device) and the broker's
// PowerDeadline (20 s), a power request ends well within the server's write
// timeout of 30 s.
const updateCheckTimeout = 3 * time.Second

type powerAPI struct {
	authAPI
	p *PowerConfig
}

func (a *powerAPI) unavailable(w http.ResponseWriter, r *http.Request) bool {
	if a.p != nil && a.p.Broker != nil {
		return false
	}
	writeError(w, r, http.StatusServiceUnavailable, apiError{
		Code:     "power_unavailable",
		Message:  "Restart and shutdown are not available in this build.",
		Recovery: "Restart Oarfish.",
	})
	return true
}

func (a *powerAPI) updates(r *http.Request) updater.Activity {
	if a.p.Updates == nil {
		return updater.Activity{}
	}
	ctx, cancel := context.WithTimeout(r.Context(), updateCheckTimeout)
	defer cancel()
	return a.p.Updates.Activity(ctx)
}

func (a *powerAPI) serviceRunning() bool { return a.p.Services != nil && a.p.Services.ActionRunning() }

// The kinds of the list of waiting actions (ADR 0011 B6, ORB-PWR-002 as D-C87
// item 11 changed it). The set is closed; the page shows a kind it does not
// know neutrally.
const (
	waitUpdateStaged         = "update_staged"
	waitUpdateRunning        = "update_running"
	waitServiceActionRunning = "service_action_running"
	waitOtherSessions        = "other_sessions"
	waitOtherViews           = "other_views"
)

type waitingItem struct {
	Kind  string `json:"kind"`
	Count int    `json:"count,omitempty"`
}

// waiting answers GET /api/v1/power: whether the broker's socket exists (a stat
// only) and what a reboot or shutdown would interrupt. It runs no program and
// opens no connection to the broker; the update part is the read of GET
// /api/v1/update (the job in memory and the helper's status). Other sessions
// are the service's own (Sessions.Len, the caller's session not counted);
// login sessions of the device are not counted (D-C87 item 11).
func (a *powerAPI) waiting(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if a.unavailable(w, r) {
		return
	}
	items := []waitingItem{}
	up := a.updates(r)
	if up.Staged {
		items = append(items, waitingItem{Kind: waitUpdateStaged})
	}
	if up.Running {
		items = append(items, waitingItem{Kind: waitUpdateRunning})
	}
	if a.serviceRunning() {
		items = append(items, waitingItem{Kind: waitServiceActionRunning})
	}
	if a.cfg.auth != nil {
		if n := a.cfg.auth.Sessions().Len() - 1; n >= 1 {
			items = append(items, waitingItem{Kind: waitOtherSessions, Count: n})
		}
	}
	if a.p.Views != nil {
		if n := a.p.Views(); n >= 1 {
			items = append(items, waitingItem{Kind: waitOtherViews, Count: n})
		}
	}
	writeJSON(w, http.StatusOK, struct {
		SchemaVersion   int           `json:"schema_version"`
		BrokerAvailable bool          `json:"broker_available"`
		Waiting         []waitingItem `json:"waiting"`
	}{PowerSchemaVersion, a.p.Broker.Available(), items})
}

var powerOps = map[string]struct {
	op     brokerclient.PowerOp
	action audit.Action
}{
	"reboot":   {brokerclient.OpReboot, audit.SystemReboot},
	"poweroff": {brokerclient.OpPoweroff, audit.SystemPoweroff},
}

// powerError is the typed error of a refused power request. The message is
// fixed English text for the code; the page shows its own sentence.
func powerError(code brokerclient.Code) (int, apiError) {
	switch code {
	case brokerclient.CodeBusy:
		return http.StatusConflict, apiError{Code: "busy", Message: "Another action is still running.", Recovery: "Wait until it has finished."}
	case brokerclient.CodeUpdateRunning:
		return http.StatusConflict, apiError{Code: "update_running", Message: "An Oarfish update is running.", Recovery: "Restart and shut down after the update."}
	case brokerclient.CodeAlreadyRequested:
		return http.StatusConflict, apiError{Code: "already_requested", Message: "A restart or shutdown was already requested.", Recovery: "Wait for the device."}
	case brokerclient.CodeUnavailable:
		return http.StatusServiceUnavailable, apiError{Code: "unavailable", Message: "The privilege broker cannot be reached.", Recovery: "Check the installation of Oarfish on the device."}
	case brokerclient.CodeJobFailed:
		return http.StatusBadGateway, apiError{Code: "job_failed", Message: "The system did not accept the request.", Recovery: "Try again."}
	case brokerclient.CodeTimeout:
		return http.StatusBadGateway, apiError{Code: "timeout", Message: "The system did not answer in time.", Recovery: "Wait a moment, then reload the page."}
	case brokerclient.CodeBadRequest:
		return http.StatusBadGateway, apiError{Code: "bad_request", Message: "The request failed.", Recovery: "Reload the page and try again."}
	}
	return http.StatusBadGateway, apiError{Code: "internal", Message: "The request failed.", Recovery: "Try again."}
}

// request answers POST /api/v1/power. The order: the session and the three
// CSRF layers (the router), the body (at most 1 KiB, JSON, exactly the members
// action and current_password), the closed set of the action (400
// invalid_request, not audited as a power action), the password through the
// shared limiter (wrong_password or rate_limited as on the update route,
// audited), the broker's socket (503 unavailable, audited), the update (409
// update_running, audited), a running service action (409 busy, audited), then
// the broker inside the request. A wrong or limited password never reaches the
// broker. accepted is audited once, flushed and fsynced before the answer.
func (a *powerAPI) request(w http.ResponseWriter, r *http.Request, s *auth.Session) {
	if a.unavailable(w, r) {
		return
	}
	var body struct {
		Action          string `json:"action"`
		CurrentPassword string `json:"current_password"`
	}
	if !decodeUpdateBody(w, r, &body) {
		return
	}
	op, ok := powerOps[body.Action]
	if !ok {
		body.CurrentPassword = ""
		writeError(w, r, http.StatusBadRequest, errInvalidRequest)
		return
	}
	err := a.cfg.auth.ConfirmPassword(r.Context(), body.CurrentPassword)
	body.CurrentPassword = "" // not needed past the check
	if err != nil {
		a.accountError(w, r, op.action, audit.TargetSystem, s.Username, err)
		return
	}
	refuse := func(code brokerclient.Code) {
		a.record(r, s.Username, op.action, audit.TargetSystem, audit.PowerResult(string(code)))
		status, e := powerError(code)
		writeError(w, r, status, e)
	}
	if !a.p.Broker.Available() {
		refuse(brokerclient.CodeUnavailable)
		return
	}
	if a.updates(r).Running {
		refuse(brokerclient.CodeUpdateRunning)
		return
	}
	if a.serviceRunning() {
		refuse(brokerclient.CodeBusy)
		return
	}
	// The broker call does not end with the request: a browser that closes now
	// must not turn an operation that systemd may have queued into an unaudited
	// one. PowerDeadline bounds it.
	code := a.p.Broker.Power(context.WithoutCancel(r.Context()), op.op, correlationID(r))
	if code != brokerclient.CodeAccepted {
		refuse(code)
		return
	}
	if a.cfg.audit != nil {
		// A failed write or sync is counted by the log; the answer stays
		// accepted, because systemd has already queued the job.
		a.cfg.audit.RecordDurable(audit.Event{User: s.Username, Action: op.action, Target: audit.TargetSystem,
			Result: audit.Success, CorrelationID: correlationID(r)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"schema_version": PowerSchemaVersion, "accepted": true})
}
