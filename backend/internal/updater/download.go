package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// The bundle bounds of ADR 0010 U3 and U12.
const (
	MaxBundleBytes  = 40_000_000 // ORB-NFR-005 is 35 MB; never more than this
	copyBuffer      = 32 << 10
	stallTimeout    = 30 * time.Second
	absoluteTimeout = 15 * time.Minute
	retryDelay      = 5 * time.Second
)

// fetchSmall reads one of the small release files into memory, at most 64 KiB,
// within 30 s. It is the check's view of a release (advisory verification).
func (g *ghClient) fetchSmall(ctx context.Context, version, file string) ([]byte, *Error) {
	ctx, cancel := context.WithTimeout(ctx, smallTimeout)
	defer cancel()
	resp, e := g.get(ctx, g.assetURL(version, file), "application/octet-stream", "")
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if _, limited := g.rateLimit(resp); limited && resp.StatusCode != http.StatusOK {
		return nil, fail(CodeRateLimited)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fail(httpCode(resp.StatusCode))
	}
	if resp.ContentLength > maxSmallFile {
		return nil, fail(CodeTooLarge)
	}
	return readBounded(resp.Body, maxSmallFile)
}

// stallReader cancels the request when no byte arrives for the stall time.
type stallReader struct {
	r     io.Reader
	timer *time.Timer
	stall time.Duration
}

func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.timer.Reset(s.stall)
	}
	return n, err
}

// downloadTo streams one release file to dir/<name>.part: a 32 KiB buffer, never
// held in memory, at most max bytes, a stall timeout and an absolute limit. The
// SHA-256 is computed over the bytes as they are written. verify decides about
// the result (hash and size against the signed values); only when it accepts
// does the file get its final name, with the explicit mode 0644 that the helper
// needs, because the unit's UMask would make it unreadable for the helper. A
// failed or interrupted download leaves nothing behind.
func (g *ghClient) downloadTo(ctx context.Context, rawURL, dir, name string, max int64, stall, abs time.Duration, verify func(sum string, n int64) *Error) *Error {
	ctx, cancel := context.WithTimeout(ctx, abs)
	defer cancel()
	// The stall watchdog cancels the context; it is stopped when the function ends.
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	timer := time.AfterFunc(stall, wcancel)
	defer timer.Stop()

	part := filepath.Join(dir, name+".part")
	final := filepath.Join(dir, name)
	_ = os.Remove(part)
	resp, e := g.get(wctx, rawURL, "application/octet-stream", "")
	if e != nil {
		return stallOr(ctx, wctx, e)
	}
	defer resp.Body.Close()
	if _, limited := g.rateLimit(resp); limited && resp.StatusCode != http.StatusOK {
		return fail(CodeRateLimited)
	}
	if resp.StatusCode != http.StatusOK {
		return fail(httpCode(resp.StatusCode))
	}
	if resp.ContentLength > max {
		return fail(CodeTooLarge)
	}
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fail(CodeCacheError)
	}
	ok := false
	defer func() {
		if !ok {
			f.Close()
			_ = os.Remove(part)
		}
	}()
	h := sha256.New()
	sr := &stallReader{r: io.LimitReader(resp.Body, max+1), timer: timer, stall: stall}
	n, err := io.CopyBuffer(io.MultiWriter(f, h), sr, make([]byte, copyBuffer))
	if err != nil {
		return stallOr(ctx, wctx, classifyNet(wctx, err))
	}
	if n > max {
		return fail(CodeTooLarge)
	}
	if e := verify(hex.EncodeToString(h.Sum(nil)), n); e != nil {
		return e
	}
	if err := f.Chmod(0o644); err != nil || f.Sync() != nil || f.Close() != nil {
		return fail(CodeCacheError)
	}
	if err := os.Rename(part, final); err != nil {
		return fail(CodeCacheError)
	}
	ok = true
	return nil
}

// stallOr turns the cancellation of the watchdog into a timeout, and keeps the
// cancellation of the service itself.
func stallOr(ctx, wctx context.Context, e *Error) *Error {
	if e != nil && (e.Code == CodeCancelled || e.Code == CodeUnreachable) && wctx.Err() != nil && ctx.Err() == nil {
		return fail(CodeTimeout)
	}
	return e
}
