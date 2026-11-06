package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/swingstate/oarfish/backend/internal/audit"
	"github.com/swingstate/oarfish/backend/internal/auth"
	"github.com/swingstate/oarfish/backend/internal/updater"
)

// UpdateService is what the update routes need from the updater (ADR 0010 U9,
// A11). *updater.Updater implements it.
type UpdateService interface {
	State(ctx context.Context) updater.State
	Check(ctx context.Context, force bool) (updater.State, *updater.Error)
	Notice(ctx context.Context) updater.Notice
	Reserve() (updater.Slot, *updater.Error)
	Applicable(version string) *updater.Error
	Resume(version string) *updater.Error
	Awaiting(version string) bool
	Discard(ctx context.Context, version, correlationID string) *updater.Error
}

var _ UpdateService = (*updater.Updater)(nil)

// WithUpdater enables the update routes of ADR 0010 U9 and A11.1:
// GET /api/v1/update, GET /api/v1/update/notice and the POST routes check,
// apply and discard. Without it they answer 503.
func WithUpdater(s UpdateService) Option { return func(c *config) { c.updater = s } }

type updateAPI struct {
	authAPI
	svc UpdateService
}

func (a *updateAPI) unavailable(w http.ResponseWriter, r *http.Request) bool {
	if a.svc != nil {
		return false
	}
	writeError(w, r, http.StatusServiceUnavailable, apiError{
		Code:     "update_unavailable",
		Message:  "Updates are not available in this build.",
		Recovery: "Restart Oarfish.",
	})
	return true
}

// updateStatus maps a typed code to an HTTP status.
func updateStatus(c updater.Code) int {
	switch c {
	case updater.CodeBadRequest:
		return http.StatusBadRequest
	case updater.CodeNotFound:
		return http.StatusNotFound
	case updater.CodeBusy, updater.CodeNotChecked, updater.CodeVersionMismatch, updater.CodeCooldown,
		updater.CodeConfirmationRequired, updater.CodeAlreadyInstalled, updater.CodeCurrentUnhealthy:
		return http.StatusConflict
	case updater.CodeRateLimited:
		return http.StatusTooManyRequests
	case updater.CodeUnavailable, updater.CodeCacheError:
		return http.StatusServiceUnavailable
	}
	return http.StatusUnprocessableEntity
}

// updateError answers with the code of an updater error and its fixed text.
// Nothing of the underlying error, the network or the helper is in it.
func updateError(w http.ResponseWriter, r *http.Request, e *updater.Error) {
	ae := apiError{Code: string(e.Code), Message: updater.Text(e.Code), Recovery: "Open the update page to see the state, then try again."}
	if e.Code == updater.CodeRateLimited && e.RetryAfter > 0 {
		ae.RetryAfterSeconds = retryAfter(w, e.RetryAfter)
	}
	writeError(w, r, updateStatus(e.Code), ae)
}

// decodeOptionalBody is decodeBody, but an empty body is the empty object.
func decodeOptionalBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAuthBody))
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if errors.Is(err, io.EOF) {
		return true
	}
	if err == nil && dec.More() {
		err = errors.New("trailing data")
	}
	if err == nil {
		return true
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, r, http.StatusRequestEntityTooLarge, apiError{Code: "request_too_large", Message: "The request is too large.", Recovery: "Send a smaller request."})
		return false
	}
	writeError(w, r, http.StatusBadRequest, apiError{Code: "invalid_request", Message: "The request could not be read.", Recovery: "Reload the page and try again."})
	return false
}

func (a *updateAPI) state(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if a.unavailable(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, a.svc.State(r.Context()))
}

func (a *updateAPI) notice(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if a.unavailable(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, a.svc.Notice(r.Context()))
}

func (a *updateAPI) check(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if a.unavailable(w, r) {
		return
	}
	var body struct {
		Force bool `json:"force"`
	}
	if !decodeOptionalBody(w, r, &body) {
		return
	}
	st, e := a.svc.Check(r.Context(), body.Force)
	if e != nil {
		updateError(w, r, e)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// apply starts an update. The order is the one of ADR 0010 U9 and U12: cheap
// checks first (the version must be the one of the last check), then the fresh
// password through the shared limiter and the single Argon2id hash, which is
// finished and its memory returned before the download starts, then the job.
func (a *updateAPI) apply(w http.ResponseWriter, r *http.Request, s *auth.Session) {
	if a.unavailable(w, r) {
		return
	}
	var body struct {
		Version             string `json:"version"`
		CurrentPassword     string `json:"current_password"`
		ComponentsConfirmed bool   `json:"components_confirmed"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if !validUpdateVersion(body.Version) {
		writeError(w, r, http.StatusBadRequest, apiError{Code: "invalid_request", Message: "The request could not be read.", Recovery: "Reload the page and try again."})
		return
	}
	target := audit.UpdateTarget(body.Version)

	// Second stage: a job waits for the confirmation of the component update.
	if body.ComponentsConfirmed && a.svc.Awaiting(body.Version) {
		if err := a.cfg.auth.ConfirmPassword(r.Context(), body.CurrentPassword); err != nil {
			a.accountError(w, r, audit.UpdateStarted, target, s.Username, err)
			return
		}
		if e := a.svc.Resume(body.Version); e != nil {
			updateError(w, r, e)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"schema_version": updater.StateSchemaVersion, "accepted": true})
		return
	}

	slot, e := a.svc.Reserve()
	if e != nil {
		updateError(w, r, e)
		return
	}
	defer slot.Release()
	if e := a.svc.Applicable(body.Version); e != nil {
		updateError(w, r, e)
		return
	}
	if err := a.cfg.auth.ConfirmPassword(r.Context(), body.CurrentPassword); err != nil {
		a.accountError(w, r, audit.UpdateStarted, target, s.Username, err)
		return
	}
	if e := slot.Start(updater.ApplyRequest{Version: body.Version, User: s.Username,
		CorrelationID: correlationID(r), ComponentsConfirmed: body.ComponentsConfirmed}); e != nil {
		updateError(w, r, e)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"schema_version": updater.StateSchemaVersion, "accepted": true})
}

func (a *updateAPI) discard(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if a.unavailable(w, r) {
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if !validUpdateVersion(body.Version) {
		writeError(w, r, http.StatusBadRequest, apiError{Code: "invalid_request", Message: "The request could not be read.", Recovery: "Reload the page and try again."})
		return
	}
	if e := a.svc.Discard(r.Context(), body.Version, correlationID(r)); e != nil {
		updateError(w, r, e)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schema_version": updater.StateSchemaVersion, "ok": true})
}

// validUpdateVersion is the version rule of ADR 0010 U2.
func validUpdateVersion(v string) bool { return updater.ValidVersion(v) }
