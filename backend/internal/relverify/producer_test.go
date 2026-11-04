package relverify

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/swingstate/oarfish/backend/internal/trustkey"
)

// goBinary finds the go tool of the running toolchain.
func goBinary() string {
	p := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return "go"
}

func run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

// TestProducerCompatibility proves that the package accepts what the release
// tooling produces. scripts/bundlegen is a separate module that backend tests
// cannot import, so this test builds it, makes a release of each bundle shape
// with the real scripts (bundlegen, stamp-installer.sh, sign-release.sh with a
// throwaway minisign key) and verifies the result with this package.
//
// It is skipped unless OARFISH_RELVERIFY_PRODUCER=1, because it needs bash,
// minisign and a Go build of another module. The signing job of CI sets it
// (make test-relverify-producer), so it runs on every change there; when it is
// set, a missing tool is a failure, not a skip.
func TestProducerCompatibility(t *testing.T) {
	if os.Getenv("OARFISH_RELVERIFY_PRODUCER") != "1" {
		t.Skip("set OARFISH_RELVERIFY_PRODUCER=1 (make test-relverify-producer; the signing job of CI does)")
	}
	for _, tool := range []string{"bash", "minisign"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s is required when OARFISH_RELVERIFY_PRODUCER=1: %v", tool, err)
		}
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	bundlegen := filepath.Join(tmp, "bundlegen")
	run(t, filepath.Join(repo, "scripts", "bundlegen"), goBinary(), "build", "-trimpath", "-o", bundlegen, ".")

	// Real ELF headers for the two programs; the bytes after them are text.
	for _, n := range []string{"oarfish", "helper"} {
		if err := os.WriteFile(filepath.Join(tmp, n), elfStub(n+" producer test"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const commit = "0123456789abcdef0123456789abcdef01234567"
	shapes := []struct {
		name       string
		version    string
		components bool
	}{
		{"legacy", "0.1.0-test.1", false},
		{"components", "0.1.0-test.2", true},
	}
	sawShape := map[bool]bool{}
	for _, sh := range shapes {
		t.Run(sh.name, func(t *testing.T) {
			dir := filepath.Join(tmp, sh.name)
			args := []string{"bundle",
				"-binary", filepath.Join(tmp, "oarfish"), "-license", filepath.Join(repo, "LICENSE"),
				"-notices", filepath.Join(repo, "THIRD_PARTY_NOTICES.txt"), "-unit", filepath.Join(repo, "install", "oarfish.service"),
				"-version", sh.version, "-commit", commit, "-epoch", "1700000000", "-out", dir}
			if sh.components {
				args = append(args,
					"-helper", filepath.Join(tmp, "helper"),
					"-helper-service", filepath.Join(repo, "install", "oarfish-update-helper.service"),
					"-helper-socket", filepath.Join(repo, "install", "oarfish-update-helper.socket"),
					"-recover-service", filepath.Join(repo, "install", "oarfish-update-recover.service"),
					"-components-protocol", "1", "-components-key-id", "11f1c52777090bfc",
					"-min-installed-version", "0.1.0-test.0", "-requires-helper-protocol", "1")
			}
			run(t, repo, bundlegen, args...)
			if err := os.WriteFile(filepath.Join(dir, "sbom.json"), []byte("{\"fixture\":true}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			run(t, repo, "bash", filepath.Join(repo, "scripts", "stamp-installer.sh"), sh.version, filepath.Join(dir, "install.sh"))
			bundle := "oarfish_" + sh.version + "_linux_arm64.tar.gz"
			run(t, repo, bundlegen, "checksums", "-dir", dir, bundle, "install.sh", "manifest.json", "sbom.json")
			pub := filepath.Join(tmp, sh.name+".pub")
			run(t, repo, "bash", filepath.Join(repo, "scripts", "sign-release.sh"), "--ephemeral", pub, dir)
			// The producer's own check agrees that the release is well formed.
			run(t, repo, bundlegen, "check", "-dir", dir)

			read := func(n string) []byte {
				b, err := os.ReadFile(filepath.Join(dir, n))
				if err != nil {
					t.Fatal(err)
				}
				return b
			}
			pubFile, err := os.ReadFile(pub)
			if err != nil {
				t.Fatal(err)
			}
			key, err := trustkey.Parse(pubFile)
			if err != nil {
				t.Fatal(err)
			}
			md := Metadata{Key: key, Version: sh.version, Architecture: "arm64",
				Checksums: read("checksums.txt"), Signature: read("checksums.txt.sig"), Manifest: read("manifest.json")}
			rel, err := VerifyMetadata(md)
			if err != nil {
				t.Fatalf("VerifyMetadata: %v (%s)", err, detail(err))
			}
			if (rel.Manifest.Components != nil) != sh.components {
				t.Fatal("shape")
			}
			sawShape[sh.components] = true
			if sh.components {
				if rel.Manifest.Components.KeyID != "11f1c52777090bfc" || rel.Manifest.Components.Protocol != 1 ||
					rel.Manifest.Requirements.MinInstalledVersion != "0.1.0-test.0" || rel.Manifest.Requirements.HelperProtocol != 1 {
					t.Fatalf("optional fields: %+v %+v", rel.Manifest.Requirements, rel.Manifest.Components)
				}
				if err := CheckUpgrade(mustVersion(t, "0.1.0-test.1"), rel.Manifest, 1); err != nil {
					t.Fatal(err)
				}
			}
			for _, n := range []string{"install.sh", "sbom.json"} {
				if _, ok := rel.Checksums.Hash(n); !ok {
					t.Errorf("%s not exposed", n)
				}
			}
			bundleBytes := read(bundle)
			if err := rel.CheckBundle(SumHex(bundleBytes), int64(len(bundleBytes))); err != nil {
				t.Fatal(err)
			}
			sink := newMemSink()
			if err := Inspect(bytes.NewReader(bundleBytes), rel.Manifest, nil); err != nil {
				t.Fatalf("Inspect (verify only): %v (%s)", err, detail(err))
			}
			if err := Inspect(bytes.NewReader(bundleBytes), rel.Manifest, sink); err != nil {
				t.Fatalf("Inspect: %v (%s)", err, detail(err))
			}
			want := 4
			if sh.components {
				want = 8
			}
			if len(sink.finished) != want {
				t.Fatalf("%d files finished, want %d", len(sink.finished), want)
			}

			// And the producer's output is refused when it is changed.
			flipped := append([]byte(nil), bundleBytes...)
			flipped[len(flipped)/2] ^= 0xff
			wantCode(t, rel.CheckBundle(SumHex(flipped), int64(len(flipped))), CodeChecksumMismatch)
			md.Checksums = append(append([]byte(nil), md.Checksums...), ' ')
			_, err = VerifyMetadata(md)
			wantCode(t, err, CodeSignatureInvalid)
		})
	}
	if !sawShape[true] || !sawShape[false] {
		t.Fatal("both bundle shapes must be exercised")
	}
}
