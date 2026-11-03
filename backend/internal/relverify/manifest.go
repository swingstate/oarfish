package relverify

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const (
	maxManifestFile = 64 << 10
	// maxContentBytes is the most the files of a bundle may add up to (ADR 0010 U5).
	maxContentBytes = 128 << 20
	maxJSONDepth    = 8

	productName = "oarfish"
	osName      = "linux"
	archName    = "arm64"

	maxRuntimeDependencies = 16
)

// BundleFileName returns the name of the bundle of a version,
// oarfish_<version>_linux_arm64.tar.gz.
func BundleFileName(v Version) string { return bundleRoot(v.s) + ".tar.gz" }

// bundleRoot is the name of the single top-level directory of the bundle.
func bundleRoot(version string) string {
	return productName + "_" + version + "_" + osName + "_" + archName
}

// Manifest is manifest.json. Its fields follow the manifest that
// scripts/bundlegen writes. A Manifest returned by ParseManifest has passed
// every check that needs no knowledge of the device or the request; Check adds
// those.
type Manifest struct {
	SchemaVersion int
	Name          string
	Version       string
	Tag           string
	Prerelease    bool
	Commit        string
	BuildDate     string
	OS            string
	Architecture  string
	Requirements  Requirements
	Bundle        BundleInfo
	Contents      []Entry
	// Components is non-nil exactly for the components shape: the bundle then
	// delivers the update helper and its three units.
	Components *Components
}

// Requirements is the manifest's requirements object. Libc and
// RuntimeDependencies are exposed for display and not enforced.
type Requirements struct {
	OS                  string
	Architecture        string
	CPU                 string
	Libc                string
	RuntimeDependencies []string
	// MinInstalledVersion is empty when the field is absent; a present field
	// always holds a valid version.
	MinInstalledVersion string
	// HelperProtocol is 0 when the field is absent; a present field is at least 1.
	HelperProtocol int
}

// BundleInfo is the manifest's bundle object.
type BundleInfo struct {
	File   string
	SHA256 string
	Size   int64
}

// Entry is one element of contents. Path is the path in the tarball,
// "<top-level directory>/<file>". Mode is 0o644 or 0o755.
type Entry struct {
	Path   string
	Mode   uint32
	Size   int64
	SHA256 string
}

// Components describes the helper that a components-shape bundle delivers.
type Components struct {
	// KeyID is the 16-digit id of the key embedded in the delivered helper.
	KeyID string
	// Protocol is the highest protocol the delivered helper implements.
	Protocol int
}

// Semver parses the manifest's version. A manifest from ParseManifest always
// has a valid one.
func (m *Manifest) Semver() (Version, error) {
	if m == nil {
		return Version{}, fail(CodeInternal, "no manifest")
	}
	return ParseVersion(m.Version)
}

type rawManifest struct {
	SchemaVersion *int             `json:"schema_version"`
	Name          *string          `json:"name"`
	Version       *string          `json:"version"`
	Tag           *string          `json:"tag"`
	Prerelease    *bool            `json:"prerelease"`
	Commit        *string          `json:"commit"`
	BuildDate     *string          `json:"build_date"`
	OS            *string          `json:"os"`
	Architecture  *string          `json:"architecture"`
	Requirements  *rawRequirements `json:"requirements"`
	Bundle        *rawBundle       `json:"bundle"`
	Contents      []rawEntry       `json:"contents"`
	Components    *rawComponents   `json:"components"`
}

type rawRequirements struct {
	OS                  *string  `json:"os"`
	Architecture        *string  `json:"architecture"`
	CPU                 *string  `json:"cpu"`
	Libc                *string  `json:"libc"`
	RuntimeDependencies []string `json:"runtime_dependencies"`
	MinInstalledVersion *string  `json:"min_installed_version"`
	HelperProtocol      *int     `json:"helper_protocol"`
}

type rawBundle struct {
	File   *string `json:"file"`
	SHA256 *string `json:"sha256"`
	Size   *int64  `json:"size"`
}

type rawEntry struct {
	Path   *string `json:"path"`
	Mode   *string `json:"mode"`
	Size   *int64  `json:"size"`
	SHA256 *string `json:"sha256"`
}

type rawComponents struct {
	KeyID    *string `json:"key_id"`
	Protocol *int    `json:"protocol"`
}

// ParseManifest decodes and validates manifest.json (at most 64 KiB).
//
// Unknown fields are ignored: this is the consumer on the device, and additive
// changes of the manifest must stay compatible (ADR 0010 U4). This differs
// deliberately from `bundlegen check`, the producer side, which rejects unknown
// fields. "Unknown or missing known fields fail" in U4 is read as: a known
// field that is missing, has the wrong type or holds an unknown value (for
// example schema_version 2) fails. Because encoding/json matches field names
// without regard to case and takes the last of two equal keys, ParseManifest
// additionally rejects a key that differs from a known field only in letter
// case and a key that appears twice in one object.
//
// It checks: schema_version 1; name "oarfish"; a valid version, the tag
// "v<version>", prerelease equal to "the version has a hyphen"; commit of 40
// lowercase hexadecimal digits; os "linux" and architecture "arm64" at the top
// and in requirements (CodeRequirementsNotMet when a value differs, which is
// how another operating system or architecture is refused); the optional
// requirements.min_installed_version (a valid version), requirements.helper_protocol
// (at least 1) and the components object (a 16-digit lowercase key_id, protocol
// at least 1); bundle.file equal to the name derived from the version, a valid
// bundle.sha256 and a positive bundle.size; and contents: exactly the files of
// the legacy shape (LICENSE, THIRD_PARTY_NOTICES.txt, oarfish, oarfish.service)
// or, if and only if the components object is present, of the components shape
// (plus the helper and its three units), each once, below the top-level
// directory, with the mode 0644 (0755 for the two programs), a size and a
// SHA-256, adding up to at most 128 MiB.
//
// Every other failure is a *Error with CodeManifestInvalid.
func ParseManifest(data []byte) (*Manifest, error) {
	if len(data) == 0 || len(data) > maxManifestFile {
		return nil, fail(CodeManifestInvalid, "manifest.json is empty or larger than the limit")
	}
	var raw rawManifest
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fail(CodeManifestInvalid, "manifest.json is not valid JSON of the expected shape")
	}
	if err := lintKeys(data); err != nil {
		return nil, err
	}
	return buildManifest(&raw)
}

func buildManifest(raw *rawManifest) (*Manifest, error) {
	missing := func() (*Manifest, error) {
		return nil, fail(CodeManifestInvalid, "a required manifest field is missing")
	}
	if raw.SchemaVersion == nil || raw.Name == nil || raw.Version == nil || raw.Tag == nil || raw.Prerelease == nil ||
		raw.Commit == nil || raw.BuildDate == nil || raw.OS == nil || raw.Architecture == nil ||
		raw.Requirements == nil || raw.Bundle == nil || raw.Contents == nil {
		return missing()
	}
	r, b := raw.Requirements, raw.Bundle
	if r.OS == nil || r.Architecture == nil || r.CPU == nil || r.Libc == nil || r.RuntimeDependencies == nil ||
		b.File == nil || b.SHA256 == nil || b.Size == nil {
		return missing()
	}
	if *raw.SchemaVersion != 1 {
		return nil, fail(CodeManifestInvalid, "unsupported schema_version")
	}
	if *raw.Name != productName {
		return nil, fail(CodeManifestInvalid, "the manifest is not for oarfish")
	}
	ver, err := ParseVersion(*raw.Version)
	if err != nil {
		return nil, fail(CodeManifestInvalid, "the manifest version is not valid")
	}
	if *raw.Tag != "v"+*raw.Version {
		return nil, fail(CodeManifestInvalid, "the tag differs from v<version>")
	}
	if *raw.Prerelease != ver.Prerelease() {
		return nil, fail(CodeManifestInvalid, "prerelease disagrees with the version")
	}
	if len(*raw.Commit) != 40 || !isLowerHex(*raw.Commit) {
		return nil, fail(CodeManifestInvalid, "the commit is not 40 lowercase hexadecimal digits")
	}
	if !printable(*raw.BuildDate, 64) || !printable(*r.CPU, 256) || !printable(*r.Libc, 256) {
		return nil, fail(CodeManifestInvalid, "a descriptive manifest field is empty, too long or not printable")
	}
	if *raw.OS != osName || *raw.Architecture != archName || *r.OS != osName || *r.Architecture != archName {
		return nil, fail(CodeRequirementsNotMet, "the release is not for linux/arm64")
	}
	if len(r.RuntimeDependencies) > maxRuntimeDependencies {
		return nil, fail(CodeManifestInvalid, "too many runtime dependencies")
	}
	for _, d := range r.RuntimeDependencies {
		if !printable(d, 128) {
			return nil, fail(CodeManifestInvalid, "a runtime dependency is empty, too long or not printable")
		}
	}
	m := &Manifest{
		SchemaVersion: *raw.SchemaVersion, Name: *raw.Name, Version: *raw.Version, Tag: *raw.Tag,
		Prerelease: *raw.Prerelease, Commit: *raw.Commit, BuildDate: *raw.BuildDate,
		OS: *raw.OS, Architecture: *raw.Architecture,
		Requirements: Requirements{
			OS: *r.OS, Architecture: *r.Architecture, CPU: *r.CPU, Libc: *r.Libc,
			RuntimeDependencies: append([]string{}, r.RuntimeDependencies...),
		},
		Bundle: BundleInfo{File: *b.File, SHA256: *b.SHA256, Size: *b.Size},
	}
	if r.MinInstalledVersion != nil {
		if _, err := ParseVersion(*r.MinInstalledVersion); err != nil {
			return nil, fail(CodeManifestInvalid, "requirements.min_installed_version is not a valid version")
		}
		m.Requirements.MinInstalledVersion = *r.MinInstalledVersion
	}
	if r.HelperProtocol != nil {
		if *r.HelperProtocol < 1 {
			return nil, fail(CodeManifestInvalid, "requirements.helper_protocol is not a positive integer")
		}
		m.Requirements.HelperProtocol = *r.HelperProtocol
	}
	if c := raw.Components; c != nil {
		if c.KeyID == nil || c.Protocol == nil {
			return missing()
		}
		if len(*c.KeyID) != 16 || !isLowerHex(*c.KeyID) {
			return nil, fail(CodeManifestInvalid, "components.key_id is not 16 lowercase hexadecimal digits")
		}
		if *c.Protocol < 1 {
			return nil, fail(CodeManifestInvalid, "components.protocol is not a positive integer")
		}
		m.Components = &Components{KeyID: *c.KeyID, Protocol: *c.Protocol}
	}
	if m.Bundle.File != BundleFileName(ver) {
		return nil, fail(CodeManifestInvalid, "bundle.file does not match the version")
	}
	if len(m.Bundle.SHA256) != 64 || !isLowerHex(m.Bundle.SHA256) {
		return nil, fail(CodeManifestInvalid, "bundle.sha256 is not 64 lowercase hexadecimal digits")
	}
	if m.Bundle.Size < 1 {
		return nil, fail(CodeManifestInvalid, "bundle.size is not positive")
	}
	if err := buildContents(m, raw.Contents); err != nil {
		return nil, err
	}
	return m, nil
}

func buildContents(m *Manifest, raw []rawEntry) error {
	root := bundleRoot(m.Version) + "/"
	want := layout(m.Components != nil)
	if len(raw) != len(want) {
		return fail(CodeManifestInvalid, "contents does not list the files of the declared bundle shape")
	}
	seen := make(map[string]bool, len(raw))
	var total int64
	for _, e := range raw {
		if e.Path == nil || e.Mode == nil || e.Size == nil || e.SHA256 == nil {
			return fail(CodeManifestInvalid, "a contents entry is missing a field")
		}
		if !strings.HasPrefix(*e.Path, root) {
			return fail(CodeManifestInvalid, "a contents path is outside the top-level directory")
		}
		name := (*e.Path)[len(root):]
		lf, ok := lookupLayout(name, m.Components != nil)
		if !ok {
			return fail(CodeManifestInvalid, "contents lists a file that the declared bundle shape does not have")
		}
		if seen[name] {
			return fail(CodeManifestInvalid, "contents lists a path twice")
		}
		seen[name] = true
		if *e.Mode != "0644" && *e.Mode != "0755" {
			return fail(CodeManifestInvalid, "a contents mode is neither 0644 nor 0755")
		}
		mode := uint32(0o644)
		if *e.Mode == "0755" {
			mode = 0o755
		}
		if mode != lf.mode {
			return fail(CodeManifestInvalid, "a contents mode is not the mode of that file")
		}
		if *e.Size < 1 || *e.Size > maxContentBytes {
			return fail(CodeManifestInvalid, "a contents size is not between 1 byte and 128 MiB")
		}
		total += *e.Size
		if total > maxContentBytes {
			return fail(CodeManifestInvalid, "contents add up to more than 128 MiB")
		}
		if len(*e.SHA256) != 64 || !isLowerHex(*e.SHA256) {
			return fail(CodeManifestInvalid, "a contents SHA-256 is not 64 lowercase hexadecimal digits")
		}
		m.Contents = append(m.Contents, Entry{Path: *e.Path, Mode: mode, Size: *e.Size, SHA256: *e.SHA256})
	}
	return nil
}

// printable reports whether s is 1 to max characters of printable ASCII.
func printable(s string, max int) bool {
	if s == "" || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// Expect is what a verifier knows before it looks at the manifest.
type Expect struct {
	// Version is the requested version.
	Version string
	// Architecture is the architecture the verifier runs for ("arm64"). It is a
	// parameter so that tests on another host work.
	Architecture string
	// BundleFile is the expected bundle file name (BundleFileName).
	BundleFile string
	// BundleSHA256 is the hash of the bundle listed in the signed checksums.txt.
	BundleSHA256 string
}

// Check compares the manifest with what the verifier expects: the version equals
// the requested one, the architecture equals Expect.Architecture, bundle.file
// equals the expected name and bundle.sha256 equals the listed hash. The tag, the
// version in the bundle file name and the other self-contained rules were checked
// by ParseManifest. The size of the bundle is checked by Release.CheckBundle,
// when the bundle is there.
func (m *Manifest) Check(e Expect) error {
	if m == nil {
		return fail(CodeInternal, "no manifest")
	}
	if m.Version != e.Version {
		return fail(CodeManifestInvalid, "the manifest version differs from the requested version")
	}
	if m.Architecture != e.Architecture || m.Requirements.Architecture != e.Architecture {
		return fail(CodeRequirementsNotMet, "the manifest architecture differs from the architecture of this device")
	}
	if m.Bundle.File != e.BundleFile {
		return fail(CodeManifestInvalid, "bundle.file differs from the expected bundle name")
	}
	if m.Bundle.SHA256 != e.BundleSHA256 {
		return fail(CodeChecksumMismatch, "bundle.sha256 differs from the hash in checksums.txt")
	}
	return nil
}

// knownFields lists the fields of each manifest object, for lintKeys.
func knownFields(kind string) map[string]bool {
	var names []string
	switch kind {
	case "top":
		names = []string{"schema_version", "name", "version", "tag", "prerelease", "commit", "build_date", "os",
			"architecture", "requirements", "bundle", "contents", "components"}
	case "requirements":
		names = []string{"os", "architecture", "cpu", "libc", "runtime_dependencies", "min_installed_version", "helper_protocol"}
	case "bundle":
		names = []string{"file", "sha256", "size"}
	case "entry":
		names = []string{"path", "mode", "size", "sha256"}
	case "components":
		names = []string{"key_id", "protocol"}
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// childKind returns the kind of the value that follows in parent: for an object
// the name of its field set, for an array the kind of its elements.
func childKind(parent *lintFrame) string {
	switch {
	case parent == nil:
		return "top"
	case !parent.object:
		return parent.kind
	case parent.kind == "top":
		switch parent.key {
		case "requirements", "bundle", "components":
			return parent.key
		case "contents":
			return "entry"
		}
	}
	return ""
}

type lintFrame struct {
	object  bool
	kind    string // for an object: its kind; for an array: the kind of its elements
	seen    map[string]bool
	wantKey bool
	key     string
}

// lintKeys walks valid JSON and rejects, in every object, a key that appears
// twice (also when only the letter case differs) and a key that differs from a
// known field of that object only in letter case. Other unknown keys pass. The
// walk is bounded by the input (each token takes at least one byte) and by
// maxJSONDepth.
func lintKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	var stack []*lintFrame
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fail(CodeManifestInvalid, "manifest.json is not valid JSON")
		}
		var top *lintFrame
		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}
		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{', '[':
				if len(stack) >= maxJSONDepth {
					return fail(CodeManifestInvalid, "manifest.json is nested too deeply")
				}
				f := &lintFrame{object: t == '{', wantKey: true, seen: map[string]bool{}, kind: childKind(top)}
				stack = append(stack, f)
			default: // '}' or ']'
				stack = stack[:len(stack)-1]
				if n := len(stack); n > 0 && stack[n-1].object {
					stack[n-1].wantKey = true
				}
			}
		default:
			if top == nil || !top.object {
				continue
			}
			if !top.wantKey {
				top.wantKey = true
				continue
			}
			key, _ := t.(string)
			folded := strings.ToLower(key)
			if top.seen[folded] {
				return fail(CodeManifestInvalid, "a key appears twice in an object of manifest.json")
			}
			top.seen[folded] = true
			if fields := knownFields(top.kind); len(fields) > 0 && !fields[key] {
				for f := range fields {
					if strings.EqualFold(key, f) {
						return fail(CodeManifestInvalid, "a key differs from a known field only in letter case")
					}
				}
			}
			top.key = key
			top.wantKey = false
		}
	}
}
