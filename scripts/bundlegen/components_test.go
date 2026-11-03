package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// armELF is the smallest header that passes checkARM64ELF: 64 bytes, ELF magic,
// class 64, little-endian, ET_EXEC, machine 0xB7.
func armELF() []byte {
	b := make([]byte, 64)
	copy(b, "\x7fELF\x02\x01\x01")
	b[16] = 2
	b[18] = 0xb7
	return b
}

const testKeyID = "0123456789abcdef"

func componentsTestInput() bundleInput {
	in := testInput()
	in.Components = &componentsInput{
		Helper:         armELF(),
		HelperService:  []byte("[Service]\n# helper\n"),
		HelperSocket:   []byte("[Socket]\n"),
		RecoverService: []byte("[Service]\n# recover\n"),
		KeyID:          testKeyID,
		Protocol:       1,
	}
	return in
}

// rawFile is a bundle file written by the tests themselves, not by buildBundle,
// so that a rejection test can build shapes the tool would never produce.
type rawFile struct {
	name string // relative to the bundle root
	mode int64
	data []byte
}

func filesOf(in bundleInput) []rawFile {
	fs := []rawFile{
		{"LICENSE", 0o644, in.License},
		{"THIRD_PARTY_NOTICES.txt", 0o644, in.Notices},
		{"oarfish", 0o755, in.Binary},
		{"oarfish.service", 0o644, in.Unit},
	}
	if c := in.Components; c != nil {
		data := c.files()
		for _, f := range componentLayout {
			fs = append(fs, rawFile{f.name, f.mode, data[f.name]})
		}
	}
	return fs
}

func withoutFile(fs []rawFile, name string) []rawFile {
	var out []rawFile
	for _, f := range fs {
		if f.name != name {
			out = append(out, f)
		}
	}
	return out
}

func replaceFile(fs []rawFile, name string, mod func(*rawFile)) []rawFile {
	out := append([]rawFile(nil), fs...)
	for i := range out {
		if out[i].name == name {
			mod(&out[i])
		}
	}
	return out
}

func packTar(t *testing.T, root string, files []rawFile, epoch int64) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	must(t, err)
	tw := tar.NewWriter(zw)
	mtime := time.Unix(epoch, 0).UTC()
	must(t, tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: root + "/", Mode: 0o755, ModTime: mtime}))
	for _, f := range files {
		must(t, tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: root + "/" + f.name, Mode: f.mode, Size: int64(len(f.data)), ModTime: mtime}))
		_, err := tw.Write(f.data)
		must(t, err)
	}
	must(t, tw.Close())
	must(t, zw.Close())
	return buf.Bytes()
}

// completeRelease adds installer, SBOM and checksums.txt around the bundle and
// manifest that are already in dir.
func completeRelease(t *testing.T, dir string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, manifestName))
	must(t, err)
	var mf Manifest
	must(t, json.Unmarshal(raw, &mf))
	must(t, os.WriteFile(filepath.Join(dir, "sbom.json"), []byte("{}"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, installerName), []byte(stampedInstaller(mf.Version)), 0o644))
	txt, err := buildChecksums(dir, []string{mf.Bundle.File, installerName, manifestName, "sbom.json"})
	must(t, err)
	must(t, os.WriteFile(filepath.Join(dir, checksumName), []byte(txt), 0o644))
}

// packRelease writes a release directory from raw files. The manifest starts as
// the one buildBundle makes for base, gets contents and bundle hash that match
// the files exactly, and is then changed by edit. Whatever edit does not touch
// is consistent, so a rejection comes from the rule under test.
func packRelease(t *testing.T, base bundleInput, files []rawFile, edit func(*Manifest)) string {
	t.Helper()
	_, mf, err := buildBundle(base)
	must(t, err)
	root := strings.TrimSuffix(mf.Bundle.File, ".tar.gz")
	tgz := packTar(t, root, files, base.Epoch)
	mf.Contents = nil
	for _, f := range files {
		mf.Contents = append(mf.Contents, Entry{Path: root + "/" + f.name, Mode: fmt.Sprintf("%04o", f.mode), Size: int64(len(f.data)), SHA256: sum(f.data)})
	}
	mf.Bundle.SHA256, mf.Bundle.Size = sum(tgz), int64(len(tgz))
	if edit != nil {
		edit(mf)
	}
	mj, err := marshalManifest(mf)
	must(t, err)
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, mf.Bundle.File), tgz, 0o644))
	must(t, os.WriteFile(filepath.Join(dir, manifestName), mj, 0o644))
	completeRelease(t, dir)
	return dir
}

// rewriteManifest changes manifest.json as generic JSON, so a test can add
// fields the Manifest type does not know, and refreshes checksums.txt.
func rewriteManifest(t *testing.T, dir string, edit func(map[string]any)) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, manifestName))
	must(t, err)
	var m map[string]any
	must(t, json.Unmarshal(raw, &m))
	edit(m)
	out, err := json.MarshalIndent(m, "", "  ")
	must(t, err)
	must(t, os.WriteFile(filepath.Join(dir, manifestName), append(out, '\n'), 0o644))
	completeRelease(t, dir)
}

func writeComponentsRelease(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	tgz, mf, err := buildBundle(componentsTestInput())
	must(t, err)
	mj, err := marshalManifest(mf)
	must(t, err)
	must(t, os.WriteFile(filepath.Join(dir, mf.Bundle.File), tgz, 0o644))
	must(t, os.WriteFile(filepath.Join(dir, manifestName), mj, 0o644))
	completeRelease(t, dir)
	return dir
}

func gunzip(t *testing.T, b []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	must(t, err)
	out, err := io.ReadAll(zr)
	must(t, err)
	return out
}

func tarNames(t *testing.T, tgz []byte) []string {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(gunzip(t, tgz)))
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return names
		}
		must(t, err)
		names = append(names, h.Name)
	}
}

// --- legacy output stays byte-identical ------------------------------------

// The files in testdata/legacy are the output of `bundlegen bundle` built from
// origin/main at 4a1dc79 (before G6.2) for the inputs of testInput(). The legacy
// bundle and manifest must not change by a single byte (ADR 0010, U14 row G6.2).
// Regenerate them only with a bundlegen of an older commit, and only when the Go
// toolchain's compress/flate output changed (the tar stream check below tells the
// two causes apart).
func TestLegacyOutputIsByteIdenticalToBaseline(t *testing.T) {
	in := testInput()
	src := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(src, name)
		must(t, os.WriteFile(p, data, 0o644))
		return p
	}
	out := t.TempDir()
	// Through the command line, so that no flag changes the default.
	must(t, runBundle([]string{
		"-binary", write("oarfish", in.Binary), "-license", write("LICENSE", in.License),
		"-notices", write("NOTICES", in.Notices), "-unit", write("unit", in.Unit),
		"-version", in.Version, "-commit", in.Commit, "-epoch", fmt.Sprint(in.Epoch), "-out", out,
	}))

	for _, name := range []string{bundleFileName(in.Version), manifestName} {
		want, err := os.ReadFile(filepath.Join("testdata", "legacy", name))
		must(t, err)
		got, err := os.ReadFile(filepath.Join(out, name))
		must(t, err)
		t.Logf("%s sha256 %s (baseline %s)", name, sum(got), sum(want))
		if name != manifestName && !bytes.Equal(gunzip(t, got), gunzip(t, want)) {
			t.Fatalf("%s: the tar stream differs from the baseline", name)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s differs from the baseline byte for byte (tar stream is equal: gzip output changed with the Go toolchain?)", name)
		}
	}
}

func TestLegacyManifestHasNoNewFields(t *testing.T) {
	_, mf, err := buildBundle(testInput())
	must(t, err)
	raw, err := marshalManifest(mf)
	must(t, err)
	for _, key := range []string{"components", "min_installed_version", "helper_protocol", "key_id"} {
		if bytes.Contains(raw, []byte(`"`+key+`"`)) {
			t.Errorf("legacy manifest contains %q", key)
		}
	}
}

func TestLegacyManifestWithoutNewFieldsIsValid(t *testing.T) {
	dir := writeRelease(t)
	raw, err := os.ReadFile(filepath.Join(dir, manifestName))
	must(t, err)
	var m map[string]any
	must(t, json.Unmarshal(raw, &m))
	if len(m) != 12 {
		t.Errorf("legacy manifest has %d top-level fields, want 12", len(m))
	}
	if r := m["requirements"].(map[string]any); len(r) != 5 {
		t.Errorf("legacy requirements has %d fields, want 5", len(r))
	}
	must(t, checkRelease(dir))
}

// --- components bundle ------------------------------------------------------

func TestComponentsBundleLayoutAndManifest(t *testing.T) {
	in := componentsTestInput()
	tgz, mf, err := buildBundle(in)
	must(t, err)
	root := "oarfish_0.1.0-alpha.1_linux_arm64"
	want := []string{
		root + "/", root + "/LICENSE", root + "/THIRD_PARTY_NOTICES.txt", root + "/oarfish",
		root + "/oarfish-update-helper", root + "/oarfish-update-helper.service", root + "/oarfish-update-helper.socket",
		root + "/oarfish-update-recover.service", root + "/oarfish.service",
	}
	if got := tarNames(t, tgz); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("entries %v, want %v", got, want)
	}
	if mf.SchemaVersion != 1 {
		t.Errorf("schema_version %d, want 1", mf.SchemaVersion)
	}
	if mf.Components == nil || mf.Components.KeyID != testKeyID || mf.Components.Protocol != 1 {
		t.Errorf("components = %+v", mf.Components)
	}
	modes := map[string]string{}
	for _, e := range mf.Contents {
		modes[e.Path] = e.Mode
		if e.Size == 0 || len(e.SHA256) != 64 {
			t.Errorf("%s: size %d, sha256 %q", e.Path, e.Size, e.SHA256)
		}
	}
	for name, mode := range map[string]string{
		"oarfish-update-helper": "0755", "oarfish-update-helper.service": "0644",
		"oarfish-update-helper.socket": "0644", "oarfish-update-recover.service": "0644",
	} {
		if modes[root+"/"+name] != mode {
			t.Errorf("%s: mode %q in contents, want %s", name, modes[root+"/"+name], mode)
		}
	}
	raw, err := marshalManifest(mf)
	must(t, err)
	if !bytes.Contains(raw, []byte(`"components": {`)) || bytes.Contains(raw, []byte("min_installed_version")) || bytes.Contains(raw, []byte(`"helper_protocol"`)) {
		t.Errorf("unexpected manifest fields:\n%s", raw)
	}
}

func TestComponentsBundleIsReproducible(t *testing.T) {
	a, ma, err := buildBundle(componentsTestInput())
	must(t, err)
	b, mb, err := buildBundle(componentsTestInput())
	must(t, err)
	ja, _ := marshalManifest(ma)
	jb, _ := marshalManifest(mb)
	if !bytes.Equal(a, b) || !bytes.Equal(ja, jb) {
		t.Fatal("two builds of the same components input differ")
	}
}

func TestCheckAcceptsComponentsRelease(t *testing.T) {
	must(t, checkRelease(writeComponentsRelease(t)))
}

func TestCheckAcceptsOptionalRequirements(t *testing.T) {
	min, equalMin, protocol := "0.1.0-alpha.0", "0.1.0-alpha.1", 1
	for name, edit := range map[string]func(*Manifest){
		"older min_installed_version":     func(m *Manifest) { m.Requirements.MinInstalledVersion = &min },
		"min_installed_version = version": func(m *Manifest) { m.Requirements.MinInstalledVersion = &equalMin },
		"helper_protocol = protocol":      func(m *Manifest) { m.Requirements.HelperProtocol = &protocol },
	} {
		t.Run(name, func(t *testing.T) {
			must(t, checkRelease(packRelease(t, componentsTestInput(), filesOf(componentsTestInput()), edit)))
		})
	}
	t.Run("legacy shape with requirements", func(t *testing.T) {
		must(t, checkRelease(packRelease(t, testInput(), filesOf(testInput()), func(m *Manifest) {
			m.Requirements.MinInstalledVersion, m.Requirements.HelperProtocol = &min, &protocol
		})))
	})
}

func TestBuildBundleWritesOptionalRequirements(t *testing.T) {
	min, protocol := "0.1.0-alpha.1", 1
	in := componentsTestInput()
	in.MinInstalledVersion, in.RequiresHelperProtocol = &min, &protocol
	_, mf, err := buildBundle(in)
	must(t, err)
	raw, _ := marshalManifest(mf)
	for _, want := range []string{`"min_installed_version": "0.1.0-alpha.1"`, `"helper_protocol": 1`} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Errorf("manifest lacks %s:\n%s", want, raw)
		}
	}
	for name, mut := range map[string]func(*bundleInput){
		"min_installed_version newer than version": func(i *bundleInput) { v := "0.2.0"; i.MinInstalledVersion = &v },
		"min_installed_version not SemVer":         func(i *bundleInput) { v := "v0.1.0"; i.MinInstalledVersion = &v },
		"helper_protocol above components":         func(i *bundleInput) { v := 2; i.RequiresHelperProtocol = &v },
		"helper_protocol zero":                     func(i *bundleInput) { v := 0; i.RequiresHelperProtocol = &v },
	} {
		in := componentsTestInput()
		mut(&in)
		if _, _, err := buildBundle(in); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

// --- the components set is all or nothing -----------------------------------

func TestBuildBundleRejectsIncompleteComponents(t *testing.T) {
	for name, mut := range map[string]func(*componentsInput){
		"no helper":          func(c *componentsInput) { c.Helper = nil },
		"no helper service":  func(c *componentsInput) { c.HelperService = nil },
		"no helper socket":   func(c *componentsInput) { c.HelperSocket = nil },
		"no recover service": func(c *componentsInput) { c.RecoverService = nil },
		"no key id":          func(c *componentsInput) { c.KeyID = "" },
		"key id uppercase":   func(c *componentsInput) { c.KeyID = strings.ToUpper(testKeyID) },
		"protocol 0":         func(c *componentsInput) { c.Protocol = 0 },
		"helper not ELF":     func(c *componentsInput) { c.Helper = []byte("#!/bin/sh\n") },
		"helper for x86-64":  func(c *componentsInput) { c.Helper = append([]byte(nil), armELF()...); c.Helper[18] = 0x3e },
	} {
		t.Run(name, func(t *testing.T) {
			in := componentsTestInput()
			mut(in.Components)
			if _, _, err := buildBundle(in); err == nil {
				t.Fatal("incomplete components input produced a bundle")
			}
		})
	}
}

// bundleArgs returns the flags of a complete components command line.
func bundleArgs(t *testing.T, out string) []string {
	t.Helper()
	in := componentsTestInput()
	src := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(src, name)
		must(t, os.WriteFile(p, data, 0o644))
		return p
	}
	c := in.Components
	return []string{
		"-binary", write("oarfish", in.Binary), "-license", write("LICENSE", in.License),
		"-notices", write("NOTICES", in.Notices), "-unit", write("unit", in.Unit),
		"-version", in.Version, "-commit", in.Commit, "-epoch", fmt.Sprint(in.Epoch), "-out", out,
		"-helper", write("helper", c.Helper), "-helper-service", write("hs", c.HelperService),
		"-helper-socket", write("hk", c.HelperSocket), "-recover-service", write("rs", c.RecoverService),
		"-components-protocol", "1", "-components-key-id", testKeyID,
	}
}

func TestBundleCommandLineBuildsComponentsBundle(t *testing.T) {
	out := t.TempDir()
	must(t, runBundle(bundleArgs(t, out)))
	completeRelease(t, out)
	must(t, checkRelease(out))
	must(t, checkReleaseWith(out, checkOptions{ExpectKeyID: ptr(testKeyID)}))

	// The command line produces the same bytes as the library call.
	want, _, err := buildBundle(componentsTestInput())
	must(t, err)
	got, err := os.ReadFile(filepath.Join(out, bundleFileName("0.1.0-alpha.1")))
	must(t, err)
	if !bytes.Equal(got, want) {
		t.Error("command line and buildBundle disagree")
	}
}

func TestBundleCommandLineRejectsPartialComponents(t *testing.T) {
	flags := map[string]int{"-helper": 2, "-helper-service": 2, "-helper-socket": 2, "-recover-service": 2, "-components-protocol": 2, "-components-key-id": 2}
	for flagName := range flags {
		t.Run("without "+flagName, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			var args []string
			full := bundleArgs(t, out)
			for i := 0; i < len(full); i += 2 {
				if full[i] != flagName {
					args = append(args, full[i], full[i+1])
				}
			}
			err := runBundle(args)
			if err == nil || !strings.Contains(err.Error(), "all or nothing") || !strings.Contains(err.Error(), flagName) {
				t.Fatalf("err = %v, want an all-or-nothing error naming %s", err, flagName)
			}
			if _, statErr := os.Stat(out); statErr == nil {
				t.Error("output directory was created although the command failed")
			}
		})
	}
}

func TestBundleCommandLineWritesOptionalRequirements(t *testing.T) {
	out := t.TempDir()
	must(t, runBundle(append(bundleArgs(t, out), "-min-installed-version", "0.1.0-alpha.1", "-requires-helper-protocol", "1")))
	raw, err := os.ReadFile(filepath.Join(out, manifestName))
	must(t, err)
	var mf Manifest
	must(t, json.Unmarshal(raw, &mf))
	if mf.Requirements.MinInstalledVersion == nil || *mf.Requirements.MinInstalledVersion != "0.1.0-alpha.1" || mf.Requirements.HelperProtocol == nil || *mf.Requirements.HelperProtocol != 1 {
		t.Errorf("requirements = %+v", mf.Requirements)
	}
	// An explicit but invalid value is an error, not "absent".
	if err := runBundle(append(bundleArgs(t, t.TempDir()), "-requires-helper-protocol", "0")); err == nil {
		t.Error("-requires-helper-protocol 0 was accepted")
	}
}

func ptr[T any](v T) *T { return &v }

// --- bundlegen check: rejections --------------------------------------------

func TestCheckRejectsWrongShape(t *testing.T) {
	legacy, comps := testInput(), componentsTestInput()
	noComponents := func(m *Manifest) { m.Components = nil }
	withComponents := func(m *Manifest) { m.Components = &Components{KeyID: testKeyID, Protocol: 1} }

	type tc struct {
		name  string
		base  bundleInput
		files []rawFile
		edit  func(*Manifest)
		want  string
	}
	cases := []tc{
		{"legacy: LICENSE missing", legacy, withoutFile(filesOf(legacy), "LICENSE"), nil, "does not list"},
		{"legacy: unit missing", legacy, withoutFile(filesOf(legacy), "oarfish.service"), nil, "does not list"},
		{"legacy: extra entry", legacy, append(filesOf(legacy), rawFile{"EXTRA", 0o644, []byte("x")}), nil, "unexpected entry"},
		{"legacy: oarfish mode 0644", legacy, replaceFile(filesOf(legacy), "oarfish", func(f *rawFile) { f.mode = 0o644 }), nil, "has mode 0644, want 0755"},
		{"legacy: unit mode 0755", legacy, replaceFile(filesOf(legacy), "oarfish.service", func(f *rawFile) { f.mode = 0o755 }), nil, "has mode 0755, want 0644"},
		{"legacy: components object without the files", legacy, filesOf(legacy), withComponents, "components object is present but contents does not list"},
		{"components: files without the components object", comps, filesOf(comps), noComponents, "no components object"},
		{"components: extra entry", comps, append(filesOf(comps), rawFile{"EXTRA", 0o644, []byte("x")}), nil, "unexpected entry"},
		{"components: LICENSE missing", comps, withoutFile(filesOf(comps), "LICENSE"), nil, "does not list"},
		{"components: helper mode 0644", comps, replaceFile(filesOf(comps), helperName, func(f *rawFile) { f.mode = 0o644 }), nil, "has mode 0644, want 0755"},
	}
	for _, f := range componentLayout {
		cases = append(cases, tc{"components: " + f.name + " missing", comps, withoutFile(filesOf(comps), f.name), nil, "components object is present but contents does not list"})
		if f.name != helperName {
			cases = append(cases, tc{"components: " + f.name + " mode 0755", comps, replaceFile(filesOf(comps), f.name, func(r *rawFile) { r.mode = 0o755 }), nil, "has mode 0755, want 0644"})
		}
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantRejected(t, checkRelease(packRelease(t, c.base, c.files, c.edit)), c.want)
		})
	}
}

func wantRejected(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("accepted, want an error containing %q", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("rejected for another reason: %v (want %q)", err, substr)
	}
}

func TestCheckRejectsHelperThatIsNotArm64ELF(t *testing.T) {
	mod := func(f func(b []byte) []byte) []rawFile {
		return replaceFile(filesOf(componentsTestInput()), helperName, func(r *rawFile) { r.data = f(append([]byte(nil), armELF()...)) })
	}
	for name, files := range map[string][]rawFile{
		"shell script": replaceFile(filesOf(componentsTestInput()), helperName, func(r *rawFile) { r.data = []byte("#!/bin/sh\nexit 0\n") }),
		"32-bit ELF":   mod(func(b []byte) []byte { b[4] = 1; return b }),
		"big-endian":   mod(func(b []byte) []byte { b[5] = 2; return b }),
		"x86-64":       mod(func(b []byte) []byte { b[18] = 0x3e; return b }),
		"machine high": mod(func(b []byte) []byte { b[19] = 1; return b }),
		"truncated":    mod(func(b []byte) []byte { return b[:12] }),
	} {
		t.Run(name, func(t *testing.T) {
			wantRejected(t, checkRelease(packRelease(t, componentsTestInput(), files, nil)), "oarfish-update-helper")
		})
	}
}

func TestCheckRejectsBadComponentsObject(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(*Manifest)
		want string
	}{
		"key id uppercase":    {func(m *Manifest) { m.Components.KeyID = strings.ToUpper(testKeyID) }, "components.key_id"},
		"key id 15 digits":    {func(m *Manifest) { m.Components.KeyID = testKeyID[:15] }, "components.key_id"},
		"key id 17 digits":    {func(m *Manifest) { m.Components.KeyID = testKeyID + "0" }, "components.key_id"},
		"key id not hex":      {func(m *Manifest) { m.Components.KeyID = "0123456789abcdeg" }, "components.key_id"},
		"key id empty":        {func(m *Manifest) { m.Components.KeyID = "" }, "components.key_id"},
		"protocol 0":          {func(m *Manifest) { m.Components.Protocol = 0 }, "components.protocol"},
		"protocol negative":   {func(m *Manifest) { m.Components.Protocol = -1 }, "components.protocol"},
		"requires newer than": {func(m *Manifest) { m.Requirements.HelperProtocol = ptr(2) }, "greater than components.protocol"},
	} {
		t.Run(name, func(t *testing.T) {
			wantRejected(t, checkRelease(packRelease(t, componentsTestInput(), filesOf(componentsTestInput()), tc.edit)), tc.want)
		})
	}
}

func TestCheckRejectsBadRequirements(t *testing.T) {
	for _, base := range []struct {
		name string
		in   bundleInput
	}{{"legacy", testInput()}, {"components", componentsTestInput()}} {
		for name, tc := range map[string]struct {
			edit func(*Manifest)
			want string
		}{
			"min_installed_version with v prefix":  {func(m *Manifest) { m.Requirements.MinInstalledVersion = ptr("v0.1.0-alpha.1") }, "not a SemVer"},
			"min_installed_version two parts":      {func(m *Manifest) { m.Requirements.MinInstalledVersion = ptr("0.1") }, "not a SemVer"},
			"min_installed_version empty":          {func(m *Manifest) { m.Requirements.MinInstalledVersion = ptr("") }, "not a SemVer"},
			"min_installed_version newer patch":    {func(m *Manifest) { m.Requirements.MinInstalledVersion = ptr("0.1.1-alpha.1") }, "newer than version"},
			"min_installed_version is the release": {func(m *Manifest) { m.Requirements.MinInstalledVersion = ptr("0.1.0") }, "newer than version"},
			"min_installed_version newer major":    {func(m *Manifest) { m.Requirements.MinInstalledVersion = ptr("9.0.0") }, "newer than version"},
			"helper_protocol 0":                    {func(m *Manifest) { m.Requirements.HelperProtocol = ptr(0) }, "helper_protocol"},
			"helper_protocol negative":             {func(m *Manifest) { m.Requirements.HelperProtocol = ptr(-3) }, "helper_protocol"},
		} {
			t.Run(base.name+": "+name, func(t *testing.T) {
				wantRejected(t, checkRelease(packRelease(t, base.in, filesOf(base.in), tc.edit)), tc.want)
			})
		}
	}
}

func TestCheckRejectsContentsTarballMismatch(t *testing.T) {
	in := componentsTestInput()
	root := "oarfish_0.1.0-alpha.1_linux_arm64/"
	entry := func(m *Manifest, name string) *Entry {
		for i := range m.Contents {
			if m.Contents[i].Path == root+name {
				return &m.Contents[i]
			}
		}
		t.Fatalf("no contents entry %s", name)
		return nil
	}
	for _, name := range []string{helperName, helperServiceName, helperSocketName, recoverServiceName} {
		t.Run(name+": hash", func(t *testing.T) {
			wantRejected(t, checkRelease(packRelease(t, in, filesOf(in), func(m *Manifest) { entry(m, name).SHA256 = sum([]byte("other")) })), "differs from the manifest")
		})
		t.Run(name+": size", func(t *testing.T) {
			wantRejected(t, checkRelease(packRelease(t, in, filesOf(in), func(m *Manifest) { entry(m, name).Size++ })), "differs from the manifest")
		})
	}
	t.Run("tar mode differs from contents", func(t *testing.T) {
		files := replaceFile(filesOf(in), helperName, func(f *rawFile) { f.mode = 0o700 })
		wantRejected(t, checkRelease(packRelease(t, in, files, func(m *Manifest) { entry(m, helperName).Mode = "0755" })), "differs from the manifest")
	})
	t.Run("contents lists a path that is not in the tarball", func(t *testing.T) {
		wantRejected(t, checkRelease(packRelease(t, in, withoutFile(filesOf(in), recoverServiceName), func(m *Manifest) {
			m.Contents = append(m.Contents, Entry{Path: root + recoverServiceName, Mode: "0644", Size: 1, SHA256: sum([]byte("x"))})
		})), "declared in the manifest but missing")
	})
	t.Run("tarball holds a file that contents does not list", func(t *testing.T) {
		wantRejected(t, checkRelease(packRelease(t, in, filesOf(in), func(m *Manifest) {
			m.Contents = m.Contents[:len(m.Contents)-1] // contents loses the last entry, the tarball keeps it
		})), "components object is present but contents does not list")
	})
	t.Run("contents lists a path twice", func(t *testing.T) {
		wantRejected(t, checkRelease(packRelease(t, in, filesOf(in), func(m *Manifest) { m.Contents = append(m.Contents, *entry(m, helperName)) })), "twice")
	})
	t.Run("tarball holds a file twice", func(t *testing.T) {
		files := append(filesOf(in), rawFile{helperName, 0o755, armELF()})
		wantRejected(t, checkRelease(packRelease(t, in, files, func(m *Manifest) { m.Contents = m.Contents[:len(m.Contents)-1] })), "appears twice")
	})
	t.Run("tarball of the legacy shape with a component file", func(t *testing.T) {
		// Manifest says legacy and lists five files; the tarball carries a sixth.
		files := append(filesOf(testInput()), rawFile{helperName, 0o755, armELF()})
		wantRejected(t, checkRelease(packRelease(t, testInput(), files, func(m *Manifest) { m.Contents = m.Contents[:len(m.Contents)-1] })), "not declared in the manifest")
	})
}

// --- bundlegen check -expect-key-id ------------------------------------------

func TestCheckExpectKeyID(t *testing.T) {
	dir := writeComponentsRelease(t)
	must(t, checkReleaseWith(dir, checkOptions{ExpectKeyID: ptr(testKeyID)}))
	wantRejected(t, checkReleaseWith(dir, checkOptions{ExpectKeyID: ptr("fedcba9876543210")}), "differs from the expected key id")
	// A release without a helper cannot satisfy an expectation about the helper.
	wantRejected(t, checkReleaseWith(writeRelease(t), checkOptions{ExpectKeyID: ptr(testKeyID)}), "no components object")
	// Not given: no expectation.
	must(t, checkReleaseWith(dir, checkOptions{}))
}

func TestCheckCommandLineExpectKeyID(t *testing.T) {
	dir := writeComponentsRelease(t)
	must(t, runCheck([]string{"-dir", dir, "-expect-key-id", testKeyID}))
	must(t, runCheck([]string{"-dir", dir}))
	wantRejected(t, runCheck([]string{"-dir", dir, "-expect-key-id", "fedcba9876543210"}), "differs from the expected key id")
	for _, bad := range []string{"", "0123456789ABCDEF", "0123", testKeyID + "0"} {
		wantRejected(t, runCheck([]string{"-dir", dir, "-expect-key-id", bad}), "-expect-key-id")
	}
}

// --- unknown fields ----------------------------------------------------------

// bundlegen check validates the manifest this tool wrote, so it stays strict.
// (The readers on the device ignore unknown fields; they are not this tool.)
func TestCheckRejectsUnknownManifestFields(t *testing.T) {
	for name, edit := range map[string]func(map[string]any){
		"top level":    func(m map[string]any) { m["future_field"] = 1 },
		"requirements": func(m map[string]any) { m["requirements"].(map[string]any)["future_requirement"] = "x" },
		"components":   func(m map[string]any) { m["components"].(map[string]any)["future_component_field"] = true },
	} {
		t.Run(name, func(t *testing.T) {
			dir := writeComponentsRelease(t)
			must(t, checkRelease(dir)) // valid before the change
			rewriteManifest(t, dir, edit)
			wantRejected(t, checkRelease(dir), "unknown field")
		})
	}
}

// --- helpers under test -------------------------------------------------------

func TestCompareSemver(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0-alpha", "1.0.0", -1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.beta", -1},
		{"1.0.0-alpha.beta", "1.0.0-beta", -1},
		{"1.0.0-beta", "1.0.0-beta.2", -1},
		{"1.0.0-beta.2", "1.0.0-beta.11", -1},
		{"1.0.0-beta.11", "1.0.0-rc.1", -1},
		{"1.0.0-rc.1", "1.0.0", -1},
		{"1.2.3", "1.10.0", -1},
		{"2.0.0", "1.99.99", 1},
		{"0.1.0-alpha.8", "0.1.0-alpha.10", -1},
		{"0.1.0-alpha.9", "0.1.0", -1},
		{"1.0.99999999999999999999", "1.0.100000000000000000000", -1},
	} {
		if got := compareSemver(tc.a, tc.b); got != tc.want {
			t.Errorf("compareSemver(%s, %s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
		if got := compareSemver(tc.b, tc.a); got != -tc.want {
			t.Errorf("compareSemver(%s, %s) = %d, want %d", tc.b, tc.a, got, -tc.want)
		}
	}
}

func TestCheckARM64ELF(t *testing.T) {
	if err := checkARM64ELF(armELF()); err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string][]byte{
		"empty": nil, "short": armELF()[:19], "text": []byte("#!/bin/sh\n#padding padding\n"),
		"class 32": func() []byte { b := armELF(); b[4] = 1; return b }(),
		"big":      func() []byte { b := armELF(); b[5] = 2; return b }(),
		"x86-64":   func() []byte { b := armELF(); b[18] = 0x3e; return b }(),
	} {
		if checkARM64ELF(b) == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
