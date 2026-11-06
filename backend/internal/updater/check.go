package updater

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/swingstate/oarfish/backend/internal/relverify"
)

// checkResult is the shared in-memory result of the last successful check. The
// update page and the notice read the same one (A11.1).
type checkResult struct {
	at    time.Time
	avail *Available // nil: nothing newer and eligible
}

// candidate is a release the service may offer: a published release whose tag
// is "v" plus a valid version (U3: the tag is validated strictly, nothing else
// of the answer names a URL).
type candidate struct {
	ver   relverify.Version
	notes string
}

func candidates(list []releaseWire) []candidate {
	var out []candidate
	for _, r := range list {
		if r.Draft || !strings.HasPrefix(r.TagName, "v") {
			continue
		}
		v, err := relverify.ParseVersion(r.TagName[1:])
		if err != nil {
			continue
		}
		out = append(out, candidate{ver: v, notes: plainNotes(r.Body)})
	}
	return out
}

// plainNotes reduces release notes to plain text: valid UTF-8, no control
// characters except newline and tab, at most 4 KiB. They are shown as text, never
// as markup (U9).
func plainNotes(s string) string {
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(s, "") {
		if r == '\r' {
			continue
		}
		if r < 0x20 && r != '\n' && r != '\t' || r == 0x7f || r == utf8.RuneError {
			continue
		}
		if b.Len()+utf8.RuneLen(r) > maxNotesBytes {
			break
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// ---- Check and Notice ------------------------------------------------------

// Check is POST /api/v1/update/check. Without force it is the check of opening
// the update page: it asks GitHub only when the last attempt, of any outcome,
// is older than 60 minutes. With force ("Check now") the last attempt must be
// older than 60 s, device-wide. Otherwise the answer comes from memory with
// Throttled set. While a rate-limit wait runs, no request is made. The check
// runs as the job of the updater; the call waits for it only a short bound
// (3 s) and the page polls for the rest (A11.3). A running job answers busy.
func (u *Updater) Check(ctx context.Context, force bool) (State, *Error) {
	u.mu.Lock()
	if u.jobRunningLocked() {
		u.mu.Unlock()
		return State{}, fail(CodeBusy)
	}
	every := u.pageEvery
	if force {
		every = u.forceEvery
	}
	now := u.now()
	var j *jobState
	throttled := false
	switch {
	case u.attempted && now.Sub(u.lastAttempt) < every:
		throttled = true
	case now.Before(u.waitUntil):
		u.lastErr = &Error{Code: CodeRateLimited, RetryAfter: u.waitUntil.Sub(now)}
	default:
		j = u.startJobLocked(jobCheck, "")
	}
	u.mu.Unlock()
	if j != nil {
		u.wait(ctx, j)
	}
	return u.stateWith(ctx, throttled), nil
}

// wait blocks until the job is done, the bound has passed or ctx ends.
func (u *Updater) wait(ctx context.Context, j *jobState) {
	t := time.NewTimer(u.bound)
	defer t.Stop()
	select {
	case <-j.doneCh:
	case <-t.C:
	case <-ctx.Done():
	}
}

// Notice is the answer of GET /api/v1/update/notice (A11.1).
type Notice struct {
	SchemaVersion    int              `json:"schema_version"`
	InstalledVersion string           `json:"installed_version"`
	Available        *NoticeAvailable `json:"available"`
}

// NoticeAvailable is the version and channel of a newer release.
type NoticeAvailable struct {
	Version string `json:"version"`
	Channel string `json:"channel"`
}

// NoticeSchemaVersion is the version of the Notice layout.
const NoticeSchemaVersion = 1

// Notice answers from the shared in-memory result. It asks GitHub only when
// that result is older than 24 hours (or there is none), the last attempt of
// any outcome is older than 60 minutes, no rate-limit wait runs and no job is
// running. It never answers busy and never shows an error: offline or any
// other failure is silent here, the typed error stays on the update page.
// There is no timer: a restart of the service forgets the result and allows one
// new check. The UI does not call this route until a user-visible switch exists
// (ADR 0010 A11.1, open point).
func (u *Updater) Notice(ctx context.Context) Notice {
	u.mu.Lock()
	now := u.now()
	due := !u.jobRunningLocked() &&
		(u.res == nil || now.Sub(u.res.at) >= u.noticeEvery) &&
		(!u.attempted || now.Sub(u.lastAttempt) >= u.noticeBackof) &&
		!now.Before(u.waitUntil)
	var j *jobState
	if due {
		j = u.startJobLocked(jobCheck, "")
	}
	u.mu.Unlock()
	if j != nil {
		u.wait(ctx, j)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	n := Notice{SchemaVersion: NoticeSchemaVersion, InstalledVersion: u.cfg.Version}
	if u.res != nil && u.res.avail != nil {
		n.Available = &NoticeAvailable{Version: u.res.avail.Version, Channel: u.res.avail.Channel}
	}
	return n
}

// ---- the check itself --------------------------------------------------------

// runCheck is the work of a check job.
func (u *Updater) runCheck(j *jobState) {
	e := u.doCheck(u.ctx)
	u.mu.Lock()
	u.lastErr = e
	if e != nil && e.Code == CodeRateLimited && e.RetryAfter > 0 {
		u.waitUntil = u.now().Add(e.RetryAfter)
	}
	u.mu.Unlock()
	if e != nil {
		u.logf("update: the check failed", e.Code)
	}
	u.finishJob(j, e)
}

func (u *Updater) doCheck(ctx context.Context) *Error {
	installed, e := u.installed()
	if e != nil {
		return e
	}
	u.mu.Lock()
	etag := u.etag
	if !u.haveList {
		etag = ""
	}
	u.mu.Unlock()
	list, notModified, wait, e := u.gh.listReleases(ctx, etag)
	u.mu.Lock()
	if wait > 0 && (e == nil || e.Code == CodeRateLimited) {
		u.waitUntil = u.now().Add(wait)
	}
	u.mu.Unlock()
	if e != nil {
		return e
	}
	u.mu.Lock()
	switch {
	case notModified && u.haveList:
		list.releases = u.releases
	case notModified:
		u.mu.Unlock()
		return fail(CodeBadResponse)
	default:
		u.etag, u.releases, u.haveList = list.etag, list.releases, true
	}
	u.mu.Unlock()

	cands := candidates(list.releases)
	vers := make([]relverify.Version, len(cands))
	for i, c := range cands {
		vers[i] = c.ver
	}
	best, found := relverify.Latest(installed, vers)
	if !found {
		u.setResult(nil)
		return nil
	}
	av := &Available{Version: best.String(), Channel: "stable"}
	if best.Prerelease() {
		av.Channel = "prerelease"
	}
	for _, c := range cands {
		if c.ver.String() == av.Version {
			av.Notes = c.notes
			break
		}
	}
	u.advisory(ctx, installed, av)
	u.setResult(av)
	return nil
}

func (u *Updater) setResult(av *Available) {
	u.mu.Lock()
	u.res = &checkResult{at: u.now(), avail: av}
	u.mu.Unlock()
}

// advisory fetches the three small files of the release into memory and
// verifies them with the package the helper uses as well. The result is for
// display and to fail early: the helper verifies its own copy again (U3).
func (u *Updater) advisory(ctx context.Context, installed relverify.Version, av *Available) {
	get := func(name string) []byte {
		b, e := u.gh.fetchSmall(ctx, av.Version, name)
		if e != nil {
			av.VerifyError = viewOf(e)
			return nil
		}
		return b
	}
	cs := get("checksums.txt")
	if cs == nil {
		return
	}
	sig := get("checksums.txt.sig")
	if sig == nil {
		return
	}
	mf := get(relverify.ManifestName)
	if mf == nil {
		return
	}
	rel, e := u.verifyMetadata(installed, av.Version, cs, sig, mf)
	if e != nil {
		av.VerifyError = viewOf(e)
		return
	}
	av.DownloadSize = rel.Manifest.Bundle.Size
	av.Verified = true
}

// verifyMetadata is VerifyMetadata plus the rules that tie the release to this
// device: strictly newer, the minimum installed version, the helper protocol,
// and the size cap. Every failure is one of the typed codes.
func (u *Updater) verifyMetadata(installed relverify.Version, version string, cs, sig, mf []byte) (*relverify.Release, *Error) {
	rel, err := relverify.VerifyMetadata(relverify.Metadata{
		Key: u.cfg.Key, Version: version, Architecture: u.arch, Checksums: cs, Signature: sig, Manifest: mf,
	})
	if err != nil {
		return nil, fail(Code(codeOfRelverify(err)))
	}
	u.mu.Lock()
	proto := u.helperProto
	u.mu.Unlock()
	if err := relverify.CheckUpgrade(installed, rel.Manifest, proto); err != nil {
		return nil, fail(Code(codeOfRelverify(err)))
	}
	if rel.Manifest.Bundle.Size > MaxBundleBytes {
		return nil, fail(CodeTooLarge)
	}
	return rel, nil
}

// codeOfRelverify returns the code of a relverify error, internal for any other.
func codeOfRelverify(err error) relverify.Code {
	if e, ok := err.(*relverify.Error); ok {
		return e.Code
	}
	return relverify.CodeInternal
}
