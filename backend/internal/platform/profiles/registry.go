package profiles

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"

	"github.com/swingstate/oarfish/backend/internal/platform/source"
)

//go:embed data
var embedded embed.FS

const maxProfileFile = 64 << 10

// How a profile was matched to the hardware.
const (
	MatchedByCompatible = "compatible"
	MatchedByModel      = "model"
	MatchedByFallback   = "generic_fallback"
)

// Registry holds the validated profiles.
type Registry struct {
	socs    []*Profile
	boards  []*Profile
	generic *Profile
}

// Match is the result of looking up the hardware in the registry.
type Match struct {
	// Profile is the board profile if one matched, else the generic profile.
	Profile *Profile
	// SoC is the SoC family profile that matched by compatible string, if any.
	SoC *Profile
	// MatchedBy is MatchedByCompatible, MatchedByModel or MatchedByFallback.
	MatchedBy string
}

// IsBoard reports whether a board profile matched.
func (m Match) IsBoard() bool { return m.Profile != nil && m.Profile.Kind == KindBoard }

// Load returns the registry of the profiles embedded in the binary.
func Load() (*Registry, error) {
	sub, err := fs.Sub(embedded, "data")
	if err != nil {
		return nil, err
	}
	return LoadFS(sub)
}

// LoadFS builds a registry from soc/, boards/ and generic/ directories of
// JSON files in fsys. Every file must be a valid profile of the matching kind;
// the first problem fails the load, because a damaged profile set must not
// silently turn into a different support statement.
func LoadFS(fsys fs.FS) (*Registry, error) {
	r := &Registry{}
	for _, d := range []struct{ dir, kind string }{
		{"soc", KindSoCFamily}, {"boards", KindBoard}, {"generic", KindGeneric},
	} {
		entries, err := fs.ReadDir(fsys, d.dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			name := d.dir + "/" + e.Name()
			data, err := source.ReadFile(fsys, name, maxProfileFile)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			p, err := Parse(data)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			if p.Kind != d.kind {
				return nil, fmt.Errorf("%s: kind %q does not belong in %s/", name, p.Kind, d.dir)
			}
			switch p.Kind {
			case KindSoCFamily:
				r.socs = append(r.socs, p)
			case KindBoard:
				r.boards = append(r.boards, p)
			case KindGeneric:
				if r.generic != nil {
					return nil, fmt.Errorf("%s: more than one generic profile", name)
				}
				r.generic = p
			}
		}
	}
	if err := r.check(); err != nil {
		return nil, err
	}
	return r, nil
}

// Parse decodes one profile strictly (unknown fields and trailing data are
// errors) and validates it.
func Parse(data []byte) (*Profile, error) {
	if len(data) > maxProfileFile {
		return nil, errors.New("profile file is too large")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var p Profile
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("decode profile: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("decode profile: unexpected data after the profile")
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// check verifies the relations between profiles.
func (r *Registry) check() error {
	if r.generic == nil {
		return errors.New("the generic ARM64 profile is missing")
	}
	seen := map[string]bool{r.generic.ProfileID: true}
	for _, p := range slices.Concat(r.socs, r.boards) {
		if seen[p.ProfileID] {
			return fmt.Errorf("duplicate profile_id %q", p.ProfileID)
		}
		seen[p.ProfileID] = true
	}
	for _, b := range r.boards {
		if r.soc(b.SoCFamily) == nil {
			return fmt.Errorf("board %q refers to unknown soc_family %q", b.ProfileID, b.SoCFamily)
		}
	}
	return nil
}

// Generic returns the built-in generic ARM64 profile.
func (r *Registry) Generic() *Profile { return r.generic }

// All returns every profile: SoC families, boards, then the generic profile.
func (r *Registry) All() []*Profile {
	return slices.Concat(r.socs, r.boards, []*Profile{r.generic})
}

func (r *Registry) soc(family string) *Profile {
	for _, s := range r.socs {
		if s.SoCFamily == family {
			return s
		}
	}
	return nil
}

// Match finds the profile for the hardware. compatible is the device-tree list,
// most specific first; model is the device-tree or DMI model text. The compatible
// strings are tried first; the model is secondary. Without a board match the
// generic profile is returned.
func (r *Registry) Match(compatible []string, model string) Match {
	m := Match{Profile: r.generic, MatchedBy: MatchedByFallback}

	for _, c := range compatible {
		if b := r.boardByCompatible(c); b != nil {
			m.Profile, m.MatchedBy = b, MatchedByCompatible
			break
		}
	}
	if !m.IsBoard() {
		for _, b := range r.boards {
			if modelMatches(model, b.Model) {
				m.Profile, m.MatchedBy = b, MatchedByModel
				break
			}
		}
	}
	if m.IsBoard() {
		m.SoC = r.soc(m.Profile.SoCFamily)
		return m
	}
	for _, c := range compatible {
		for _, s := range r.socs {
			if patternsMatch(s.CompatiblePatterns, c) {
				m.SoC = s
				return m
			}
		}
	}
	return m
}

func (r *Registry) boardByCompatible(c string) *Profile {
	for _, b := range r.boards {
		if patternsMatch(b.CompatiblePatterns, c) {
			return b
		}
	}
	return nil
}

func patternsMatch(patterns []string, c string) bool {
	for _, p := range patterns {
		if prefix, wild := strings.CutSuffix(p, "*"); wild {
			if strings.HasPrefix(c, prefix) {
				return true
			}
		} else if p == c {
			return true
		}
	}
	return false
}

// modelMatches reports whether the reported model is the profile model, with
// an optional revision suffix as the device tree reports it ("... Rev 1.0").
func modelMatches(reported, model string) bool {
	rest, ok := strings.CutPrefix(reported, model)
	if !ok || model == "" {
		return false
	}
	if rest == "" {
		return true
	}
	rev, ok := strings.CutPrefix(rest, " Rev ")
	if !ok || rev == "" || len(rev) > 8 {
		return false
	}
	return strings.Trim(rev, "0123456789.") == ""
}

// KernelAtLeast reports whether the kernel release (for example
// "6.18.50+rpt-rpi-v8") is at least min ("major.minor"). An empty min is
// always satisfied; a kernel release that cannot be parsed is not.
func KernelAtLeast(release, min string) bool {
	if min == "" {
		return true
	}
	wantMajor, wantMinor, ok := parseKernelMinor(min)
	if !ok {
		return false
	}
	major, minor, ok := parseKernelMinor(release)
	if !ok {
		return false
	}
	return major > wantMajor || (major == wantMajor && minor >= wantMinor)
}
