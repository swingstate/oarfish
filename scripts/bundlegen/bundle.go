package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	productName  = "oarfish"
	osName       = "linux"
	archName     = "arm64"
	manifestName = "manifest.json"
	checksumName = "checksums.txt"
	// installerName is the release's install.sh, stamped with the release version
	// (scripts/stamp-installer.sh) and covered by checksums.txt like every asset.
	installerName = "install.sh"
)

// versionPattern is SemVer 2.0 without a leading "v".
var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$`)

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Manifest is manifest.json. Adding fields is compatible; removing or renaming
// them requires a new SchemaVersion.
type Manifest struct {
	SchemaVersion int          `json:"schema_version"`
	Name          string       `json:"name"`
	Version       string       `json:"version"`
	Tag           string       `json:"tag"`
	Prerelease    bool         `json:"prerelease"`
	Commit        string       `json:"commit"`
	BuildDate     string       `json:"build_date"`
	OS            string       `json:"os"`
	Architecture  string       `json:"architecture"`
	Requirements  Requirements `json:"requirements"`
	Bundle        BundleInfo   `json:"bundle"`
	Contents      []Entry      `json:"contents"`
	// Components is present if and only if the bundle delivers the update helper
	// and its units (ADR 0010). A legacy manifest does not carry it.
	Components *Components `json:"components,omitempty"`
}

// Requirements lists what the bundle needs from the target. Kernel and memory
// minimums are deliberately absent until they have been measured on a device.
type Requirements struct {
	OS                  string   `json:"os"`
	Architecture        string   `json:"architecture"`
	CPU                 string   `json:"cpu"`
	Libc                string   `json:"libc"`
	RuntimeDependencies []string `json:"runtime_dependencies"`
	// MinInstalledVersion and HelperProtocol are optional and additive (ADR 0010,
	// U4). They are pointers so that an explicit empty string or 0 in a manifest
	// is told apart from an absent field and rejected by checkExtras.
	MinInstalledVersion *string `json:"min_installed_version,omitempty"`
	HelperProtocol      *int    `json:"helper_protocol,omitempty"`
}

type BundleInfo struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type Entry struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type bundleInput struct {
	Binary  []byte
	License []byte
	// Notices is THIRD_PARTY_NOTICES.txt: the licence texts of the third-party
	// code compiled into the binary, which their licences require to ship with it.
	Notices []byte
	// Unit is the systemd unit (install/oarfish.service). It travels inside the
	// signed bundle so that it always matches the binary it starts.
	Unit    []byte
	Version string
	Commit  string
	Epoch   int64
	// Components is nil for the legacy bundle, the default. When set, the helper
	// and its three units join the bundle and the manifest gets "components".
	Components *componentsInput
	// MinInstalledVersion and RequiresHelperProtocol fill the optional
	// requirements fields of the same names; nil leaves them out.
	MinInstalledVersion    *string
	RequiresHelperProtocol *int
}

func runBundle(args []string) error {
	fs := flag.NewFlagSet("bundle", flag.ExitOnError)
	binary := fs.String("binary", "", "path to the linux/arm64 oarfish binary")
	license := fs.String("license", "", "path to the LICENSE file")
	notices := fs.String("notices", "", "path to THIRD_PARTY_NOTICES.txt")
	unit := fs.String("unit", "", "path to the systemd unit oarfish.service")
	version := fs.String("version", "", "version without leading v, e.g. 0.1.0-alpha.1")
	commit := fs.String("commit", "", "full 40-character commit hash")
	epoch := fs.Int64("epoch", -1, "commit timestamp (Unix seconds); used for tar mtimes and build_date")
	out := fs.String("out", "", "output directory")
	// The components set is all or nothing: these six flags are given together
	// or not at all. Without them the output is the legacy bundle.
	helper := fs.String("helper", "", "components: path to the linux/arm64 oarfish-update-helper binary")
	helperService := fs.String("helper-service", "", "components: path to oarfish-update-helper.service")
	helperSocket := fs.String("helper-socket", "", "components: path to oarfish-update-helper.socket")
	recoverService := fs.String("recover-service", "", "components: path to oarfish-update-recover.service")
	componentsProtocol := fs.Int("components-protocol", 0, "components: highest protocol the delivered helper implements (at least 1)")
	componentsKeyID := fs.String("components-key-id", "", "components: 16 lowercase hex digits, the key id embedded in the delivered helper")
	// Independent of the components set; absent unless given.
	minInstalled := fs.String("min-installed-version", "", "optional: requirements.min_installed_version (SemVer, not greater than -version)")
	requiresProtocol := fs.Int("requires-helper-protocol", 0, "optional: requirements.helper_protocol (at least 1)")
	fs.Parse(args)

	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })

	if *binary == "" || *license == "" || *notices == "" || *unit == "" || *out == "" || *epoch < 0 {
		return errors.New("bundle: -binary, -license, -notices, -unit, -epoch and -out are required")
	}
	componentFlags := []string{"helper", "helper-service", "helper-socket", "recover-service", "components-protocol", "components-key-id"}
	var missing []string
	for _, n := range componentFlags {
		if !given[n] {
			missing = append(missing, "-"+n)
		}
	}
	if len(missing) != 0 && len(missing) != len(componentFlags) {
		return fmt.Errorf("bundle: the components set is all or nothing; missing %s", strings.Join(missing, ", "))
	}
	bin, err := os.ReadFile(*binary)
	if err != nil {
		return err
	}
	lic, err := os.ReadFile(*license)
	if err != nil {
		return err
	}
	not, err := os.ReadFile(*notices)
	if err != nil {
		return err
	}
	unitData, err := os.ReadFile(*unit)
	if err != nil {
		return err
	}
	in := bundleInput{Binary: bin, License: lic, Notices: not, Unit: unitData, Version: *version, Commit: *commit, Epoch: *epoch}
	if len(missing) == 0 {
		c := &componentsInput{KeyID: *componentsKeyID, Protocol: *componentsProtocol}
		for _, f := range []struct {
			path string
			dst  *[]byte
		}{
			{*helper, &c.Helper}, {*helperService, &c.HelperService},
			{*helperSocket, &c.HelperSocket}, {*recoverService, &c.RecoverService},
		} {
			if *f.dst, err = os.ReadFile(f.path); err != nil {
				return err
			}
		}
		in.Components = c
	}
	if given["min-installed-version"] {
		in.MinInstalledVersion = minInstalled
	}
	if given["requires-helper-protocol"] {
		in.RequiresHelperProtocol = requiresProtocol
	}
	tgz, mf, err := buildBundle(in)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, mf.Bundle.File), tgz, 0o644); err != nil {
		return err
	}
	mj, err := marshalManifest(mf)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, manifestName), mj, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s  %s (%d bytes)\n", mf.Bundle.SHA256, mf.Bundle.File, mf.Bundle.Size)
	return nil
}

func bundleFileName(version string) string {
	return fmt.Sprintf("%s_%s_%s_%s.tar.gz", productName, version, osName, archName)
}

// buildBundle returns the tar.gz bytes and the matching manifest. The output is
// a pure function of its input: entries are sorted, mtimes are the commit
// timestamp, owners are numeric 0, and gzip carries no name or timestamp.
func buildBundle(in bundleInput) ([]byte, *Manifest, error) {
	if !versionPattern.MatchString(in.Version) {
		return nil, nil, fmt.Errorf("invalid version %q (want SemVer without leading v)", in.Version)
	}
	if !commitPattern.MatchString(in.Commit) {
		return nil, nil, fmt.Errorf("invalid commit %q (want 40 lowercase hex characters)", in.Commit)
	}
	if len(in.Binary) == 0 || len(in.License) == 0 || len(in.Notices) == 0 || len(in.Unit) == 0 {
		return nil, nil, errors.New("binary, license, third-party notices and unit must not be empty")
	}

	root := fmt.Sprintf("%s_%s_%s_%s", productName, in.Version, osName, archName)
	mtime := time.Unix(in.Epoch, 0).UTC()

	type file struct {
		name string
		mode int64
		data []byte
	}
	files := []file{
		{root + "/LICENSE", 0o644, in.License},
		{root + "/THIRD_PARTY_NOTICES.txt", 0o644, in.Notices},
		{root + "/oarfish", 0o755, in.Binary},
		{root + "/oarfish.service", 0o644, in.Unit},
	}
	if in.Components != nil {
		if err := in.Components.validate(); err != nil {
			return nil, nil, err
		}
		data := in.Components.files()
		for _, f := range componentLayout {
			files = append(files, file{root + "/" + f.name, f.mode, data[f.name]})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })

	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, nil, err
	}
	// Header stays zero: no file name, no modification time.
	tw := tar.NewWriter(zw)

	dirHdr := &tar.Header{
		Typeflag: tar.TypeDir, Name: root + "/", Mode: 0o755,
		ModTime: mtime,
	}
	if err := tw.WriteHeader(dirHdr); err != nil {
		return nil, nil, err
	}

	var contents []Entry
	for _, f := range files {
		hdr := &tar.Header{
			Typeflag: tar.TypeReg, Name: f.name, Mode: f.mode, Size: int64(len(f.data)),
			ModTime: mtime,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, nil, err
		}
		if _, err := tw.Write(f.data); err != nil {
			return nil, nil, err
		}
		contents = append(contents, Entry{
			Path: f.name, Mode: fmt.Sprintf("%04o", f.mode), Size: int64(len(f.data)), SHA256: sum(f.data),
		})
	}
	if err := tw.Close(); err != nil {
		return nil, nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, nil, err
	}
	tgz := buf.Bytes()

	mf := &Manifest{
		SchemaVersion: 1,
		Name:          productName,
		Version:       in.Version,
		Tag:           "v" + in.Version,
		Prerelease:    strings.Contains(in.Version, "-"),
		Commit:        in.Commit,
		BuildDate:     mtime.Format(time.RFC3339),
		OS:            osName,
		Architecture:  archName,
		Requirements: Requirements{
			OS:                  osName,
			Architecture:        archName,
			CPU:                 "ARMv8-A 64-bit (aarch64)",
			Libc:                "none (static binary, CGO_ENABLED=0)",
			RuntimeDependencies: []string{},
		},
		Bundle:   BundleInfo{File: bundleFileName(in.Version), SHA256: sum(tgz), Size: int64(len(tgz))},
		Contents: contents,
	}
	mf.Requirements.MinInstalledVersion = in.MinInstalledVersion
	mf.Requirements.HelperProtocol = in.RequiresHelperProtocol
	if in.Components != nil {
		mf.Components = &Components{KeyID: in.Components.KeyID, Protocol: in.Components.Protocol}
	}
	// The builder must not produce a manifest that `bundlegen check` rejects.
	if err := checkExtras(mf); err != nil {
		return nil, nil, err
	}
	return tgz, mf, nil
}

func marshalManifest(mf *Manifest) ([]byte, error) {
	b, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// cleanBase rejects anything that is not a plain file name.
func cleanBase(name string) error {
	if name == "" || name != path.Base(name) || name == "." || name == ".." || strings.ContainsAny(name, "\\\n") {
		return fmt.Errorf("invalid file name %q", name)
	}
	return nil
}
