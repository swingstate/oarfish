package main

// The release check of D-C92 (path A): no release brings an older component
// set.
//
//	bundlegen history -manifest FILE [-exception TAG]
//	                  (-base-url URL | -manifests-dir DIR) TAG...
//
// FILE is the manifest.json of the release being built. TAG... are the earlier
// published releases in release order, oldest first; the last one is the
// directly preceding release. scripts/check-component-history.sh lists them
// from the repository's tags (creation order, docs/RELEASING.md) and passes
// -exception only when release/release-exceptions.txt names this tag.
//
// What is compared. The component set is the seven files of ADR 0010 A20.1
// (the helper, its three units, the broker and its two units, in that order;
// oarfish.service is not part of it). The manifests of schema 1 carry no set
// hash, so the check computes the set hash of A20.3 (SHA-256 over the seven
// SHA-256 digests, concatenated in that order) from the per-file digests in
// "contents", for the release being built and for every earlier release that
// lists all seven files. One rule for every manifest: two sets are equal if and
// only if their set hashes are equal, that is, if all seven file digests are
// equal. The hash printed is the one the helper records as
// components_set_sha256 at stage.
//
// The rule. The set may equal the set of the directly preceding release
// (unchanged). It must not equal the set of any other earlier release. A match
// fails the check unless -exception names this release's tag (the owner's
// approval for that tag, D-C92 item 2).
//
// Not comparable, skipped with a printed line: a legacy manifest (no
// "components" object and no component file) and a four-file set (the helper's
// four files, before G7.3a). A preceding release that is skipped leaves no set
// that may be equal.
//
// Fail closed. A manifest that cannot be fetched (network, HTTP status, size)
// or that is malformed (not JSON, another tag, a bad digest, a duplicate path,
// component files without "components", or a set that is neither four nor
// seven files) fails the check; nothing is skipped because it could not be
// read. Downloads are anonymous HTTPS: no token, no credentials.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// setOrder is the order of A20.1 and of the helper's componentSet. It differs
// from componentLayout, which is the order of the tarball's layout table.
var setOrder = []string{
	helperName, helperSocketName, helperServiceName, recoverServiceName,
	brokerName, brokerSocketName, brokerServiceName,
}

// fourFileSet is the component set before G7.3a.
var fourFileSet = []string{helperName, helperSocketName, helperServiceName, recoverServiceName}

// releaseTag is the tag rule of release.yml.
var releaseTag = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$`)

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

const (
	maxManifestBytes = 1 << 20
	fetchTimeout     = 60 * time.Second
)

// fetchFunc returns the manifest.json of one published release.
type fetchFunc func(tag string) ([]byte, error)

// setKind is what a manifest's component files are.
type setKind int

const (
	setLegacy setKind = iota // no components object, no component file
	setFour                  // the four helper files, before G7.3a
	setSeven                 // the seven files of A20
)

// releaseSet is one manifest, classified.
type releaseSet struct {
	tag  string
	kind setKind
	hash string // the set hash of A20.3; only for setSeven
}

// historyManifest is the part of a manifest the check reads. Unknown fields
// are ignored: a published manifest is read, not validated as the tool's own.
type historyManifest struct {
	Tag        string           `json:"tag"`
	Contents   []Entry          `json:"contents"`
	Components *json.RawMessage `json:"components"`
}

// classify reads one manifest. want is the tag it must name ("" for the
// release being built, whose tag is taken from it).
func classify(data []byte, want string) (releaseSet, error) {
	var m historyManifest
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&m); err != nil {
		return releaseSet{}, fmt.Errorf("not a JSON manifest: %v", err)
	}
	if dec.More() {
		return releaseSet{}, errors.New("not a JSON manifest: data after the object")
	}
	if !releaseTag.MatchString(m.Tag) {
		return releaseSet{}, fmt.Errorf("invalid tag %q", m.Tag)
	}
	if want != "" && m.Tag != want {
		return releaseSet{}, fmt.Errorf("the manifest names %s", m.Tag)
	}
	if len(m.Contents) == 0 {
		return releaseSet{}, errors.New("no contents")
	}
	digests := map[string]string{}
	for _, e := range m.Contents {
		root, name, ok := strings.Cut(e.Path, "/")
		if !ok || root == "" || name == "" || strings.Contains(name, "/") {
			return releaseSet{}, fmt.Errorf("invalid path %q", e.Path)
		}
		if !sha256Hex.MatchString(e.SHA256) {
			return releaseSet{}, fmt.Errorf("invalid sha256 for %s", e.Path)
		}
		if _, dup := digests[name]; dup {
			return releaseSet{}, fmt.Errorf("%s is listed twice", name)
		}
		digests[name] = e.SHA256
	}
	var present []string
	for _, n := range setOrder {
		if _, ok := digests[n]; ok {
			present = append(present, n)
		}
	}
	rs := releaseSet{tag: m.Tag}
	hasObject := m.Components != nil && string(*m.Components) != "null"
	switch {
	case !hasObject && len(present) == 0:
		rs.kind = setLegacy
	case !hasObject:
		return releaseSet{}, errors.New("component files without a components object")
	case equalNames(present, fourFileSet):
		rs.kind = setFour
	case equalNames(present, setOrder):
		rs.kind = setSeven
		h := sha256.New()
		for _, n := range setOrder {
			b, _ := hex.DecodeString(digests[n])
			h.Write(b)
		}
		rs.hash = hex.EncodeToString(h.Sum(nil))
	default:
		return releaseSet{}, fmt.Errorf("a components object with %d of the seven component files", len(present))
	}
	return rs, nil
}

func equalNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// checkHistory applies the rule of D-C92. current is the manifest of the
// release being built, earlier the earlier releases oldest first, exception
// the tag the owner's exception names ("" for none). It prints one line per
// release to out and returns an error when the check fails.
func checkHistory(out io.Writer, current []byte, earlier []string, exception string, fetch fetchFunc) error {
	cur, err := classify(current, "")
	if err != nil {
		return fmt.Errorf("component-set: FAIL: the manifest of the release being built: %v", err)
	}
	if cur.kind != setSeven {
		return fmt.Errorf("component-set: FAIL: %s does not carry the seven-file component set (ADR 0010 A20)", cur.tag)
	}
	if exception != "" && exception != cur.tag {
		return fmt.Errorf("component-set: FAIL: the exception names %s, not the release being built (%s)", exception, cur.tag)
	}
	if len(earlier) == 0 {
		return fmt.Errorf("component-set: FAIL: no earlier release is listed; the check cannot run without them")
	}
	fmt.Fprintf(out, "component-set: %s: set sha256 %s\n", cur.tag, cur.hash)
	if exception != "" {
		fmt.Fprintf(out, "component-set: release/release-exceptions.txt names an exception for %s\n", cur.tag)
	}

	seen := map[string]bool{}
	var sets []releaseSet
	for _, tag := range earlier {
		if !releaseTag.MatchString(tag) {
			return fmt.Errorf("component-set: FAIL: %q is not a release tag", tag)
		}
		if tag == cur.tag || seen[tag] {
			return fmt.Errorf("component-set: FAIL: %s is listed twice or is the release being built", tag)
		}
		seen[tag] = true
		data, err := fetch(tag)
		if err != nil {
			return fmt.Errorf("component-set: FAIL: cannot read the manifest of %s: %v (the check fails closed, D-C92)", tag, err)
		}
		rs, err := classify(data, tag)
		if err != nil {
			return fmt.Errorf("component-set: FAIL: the manifest of %s is malformed: %v (the check fails closed, D-C92)", tag, err)
		}
		switch rs.kind {
		case setLegacy:
			fmt.Fprintf(out, "component-set: %s: skipped, legacy shape (no components object), not comparable\n", tag)
		case setFour:
			fmt.Fprintf(out, "component-set: %s: skipped, four-file component set (before G7.3a), not comparable\n", tag)
		case setSeven:
			fmt.Fprintf(out, "component-set: %s: set sha256 %s\n", tag, rs.hash)
		}
		sets = append(sets, rs)
	}

	preceding := sets[len(sets)-1]
	fmt.Fprintf(out, "component-set: directly preceding release: %s\n", preceding.tag)
	if preceding.kind == setSeven && preceding.hash == cur.hash {
		fmt.Fprintf(out, "component-set: PASS: %s brings the set of the preceding release %s unchanged\n", cur.tag, preceding.tag)
		return nil
	}
	var older []string
	for _, rs := range sets[:len(sets)-1] {
		if rs.kind == setSeven && rs.hash == cur.hash {
			older = append(older, rs.tag)
		}
	}
	comparable := 0
	for _, rs := range sets {
		if rs.kind == setSeven {
			comparable++
		}
	}
	switch {
	case len(older) == 0 && comparable == 0:
		fmt.Fprintf(out, "component-set: PASS: no earlier release carries a seven-file set; nothing to compare\n")
		return nil
	case len(older) == 0:
		fmt.Fprintf(out, "component-set: PASS: %s brings a set that no earlier release brought (%d compared)\n", cur.tag, comparable)
		return nil
	case exception == cur.tag:
		fmt.Fprintf(out, "component-set: EXCEPTION: %s brings the set of %s, which is not the preceding release %s; passed only because release/release-exceptions.txt names %s (D-C92 item 2; the release notes must say so)\n",
			cur.tag, strings.Join(older, ", "), preceding.tag, cur.tag)
		return nil
	}
	return fmt.Errorf("component-set: FAIL: %s brings the component set of %s, which is not the directly preceding release %s. D-C92: no release brings an older component set. An exception needs the owner's approval in chat for this tag, a line in release/release-exceptions.txt and a sentence in the release notes (docs/RELEASING.md)",
		cur.tag, strings.Join(older, ", "), preceding.tag)
}

// httpFetcher downloads BASE/<tag>/manifest.json anonymously over HTTPS.
func httpFetcher(client *http.Client, base string) fetchFunc {
	return func(tag string) ([]byte, error) {
		req, err := http.NewRequest(http.MethodGet, strings.TrimSuffix(base, "/")+"/"+tag+"/manifest.json", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "oarfish-release-check")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP status %d", resp.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestBytes+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxManifestBytes {
			return nil, errors.New("manifest larger than 1 MiB")
		}
		return data, nil
	}
}

// anonymousClient follows redirects only to https and sends no credentials
// (it has no cookie jar, and the request carries no Authorization header).
func anonymousClient() *http.Client {
	return &http.Client{
		Timeout: fetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" {
				return errors.New("redirect to a non-https address")
			}
			req.Header.Del("Authorization")
			return nil
		},
	}
}

// dirFetcher reads DIR/<tag>/manifest.json, for fixtures.
func dirFetcher(dir string) fetchFunc {
	return func(tag string) ([]byte, error) {
		return os.ReadFile(filepath.Join(dir, tag, "manifest.json"))
	}
}

func runHistory(args []string) error {
	fs := flag.NewFlagSet("history", flag.ExitOnError)
	manifest := fs.String("manifest", "", "manifest.json of the release being built")
	exception := fs.String("exception", "", "the tag of this release, when the owner approved an exception for it")
	base := fs.String("base-url", "", "https base of the release downloads, for example https://github.com/OWNER/REPO/releases/download")
	dir := fs.String("manifests-dir", "", "read DIR/<tag>/manifest.json instead of downloading (fixtures)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *manifest == "" || (*base == "") == (*dir == "") {
		return errors.New("history needs -manifest and exactly one of -base-url and -manifests-dir")
	}
	var fetch fetchFunc
	if *base != "" {
		if !strings.HasPrefix(*base, "https://") {
			return errors.New("-base-url must be an https address")
		}
		fetch = httpFetcher(anonymousClient(), *base)
	} else {
		fetch = dirFetcher(*dir)
	}
	current, err := os.ReadFile(*manifest)
	if err != nil {
		return fmt.Errorf("component-set: FAIL: %v", err)
	}
	return checkHistory(os.Stdout, current, fs.Args(), *exception, fetch)
}
