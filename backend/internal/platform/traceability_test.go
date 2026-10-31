package platform

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// inScope are the MUST requirements of Gate G4 (Pflichtenheft 12.4: every MUST
// requirement is mapped to at least one test).
var inScope = []string{
	"ORB-HW-001", "ORB-HW-002", "ORB-HW-003", "ORB-HW-004", "ORB-HW-005",
	"ORB-HW-006", "ORB-HW-007", "ORB-HW-008", "ORB-HW-009", "ORB-HW-010",
	"ORB-GEN-004", "ORB-NFR-015",
}

func repoFile(t testing.TB, rel ...string) string {
	t.Helper()
	return filepath.Join(append([]string{backendDir(t), ".."}, rel...)...)
}

// goTestFunctions returns the names of all Test and Fuzz functions in the backend.
func goTestFunctions(t testing.TB) map[string]bool {
	t.Helper()
	re := regexp.MustCompile(`(?m)^func ((?:Test|Fuzz)\w+)\(`)
	names := map[string]bool{}
	err := filepath.WalkDir(backendDir(t), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range re.FindAllStringSubmatch(string(data), -1) {
			names[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// frontendTestHas reports whether a frontend test file contains a test whose
// title starts with the given text.
func frontendTestHas(t testing.TB, file, titlePrefix string) bool {
	t.Helper()
	var found bool
	root := repoFile(t, "frontend", "src")
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(path) != file {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, q := range []string{"'", "\""} {
			if strings.Contains(string(data), "it("+q+titlePrefix) {
				found = true
			}
		}
		return nil
	})
	return found
}

// checkTraceability verifies the "Requirement to test" table of a document:
// every in-scope requirement is listed with at least one test, and every test
// the table names exists (Pflichtenheft 12.4).
func checkTraceability(t *testing.T, doc string, scope []string) {
	t.Helper()
	data, err := os.ReadFile(repoFile(t, "docs", doc))
	if err != nil {
		t.Fatal(err)
	}
	_, table, found := strings.Cut(string(data), "## Requirement to test")
	if !found {
		t.Fatalf("docs/%s has no \"Requirement to test\" section", doc)
	}
	table, _, _ = strings.Cut(table, "\n## ")

	goTests := goTestFunctions(t)
	token := regexp.MustCompile("`([^`]+)`")
	mapped := map[string]int{} // requirement -> number of resolved tests

	for _, line := range strings.Split(table, "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| Requirement") || strings.HasPrefix(line, "|---") {
			continue
		}
		cells := strings.SplitN(strings.Trim(line, "| "), " | ", 2)
		if len(cells) != 2 {
			t.Errorf("malformed table row: %s", line)
			continue
		}
		id := strings.Fields(cells[0])[0]
		resolved := 0
		for _, m := range token.FindAllStringSubmatch(cells[1], -1) {
			name := m[1]
			switch {
			case regexp.MustCompile(`^(Test|Fuzz)\w+$`).MatchString(name):
				if !goTests[name] {
					t.Errorf("%s: Go test %s does not exist", id, name)
					continue
				}
			case strings.Contains(name, ".test.ts "):
				file, title, _ := strings.Cut(name, " ")
				title = strings.Trim(title, `"`)
				if !frontendTestHas(t, file, title) {
					t.Errorf("%s: frontend test %s %q does not exist", id, file, title)
					continue
				}
			default:
				t.Errorf("%s: cannot resolve test reference %q", id, name)
				continue
			}
			resolved++
		}
		mapped[id] = resolved
	}

	for _, id := range scope {
		if mapped[id] == 0 {
			t.Errorf("requirement %s has no test in the table of docs/%s", id, doc)
		}
	}
}

// Pflichtenheft 12.4: the requirement-to-test table in docs/DETECTION.md lists
// every in-scope requirement, and every test it names exists.
func TestTraceabilityTableIsComplete(t *testing.T) {
	checkTraceability(t, "DETECTION.md", inScope)
}

// dashboardInScope are the MUST requirements of Gate G5 and decision D-C29.
var dashboardInScope = []string{
	"ORB-DASH-001", "ORB-DASH-002", "ORB-DASH-005", "ORB-DASH-006",
	"ORB-NFR-009", "ORB-NFR-014", "ORB-NFR-010", "ORB-NFR-015", "ORB-GEN-003", "ORB-GEN-006", "ORB-ONB-003", "D-C29",
}

// Pflichtenheft 12.4 for Gate G5: the table in docs/DASHBOARD.md.
func TestDashboardTraceabilityTableIsComplete(t *testing.T) {
	checkTraceability(t, "DASHBOARD.md", dashboardInScope)
}

// Documentation carries no identifying values either (privacy rule).
func TestDetectionDocsContainNoPrivateValues(t *testing.T) {
	allowedIP := regexp.MustCompile(`\b127\.0\.0\.1\b`)
	for _, rel := range [][]string{{"docs", "DETECTION.md"}, {"docs", "adr", "0007-detection-and-profiles.md"}, {"backend", "internal", "platform", "testdata", "README.md"}} {
		data, err := os.ReadFile(repoFile(t, rel...))
		if err != nil {
			t.Fatal(err)
		}
		text := allowedIP.ReplaceAllString(string(data), "")
		for name, re := range privatePatterns {
			switch name {
			case "serial number", "host name", "home directory", "user name entry", "environment", "SSH data":
				continue // these words appear in the rules that forbid them
			}
			if m := re.FindString(text); m != "" {
				t.Errorf("%s: %s: %q", filepath.Join(rel...), name, m)
			}
		}
	}
}

// Documentation of Gate G5 carries no identifying values: IP addresses only from
// the documentation ranges or the loopback listen address, no MAC address, no
// serial number, no machine id, no real host name.
func TestDashboardDocsContainNoPrivateValues(t *testing.T) {
	for _, rel := range [][]string{{"docs", "DASHBOARD.md"}, {"docs", "adr", "0008-dashboard-metrics.md"}, {"backend", "internal", "metrics", "testdata", "README.md"}, {"README.md"}} {
		data, err := os.ReadFile(repoFile(t, rel...))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.NewReplacer("127.0.0.1", "", "127.0.0.0/8", "").Replace(string(data)) // the loopback listen address and range
		for _, m := range scanPrivate(text) {
			name, _, _ := strings.Cut(m, ":")
			switch name {
			case "serial number", "host name", "home directory", "user name entry", "environment", "SSH data":
				continue // these words appear in the rules that forbid them
			}
			t.Errorf("%s: %s", filepath.Join(rel...), m)
		}
	}
}

// authInScope are the MUST requirements of Gate G5.1 (spec 14.2, D-C27) and
// the acceptance test AT-023.
var authInScope = []string{
	"ORB-ONB-002", "ORB-AUTH-001", "ORB-AUTH-002", "ORB-AUTH-003",
	"ORB-SEC-004", "ORB-SEC-005", "ORB-SEC-006", "ORB-SEC-007", "AT-023", "ORB-NFR-009",
}

// Pflichtenheft 12.4 for Gate G5.1: the table in docs/AUTH.md.
func TestAuthTraceabilityTableIsComplete(t *testing.T) {
	checkTraceability(t, "AUTH.md", authInScope)
}

// Documentation of Gate G5.1 carries no identifying values and no token that
// looks real: no 32-hex-digit value (the setup token's form), no IP address
// outside the documentation ranges besides loopback.
func TestAuthDocsContainNoPrivateValues(t *testing.T) {
	for _, rel := range [][]string{{"docs", "AUTH.md"}, {"docs", "adr", "0009-local-access-control-and-audit.md"}} {
		data, err := os.ReadFile(repoFile(t, rel...))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.NewReplacer("127.0.0.1", "", "127.0.0.0/8", "").Replace(string(data))
		for _, m := range scanPrivate(text) {
			name, _, _ := strings.Cut(m, ":")
			switch name {
			case "serial number", "host name", "home directory", "user name entry", "environment", "SSH data":
				continue // these words appear in the rules that forbid them
			}
			t.Errorf("%s: %s", filepath.Join(rel...), m)
		}
	}
}
