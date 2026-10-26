package platform

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/swingstate/oarfish/backend/internal/platform/capabilities"
	"github.com/swingstate/oarfish/backend/internal/platform/detector"
	"github.com/swingstate/oarfish/backend/internal/platform/operating_system"
	"github.com/swingstate/oarfish/backend/internal/platform/profiles"
)

// ORB-GEN-006, ORB-HW-009: every value the API can produce has a text in the
// German and the English translation file, so the GUI never shows a raw key.
func TestEveryAPIValueHasATranslation(t *testing.T) {
	reg, err := profiles.Load()
	if err != nil {
		t.Fatal(err)
	}

	var want []string
	add := func(prefix string, values ...string) {
		for _, v := range values {
			want = append(want, prefix+v)
		}
	}
	want = append(want, detector.ExplanationKeys()...)
	add("limitation.", detector.LimitationKeys()...)
	for _, p := range reg.All() {
		add("limitation.", p.KnownLimitations...)
	}
	add("tier.", detector.TierVerified, detector.TierCompatible, detector.TierCommunity, detector.TierExperimental, detector.TierGeneric, detector.TierUnsupported)
	add("capability.", capabilities.All...)
	add("capabilityReason.", capabilities.Reasons...)
	add("bootMedium.", capabilities.Media...)
	add("adapter.", operating_system.AdapterNames()...)
	add("adapterStatus.", operating_system.Statuses...)
	add("matchedBy.", profiles.MatchedByCompatible, profiles.MatchedByModel, profiles.MatchedByFallback)

	key := regexp.MustCompile(`(?m)^\s*'([^']+)':`)
	for _, lang := range []string{"de", "en"} {
		path := filepath.Join(backendDir(t), "..", "frontend", "src", "lib", "locales", lang+".ts")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("translation file: %v", err)
		}
		have := map[string]bool{}
		for _, m := range key.FindAllStringSubmatch(string(data), -1) {
			have[m[1]] = true
		}
		if len(have) < 40 {
			t.Fatalf("%s.ts: found only %d dotted keys; is the file format what this test expects?", lang, len(have))
		}
		for _, k := range want {
			if !have[k] {
				t.Errorf("%s.ts has no text for %q", lang, k)
			}
		}
	}
}
