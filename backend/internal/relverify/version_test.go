package relverify

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// versionTable is the shared corpus of accepted and rejected version strings.
// installer is the verdict of valid_version in install/install.sh (checked by
// running that function), rel the verdict of ParseVersion. The only rows where
// they differ are the two bounds that ADR 0010 U2 adds to the version argument
// of the helper protocol: at most 64 characters and no "..".
var versionTable = []struct {
	s         string
	installer bool
	rel       bool
}{
	// accepted by both
	{"0.0.0", true, true},
	{"1.2.3", true, true},
	{"10.20.30", true, true},
	{"99999999999999999999.0.0", true, true},
	{"0.1.0-alpha.6", true, true},
	{"0.1.0-alpha.6.1", true, true},
	{"0.1.0-test.1", true, true},
	{"1.0.0-rc.1", true, true},
	{"1.0.0-0.3.7", true, true},
	{"1.0.0-x.7.z.92", true, true},
	{"1.0.0-alpha-a.b-c", true, true},
	{"1.0.0-0", true, true},
	{"1.0.0-A", true, true},
	{"1.0.0--", true, true},
	{"1.0.0-a-", true, true},
	// the pattern is wider than SemVer 2.0 here (leading zero, empty identifier)
	{"1.0.0-01", true, true},
	{"1.0.0-a.01", true, true},
	{"1.0.0-a.", true, true},
	{"1.0.0-.a", true, true},
	// exactly 64 characters
	{"1.0.0-" + strings.Repeat("a", 58), true, true},
	// accepted by the installer, refused here by the bounds of U2
	{"1.0.0-a..b", true, false},
	{"1.0.0-..", true, false},
	{"1.0.0-" + strings.Repeat("a", 59), true, false},
	{"1.0.0-" + strings.Repeat("a", 200), true, false},
	// refused by both
	{"", false, false},
	{"1", false, false},
	{"1.0", false, false},
	{strings.Join([]string{"1", "0", "0", "0"}, "."), false, false}, // four numbers
	{"01.0.0", false, false},
	{"1.00.0", false, false},
	{"1.0.00", false, false},
	{"-1.0.0", false, false},
	{"1.-1.0", false, false},
	{"1.0.0-", false, false},
	{"v1.0.0", false, false},
	{"V1.0.0", false, false},
	{"1.0.0+build", false, false},
	{"1.0.0-a+build", false, false},
	{"1.0.0+", false, false},
	{"+1.0.0", false, false},
	{" 1.0.0", false, false},
	{"1.0.0 ", false, false},
	{"1.0.0\n", false, false},
	{"1.0.0\n2.0.0", false, false},
	{"1.0.0-a\nb", false, false},
	{"1.0.0\x00", false, false},
	{"1.0.0-é", false, false},
	{"1.0.0-a_b", false, false},
	{"1.0.0-a:b", false, false},
	{"1.0.0-a/b", false, false},
	{"1.0.0-a b", false, false},
	{"1..0", false, false},
	{"1.0.", false, false},
	{".1.0.0", false, false},
	{"1.0.0.", false, false},
	{"../1.0.0", false, false},
	{"1.0.0/..", false, false},
	{"a.b.c", false, false},
	{"1.0.x", false, false},
	{"0x1.0.0", false, false},
	{"1.0.0e1", false, false},
	{"1,0.0", false, false},
}

func TestVersionSyntax(t *testing.T) {
	for _, tc := range versionTable {
		v, err := ParseVersion(tc.s)
		if (err == nil) != tc.rel {
			t.Errorf("ParseVersion(%q): err=%v, want accepted=%v", tc.s, err, tc.rel)
			continue
		}
		if err != nil {
			wantCode(t, err, CodeBadRequest)
			continue
		}
		if v.String() != tc.s {
			t.Errorf("String() = %q", v.String())
		}
		if v.Prerelease() != strings.Contains(tc.s, "-") {
			t.Errorf("Prerelease(%q) = %v", tc.s, v.Prerelease())
		}
	}
}

// The installer's valid_version, extracted from install/install.sh and run, gives
// the verdicts in the table. The test does not change install.sh.
func TestVersionSyntaxEqualsInstaller(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	raw, err := os.ReadFile("../../../install/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	var fn []string
	in := false
	for _, l := range lines {
		if l == "valid_version() {" {
			in = true
		}
		if in {
			fn = append(fn, l)
			if l == "}" {
				break
			}
		}
	}
	if len(fn) < 3 || fn[len(fn)-1] != "}" {
		t.Fatal("valid_version not found in install.sh")
	}
	script := strings.Join(fn, "\n") + "\n" + `for v in "$@"; do if valid_version "$v"; then echo 1; else echo 0; fi; done` + "\n"
	var args []string
	for _, tc := range versionTable {
		if strings.ContainsRune(tc.s, 0) {
			continue // not possible in an argument
		}
		args = append(args, tc.s)
	}
	out, err := exec.Command(sh, append([]string{"-c", script, "sh"}, args...)...).Output()
	if err != nil {
		t.Skipf("cannot run the installer's function: %v", err)
	}
	got := strings.Fields(string(out))
	if len(got) != len(args) {
		t.Fatalf("%d verdicts for %d strings", len(got), len(args))
	}
	i := 0
	for _, tc := range versionTable {
		if strings.ContainsRune(tc.s, 0) {
			continue
		}
		if (got[i] == "1") != tc.installer {
			t.Errorf("installer valid_version(%q) = %s, table says %v", tc.s, got[i], tc.installer)
		}
		i++
	}
	// The differences are exactly the bounds of ADR 0010 U2.
	for _, tc := range versionTable {
		if tc.installer != tc.rel && !(len(tc.s) > 64 || strings.Contains(tc.s, "..")) {
			t.Errorf("%q differs between installer and relverify for a reason that is not a U2 bound", tc.s)
		}
	}
}

// The version pattern of scripts/bundlegen (the producer) is the installer's.
func TestVersionPatternEqualsBundlegen(t *testing.T) {
	raw, err := os.ReadFile("../../../scripts/bundlegen/bundle.go")
	if err != nil {
		t.Fatal(err)
	}
	const pattern = "`^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$`"
	if !strings.Contains(string(raw), "versionPattern = regexp.MustCompile("+pattern+")") {
		t.Fatal("bundlegen's version pattern changed; revisit version.go")
	}
}

// SemVer 2.0 section 11, in increasing precedence.
func TestComparePrecedenceSpecExamples(t *testing.T) {
	chain := []string{
		"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0",
		"2.0.0", "2.1.0", "2.1.1",
	}
	for i := range chain {
		for j := range chain {
			a, b := mustVersion(t, chain[i]), mustVersion(t, chain[j])
			want := compareInts(i, j)
			if got := Compare(a, b); got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", chain[i], chain[j], got, want)
			}
		}
	}
}

func TestCompareCases(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0-alpha", "1.0.0-alpha", 0},
		{"0.1.0-alpha.6", "0.1.0-alpha.6.1", -1}, // the longer list is newer
		{"0.1.0-alpha.6.1", "0.1.0", -1},
		{"0.1.0-alpha.9", "0.1.0-alpha.10", -1}, // numeric, not text
		{"0.1.0-alpha.10", "0.1.0-alpha.9", 1},
		{"0.9.0", "0.10.0", -1},
		{"1.9.9", "1.10.0", -1},
		{"9.0.0", "10.0.0", -1},
		{"1.0.0-1", "1.0.0-a", -1}, // numeric below alphanumeric
		{"1.0.0-a", "1.0.0-1", 1},
		{"1.0.0-2", "1.0.0-10", -1},
		{"1.0.0-alpha", "1.0.0-Alpha", 1}, // ASCII: lower case after upper case
		{"1.0.0-a.1", "1.0.0-a.b", -1},
		{"1.0.0-a-b", "1.0.0-a", 1},
		{"99999999999999999999.0.0", "99999999999999999998.0.0", 1}, // no overflow
		{"99999999999999999999.0.0", "100000000000000000000.0.0", -1},
		{"0.0.1", "0.1.0", -1},
		{"1.0.0-rc.1", "1.0.0", -1},
		{"1.0.0", "1.0.0-rc.1", 1},
		// Beyond SemVer: still a total order, zero only for equal strings.
		{"1.0.0-01", "1.0.0-1", 1},
		{"1.0.0-01", "1.0.0-2", -1},
		{"1.0.0-a.", "1.0.0-a", 1},
		{"1.0.0-.a", "1.0.0-0", 1}, // an empty identifier is alphanumeric
	} {
		a, b := mustVersion(t, tc.a), mustVersion(t, tc.b)
		if got := Compare(a, b); got != tc.want {
			t.Errorf("Compare(%s, %s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
		if got := Compare(b, a); got != -tc.want {
			t.Errorf("Compare(%s, %s) = %d, want %d", tc.b, tc.a, got, -tc.want)
		}
	}
}

// Over every accepted string of the corpus: antisymmetry, zero only for equal
// strings, and transitivity on every triple.
func TestCompareIsATotalOrder(t *testing.T) {
	var vs []Version
	for _, tc := range versionTable {
		if v, err := ParseVersion(tc.s); err == nil {
			vs = append(vs, v)
		}
	}
	for _, a := range vs {
		for _, b := range vs {
			ab, ba := Compare(a, b), Compare(b, a)
			if ab != -ba {
				t.Fatalf("not antisymmetric: %s %s", a, b)
			}
			if (ab == 0) != (a.String() == b.String()) {
				t.Fatalf("zero iff equal strings violated: %s %s", a, b)
			}
			for _, c := range vs {
				if ab <= 0 && Compare(b, c) <= 0 && Compare(a, c) > 0 {
					t.Fatalf("not transitive: %s %s %s", a, b, c)
				}
			}
		}
	}
}

func TestCheckNewer(t *testing.T) {
	for _, tc := range []struct {
		installed, candidate string
		want                 Code
	}{
		{"0.1.0-alpha.6", "0.1.0-alpha.7", ""},
		{"0.1.0-alpha.6", "0.1.0", ""},
		{"0.1.0", "0.1.1", ""},
		{"0.1.0-alpha.6", "0.1.0-alpha.6", CodeAlreadyInstalled},
		{"0.1.0", "0.1.0", CodeAlreadyInstalled},
		{"0.1.0", "0.1.0-alpha.6", CodeDowngradeRefused},
		{"0.1.0-alpha.7", "0.1.0-alpha.6", CodeDowngradeRefused},
		{"0.1.0-alpha.10", "0.1.0-alpha.9", CodeDowngradeRefused},
		{"1.0.0", "0.99.99", CodeDowngradeRefused},
	} {
		err := CheckNewer(mustVersion(t, tc.installed), mustVersion(t, tc.candidate))
		if tc.want == "" {
			if err != nil {
				t.Errorf("%s -> %s: %v", tc.installed, tc.candidate, err)
			}
			continue
		}
		if codeOf(err) != tc.want {
			t.Errorf("%s -> %s: %v, want %s", tc.installed, tc.candidate, err, tc.want)
		}
	}
}

// Owner decision Q7.
func TestEligibleAndLatest(t *testing.T) {
	pre, stable := mustVersion(t, "0.1.0-alpha.6"), mustVersion(t, "0.1.0")
	table := []struct {
		candidate  string
		onPre, onS bool
	}{
		{"0.1.0-alpha.7", true, false},
		{"0.1.0-alpha.5", true, false},
		{"0.1.0", true, true},
		{"0.2.0-rc.1", true, false},
		{"0.2.0", true, true},
		{"0.0.9", true, true},
	}
	for _, tc := range table {
		c := mustVersion(t, tc.candidate)
		if got := Eligible(pre, c); got != tc.onPre {
			t.Errorf("Eligible(%s, %s) = %v", pre, c, got)
		}
		if got := Eligible(stable, c); got != tc.onS {
			t.Errorf("Eligible(%s, %s) = %v", stable, c, got)
		}
	}
	list := func(ss ...string) []Version {
		var out []Version
		for _, s := range ss {
			out = append(out, mustVersion(t, s))
		}
		return out
	}
	t.Run("prerelease device takes the highest, prerelease or not", func(t *testing.T) {
		got, ok := Latest(pre, list("0.1.0-alpha.7", "0.1.0", "0.2.0-rc.1", "0.1.0-alpha.5"))
		if !ok || got.String() != "0.2.0-rc.1" {
			t.Fatalf("%v %v", got, ok)
		}
	})
	t.Run("a stable release beats a lower prerelease", func(t *testing.T) {
		got, ok := Latest(pre, list("0.1.0-alpha.7", "0.1.0"))
		if !ok || got.String() != "0.1.0" {
			t.Fatalf("%v %v", got, ok)
		}
	})
	t.Run("stable device ignores prereleases", func(t *testing.T) {
		got, ok := Latest(stable, list("0.1.0-alpha.7", "0.2.0-rc.1", "0.1.1", "0.0.9", "0.1.0"))
		if !ok || got.String() != "0.1.1" {
			t.Fatalf("%v %v", got, ok)
		}
	})
	t.Run("nothing newer and eligible", func(t *testing.T) {
		if _, ok := Latest(stable, list("0.2.0-rc.1", "0.1.0", "0.0.1")); ok {
			t.Fatal("offered something")
		}
		if _, ok := Latest(pre, list("0.1.0-alpha.6", "0.1.0-alpha.5")); ok {
			t.Fatal("offered the installed or an older one")
		}
		if _, ok := Latest(pre, nil); ok {
			t.Fatal("empty list")
		}
	})
	t.Run("order of the list does not matter", func(t *testing.T) {
		a, _ := Latest(pre, list("0.3.0", "0.2.0", "0.4.0-rc.1", "0.1.0"))
		b, _ := Latest(pre, list("0.1.0", "0.4.0-rc.1", "0.2.0", "0.3.0"))
		if a.String() != "0.4.0-rc.1" || b.String() != a.String() {
			t.Fatal(a, b)
		}
	})
	t.Run("a list over the bound is refused as a whole", func(t *testing.T) {
		big := make([]Version, maxCandidates+1)
		for i := range big {
			big[i] = mustVersion(t, "9.9.9")
		}
		if _, ok := Latest(pre, big); ok {
			t.Fatal("accepted")
		}
		if _, ok := Latest(pre, big[:maxCandidates]); !ok {
			t.Fatal("a list at the bound is fine")
		}
	})
}

func manifestFor(t *testing.T, version string, comps bool, edit func(m map[string]any)) *Manifest {
	t.Helper()
	r := newRelease(t, version, comps)
	if edit != nil {
		edit(r.man)
	}
	b, _ := json.Marshal(r.man)
	m, err := ParseManifest(b)
	if err != nil {
		t.Fatalf("%v (%s)", err, detail(err))
	}
	return m
}

func TestCheckMinInstalled(t *testing.T) {
	m := manifestFor(t, "0.2.0", false, func(m map[string]any) { setPath(t, m, "requirements.min_installed_version", "0.1.0-alpha.6") })
	for _, tc := range []struct {
		installed string
		ok        bool
	}{
		{"0.1.0-alpha.5", false}, {"0.1.0-alpha.6", true}, {"0.1.0-alpha.7", true}, {"0.1.0", true}, {"0.0.1", false}, {"1.0.0", true},
	} {
		err := CheckMinInstalled(mustVersion(t, tc.installed), m)
		if tc.ok && err != nil {
			t.Errorf("%s: %v", tc.installed, err)
		}
		if !tc.ok {
			wantCode(t, err, CodeRequirementsNotMet)
		}
	}
	none := manifestFor(t, "0.2.0", false, nil)
	if err := CheckMinInstalled(mustVersion(t, "0.0.1"), none); err != nil {
		t.Fatal("no requirement, no refusal")
	}
}

func TestCheckHelperProtocol(t *testing.T) {
	need := func(n int) func(m map[string]any) {
		return func(m map[string]any) { setPath(t, m, "requirements.helper_protocol", n) }
	}
	comps := func(p int) func(m map[string]any) {
		return func(m map[string]any) { setPath(t, m, "components.protocol", p) }
	}
	both := func(n, p int) func(m map[string]any) {
		return func(m map[string]any) { need(n)(m); comps(p)(m) }
	}
	for _, tc := range []struct {
		name      string
		m         *Manifest
		installed int
		want      Code
	}{
		{"no requirement", manifestFor(t, "0.2.0", false, nil), 1, ""},
		{"requirement met by the installed helper", manifestFor(t, "0.2.0", false, need(1)), 1, ""},
		{"requirement not met, no components", manifestFor(t, "0.2.0", false, need(2)), 1, CodeHelperTooOld},
		{"requirement not met, installed helper newer", manifestFor(t, "0.2.0", false, need(2)), 3, ""},
		{"components deliver the protocol", manifestFor(t, "0.2.0", true, both(2, 2)), 1, ""},
		{"components deliver too little", manifestFor(t, "0.2.0", true, both(3, 2)), 5, CodeHelperTooOld},
		{"components newer than needed", manifestFor(t, "0.2.0", true, both(1, 4)), 1, ""},
		{"the installed helper does not count when components replace it", manifestFor(t, "0.2.0", true, both(2, 1)), 9, CodeHelperTooOld},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckHelperProtocol(tc.m, tc.installed)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			wantCode(t, err, tc.want)
		})
	}
}

func TestCheckUpgradeOrder(t *testing.T) {
	m := manifestFor(t, "0.1.0-alpha.6", false, func(m map[string]any) {
		setPath(t, m, "requirements.min_installed_version", "0.1.0-alpha.5")
		setPath(t, m, "requirements.helper_protocol", 2)
	})
	// Everything wrong at once: the version rule speaks first.
	wantCode(t, CheckUpgrade(mustVersion(t, "0.1.0-alpha.6"), m, 1), CodeAlreadyInstalled)
	wantCode(t, CheckUpgrade(mustVersion(t, "0.1.0-alpha.7"), m, 1), CodeDowngradeRefused)
	// Newer, but below the minimum: the minimum speaks before the protocol.
	m2 := manifestFor(t, "0.2.0", false, func(m map[string]any) {
		setPath(t, m, "requirements.min_installed_version", "0.1.0-alpha.5")
		setPath(t, m, "requirements.helper_protocol", 2)
	})
	wantCode(t, CheckUpgrade(mustVersion(t, "0.1.0-alpha.4"), m2, 1), CodeRequirementsNotMet)
	wantCode(t, CheckUpgrade(mustVersion(t, "0.1.0-alpha.6"), m2, 1), CodeHelperTooOld)
	if err := CheckUpgrade(mustVersion(t, "0.1.0-alpha.6"), m2, 2); err != nil {
		t.Fatal(err)
	}
}
