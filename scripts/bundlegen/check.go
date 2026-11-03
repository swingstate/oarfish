package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func runChecksums(args []string) error {
	fs := flag.NewFlagSet("checksums", flag.ExitOnError)
	dir := fs.String("dir", "", "directory containing the files")
	fs.Parse(args)
	names := fs.Args()
	if *dir == "" || len(names) == 0 {
		return errors.New("checksums: -dir and at least one file name are required")
	}
	text, err := buildChecksums(*dir, names)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(*dir, checksumName), []byte(text), 0o644)
}

// buildChecksums returns sha256sum-compatible text ("<hash>  <name>"), sorted
// by name, so `sha256sum -c` and `shasum -a 256 -c` both accept it.
func buildChecksums(dir string, names []string) (string, error) {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	var sb strings.Builder
	for i, n := range sorted {
		if err := cleanBase(n); err != nil {
			return "", err
		}
		if n == checksumName || strings.HasSuffix(n, ".sig") {
			return "", fmt.Errorf("%s must not be listed in %s", n, checksumName)
		}
		if i > 0 && sorted[i-1] == n {
			return "", fmt.Errorf("duplicate file name %s", n)
		}
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&sb, "%s  %s\n", sum(b), n)
	}
	return sb.String(), nil
}

func runCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	dir := fs.String("dir", "", "release directory")
	expectKeyID := fs.String("expect-key-id", "", "require components.key_id to equal this value (16 lowercase hex digits)")
	fs.Parse(args)
	if *dir == "" {
		return errors.New("check: -dir is required")
	}
	opts := checkOptions{}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "expect-key-id" {
			opts.ExpectKeyID = expectKeyID
		}
	})
	if opts.ExpectKeyID != nil && !keyIDPattern.MatchString(*opts.ExpectKeyID) {
		return fmt.Errorf("check: -expect-key-id %q is not 16 lowercase hexadecimal digits", *opts.ExpectKeyID)
	}
	return checkReleaseWith(*dir, opts)
}

// checkOptions are the optional extra requirements of `bundlegen check`.
type checkOptions struct {
	// ExpectKeyID, when non-nil, requires components.key_id to equal it. A
	// release without a components object fails: the caller expected a helper.
	ExpectKeyID *string
}

// checkRelease verifies, without trusting any single file on its own:
//  1. every file listed in checksums.txt exists and matches its hash,
//  2. manifest.json, install.sh and the bundle are all listed, and install.sh
//     is stamped with exactly the manifest's version,
//  3. the manifest agrees with the bundle (name, size, SHA-256),
//  4. the manifest declares exactly one of the two bundle shapes (legacy, or
//     components with the update helper and its three units), the optional
//     requirements fields and the components object are valid, and
//  5. the tarball holds exactly the files the manifest declares, with the
//     declared modes and hashes, owned by uid/gid 0, and the helper is an
//     ARM64 ELF executable.
//
// The manifest is decoded strictly: an unknown field is an error, because this
// tool validates the manifest we produce at release time. (The readers on the
// device ignore unknown fields so that additive changes stay compatible, ADR 0010,
// U4; that is their business, not this tool's.) It does not check the signature;
// that is minisign's job and must happen first.
func checkRelease(dir string) error { return checkReleaseWith(dir, checkOptions{}) }

func checkReleaseWith(dir string, opts checkOptions) error {
	listed, err := readChecksums(filepath.Join(dir, checksumName))
	if err != nil {
		return err
	}
	for name, want := range listed {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if have := sum(got); have != want {
			return fmt.Errorf("%s: SHA-256 mismatch (checksums.txt %s, file %s)", name, want, have)
		}
	}

	mraw, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return err
	}
	var mf Manifest
	dec := json.NewDecoder(bytes.NewReader(mraw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&mf); err != nil {
		return fmt.Errorf("%s: %w", manifestName, err)
	}
	if _, ok := listed[manifestName]; !ok {
		return fmt.Errorf("%s is not listed in %s", manifestName, checksumName)
	}
	if mf.SchemaVersion != 1 || mf.Name != productName || mf.OS != osName || mf.Architecture != archName {
		return errors.New("manifest: unexpected schema_version, name, os or architecture")
	}
	if !versionPattern.MatchString(mf.Version) || mf.Tag != "v"+mf.Version || !commitPattern.MatchString(mf.Commit) {
		return errors.New("manifest: invalid version, tag or commit")
	}
	if mf.Bundle.File != bundleFileName(mf.Version) {
		return fmt.Errorf("manifest: bundle.file %q does not match version", mf.Bundle.File)
	}
	if err := checkExtras(&mf); err != nil {
		return err
	}
	if err := checkContents(&mf); err != nil {
		return err
	}
	if opts.ExpectKeyID != nil {
		if mf.Components == nil {
			return fmt.Errorf("manifest: -expect-key-id %s was given but the release has no components object", *opts.ExpectKeyID)
		}
		if mf.Components.KeyID != *opts.ExpectKeyID {
			return fmt.Errorf("manifest: components.key_id %s differs from the expected key id %s", mf.Components.KeyID, *opts.ExpectKeyID)
		}
	}
	if _, ok := listed[installerName]; !ok {
		return fmt.Errorf("%s is not listed in %s", installerName, checksumName)
	}
	iraw, err := os.ReadFile(filepath.Join(dir, installerName))
	if err != nil {
		return err
	}
	if err := checkInstaller(iraw, mf.Version); err != nil {
		return err
	}
	if want, ok := listed[mf.Bundle.File]; !ok || want != mf.Bundle.SHA256 {
		return fmt.Errorf("manifest: bundle SHA-256 differs from %s", checksumName)
	}
	braw, err := os.ReadFile(filepath.Join(dir, mf.Bundle.File))
	if err != nil {
		return err
	}
	if int64(len(braw)) != mf.Bundle.Size || sum(braw) != mf.Bundle.SHA256 {
		return errors.New("manifest: bundle size or SHA-256 does not match the file")
	}
	return checkTarball(braw, &mf)
}

func readChecksums(p string) (map[string]string, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		hash, name, ok := strings.Cut(line, "  ")
		if !ok || len(hash) != 64 {
			return nil, fmt.Errorf("%s: malformed line %q", checksumName, line)
		}
		if err := cleanBase(name); err != nil {
			return nil, err
		}
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("%s: duplicate entry %s", checksumName, name)
		}
		out[name] = hash
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s is empty", checksumName)
	}
	return out, nil
}

func checkTarball(tgz []byte, mf *Manifest) error {
	zr, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return err
	}
	if zr.Name != "" || !zr.ModTime.IsZero() {
		return errors.New("bundle: gzip header carries a name or timestamp")
	}
	root := strings.TrimSuffix(mf.Bundle.File, ".tar.gz")
	want := map[string]Entry{}
	for _, e := range mf.Contents {
		want[e.Path] = e
	}
	seen := map[string]bool{}
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Uid != 0 || hdr.Gid != 0 {
			return fmt.Errorf("bundle: %s is not owned by 0:0", hdr.Name)
		}
		if hdr.Typeflag == tar.TypeDir {
			if hdr.Name != root+"/" {
				return fmt.Errorf("bundle: unexpected directory %s", hdr.Name)
			}
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return fmt.Errorf("bundle: %s is not a regular file", hdr.Name)
		}
		e, ok := want[hdr.Name]
		if !ok {
			return fmt.Errorf("bundle: %s is not declared in the manifest", hdr.Name)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return err
		}
		if sum(data) != e.SHA256 || int64(len(data)) != e.Size || fmt.Sprintf("%04o", hdr.Mode) != e.Mode {
			return fmt.Errorf("bundle: %s differs from the manifest (hash, size or mode)", hdr.Name)
		}
		if seen[hdr.Name] {
			return fmt.Errorf("bundle: %s appears twice", hdr.Name)
		}
		if hdr.Name == root+"/"+helperName {
			if err := checkARM64ELF(data); err != nil {
				return fmt.Errorf("bundle: %s: %w", hdr.Name, err)
			}
		}
		seen[hdr.Name] = true
	}
	for p := range want {
		if !seen[p] {
			return fmt.Errorf("bundle: %s is declared in the manifest but missing", p)
		}
	}
	return nil
}

const installerPlaceholder = "@@OARFISH_VERSION@@"

// checkInstaller verifies that install.sh is a shell script stamped with the
// release version: exactly one OARFISH_STAMPED_VERSION line holding that version
// and no placeholder left anywhere.
func checkInstaller(raw []byte, version string) error {
	if !bytes.HasPrefix(raw, []byte("#!/bin/sh\n")) {
		return errors.New("install.sh: does not start with #!/bin/sh")
	}
	if bytes.Contains(raw, []byte(installerPlaceholder)) {
		return errors.New("install.sh: version placeholder is still present (not stamped)")
	}
	want := "OARFISH_STAMPED_VERSION='" + version + "'"
	n := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "OARFISH_STAMPED_VERSION=") {
			n++
			if line != want {
				return fmt.Errorf("install.sh: stamped version line is %q, want %q", line, want)
			}
		}
	}
	if n != 1 {
		return fmt.Errorf("install.sh: found %d OARFISH_STAMPED_VERSION lines, want 1", n)
	}
	return nil
}
