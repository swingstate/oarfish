package relverify

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"
)

// These targets run their seed corpora as ordinary tests. `go test -fuzz` mutates
// the seeds; a crash found is fixed and its input kept in testdata/fuzz.

func FuzzParseSignature(f *testing.F) {
	k := newTestKey(f, "fuzz")
	msg := []byte("0000  fixture\n")
	good := k.sign(msg)
	f.Add(good)
	f.Add(good[:len(good)/2])
	f.Add(bytes.ReplaceAll(good, []byte("\n"), []byte("\r\n")))
	f.Add(k.payload("Ed", msg))
	f.Add(sigFile(k.payload("ED", msg), "x"))
	f.Add([]byte{})
	f.Add([]byte("\n\n\n\n"))
	if real, err := os.ReadFile("testdata/v0.1.0-alpha.6/checksums.txt.sig"); err == nil {
		f.Add(real)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		err := VerifySignature(msg, data, k.key)
		if err != nil {
			if c := codeOf(err); c != CodeSignatureInvalid {
				t.Fatalf("code %q", c)
			}
			return
		}
		// Accepted: the file must be exactly what the rules describe.
		p, perr := parseSignatureFile(data)
		if perr != nil {
			t.Fatal("accepted, but the container does not parse")
		}
		if string(p[:2]) != "Ed" || !bytes.Equal(p[2:10], k.id[:]) {
			t.Fatal("accepted with another algorithm or key id")
		}
		if !ed25519.Verify(k.priv.Public().(ed25519.PublicKey), msg, p[10:]) {
			t.Fatal("accepted without a valid signature")
		}
		if len(data) > maxSignatureFile || bytes.Count(data, []byte("\n")) != 4 || bytes.ContainsAny(data, "\r\x00") {
			t.Fatal("accepted a malformed container")
		}
	})
}

func serializeChecksums(c *Checksums) []byte {
	var b bytes.Buffer
	for _, n := range c.Names() {
		h, _ := c.Hash(n)
		b.WriteString(h + "  " + n + "\n")
	}
	return b.Bytes()
}

func FuzzParseChecksums(f *testing.F) {
	const bundle = "oarfish_0.1.0-test.1_linux_arm64.tar.gz"
	r := newRelease(f, "0.1.0-test.1", true)
	f.Add(r.build().Checksums)
	f.Add([]byte(h1 + "  manifest.json\n" + h3 + "  " + bundle + "\n"))
	f.Add([]byte(h1 + "  manifest.json\n" + h1 + "  manifest.json\n"))
	f.Add([]byte(strings.ToUpper(h3) + "  manifest.json\n"))
	f.Add([]byte{})
	f.Add([]byte("\n"))
	if real, err := os.ReadFile("testdata/v0.1.0-alpha.6/checksums.txt"); err == nil {
		f.Add(real)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := ParseChecksums(data, bundle)
		if err != nil {
			if codeOf(err) != CodeChecksumMismatch {
				t.Fatalf("code %q", codeOf(err))
			}
			return
		}
		// What the parser accepts is exactly the canonical text: parsing and
		// writing again gives the input back.
		if !bytes.Equal(serializeChecksums(c), data) {
			t.Fatal("accepted a spelling that is not canonical")
		}
		seen := map[string]bool{}
		for _, n := range c.Names() {
			if !validFileName(n) || seen[n] {
				t.Fatal("bad or repeated name")
			}
			seen[n] = true
			if h, _ := c.Hash(n); len(h) != 64 || !isLowerHex(h) {
				t.Fatal("bad hash")
			}
		}
		if !seen[bundle] || !seen[ManifestName] {
			t.Fatal("bundle or manifest not listed")
		}
	})
}

func FuzzParseManifest(f *testing.F) {
	for _, comps := range []bool{false, true} {
		r := newRelease(f, "0.1.0-test.1", comps)
		b, _ := json.Marshal(r.man)
		f.Add(b)
	}
	r := newRelease(f, "1.2.3", false)
	setPath(f, r.man, "requirements.min_installed_version", "1.0.0")
	setPath(f, r.man, "requirements.helper_protocol", 2)
	b, _ := json.Marshal(r.man)
	f.Add(b)
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"name":"oarfish","NAME":"x"}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`[[[[[[[[[[[[[[[[[[[[]]]]]]]]]]]]]]]]]]]]`))
	if real, err := os.ReadFile("testdata/v0.1.0-alpha.6/manifest.json"); err == nil {
		f.Add(real)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := ParseManifest(data)
		if err != nil {
			switch codeOf(err) {
			case CodeManifestInvalid, CodeRequirementsNotMet:
			default:
				t.Fatalf("code %q", codeOf(err))
			}
			return
		}
		v, verr := m.Semver()
		if verr != nil || v.String() != m.Version || m.Tag != "v"+m.Version || m.Prerelease != v.Prerelease() {
			t.Fatal("version, tag or prerelease inconsistent")
		}
		if m.SchemaVersion != 1 || m.Name != "oarfish" || m.OS != "linux" || m.Architecture != "arm64" || len(m.Commit) != 40 {
			t.Fatal("constant fields")
		}
		if m.Bundle.File != BundleFileName(v) || m.Bundle.Size < 1 || len(m.Bundle.SHA256) != 64 {
			t.Fatal("bundle")
		}
		if len(m.Contents) != len(layout(m.Components != nil)) {
			t.Fatal("contents do not have the declared shape")
		}
		var total int64
		seen := map[string]bool{}
		for _, e := range m.Contents {
			total += e.Size
			if seen[e.Path] || (e.Mode != 0o644 && e.Mode != 0o755) || e.Size < 1 || len(e.SHA256) != 64 || !isLowerHex(e.SHA256) {
				t.Fatal("bad entry")
			}
			seen[e.Path] = true
		}
		if total > maxContentBytes {
			t.Fatal("contents too large")
		}
		if mi := m.Requirements.MinInstalledVersion; mi != "" {
			if _, err := ParseVersion(mi); err != nil {
				t.Fatal("min_installed_version")
			}
		}
		// The inspector accepts what the parser produced (it does not panic on it).
		_ = Inspect(bytes.NewReader(nil), m, nil)
	})
}

func FuzzVersion(f *testing.F) {
	for i, tc := range versionTable {
		f.Add(tc.s, versionTable[(i+1)%len(versionTable)].s, versionTable[(i+7)%len(versionTable)].s)
	}
	for _, s := range []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0"} {
		f.Add(s, "1.0.0-beta.2", s)
	}
	f.Fuzz(func(t *testing.T, sa, sb, sc string) {
		var vs []Version
		for _, s := range []string{sa, sb, sc} {
			v, err := ParseVersion(s)
			if err != nil {
				if codeOf(err) != CodeBadRequest {
					t.Fatalf("code %q", codeOf(err))
				}
				continue
			}
			if v.String() != s || v.Prerelease() != strings.Contains(s, "-") || len(s) > 64 || strings.Contains(s, "..") {
				t.Fatalf("parse of %q inconsistent", s)
			}
			vs = append(vs, v)
		}
		for _, a := range vs {
			if Compare(a, a) != 0 {
				t.Fatal("not reflexive")
			}
			for _, b := range vs {
				ab, ba := Compare(a, b), Compare(b, a)
				if ab != -ba {
					t.Fatalf("not antisymmetric: %q %q", a, b)
				}
				if (ab == 0) != (a.String() == b.String()) {
					t.Fatalf("zero iff equal strings violated: %q %q", a, b)
				}
				// the rule agrees with Compare: installed a, candidate b
				err := CheckNewer(a, b)
				switch {
				case ba > 0 && err != nil, ba == 0 && codeOf(err) != CodeAlreadyInstalled, ba < 0 && codeOf(err) != CodeDowngradeRefused:
					t.Fatalf("CheckNewer(%q, %q) disagrees with Compare", a, b)
				}
				for _, c := range vs {
					if ab <= 0 && Compare(b, c) <= 0 && Compare(a, c) > 0 {
						t.Fatalf("not transitive: %q %q %q", a, b, c)
					}
				}
			}
		}
	})
}

func FuzzInspectTar(f *testing.F) {
	leg := newRelease(f, "0.1.0-test.1", false)
	com := newRelease(f, "0.1.0-test.1", true)
	legM, comM := parsed(f, leg), parsed(f, com)
	f.Add(leg.bundle, false)
	f.Add(com.bundle, true)
	f.Add(makeTar(entsOf(leg)), false)
	f.Add(makeTar(entsOf(com)), true)
	f.Add(gz(nil), false)
	f.Add([]byte{}, true)
	f.Add(makeTar(append(entsOf(leg), tarEnt{name: leg.root + "/link", typ: 50, link: "/etc/passwd"})), false)
	f.Add(makeTar(editEnt(entsOf(leg), "oarfish", func(e *tarEnt) { e.name = "/oarfish" })), false)
	f.Add(makeTar(dropEnt(entsOf(com), "oarfish-update-helper.socket")), true)
	f.Add(append(makeTar(entsOf(leg)), make([]byte, 4096)...), false)
	f.Fuzz(func(t *testing.T, data []byte, components bool) {
		m, r := legM, leg
		if components {
			m, r = comM, com
		}
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for _, input := range [][]byte{data, gz(data)} {
			errNil := Inspect(bytes.NewReader(input), m, nil)
			s := newMemSink()
			err := Inspect(bytes.NewReader(input), m, s)
			if (errNil == nil) != (err == nil) {
				t.Fatal("a nil sink and a real sink disagree")
			}
			if err != nil {
				c := codeOf(err)
				if c == "" || c == CodeInternal || c == CodeIOError {
					t.Fatalf("code %q", c)
				}
			}
			// Finish only for files that match the manifest exactly.
			for name, ok := range s.finished {
				if ok && !bytes.Equal(s.files[name], dataOf(r, name)) {
					t.Fatalf("%s finished with bytes that are not the manifest's", name)
				}
			}
			if err == nil && len(s.finished) != len(r.files) {
				t.Fatal("accepted, but not every file was finished")
			}
		}
		runtime.ReadMemStats(&after)
		if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 64<<20 {
			t.Fatalf("allocated %d bytes for an input of %d bytes", alloc, len(data))
		}
	})
}
