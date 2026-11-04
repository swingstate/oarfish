package relverify

import (
	"bytes"
	"strings"
	"testing"
)

func TestVerifyMetadataValid(t *testing.T) {
	for _, comps := range []bool{false, true} {
		for _, v := range []string{"0.1.0-test.1", "1.2.3"} {
			r := newRelease(t, v, comps)
			rel, b := r.verified()
			if rel.Manifest.Version != v {
				t.Fatal(rel.Manifest.Version)
			}
			if err := rel.CheckBundle(SumHex(b.Bundle), int64(len(b.Bundle))); err != nil {
				t.Fatal(err)
			}
			// install.sh and the SBOM are listed and exposed, not interpreted.
			if _, ok := rel.Checksums.Hash("install.sh"); !ok {
				t.Fatal("install.sh not exposed")
			}
		}
	}
}

func TestVerifyMetadataTamper(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", true)
	b := r.build()
	t.Run("checksums.txt", func(t *testing.T) {
		m := r.meta(b)
		m.Checksums = append(append([]byte(nil), b.Checksums...), "extra line\n"...)
		_, err := VerifyMetadata(m)
		wantCode(t, err, CodeSignatureInvalid)
	})
	t.Run("signature file", func(t *testing.T) {
		m := r.meta(b)
		m.Signature = bytes.Replace(b.Sig, []byte("RW"), []byte("Rv"), 1)
		_, err := VerifyMetadata(m)
		wantCode(t, err, CodeSignatureInvalid)
	})
	t.Run("manifest.json, signature and checksums intact", func(t *testing.T) {
		m := r.meta(b)
		m.Manifest = append(append([]byte(nil), b.Manifest...), ' ')
		_, err := VerifyMetadata(m)
		wantDetail(t, err, CodeChecksumMismatch, "differs from the one in checksums.txt")
	})
	t.Run("bundle, signature and checksums intact", func(t *testing.T) {
		rel, _ := r.verified()
		flipped := append([]byte(nil), b.Bundle...)
		flipped[len(flipped)/2] ^= 0xff
		wantCode(t, rel.CheckBundle(SumHex(flipped), int64(len(flipped))), CodeChecksumMismatch)
		wantCode(t, rel.CheckBundle(SumHex(b.Bundle), int64(len(b.Bundle))+1), CodeChecksumMismatch)
		wantCode(t, rel.CheckBundle(SumHex(b.Bundle), int64(len(b.Bundle))-1), CodeChecksumMismatch)
	})
	t.Run("checksums.txt rewritten to match, signature of the old one", func(t *testing.T) {
		m := r.meta(b)
		m.Manifest = append(append([]byte(nil), b.Manifest...), ' ')
		m.Checksums = []byte(strings.Replace(string(b.Checksums), SumHex(b.Manifest), SumHex(m.Manifest), 1))
		_, err := VerifyMetadata(m)
		wantCode(t, err, CodeSignatureInvalid)
	})
	t.Run("signed by another key", func(t *testing.T) {
		m := r.meta(b)
		m.Key = newTestKey(t, "somebody else").key
		_, err := VerifyMetadata(m)
		wantCode(t, err, CodeSignatureInvalid)
	})
	t.Run("missing signature", func(t *testing.T) {
		m := r.meta(b)
		m.Signature = nil
		_, err := VerifyMetadata(m)
		wantCode(t, err, CodeSignatureInvalid)
	})
}

// A signed checksums.txt and signature of release A with the manifest and the
// bundle of release B: closed by the hashes, not by the signature.
func TestMixAndMatch(t *testing.T) {
	a := newRelease(t, "0.1.0-test.1", true)
	bRel := newRelease(t, "0.1.0-test.2", true)
	ba, bb := a.build(), bRel.build()

	t.Run("manifest of B with the checksums of A", func(t *testing.T) {
		m := a.meta(ba)
		m.Manifest = bb.Manifest
		_, err := VerifyMetadata(m)
		wantDetail(t, err, CodeChecksumMismatch, "differs from the one in checksums.txt")
	})
	t.Run("bundle of B with the files of A", func(t *testing.T) {
		rel, _ := a.verified()
		wantCode(t, rel.CheckBundle(SumHex(bb.Bundle), int64(len(bb.Bundle))), CodeChecksumMismatch)
	})
	t.Run("checksums of A, signature of B", func(t *testing.T) {
		// Same key, so the signature is valid for B's checksums only.
		m := a.meta(ba)
		m.Signature = bb.Sig
		_, err := VerifyMetadata(m)
		wantCode(t, err, CodeSignatureInvalid)
	})
	t.Run("all files of B, requested version A", func(t *testing.T) {
		// Everything is consistent and signed, but it is not the release that
		// was asked for: the listed bundle name contains the version.
		m := bRel.meta(bb)
		m.Version = "0.1.0-test.1"
		_, err := VerifyMetadata(m)
		wantCode(t, err, CodeChecksumMismatch)
	})
	t.Run("same version, same key, different bundle hash in the manifest", func(t *testing.T) {
		a2 := newRelease(t, "0.1.0-test.1", true)
		setPath(t, a2.man, "bundle.sha256", SumHex([]byte("something else")))
		_, err := VerifyMetadata(a2.meta(a2.build()))
		wantCode(t, err, CodeChecksumMismatch)
	})
}

// The manifest claims another version than the file names say.
func TestManifestVersionMismatch(t *testing.T) {
	for name, edit := range map[string]func(r *release){
		"version only": func(r *release) { setPath(t, r.man, "version", "0.1.0-test.9") },
		"version and tag": func(r *release) {
			setPath(t, r.man, "version", "0.1.0-test.9")
			setPath(t, r.man, "tag", "v0.1.0-test.9")
		},
		"tag only":          func(r *release) { setPath(t, r.man, "tag", "v0.1.0-test.9") },
		"bundle.file only":  func(r *release) { setPath(t, r.man, "bundle.file", "oarfish_0.1.0-test.9_linux_arm64.tar.gz") },
		"stable vs hyphen":  func(r *release) { setPath(t, r.man, "version", "0.1.0"); setPath(t, r.man, "tag", "v0.1.0") },
		"contents root dir": func(r *release) { setPath(t, r.man, "contents.0.path", "oarfish_0.1.0-test.9_linux_arm64/LICENSE") },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRelease(t, "0.1.0-test.1", true)
			edit(r)
			_, err := VerifyMetadata(r.meta(r.build()))
			wantCode(t, err, CodeManifestInvalid)
		})
	}
}

func TestVerifyMetadataArchitecture(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", false)
	m := r.meta(r.build())
	m.Architecture = "amd64"
	_, err := VerifyMetadata(m)
	wantCode(t, err, CodeRequirementsNotMet)
	m.Architecture = ""
	_, err = VerifyMetadata(m)
	wantCode(t, err, CodeRequirementsNotMet)
}

func TestVerifyMetadataBadRequestedVersion(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", false)
	for _, v := range []string{"", "1.0", "v0.1.0-test.1", "0.1.0-test.1+x", "../0.1.0", strings.Repeat("1", 65)} {
		m := r.meta(r.build())
		m.Version = v
		_, err := VerifyMetadata(m)
		wantCode(t, err, CodeBadRequest)
	}
}

// The signature of a validly signed older release verifies; the strictly-newer
// rule is what refuses it.
func TestReplay(t *testing.T) {
	old := newRelease(t, "0.1.0-test.1", true)
	rel, _ := old.verified()
	if rel.Manifest.Version != "0.1.0-test.1" {
		t.Fatal("the old release must verify")
	}
	installed := mustVersion(t, "0.1.0-test.2")
	wantCode(t, CheckUpgrade(installed, rel.Manifest, 1), CodeDowngradeRefused)
	same := mustVersion(t, "0.1.0-test.1")
	wantCode(t, CheckUpgrade(same, rel.Manifest, 1), CodeAlreadyInstalled)
	older := mustVersion(t, "0.1.0-test.0")
	if err := CheckUpgrade(older, rel.Manifest, 1); err != nil {
		t.Fatal(err)
	}
}

// The components shape with and without the matching files: the signed
// manifest and the tarball must agree (see also TestInspectShape).
func TestComponentsShapeThroughTheWholeChain(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", true)
	rel, b := r.verified()
	if rel.Manifest.Components == nil {
		t.Fatal("components shape expected")
	}
	if err := Inspect(bytes.NewReader(b.Bundle), rel.Manifest, newMemSink()); err != nil {
		t.Fatalf("%v (%s)", err, detail(err))
	}
	// Signed manifest says components; the bundle is a legacy bundle.
	legacy := newRelease(t, "0.1.0-test.1", false)
	err := Inspect(bytes.NewReader(legacy.bundle), rel.Manifest, newMemSink())
	if err == nil {
		t.Fatal("legacy bundle accepted for a components manifest")
	}
	// And the other way round.
	lrel, _ := legacy.verified()
	if err := Inspect(bytes.NewReader(b.Bundle), lrel.Manifest, newMemSink()); err == nil {
		t.Fatal("components bundle accepted for a legacy manifest")
	}
}

// A nil argument is a programming error and gives CodeInternal, not a panic.
func TestNilArguments(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", false)
	rel, _ := r.verified()
	v := mustVersion(t, "0.1.0")
	wantCode(t, Inspect(nil, rel.Manifest, nil), CodeInternal)
	wantCode(t, Inspect(bytes.NewReader(r.bundle), nil, nil), CodeInternal)
	wantCode(t, CheckMinInstalled(v, nil), CodeInternal)
	wantCode(t, CheckHelperProtocol(nil, 1), CodeInternal)
	wantCode(t, CheckUpgrade(v, nil, 1), CodeInternal)
	var nilRelease *Release
	wantCode(t, nilRelease.CheckBundle("", 0), CodeInternal)
	wantCode(t, (&Release{}).CheckBundle("", 0), CodeInternal)
	var nilManifest *Manifest
	wantCode(t, nilManifest.Check(Expect{}), CodeInternal)
	if _, err := nilManifest.Semver(); codeOf(err) != CodeInternal {
		t.Fatal("Semver")
	}
	var nilChecksums *Checksums
	wantCode(t, nilChecksums.Check("x", ""), CodeInternal)
	if _, ok := nilChecksums.Hash("x"); ok || nilChecksums.Names() != nil {
		t.Fatal("nil Checksums")
	}
	// The zero Version compares without a panic.
	_ = Compare(Version{}, v)
}
