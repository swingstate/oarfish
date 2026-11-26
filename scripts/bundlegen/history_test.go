package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The fixtures are synthetic: a digest is the SHA-256 of a label, so "set X"
// means seven files whose digests are derived from "X". No real hash appears.

func fakeDigest(label string) string {
	s := sha256.Sum256([]byte(label))
	return hex.EncodeToString(s[:])
}

type shape int

const (
	shapeLegacy shape = iota
	shapeFour
	shapeSeven
)

// fakeManifest builds the manifest of tag with the component files of the
// given shape. set names the component bytes; two manifests with the same set
// carry the same seven digests. The main binary always differs per tag.
func fakeManifest(tag string, sh shape, set string) []byte {
	root := "oarfish_" + strings.TrimPrefix(tag, "v") + "_linux_arm64/"
	contents := []Entry{
		{Path: root + "LICENSE", Mode: "0644", Size: 1, SHA256: fakeDigest("license")},
		{Path: root + "oarfish", Mode: "0755", Size: 1, SHA256: fakeDigest("oarfish " + tag)},
		{Path: root + "oarfish.service", Mode: "0644", Size: 1, SHA256: fakeDigest("service")},
	}
	var names []string
	switch sh {
	case shapeFour:
		names = fourFileSet
	case shapeSeven:
		names = setOrder
	}
	for _, n := range names {
		contents = append(contents, Entry{Path: root + n, Mode: "0644", Size: 1, SHA256: fakeDigest(set + " " + n)})
	}
	m := map[string]any{"schema_version": 1, "tag": tag, "contents": contents}
	if sh != shapeLegacy {
		m["components"] = map[string]any{"key_id": testKeyID, "protocol": 1}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		panic(err)
	}
	return b
}

// fakeRelease is one earlier release for the fake fetcher.
type fakeRelease struct {
	tag  string
	data []byte
	err  error
}

func rel(tag string, sh shape, set string) fakeRelease {
	return fakeRelease{tag: tag, data: fakeManifest(tag, sh, set)}
}

// runFake runs the check with a fake fetcher over the given releases (oldest
// first) and counts the fetches.
func runFake(t *testing.T, current []byte, exception string, releases ...fakeRelease) (string, error, int) {
	t.Helper()
	byTag := map[string]fakeRelease{}
	var tags []string
	for _, r := range releases {
		byTag[r.tag] = r
		tags = append(tags, r.tag)
	}
	fetches := 0
	fetch := func(tag string) ([]byte, error) {
		fetches++
		r, ok := byTag[tag]
		if !ok {
			return nil, errors.New("unknown tag")
		}
		return r.data, r.err
	}
	var out bytes.Buffer
	err := checkHistory(&out, current, tags, exception, fetch)
	return out.String(), err, fetches
}

const cur = "v0.2.0"

func current(set string) []byte { return fakeManifest(cur, shapeSeven, set) }

func mustPass(t *testing.T, out string, err error, want ...string) {
	t.Helper()
	if err != nil {
		t.Fatalf("check failed: %v\n%s", err, out)
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
}

func mustFail(t *testing.T, out string, err error, want ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("check passed, want a failure:\n%s", out)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error lacks %q: %v", w, err)
		}
	}
}

func TestHistoryNewSetPasses(t *testing.T) {
	out, err, _ := runFake(t, current("Y"), "",
		rel("v0.1.0", shapeSeven, "W"), rel("v0.1.1", shapeSeven, "X"))
	mustPass(t, out, err, "PASS: v0.2.0 brings a set that no earlier release brought (2 compared)")
}

func TestHistoryEqualToPrecedingPasses(t *testing.T) {
	out, err, _ := runFake(t, current("X"), "",
		rel("v0.1.0", shapeSeven, "W"), rel("v0.1.1", shapeSeven, "X"))
	mustPass(t, out, err, "directly preceding release: v0.1.1", "PASS: v0.2.0 brings the set of the preceding release v0.1.1 unchanged")
}

// The preceding release brought the set back already (with its own
// exception); bringing it again unchanged is allowed.
func TestHistoryEqualToPrecedingAndToAnOlderPasses(t *testing.T) {
	out, err, _ := runFake(t, current("X"), "",
		rel("v0.1.0", shapeSeven, "X"), rel("v0.1.1", shapeSeven, "Y"), rel("v0.1.2", shapeSeven, "X"))
	mustPass(t, out, err, "unchanged")
}

func TestHistoryEqualToAnOlderSetFails(t *testing.T) {
	for _, tc := range []struct {
		name     string
		releases []fakeRelease
		older    string
	}{
		// The release before the preceding one.
		{"the one before the preceding", []fakeRelease{rel("v0.1.0", shapeSeven, "X"), rel("v0.1.1", shapeSeven, "Y")}, "v0.1.0"},
		// The oldest of several, so a check of only the newest older release misses it.
		{"the oldest of several", []fakeRelease{rel("v0.1.0", shapeSeven, "X"), rel("v0.1.1", shapeSeven, "Z"), rel("v0.1.2", shapeSeven, "W"), rel("v0.1.3", shapeSeven, "Y")}, "v0.1.0"},
		// A middle one, so a check of only the oldest release misses it.
		{"a middle one", []fakeRelease{rel("v0.1.0", shapeSeven, "Z"), rel("v0.1.1", shapeSeven, "X"), rel("v0.1.2", shapeSeven, "W"), rel("v0.1.3", shapeSeven, "Y")}, "v0.1.1"},
		// The preceding release is not comparable, so nothing may be equal.
		{"preceding is a four-file set", []fakeRelease{rel("v0.1.0", shapeSeven, "X"), rel("v0.1.1", shapeFour, "X")}, "v0.1.0"},
		{"preceding is legacy", []fakeRelease{rel("v0.1.0", shapeSeven, "X"), rel("v0.1.1", shapeLegacy, "")}, "v0.1.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err, _ := runFake(t, current("X"), "", tc.releases...)
			prec := tc.releases[len(tc.releases)-1].tag
			mustFail(t, out, err,
				"component-set: FAIL: v0.2.0 brings the component set of "+tc.older+", which is not the directly preceding release "+prec,
				"release/release-exceptions.txt")
		})
	}
}

func TestHistoryExceptionPassesAndIsPrinted(t *testing.T) {
	out, err, _ := runFake(t, current("X"), cur,
		rel("v0.1.0", shapeSeven, "X"), rel("v0.1.1", shapeSeven, "Y"))
	mustPass(t, out, err,
		"names an exception for v0.2.0",
		"EXCEPTION: v0.2.0 brings the set of v0.1.0, which is not the preceding release v0.1.1")
}

func TestHistoryExceptionForAnotherTagFails(t *testing.T) {
	out, err, fetches := runFake(t, current("X"), "v0.1.9",
		rel("v0.1.0", shapeSeven, "X"), rel("v0.1.1", shapeSeven, "Y"))
	mustFail(t, out, err, "the exception names v0.1.9, not the release being built (v0.2.0)")
	if fetches != 0 {
		t.Errorf("%d fetches before the exception was rejected", fetches)
	}
}

// Without an exception the same history fails (the exception is not on by
// default); with an unused exception a new set still passes.
func TestHistoryExceptionIsNotTheDefault(t *testing.T) {
	out, err, _ := runFake(t, current("X"), "", rel("v0.1.0", shapeSeven, "X"), rel("v0.1.1", shapeSeven, "Y"))
	mustFail(t, out, err, "FAIL")
	out, err, _ = runFake(t, current("Q"), cur, rel("v0.1.0", shapeSeven, "X"), rel("v0.1.1", shapeSeven, "Y"))
	mustPass(t, out, err, "PASS: v0.2.0 brings a set that no earlier release brought")
}

func TestHistoryFirstSevenFileReleasePasses(t *testing.T) {
	out, err, _ := runFake(t, current("X"), "",
		rel("v0.1.0", shapeLegacy, ""), rel("v0.1.1", shapeFour, "A"), rel("v0.1.2", shapeFour, "B"))
	mustPass(t, out, err, "PASS: no earlier release carries a seven-file set; nothing to compare")
}

func TestHistoryNonComparableReleasesAreSkippedWithALine(t *testing.T) {
	out, err, _ := runFake(t, current("X"), "",
		rel("v0.1.0", shapeLegacy, ""), rel("v0.1.1", shapeFour, "X"), rel("v0.1.2", shapeSeven, "Y"))
	mustPass(t, out, err,
		"component-set: v0.1.0: skipped, legacy shape (no components object), not comparable",
		"component-set: v0.1.1: skipped, four-file component set (before G7.3a), not comparable",
		"component-set: v0.1.2: set sha256 ",
		"(1 compared)")
}

// A legacy manifest of the oldest shape (alpha.1 listed two files only).
func TestHistoryLegacyWithoutServiceIsSkipped(t *testing.T) {
	m := map[string]any{"tag": "v0.1.0", "contents": []Entry{{Path: "oarfish_0.1.0_linux_arm64/oarfish", SHA256: fakeDigest("o")}}}
	data, _ := json.Marshal(m)
	out, err, _ := runFake(t, current("X"), "", fakeRelease{tag: "v0.1.0", data: data})
	mustPass(t, out, err, "v0.1.0: skipped, legacy shape")
}

func TestHistoryMalformedManifestFailsClosed(t *testing.T) {
	good := string(fakeManifest("v0.1.1", shapeSeven, "Y"))
	sevenNoObject := func() []byte {
		var m map[string]any
		_ = json.Unmarshal(fakeManifest("v0.1.1", shapeSeven, "X"), &m)
		delete(m, "components")
		b, _ := json.Marshal(m)
		return b
	}
	fiveOfSeven := func() []byte {
		var m historyManifest
		_ = json.Unmarshal(fakeManifest("v0.1.1", shapeSeven, "X"), &m)
		m.Contents = m.Contents[:len(m.Contents)-2]
		raw := json.RawMessage(`{"key_id":"0123456789abcdef","protocol":1}`)
		m.Components = &raw
		b, _ := json.Marshal(m)
		return b
	}
	for _, tc := range []struct {
		name, want string
		data       []byte
	}{
		{"not JSON", "not a JSON manifest", []byte("<html>not found</html>")},
		{"empty", "not a JSON manifest", nil},
		{"two objects", "data after the object", []byte(good + good)},
		{"another tag", "the manifest names v0.1.0", fakeManifest("v0.1.0", shapeSeven, "Y")},
		{"no contents", "no contents", []byte(`{"tag":"v0.1.1","contents":[]}`)},
		{"bad digest", "invalid sha256", []byte(strings.Replace(good, fakeDigest("Y oarfish-broker"), "ABC", 1))},
		{"bad path", "invalid path", []byte(strings.Replace(good, "oarfish_0.1.1_linux_arm64/LICENSE", "LICENSE", 1))},
		{"duplicate", "listed twice", []byte(strings.Replace(good, "linux_arm64/LICENSE", "linux_arm64/oarfish-broker", 1))},
		{"component files without components", "component files without a components object", sevenNoObject()},
		{"five of seven", "a components object with 5 of the seven component files", fiveOfSeven()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err, _ := runFake(t, current("X"), "",
				rel("v0.1.0", shapeSeven, "W"), fakeRelease{tag: "v0.1.1", data: tc.data})
			mustFail(t, out, err, "the manifest of v0.1.1 is malformed", tc.want, "fails closed")
		})
	}
}

// A malformed manifest fails also when it would have been skipped, and also
// when an exception is named.
func TestHistoryMalformedFailsEvenWithAnException(t *testing.T) {
	out, err, _ := runFake(t, current("X"), cur, fakeRelease{tag: "v0.1.0", data: []byte("{")})
	mustFail(t, out, err, "malformed")
}

func TestHistoryNetworkFailureFailsClosed(t *testing.T) {
	netErr := errors.New("dial tcp: connection refused")
	for _, at := range []int{0, 1, 2} {
		t.Run(fmt.Sprint("release ", at), func(t *testing.T) {
			rs := []fakeRelease{rel("v0.1.0", shapeLegacy, ""), rel("v0.1.1", shapeFour, "A"), rel("v0.1.2", shapeSeven, "Y")}
			rs[at].err = netErr
			rs[at].data = nil
			out, err, _ := runFake(t, current("X"), "", rs...)
			mustFail(t, out, err, "cannot read the manifest of "+rs[at].tag, "connection refused", "fails closed")
		})
	}
	// With an exception as well.
	out, err, _ := runFake(t, current("X"), cur, fakeRelease{tag: "v0.1.0", err: netErr})
	mustFail(t, out, err, "cannot read the manifest")
}

func TestHistoryInputErrors(t *testing.T) {
	_, err, _ := runFake(t, current("X"), "")
	mustFail(t, "", err, "no earlier release is listed")

	_, err, _ = runFake(t, fakeManifest(cur, shapeFour, "X"), "", rel("v0.1.0", shapeSeven, "X"))
	mustFail(t, "", err, "does not carry the seven-file component set")

	_, err, _ = runFake(t, []byte("{"), "", rel("v0.1.0", shapeSeven, "X"))
	mustFail(t, "", err, "the manifest of the release being built")

	_, err, _ = runFake(t, current("X"), "", rel(cur, shapeSeven, "Y"))
	mustFail(t, "", err, "is the release being built")

	var out bytes.Buffer
	err = checkHistory(&out, current("X"), []string{"v0.1.0", "v0.1.0"}, "", dirFetcher(t.TempDir()))
	if err == nil {
		t.Error("a missing fixture file passed")
	}
	err = checkHistory(&out, current("X"), []string{"latest"}, "", dirFetcher(t.TempDir()))
	mustFail(t, "", err, `"latest" is not a release tag`)
}

// The set hash is the one of ADR 0010 A20.3: SHA-256 over the seven raw
// digests in the order of A20.1.
func TestHistorySetHashIsTheHashOfA203(t *testing.T) {
	rs, err := classify(current("X"), "")
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	for _, n := range []string{"oarfish-update-helper", "oarfish-update-helper.socket", "oarfish-update-helper.service",
		"oarfish-update-recover.service", "oarfish-broker", "oarfish-broker.socket", "oarfish-broker.service"} {
		b, _ := hex.DecodeString(fakeDigest("X " + n))
		h.Write(b)
	}
	if want := hex.EncodeToString(h.Sum(nil)); rs.hash != want {
		t.Errorf("set hash %s, want %s", rs.hash, want)
	}
	// oarfish.service and the main binary are not part of the set.
	other := bytes.Replace(current("X"), []byte(fakeDigest("service")), []byte(fakeDigest("other")), 1)
	rs2, _ := classify(other, "")
	if rs2.hash != rs.hash {
		t.Error("a different oarfish.service changed the set hash")
	}
}

func TestHistoryHTTPFetcher(t *testing.T) {
	body := fakeManifest("v0.1.0", shapeSeven, "X")
	var sawAuth bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			sawAuth = true
		}
		switch r.URL.Path {
		case "/dl/v0.1.0/manifest.json":
			_, _ = w.Write(body)
		case "/dl/v0.1.1/manifest.json":
			_, _ = w.Write(bytes.Repeat([]byte(" "), maxManifestBytes+1))
		case "/dl/v0.1.2/manifest.json":
			http.Redirect(w, r, "http://example.invalid/x", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := srv.Client()
	client.CheckRedirect = anonymousClient().CheckRedirect
	fetch := httpFetcher(client, srv.URL+"/dl/")

	if got, err := fetch("v0.1.0"); err != nil || !bytes.Equal(got, body) {
		t.Errorf("fetch: %v", err)
	}
	if _, err := fetch("v0.1.1"); err == nil || !strings.Contains(err.Error(), "larger than 1 MiB") {
		t.Errorf("oversize: %v", err)
	}
	if _, err := fetch("v0.1.2"); err == nil || !strings.Contains(err.Error(), "non-https") {
		t.Errorf("redirect to http: %v", err)
	}
	if _, err := fetch("v0.1.3"); err == nil || !strings.Contains(err.Error(), "HTTP status 404") {
		t.Errorf("404: %v", err)
	}
	if sawAuth {
		t.Error("a request carried an Authorization header")
	}

	// A server that is gone: the whole check fails closed.
	srv.Close()
	var out bytes.Buffer
	err := checkHistory(&out, current("Y"), []string{"v0.1.0"}, "", fetch)
	mustFail(t, out.String(), err, "cannot read the manifest of v0.1.0", "fails closed")
}

func TestHistoryFlags(t *testing.T) {
	for _, args := range [][]string{
		{"v0.1.0"},
		{"-manifest", "m.json", "v0.1.0"},
		{"-manifest", "m.json", "-base-url", "https://x", "-manifests-dir", "d", "v0.1.0"},
		{"-manifest", "m.json", "-base-url", "http://x", "v0.1.0"},
	} {
		if err := runHistory(args); err == nil {
			t.Errorf("runHistory(%q) passed", args)
		}
	}
}
