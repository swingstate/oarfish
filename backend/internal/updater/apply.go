package updater

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/swingstate/oarfish/backend/internal/audit"
	"github.com/swingstate/oarfish/backend/internal/relverify"
)

type jobKind string

const (
	jobCheck jobKind = "check"
	jobApply jobKind = "apply"
)

// The steps of a job. The set is closed; the page shows them by name.
type step string

const (
	stepChecking    step = "checking"
	stepFetching    step = "fetching_metadata"
	stepDownloading step = "downloading"
	stepStaging     step = "staging"
	stepAwaiting    step = "awaiting_components_confirmation"
	stepActivating  step = "activating"
	stepDone        step = "done"
	stepFailed      step = "failed"
)

// jobState is the one job (A11.3). It outlives the browser session that
// started it: nothing here refers to a request.
type jobState struct {
	kind    jobKind
	step    step
	version string
	cid     string
	differs []string // what the staged release replaces (the stage answer, closed list)

	stageRefused bool      // err is the helper's refusal of stage (for the installer view, F1)
	activated    time.Time // when the helper accepted activate (for the hand-over rule, F4)

	started, finished time.Time
	err               *Error
	done              bool
	doneCh            chan struct{}

	resume    chan struct{} // the second confirmation arrived
	abort     chan struct{} // discard while waiting for it
	abortOnce sync.Once
}

func (u *Updater) jobRunningLocked() bool { return u.reserved || (u.job != nil && !u.job.done) }

// startJobLocked records the job and, for a check, starts it. The caller holds
// the lock and has checked that no job runs.
func (u *Updater) startJobLocked(kind jobKind, version string) *jobState {
	now := u.now()
	j := &jobState{kind: kind, step: stepChecking, version: version, started: now, doneCh: make(chan struct{}),
		resume: make(chan struct{}, 1), abort: make(chan struct{})}
	u.job = j
	if kind == jobCheck {
		u.attempted, u.lastAttempt = true, now
		u.wg.Add(1)
		go func() { defer u.wg.Done(); u.runCheck(j) }()
	}
	return j
}

func (u *Updater) setStep(j *jobState, s step) {
	u.mu.Lock()
	j.step = s
	u.mu.Unlock()
}

func (u *Updater) finishJob(j *jobState, e *Error) {
	u.mu.Lock()
	j.done, j.err, j.finished = true, e, u.now()
	j.step = stepDone
	if e != nil {
		j.step = stepFailed
	}
	u.mu.Unlock()
	close(j.doneCh)
}

// Slot is the reservation of the one job. It is taken before the password is
// checked, so that a second request answers busy while the hash runs. It is an
// interface so that the routes can be tested against a fake.
type Slot interface {
	// Release gives the slot back (the password was wrong, the request was refused).
	Release()
	// Start turns the reservation into the running apply job and returns at once.
	// The audit entry update_started is written here: the administrator confirmed.
	Start(req ApplyRequest) *Error
}

type slot struct {
	u    *Updater
	once sync.Once
}

// Reserve takes the slot of the one job, or answers busy.
func (u *Updater) Reserve() (Slot, *Error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.jobRunningLocked() {
		return nil, fail(CodeBusy)
	}
	u.reserved = true
	return &slot{u: u}, nil
}

func (s *slot) Release() {
	s.once.Do(func() {
		s.u.mu.Lock()
		s.u.reserved = false
		s.u.mu.Unlock()
	})
}

// Applicable says whether version may be applied: it must be the version of the
// last check, and the advisory verification of that check must have passed. The
// helper validates it again (U9).
func (u *Updater) Applicable(version string) *Error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.res == nil || u.res.avail == nil {
		return fail(CodeNotChecked)
	}
	av := u.res.avail
	if av.Version != version {
		return fail(CodeVersionMismatch)
	}
	if av.VerifyError != nil {
		return fail(av.VerifyError.Code)
	}
	if !av.Verified {
		return fail(CodeManifestInvalid)
	}
	return nil
}

// ApplyRequest is what the route hands over after the password gate.
type ApplyRequest struct {
	Version             string
	User                string // the account name, for update_started
	CorrelationID       string // the request's server-made ID; it links both entries
	ComponentsConfirmed bool
}

func (s *slot) Start(req ApplyRequest) *Error {
	u := s.u
	if e := u.Applicable(req.Version); e != nil {
		s.Release()
		return e
	}
	u.mu.Lock()
	j := u.startJobLocked(jobApply, req.Version)
	j.cid = req.CorrelationID
	j.step = stepFetching
	u.reserved = false
	u.wg.Add(1)
	u.mu.Unlock()
	s.once.Do(func() {})
	u.record(audit.Event{User: req.User, Action: audit.UpdateStarted, Target: audit.UpdateTarget(req.Version),
		Result: audit.Success, CorrelationID: req.CorrelationID})
	go func() {
		defer u.wg.Done()
		u.runApply(j, req.ComponentsConfirmed)
	}()
	return nil
}

// Resume delivers the second confirmation (components_confirmed) to a job that
// waits for it after stage (ADR 0010 U9, U15 rule 7).
func (u *Updater) Resume(version string) *Error {
	u.mu.Lock()
	defer u.mu.Unlock()
	j := u.job
	if j == nil || j.done || j.step != stepAwaiting || j.version != version {
		return fail(CodeNotFound)
	}
	select {
	case j.resume <- struct{}{}:
	default:
	}
	return nil
}

// Awaiting reports whether a job waits for the second confirmation of version.
func (u *Updater) Awaiting(version string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	j := u.job
	return j != nil && !j.done && j.step == stepAwaiting && j.version == version
}

func (u *Updater) cacheDir(version string) (string, *Error) {
	if u.cfg.CacheDir == "" {
		return "", fail(CodeCacheError)
	}
	base := filepath.Join(u.cfg.CacheDir, "update")
	dir := filepath.Join(base, version)
	// The cache is recoverable and the service's own: a leftover is removed.
	_ = os.RemoveAll(dir)
	for _, d := range []string{base, dir} {
		if err := os.Mkdir(d, 0o755); err != nil && !os.IsExist(err) {
			return "", fail(CodeCacheError)
		}
		// The unit's UMask would make the directory unreadable for the helper.
		if err := os.Chmod(d, 0o755); err != nil {
			return "", fail(CodeCacheError)
		}
	}
	return dir, nil
}

// CleanCache removes everything below <cache>/update: the files of an
// interrupted download and a stale directory. The cache is recoverable (spec
// 8.2); it is called once at start, and the files of a version are removed
// after its stage.
func (u *Updater) CleanCache() {
	if u.cfg.CacheDir != "" {
		_ = os.RemoveAll(filepath.Join(u.cfg.CacheDir, "update"))
	}
}

// withRetry runs f and, for a failure that may be temporary, once more after
// 5 s (U3). There is no resume: f starts the file from the beginning.
func (u *Updater) withRetry(ctx context.Context, f func() *Error) *Error {
	e := f()
	if e == nil || !retryable(e.Code) {
		return e
	}
	if u.sleep(ctx, u.retry) != nil {
		return fail(CodeCancelled)
	}
	return f()
}

func retryable(c Code) bool {
	switch c {
	case CodeTimeout, CodeUnreachable, CodeDNSFailure, CodeBadResponse:
		return true
	}
	n, ok := httpStatus(c)
	return ok && n >= 500
}

// runApply is the work of an apply job: metadata, bundle, advisory
// verification, stage, the pause for a component update, activate.
func (u *Updater) runApply(j *jobState, confirmed bool) {
	e := u.applyFlow(j, confirmed)
	u.finishJob(j, e)
	if e != nil && e.Code != CodeCancelled { // a cancel has its own entry (Discard) or is a shutdown
		u.logf("update: the update did not start", e.Code)
		// Nothing was activated, so the helper writes no result for this: the
		// service writes the finishing entry itself (A6).
		u.record(audit.Event{User: audit.NoUser, Action: audit.UpdateFinished, Target: audit.UpdateTarget(j.version),
			Result: audit.UpdateResult(string(e.Code)), CorrelationID: j.cid})
	}
}

func (u *Updater) applyFlow(j *jobState, confirmed bool) *Error {
	ctx := u.ctx
	version := j.version
	dir, e := u.cacheDir(version)
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	installed, e := u.installed()
	if e != nil {
		return e
	}

	// The three small files, into the cache and back into memory for the
	// advisory verification. What is verified is what the helper will copy.
	files := map[string][]byte{}
	for _, name := range []string{"checksums.txt", "checksums.txt.sig", relverify.ManifestName} {
		name := name
		if e := u.withRetry(ctx, func() *Error {
			return u.gh.downloadTo(ctx, u.gh.assetURL(version, name), dir, name, maxSmallFile, u.stall, smallTimeout,
				func(string, int64) *Error { return nil })
		}); e != nil {
			return e
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fail(CodeCacheError)
		}
		files[name] = b
	}
	rel, e := u.verifyMetadata(installed, version, files["checksums.txt"], files["checksums.txt.sig"], files[relverify.ManifestName])
	if e != nil {
		return e
	}

	// The bundle: streamed, never held in memory, at most the signed size.
	u.setStep(j, stepDownloading)
	ver, _ := relverify.ParseVersion(version)
	bundle := relverify.BundleFileName(ver)
	if e := u.withRetry(ctx, func() *Error {
		return u.gh.downloadTo(ctx, u.gh.assetURL(version, bundle), dir, bundle, rel.Manifest.Bundle.Size, u.stall, u.abs,
			func(sum string, n int64) *Error {
				if err := rel.CheckBundle(sum, n); err != nil {
					return fail(Code(codeOfRelverify(err)))
				}
				return nil
			})
	}); e != nil {
		return e
	}

	// The helper copies and verifies its own copy.
	u.setStep(j, stepStaging)
	if u.cfg.Helper == nil {
		return fail(CodeUnavailable)
	}
	info, he := u.cfg.Helper.Stage(ctx, version, j.cid)
	if he != nil {
		u.mu.Lock()
		j.stageRefused = true
		u.mu.Unlock()
		return he
	}
	_ = os.RemoveAll(dir) // the staged copy is the helper's; the cache is done
	u.mu.Lock()
	j.differs = info.Differs
	u.mu.Unlock()

	if info.ComponentsDiffer && !confirmed {
		u.setStep(j, stepAwaiting)
		t := time.NewTimer(u.pause)
		defer t.Stop()
		select {
		case <-j.resume:
		case <-j.abort:
			return fail(CodeCancelled)
		case <-t.C:
			_ = u.cfg.Helper.Discard(context.WithoutCancel(ctx), version)
			return fail(CodeStageExpired)
		case <-ctx.Done():
			return fail(CodeCancelled)
		}
		confirmed = true
	}

	u.setStep(j, stepActivating)
	if _, he := u.cfg.Helper.Activate(ctx, version, j.cid, confirmed); he != nil {
		// F2 (ADR 0010 A17): a refusal of activate (cooldown, confirmation,
		// pre-check, damaged components, …) can leave the release staged, and the
		// next stage would answer busy until it expires. The service discards it,
		// so that a new attempt is possible at once; the cooldown markers of the
		// helper are not touched by discard, and discard acts only on a release
		// that is still staged under this version. A failure of the call itself
		// (unavailable, timeout, cancelled) leaves open whether the helper took
		// the request, so nothing is sent then and the expiry of U5 applies.
		if helperCodes[he.Code] {
			_ = u.cfg.Helper.Discard(context.WithoutCancel(ctx), version)
		}
		return he
	}
	u.mu.Lock()
	j.activated = u.now()
	u.mu.Unlock()
	return nil
}

// Discard removes a staged release (POST /api/v1/update/discard). A job that
// waits for the second confirmation of that version ends; any other running job
// answers busy. The finishing entry says aborted.
func (u *Updater) Discard(ctx context.Context, version, correlationID string) *Error {
	if !validVersion(version) {
		return fail(CodeBadRequest)
	}
	if u.cfg.Helper == nil {
		return fail(CodeUnavailable)
	}
	u.mu.Lock()
	if j := u.job; u.reserved || (j != nil && !j.done) {
		if j == nil || j.step != stepAwaiting || j.version != version {
			u.mu.Unlock()
			return fail(CodeBusy)
		}
		j.abortOnce.Do(func() { close(j.abort) })
	}
	u.mu.Unlock()
	e := u.cfg.Helper.Discard(ctx, version)
	res := audit.Aborted
	if e != nil {
		res = audit.UpdateResult(string(e.Code))
	}
	u.record(audit.Event{User: audit.NoUser, Action: audit.UpdateFinished, Target: audit.UpdateTarget(version),
		Result: res, CorrelationID: correlationID})
	return e
}
