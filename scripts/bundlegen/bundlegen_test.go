package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testCommit = "0123456789abcdef0123456789abcdef01234567"

func testInput() bundleInput {
	return bundleInput{
		Binary:  []byte("\x7fELF-fake-oarfish-binary"),
		License: []byte("Apache License\n"),
		Notices: []byte("Third-party notices\n"),
		Version: "0.1.0-alpha.1",
		Commit:  testCommit,
		Epoch:   1_790_000_000,
	}
}

func TestBundleIsReproducible(t *testing.T) {
	a, ma, err := buildBundle(testInput())
	if err != nil {
		t.Fatal(err)
	}
	b, mb, err := buildBundle(testInput())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) || ma.Bundle.SHA256 != mb.Bundle.SHA256 {
		t.Fatal("two builds of the same input differ")
	}
}

func TestBundleDependsOnEpoch(t *testing.T) {
	a, _, _ := buildBundle(testInput())
	in := testInput()
	in.Epoch++
	b, _, _ := buildBundle(in)
	if bytes.Equal(a, b) {
		t.Fatal("mtime does not influence the bundle; epoch is ignored")
	}
}

func TestBundleLayout(t *testing.T) {
	tgz, mf, err := buildBundle(testInput())
	if err != nil {
		t.Fatal(err)
	}
	// gzip header: no name flag (FNAME, bit 3), zero MTIME.
	if tgz[3]&0x08 != 0 {
		t.Error("gzip header carries a file name")
	}
	if !bytes.Equal(tgz[4:8], []byte{0, 0, 0, 0}) {
		t.Errorf("gzip MTIME = % x, want zero", tgz[4:8])
	}

	zr, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	root := "oarfish_0.1.0-alpha.1_linux_arm64"
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
		if h.Uid != 0 || h.Gid != 0 || h.Uname != "" || h.Gname != "" {
			t.Errorf("%s: owner %d:%d %q:%q, want numeric 0:0", h.Name, h.Uid, h.Gid, h.Uname, h.Gname)
		}
		if h.ModTime.Unix() != 1_790_000_000 {
			t.Errorf("%s: mtime %v, want commit time", h.Name, h.ModTime)
		}
	}
	want := []string{root + "/", root + "/LICENSE", root + "/THIRD_PARTY_NOTICES.txt", root + "/oarfish"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("entries %v, want %v", names, want)
	}
	if mf.BuildDate != time.Unix(1_790_000_000, 0).UTC().Format(time.RFC3339) {
		t.Errorf("build_date %s", mf.BuildDate)
	}
	if !mf.Prerelease {
		t.Error("0.1.0-alpha.1 must be marked prerelease")
	}
}

func TestStableVersionIsNotPrerelease(t *testing.T) {
	in := testInput()
	in.Version = "1.2.3"
	_, mf, err := buildBundle(in)
	if err != nil {
		t.Fatal(err)
	}
	if mf.Prerelease || mf.Bundle.File != "oarfish_1.2.3_linux_arm64.tar.gz" {
		t.Errorf("prerelease=%v file=%s", mf.Prerelease, mf.Bundle.File)
	}
}

func TestRejectsBadInput(t *testing.T) {
	for name, mut := range map[string]func(*bundleInput){
		"v prefix":     func(i *bundleInput) { i.Version = "v1.0.0" },
		"not semver":   func(i *bundleInput) { i.Version = "1.0" },
		"short commit": func(i *bundleInput) { i.Commit = "abc123" },
		"empty binary": func(i *bundleInput) { i.Binary = nil },
	} {
		in := testInput()
		mut(&in)
		if _, _, err := buildBundle(in); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

// writeRelease lays out a complete release directory for the check tests.
func writeRelease(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	tgz, mf, err := buildBundle(testInput())
	if err != nil {
		t.Fatal(err)
	}
	mj, _ := marshalManifest(mf)
	must(t, os.WriteFile(filepath.Join(dir, mf.Bundle.File), tgz, 0o644))
	must(t, os.WriteFile(filepath.Join(dir, manifestName), mj, 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "sbom.json"), []byte("{}"), 0o644))
	txt, err := buildChecksums(dir, []string{mf.Bundle.File, manifestName, "sbom.json"})
	if err != nil {
		t.Fatal(err)
	}
	must(t, os.WriteFile(filepath.Join(dir, checksumName), []byte(txt), 0o644))
	return dir
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckAcceptsValidRelease(t *testing.T) {
	if err := checkRelease(writeRelease(t)); err != nil {
		t.Fatal(err)
	}
}

func TestChecksumsAreSortedAndSha256sumShaped(t *testing.T) {
	dir := writeRelease(t)
	raw, _ := os.ReadFile(filepath.Join(dir, checksumName))
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != 3 || !strings.HasSuffix(lines[0], "  manifest.json") ||
		!strings.HasSuffix(lines[1], "  oarfish_0.1.0-alpha.1_linux_arm64.tar.gz") || !strings.HasSuffix(lines[2], "  sbom.json") {
		t.Fatalf("unexpected checksums.txt:\n%s", raw)
	}
}

func TestCheckRejectsTampering(t *testing.T) {
	tamper := func(name string, f func(string)) {
		t.Run(name, func(t *testing.T) {
			dir := writeRelease(t)
			f(dir)
			if err := checkRelease(dir); err == nil {
				t.Fatal("tampered release was accepted")
			}
		})
	}
	flip := func(file string) func(string) {
		return func(dir string) {
			p := filepath.Join(dir, file)
			b, _ := os.ReadFile(p)
			b[len(b)/2] ^= 0xff
			must(t, os.WriteFile(p, b, 0o644))
		}
	}
	tamper("bundle", flip("oarfish_0.1.0-alpha.1_linux_arm64.tar.gz"))
	tamper("manifest", flip("manifest.json"))
	tamper("sbom", flip("sbom.json"))
	tamper("missing file", func(dir string) { must(t, os.Remove(filepath.Join(dir, "sbom.json"))) })
	tamper("manifest not listed", func(dir string) {
		p := filepath.Join(dir, checksumName)
		b, _ := os.ReadFile(p)
		var keep []string
		for _, l := range strings.Split(string(b), "\n") {
			if l != "" && !strings.HasSuffix(l, manifestName) {
				keep = append(keep, l)
			}
		}
		must(t, os.WriteFile(p, []byte(strings.Join(keep, "\n")+"\n"), 0o644))
	})
}

func TestCheckRejectsManifestBundleMismatch(t *testing.T) {
	dir := writeRelease(t)
	// A consistent but different bundle (other binary) with the old manifest.
	in := testInput()
	in.Binary = []byte("other binary")
	tgz, mf, _ := buildBundle(in)
	must(t, os.WriteFile(filepath.Join(dir, mf.Bundle.File), tgz, 0o644))
	txt, _ := buildChecksums(dir, []string{mf.Bundle.File, manifestName, "sbom.json"})
	must(t, os.WriteFile(filepath.Join(dir, checksumName), []byte(txt), 0o644))
	if err := checkRelease(dir); err == nil {
		t.Fatal("manifest/bundle mismatch was accepted")
	}
}

func TestChecksumsRejectPathsAndSignatures(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"../x", "a/b", "checksums.txt", "checksums.txt.sig"} {
		if _, err := buildChecksums(dir, []string{n}); err == nil {
			t.Errorf("%q accepted", n)
		}
	}
}
