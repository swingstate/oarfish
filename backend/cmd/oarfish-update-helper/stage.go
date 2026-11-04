package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"syscall"
	"time"

	"github.com/swingstate/oarfish/backend/internal/relverify"
)

// stage implements the request `stage` (ADR 0010 U3 steps 2 and 3, U4, U5):
// copy the four downloaded files out of the service-owned cache into a private
// root-owned staging directory, verify the private copies with relverify,
// extract the release, compare the helper components with the installed ones
// and move a verified release to releases/<version> as an inactive staged
// release. Nothing here activates anything.

type stageRun struct {
	h   *handler
	e   *env
	req request
	st  *state

	id        string
	installed relverify.Version
	instStr   string
	started   time.Duration

	// Progress, for the cleanup duty. dirMade is set only after this request
	// created .stage.<id>, so the cleanup never removes a directory it did not
	// make.
	dirMade bool
	aside   bool // an existing releases/<version> was moved to .trash.<id>
	renamed bool // the staged tree is releases/<version>
	staged  bool // state.json says staged

	differs []string
}

func (r *stageRun) stageDir() string { return "releases/.stage." + r.id }

// expired reports whether the request deadline has passed.
func (r *stageRun) expired() bool { return r.h.monotonic()-r.started >= r.h.deadline }

// stage answers the request `stage`.
func (h *handler) stage(req request) []byte {
	lk, code := h.lockState()
	if code != "" {
		return errorBody(code)
	}
	defer lk.release()
	e, code := h.open(true)
	if code != "" {
		return errorBody(code)
	}
	defer e.close()
	r := &stageRun{h: h, e: e, req: req, started: h.monotonic()}
	res, code := r.run()
	switch {
	case code == codeInstallerRequired:
		return differsBody(r.differs)
	case code != "":
		return errorBody(code)
	}
	return encode(res)
}

func (r *stageRun) run() (stageResponse, errorCode) {
	h, e := r.h, r.e
	st, err := e.loadState()
	if err != nil {
		return stageResponse{}, codeIOError // G5: a corrupt file is not repaired in this gate
	}
	r.st = st
	r.instStr = installedVersion(h.root)

	// a. proceed only from idle. An expired staged release is cleaned first; a
	// phase staging found while holding the lock is a crash.
	switch st.Txn.Phase {
	case phaseIdle:
	case phaseStaged:
		now, err := h.uptime()
		if err != nil {
			return stageResponse{}, codeIOError
		}
		if !stageExpired(st.Txn.StagedAtUptimeS, now) {
			return stageResponse{}, codeBusy
		}
		if r.instStr == "" {
			return stageResponse{}, codeCurrentUnhealthy
		}
		if code := e.expireStaged(st, r.instStr); code != "" {
			return stageResponse{}, code
		}
	case phaseStaging:
		if r.instStr == "" {
			return stageResponse{}, codeCurrentUnhealthy
		}
		if code := e.recoverStaging(st, r.instStr); code != "" {
			return stageResponse{}, code
		}
	default:
		return stageResponse{}, codeBusy
	}

	// b. the installed version.
	installed, err := relverify.ParseVersion(r.instStr)
	if r.instStr == "" || err != nil {
		return stageResponse{}, codeCurrentUnhealthy
	}
	r.installed = installed

	// c. open and check the four cache files before anything is written.
	set, code := openCache(h.cacheDir, r.req.Version)
	if code != "" {
		return stageResponse{}, code
	}
	defer set.close()

	// d. free-space lower bound.
	releases, err := e.opt.Open("releases")
	if err != nil {
		return stageResponse{}, codeIOError
	}
	defer releases.Close()
	if code := r.checkSpace(releases, set.totalSize()); code != "" {
		return stageResponse{}, code
	}
	if r.expired() {
		return stageResponse{}, codeIOError
	}

	// e. the write-ahead state, before anything is created.
	id, err := h.newID()
	if err != nil {
		return stageResponse{}, codeIOError
	}
	r.id = id
	st.Txn = txn{ID: id, Phase: phaseStaging, To: r.req.Version, From: r.instStr, CorrelationID: r.req.CorrelationID,
		SocketRestartPending: st.Txn.SocketRestartPending}
	if err := e.saveState(st); err != nil {
		return stageResponse{}, codeIOError
	}
	h.log(phaseStaging, r.req.Version, r.instStr, r.req.CorrelationID, "ok")

	res, code := r.build(set, releases)
	if code != "" {
		return stageResponse{}, r.abort(code)
	}
	return res, ""
}

// checkSpace requires free space of at least need plus the reserve on the
// file system of releases/.
func (r *stageRun) checkSpace(releases *os.File, need int64) errorCode {
	free, err := r.h.freeBytes(releases)
	if err != nil {
		return codeIOError
	}
	if need < 0 || free < uint64(need)+spaceReserve {
		return codeNoSpace
	}
	return ""
}

// build runs steps f to n. Any error it returns is followed by abort.
func (r *stageRun) build(set *cacheSet, releases *os.File) (stageResponse, errorCode) {
	h, e := r.h, r.e
	dir := r.stageDir()

	// f. the private staging directory: root-owned, 0700 by an explicit chmod,
	// never by the umask.
	if err := e.fs.Mkdir(treeOpt, dir, 0o700); err != nil {
		return stageResponse{}, codeIOError
	}
	r.dirMade = true
	if err := e.fs.Chmod(treeOpt, dir, 0o700); err != nil {
		return stageResponse{}, codeIOError
	}

	// g. the three small files, then the metadata.
	checksums, code := r.copyFile(dir+"/"+nameChecksums, &set.checksums, maxSmallFile, true)
	if code != "" {
		return stageResponse{}, code
	}
	signature, code := r.copyFile(dir+"/"+nameSignature, &set.signature, maxSmallFile, true)
	if code != "" {
		return stageResponse{}, code
	}
	manifest, code := r.copyFile(dir+"/"+nameManifest, &set.manifest, maxSmallFile, true)
	if code != "" {
		return stageResponse{}, code
	}
	rel, err := relverify.VerifyMetadata(relverify.Metadata{
		Key: h.key, Version: r.req.Version, Architecture: h.arch,
		Checksums: checksums.data, Signature: signature.data, Manifest: manifest.data,
	})
	if err != nil {
		return stageResponse{}, relCode(err)
	}
	if r.expired() {
		return stageResponse{}, codeIOError
	}

	// h. the exact free-space check.
	var extracted int64
	for _, c := range rel.Manifest.Contents {
		extracted += c.Size
	}
	copied := checksums.n + signature.n + manifest.n + rel.Manifest.Bundle.Size
	if code := r.checkSpace(releases, copied+extracted); code != "" {
		return stageResponse{}, code
	}

	// i. the bundle: limit manifest size plus one byte, hash over the bytes
	// written. From here on only the private copy is read.
	bundle, code := r.copyFile(dir+"/"+set.bundle.name, &set.bundle, rel.Manifest.Bundle.Size, false)
	if code != "" {
		return stageResponse{}, code
	}
	if err := rel.CheckBundle(bundle.sum, bundle.n); err != nil {
		return stageResponse{}, relCode(err)
	}

	// j. replay and downgrade protection, minimum version, protocol.
	if err := relverify.CheckUpgrade(r.installed, rel.Manifest, protocolMax); err != nil {
		return stageResponse{}, relCode(err)
	}

	// k. inspect the private bundle twice: first verify only, then extract.
	if code := r.extract(dir, set.bundle.name, rel.Manifest); code != "" {
		return stageResponse{}, code
	}
	if r.expired() {
		return stageResponse{}, codeIOError
	}

	// l. component comparison. A bundle without components (the legacy shape)
	// can not be applied while the helper runs (gap rule G1).
	if rel.Manifest.Components == nil {
		return stageResponse{}, codeRequirementsNotMet
	}
	differs, code := r.compareComponents(dir)
	if code != "" {
		return stageResponse{}, code
	}
	if len(differs) > 0 {
		r.differs = differs
		return stageResponse{}, codeInstallerRequired
	}

	// m. make it durable, drop the private copies, move it into place.
	if code := r.publish(set.bundle.name); code != "" {
		return stageResponse{}, code
	}

	// n. the staged state.
	now, err := h.uptime()
	if err != nil {
		return stageResponse{}, codeIOError
	}
	r.st.Txn.Phase = phaseStaged
	r.st.Txn.StagedAtUptimeS = now
	if err := e.saveState(r.st); err != nil {
		return stageResponse{}, codeIOError
	}
	r.staged = true
	h.log(phaseStaged, r.req.Version, r.instStr, r.req.CorrelationID, "ok")
	return stageResponse{V: protocolMax, OK: true, Version: rel.Manifest.Version, Commit: rel.Manifest.Commit}, ""
}

// copied is the result of copying one cache file.
type copied struct {
	data []byte // the bytes written (small files only)
	sum  string // SHA-256 of the bytes written
	n    int64
}

// copyFile copies a cache file into the private area with a 32 KiB buffer. It
// reads at most limit plus one byte: more means the file grew beyond what the
// signed data allows (checksum_mismatch). The SHA-256 is computed over the
// bytes it writes. The test seam onChunk runs between two chunks.
func (r *stageRun) copyFile(dst string, src *cacheFile, limit int64, keep bool) (copied, errorCode) {
	var out copied
	w, err := r.e.fs.Create(treeOpt, dst, 0o600)
	if err != nil {
		return out, codeIOError
	}
	closed := false
	defer func() {
		if !closed {
			w.Close()
		}
	}()
	sum := sha256.New()
	buf := make([]byte, chunk)
	for {
		if r.expired() {
			return out, codeIOError
		}
		if r.h.onChunk != nil {
			r.h.onChunk(src.name, out.n)
		}
		want := int64(len(buf))
		if left := limit + 1 - out.n; left < want {
			want = left
		}
		k, rerr := src.f.Read(buf[:want])
		if k > 0 {
			out.n += int64(k)
			if out.n > limit {
				return out, codeChecksumMismatch
			}
			sum.Write(buf[:k])
			if keep {
				out.data = append(out.data, buf[:k]...)
			}
			if n, werr := w.Write(buf[:k]); werr != nil || n != k {
				return out, codeIOError
			}
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return out, codeIOError
		}
	}
	if err := w.Sync(); err != nil {
		return out, codeIOError
	}
	closed = true
	if err := w.Close(); err != nil {
		return out, codeIOError
	}
	out.sum = hex.EncodeToString(sum.Sum(nil))
	return out, ""
}

// deadlineReader fails a read once the request deadline has passed.
type deadlineReader struct {
	r       io.Reader
	expired func() bool
}

func (d deadlineReader) Read(p []byte) (int, error) {
	if d.expired() {
		return 0, errDeadline
	}
	return d.r.Read(p)
}

// extract runs relverify.Inspect twice on the private copy of the bundle:
// first with a nil sink (verify only), then with the staging sink, which
// verifies again so a change between the runs is caught as well.
func (r *stageRun) extract(dir, bundleName string, m *relverify.Manifest) errorCode {
	e := r.e
	f, err := e.opt.OpenFile(dir+"/"+bundleName, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return codeIOError
	}
	defer f.Close()
	rd := deadlineReader{r: f, expired: r.expired}
	if err := relverify.Inspect(rd, m, nil); err != nil {
		return relCode(err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return codeIOError
	}
	if err := e.fs.Mkdir(treeOpt, dir+"/"+dirRelease, 0o755); err != nil {
		return codeIOError
	}
	if err := e.fs.Chmod(treeOpt, dir+"/"+dirRelease, 0o755); err != nil {
		return codeIOError
	}
	if err := e.fs.Mkdir(treeOpt, dir+"/"+dirComponents, 0o700); err != nil {
		return codeIOError
	}
	if err := e.fs.Chmod(treeOpt, dir+"/"+dirComponents, 0o700); err != nil {
		return codeIOError
	}
	sink := newStagingSink(r, dir)
	defer sink.abort()
	if err := relverify.Inspect(rd, m, sink); err != nil {
		return relCode(err)
	}
	return ""
}

// publish makes the extracted tree durable, deletes the private copies of the
// four downloaded files and renames release/ to releases/<version>. An
// existing releases/<version> (a newer version left by an installer rollback,
// gap rule G2) is moved to .trash.<id> first and deleted last. The installed
// and the recorded previous version are never touched: the version is strictly
// newer than the installed one, and a collision with the previous one is an
// error.
func (r *stageRun) publish(bundleName string) errorCode {
	e := r.e
	dir := r.stageDir()
	for _, d := range []string{dir + "/" + dirRelease, dir + "/" + dirComponents} {
		if err := e.fs.SyncDir(treeOpt, d); err != nil {
			return codeIOError
		}
	}
	for _, n := range []string{nameChecksums, nameSignature, nameManifest, bundleName} {
		if err := e.fs.RemoveAll(treeOpt, dir+"/"+n); err != nil {
			return codeIOError
		}
	}
	if err := e.fs.SyncDir(treeOpt, dir); err != nil {
		return codeIOError
	}
	target := releasePath(r.req.Version)
	if _, err := e.opt.Lstat(target); err == nil {
		if r.req.Version == r.instStr || r.req.Version == r.st.Previous {
			return codeIOError
		}
		if err := e.fs.Rename(treeOpt, target, r.trashPath()); err != nil {
			return codeIOError
		}
		r.aside = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return codeIOError
	}
	if err := e.fs.Rename(treeOpt, dir+"/"+dirRelease, target); err != nil {
		return codeIOError
	}
	r.renamed = true
	if err := e.fs.SyncDir(treeOpt, "releases"); err != nil {
		return codeIOError
	}
	if err := e.fs.RemoveAll(treeOpt, dir); err != nil {
		return codeIOError
	}
	if r.aside {
		if err := e.fs.RemoveAll(treeOpt, r.trashPath()); err != nil {
			return codeIOError
		}
	}
	return ""
}

func (r *stageRun) trashPath() string { return "releases/.trash." + r.id }

// abort is the cleanup duty (U5, U7 row 2 and 3): remove the whole staging
// tree, remove releases/<version> when it was already renamed and the state
// does not yet say staged, and set the state back to idle. A synchronous error
// writes no last result (gap rule G3). When the cleanup itself fails the
// answer is io_error and the state stays at staging with the field "to", so
// that the staging recovery finds it. The deadline is not consulted here.
func (r *stageRun) abort(code errorCode) errorCode {
	e := r.e
	if r.dirMade {
		if err := e.fs.RemoveAll(treeOpt, r.stageDir()); err != nil {
			return codeIOError
		}
	}
	if r.aside {
		if err := e.fs.RemoveAll(treeOpt, r.trashPath()); err != nil {
			return codeIOError
		}
	}
	if r.renamed && !r.staged {
		if err := e.removeRelease(r.req.Version, r.instStr, r.st.Previous); err != nil {
			return codeIOError
		}
	}
	old := r.st.Txn
	r.st.resetTxn()
	if err := e.saveState(r.st); err != nil {
		return codeIOError
	}
	r.h.log(phaseIdle, old.To, old.From, old.CorrelationID, string(code))
	return code
}

// relCode passes the code of a relverify error through unchanged (no
// re-mapping). Anything that is not one of its errors is internal.
func relCode(err error) errorCode {
	var re *relverify.Error
	if errors.As(err, &re) {
		if c := errorCode(re.Code); errorTexts[c] != "" {
			return c
		}
	}
	return codeInternal
}
