package relverify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swingstate/oarfish/backend/internal/trustkey"
)

// The golden files are three public artifacts of the published release
// v0.1.0-alpha.6, produced by the real release signing job (testdata/README.md).
// They prove that this package accepts what that job produced, with the
// embedded release key. The bundle is not committed.

const goldenDir = "testdata/v0.1.0-alpha.6"

var goldenHashes = map[string]string{
	"checksums.txt":     "8c527a9edb86d554e9e9273836244601b3ca1dbfd3650cb0249968ae53a91014",
	"checksums.txt.sig": "def20de9b9a6b696d28819bf742775ab03b22950d078135a5f4600da6b41e653",
	"manifest.json":     "1e356d9d92d3b965a374c24578d6eb33e025b0b2cc4449dd4d044728b9d4a276",
}

const (
	goldenVersion    = "0.1.0-alpha.6"
	goldenBundleHash = "e02113fb50cfb6bfb54db7a35e33eb66d37aacd15c3f5651ed565b4ae6fb1f3e"
	goldenBundleSize = 3008284
)

func golden(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(goldenDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestGoldenFilesMatchRecordedHashes(t *testing.T) {
	readme, err := os.ReadFile("testdata/README.md")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range goldenHashes {
		if got := SumHex(golden(t, name)); got != want {
			t.Errorf("%s: SHA-256 %s, recorded %s", name, got, want)
		}
		if !strings.Contains(string(readme), want) || !strings.Contains(string(readme), "releases/download/v"+goldenVersion+"/"+name) {
			t.Errorf("testdata/README.md does not record the hash and source of %s", name)
		}
	}
}

func goldenMetadata(t *testing.T) Metadata {
	key, err := trustkey.Release()
	if err != nil {
		t.Fatal(err)
	}
	return Metadata{Key: key, Version: goldenVersion, Architecture: "arm64",
		Checksums: golden(t, "checksums.txt"), Signature: golden(t, "checksums.txt.sig"), Manifest: golden(t, "manifest.json")}
}

func TestGoldenSignatureWithTheEmbeddedReleaseKey(t *testing.T) {
	m := goldenMetadata(t)
	if m.Key.ID() != "11f1c52777090bfc" {
		t.Fatalf("key id %s", m.Key.ID())
	}
	if err := VerifySignature(m.Checksums, m.Signature, m.Key); err != nil {
		t.Fatalf("%v (%s)", err, detail(err))
	}
	// And it is bound to those exact bytes and to that key.
	tampered := append([]byte(nil), m.Checksums...)
	tampered[0] = '9'
	wantCode(t, VerifySignature(tampered, m.Signature, m.Key), CodeSignatureInvalid)
	other := newTestKey(t, "not the release key")
	wantCode(t, VerifySignature(m.Checksums, m.Signature, other.key), CodeSignatureInvalid)
}

func TestGoldenRelease(t *testing.T) {
	m := goldenMetadata(t)
	rel, err := VerifyMetadata(m)
	if err != nil {
		t.Fatalf("%v (%s)", err, detail(err))
	}
	mf := rel.Manifest
	if mf.Components != nil || len(mf.Contents) != 4 {
		t.Fatal("v0.1.0-alpha.6 is a legacy-shape release")
	}
	if !mf.Prerelease || mf.Commit != "d4300bd2d0e4db7d7051f09f0aa8eb2171acda38" || mf.Bundle.Size != goldenBundleSize || mf.Bundle.SHA256 != goldenBundleHash {
		t.Fatalf("%+v", mf)
	}
	if mf.Requirements.MinInstalledVersion != "" || mf.Requirements.HelperProtocol != 0 {
		t.Fatal("the alpha.6 manifest predates the optional fields")
	}
	if err := rel.CheckBundle(goldenBundleHash, goldenBundleSize); err != nil {
		t.Fatal(err)
	}
	wantCode(t, rel.CheckBundle(goldenBundleHash, goldenBundleSize+1), CodeChecksumMismatch)
	// install.sh and the SBOMs are listed and exposed, not interpreted.
	for _, n := range []string{"install.sh", "sbom-backend.cdx.json", "sbom-frontend.cdx.json"} {
		if _, ok := rel.Checksums.Hash(n); !ok {
			t.Errorf("%s is not exposed", n)
		}
	}
	// Replay protection with the real release: it is valid, and a device on a
	// newer or equal version refuses it.
	cand := mustVersion(t, goldenVersion)
	if err := CheckUpgrade(mustVersion(t, "0.1.0-alpha.5"), mf, 1); err != nil {
		t.Fatal(err)
	}
	wantCode(t, CheckUpgrade(cand, mf, 1), CodeAlreadyInstalled)
	wantCode(t, CheckUpgrade(mustVersion(t, "0.1.0-alpha.7"), mf, 1), CodeDowngradeRefused)
	// The manifest does not belong to any other version.
	m.Version = "0.1.0-alpha.7"
	_, err = VerifyMetadata(m)
	wantCode(t, err, CodeChecksumMismatch)
}

func TestGoldenTamper(t *testing.T) {
	for _, name := range []string{"checksums.txt", "checksums.txt.sig", "manifest.json"} {
		t.Run(name, func(t *testing.T) {
			m := goldenMetadata(t)
			var b *[]byte
			switch name {
			case "checksums.txt":
				b = &m.Checksums
			case "checksums.txt.sig":
				b = &m.Signature
			default:
				b = &m.Manifest
			}
			c := append([]byte(nil), *b...)
			c[len(c)/2] ^= 1
			*b = c
			if _, err := VerifyMetadata(m); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}
