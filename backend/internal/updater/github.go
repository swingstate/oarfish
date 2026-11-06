package updater

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The bounds of ADR 0010 U3, on every request to GitHub.
const (
	repo            = "swingstate/oarfish" // the constant the installer carries
	maxRedirects    = 3
	connectTimeout  = 15 * time.Second
	tlsTimeout      = 15 * time.Second
	headerTimeout   = 20 * time.Second
	apiTimeout      = 30 * time.Second
	smallTimeout    = 30 * time.Second
	maxAPIBody      = 1 << 20
	maxSmallFile    = 64 << 10
	maxRateWait     = time.Hour // a longer wait in a header is not believed
	defaultRateWait = time.Minute
	releasesPerPage = 30
)

// ghClient is the GitHub client. It builds every URL itself from constants and
// from a strictly validated tag, and takes nothing else out of an API answer
// (U3): no browser_download_url, no URL of any kind.
type ghClient struct {
	hc        *http.Client
	apiBase   string // https://api.github.com
	dlBase    string // https://github.com
	repo      string
	userAgent string
	now       func() time.Time
}

// newGHClient returns the production client: HTTPS only, TLS 1.2 or newer, the
// system root certificates, no proxy from the environment, at most 3 redirects,
// each on https and on an allow-listed host, and no keep-alive, so that no
// connection and no goroutine outlives its request. roots replaces the system
// roots and is used by tests only.
func newGHClient(version string, roots *x509.CertPool, now func() time.Time) *ghClient {
	tr := &http.Transport{
		Proxy:                 nil, // never the environment's proxy
		DialContext:           (&net.Dialer{Timeout: connectTimeout}).DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
		TLSHandshakeTimeout:   tlsTimeout,
		ResponseHeaderTimeout: headerTimeout,
		DisableKeepAlives:     true,
		DisableCompression:    true, // the bytes are hashed as they are served
		// HTTP/1.1 only (no ForceAttemptHTTP2 with this custom TLS config), so that
		// DisableKeepAlives closes the connection after its response.
	}
	g := &ghClient{
		apiBase: "https://api.github.com", dlBase: "https://github.com", repo: repo,
		userAgent: "oarfish/" + version, now: now,
	}
	g.hc = &http.Client{Transport: tr, CheckRedirect: g.checkRedirect(allowedRedirect)}
	return g
}

// allowedRedirect is the allow-list of U3: https, github.com or a subdomain of
// githubusercontent.com, on the default port. A release asset is served by
// github.com and then by release-assets.githubusercontent.com (A3).
func allowedRedirect(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil {
		return false
	}
	if p := u.Port(); p != "" && p != "443" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if h == "github.com" {
		return true
	}
	sub, ok := strings.CutSuffix(h, ".githubusercontent.com")
	return ok && sub != "" && !strings.HasPrefix(sub, ".") && !strings.HasSuffix(sub, ".")
}

func (g *ghClient) checkRedirect(allow func(*url.URL) bool) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects || !allow(req.URL) {
			return errRedirect
		}
		return nil
	}
}

// get sends one GET. The caller closes the body.
func (g *ghClient) get(ctx context.Context, rawURL, accept, etag string) (*http.Response, *Error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fail(CodeInternal)
	}
	req.Header.Set("User-Agent", g.userAgent)
	req.Header.Set("Accept", accept)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := g.hc.Do(req)
	if err != nil {
		return nil, classifyNet(ctx, err)
	}
	return resp, nil
}

// rateLimit reads the rate-limit headers of an answer (U3). It returns the wait
// when the answer says the limit is used up: 429, or 403 with a used-up limit or
// a Retry-After, or X-RateLimit-Remaining: 0 on any answer. The wait comes from
// Retry-After or X-RateLimit-Reset, is capped, and never loops.
func (g *ghClient) rateLimit(resp *http.Response) (wait time.Duration, limited bool) {
	remaining := resp.Header.Get("X-RateLimit-Remaining")
	retry := resp.Header.Get("Retry-After")
	exhausted := remaining == "0"
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
	case resp.StatusCode == http.StatusForbidden && (exhausted || retry != ""):
	case exhausted:
	default:
		return 0, false
	}
	wait = defaultRateWait
	if n, err := strconv.ParseInt(strings.TrimSpace(retry), 10, 64); err == nil && n >= 0 {
		wait = time.Duration(n) * time.Second
	} else if n, err := strconv.ParseInt(strings.TrimSpace(resp.Header.Get("X-RateLimit-Reset")), 10, 64); err == nil {
		wait = time.Unix(n, 0).Sub(g.now())
	}
	return min(max(wait, time.Second), maxRateWait), true
}

// readBounded reads at most max bytes; one more is too_large.
func readBounded(r io.Reader, max int64) ([]byte, *Error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, classifyNet(nil, err)
	}
	if int64(len(b)) > max {
		return nil, fail(CodeTooLarge)
	}
	return b, nil
}

// releaseWire is the part of a GitHub release the service reads (U3): the tag,
// the two flags and the notes. Nothing else of the answer is looked at.
type releaseWire struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
	Body       string `json:"body"`
}

// releaseList is the list of releases and the ETag it came with.
type releaseList struct {
	etag     string
	releases []releaseWire
}

// listReleases asks for the release list. etag, when set, is sent as
// If-None-Match; notModified is true for a 304, which counts against the
// anonymous limit all the same (A3).
func (g *ghClient) listReleases(ctx context.Context, etag string) (list releaseList, notModified bool, wait time.Duration, e *Error) {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	u := g.apiBase + "/repos/" + g.repo + "/releases?per_page=" + strconv.Itoa(releasesPerPage)
	resp, e := g.get(ctx, u, "application/vnd.github+json", etag)
	if e != nil {
		return list, false, 0, e
	}
	defer resp.Body.Close()
	w, limited := g.rateLimit(resp)
	switch {
	case limited && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotModified:
		x := fail(CodeRateLimited)
		x.RetryAfter = w
		return list, false, w, x
	case resp.StatusCode == http.StatusNotModified:
		return list, true, w, nil
	case resp.StatusCode != http.StatusOK:
		return list, false, w, fail(httpCode(resp.StatusCode))
	}
	body, e := readBounded(resp.Body, maxAPIBody)
	if e != nil {
		return list, false, w, e
	}
	if json.Unmarshal(body, &list.releases) != nil {
		return list, false, w, fail(CodeBadResponse)
	}
	list.etag = sanitizeETag(resp.Header.Get("ETag"))
	return list, false, w, nil
}

// sanitizeETag keeps an ETag only when it is short printable ASCII; it is sent
// back to GitHub as a header value and must not carry anything else.
func sanitizeETag(s string) string {
	if len(s) == 0 || len(s) > 128 {
		return ""
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return ""
		}
	}
	return s
}

// assetURL builds a download URL from the strictly validated version
// (github.com/<repo>/releases/download/v<version>/<file>), the scheme of the
// installer.
func (g *ghClient) assetURL(version, file string) string {
	return g.dlBase + "/" + g.repo + "/releases/download/v" + version + "/" + file
}

// errTimeoutStall marks a download that received no bytes for the stall time.
var errTimeoutStall = errors.New("stalled")

// ValidVersion is the version rule of ADR 0010 U2 for a version that arrives in
// a request.
func ValidVersion(s string) bool { return validVersion(s) }
