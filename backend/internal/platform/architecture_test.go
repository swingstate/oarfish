package platform

import (
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/swingstate/oarfish/backend"

// backendDir is backend/, found from this package's directory (backend/internal/platform).
func backendDir(t testing.TB) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		t.Fatalf("backend/go.mod not found at %s", dir)
	}
	return dir
}

// importsOf returns the imports of the non-test Go files in one package
// directory (path relative to backend/), parsed without running the compiler.
func importsOf(t testing.TB, rel string) []string {
	t.Helper()
	dir := filepath.Join(backendDir(t), rel)
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	set := map[string]bool{}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, imp := range parsed.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			set[path] = true
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// closure returns every package (module-internal and standard library) that the
// package at rel imports, directly or indirectly.
func closure(t testing.TB, rel string) map[string]bool {
	t.Helper()
	seen := map[string]bool{}
	var walk func(rel string)
	walk = func(rel string) {
		for _, imp := range importsOf(t, rel) {
			if seen[imp] {
				continue
			}
			seen[imp] = true
			if sub, ok := strings.CutPrefix(imp, modulePath+"/"); ok {
				walk(sub)
			}
		}
	}
	walk(rel)
	return seen
}

const (
	pkgHardware = "internal/platform/hardware"
	pkgOS       = "internal/platform/operating_system"
	pkgCaps     = "internal/platform/capabilities"
	pkgProfiles = "internal/platform/profiles"
	pkgSource   = "internal/platform/source"
	pkgDetector = "internal/platform/detector"
	pkgServer   = "internal/server"
)

func mod(rel string) string { return modulePath + "/" + rel }

// ORB-HW-010, AT-027, ORB-HW-002: hardware and operating-system detection are
// separate interfaces. Neither imports the other, directly or through another
// package, and neither imports the HTTP server.
func TestArchitectureHardwareAndOperatingSystemAreSeparate(t *testing.T) {
	hw, osys := closure(t, pkgHardware), closure(t, pkgOS)

	for _, bad := range []string{mod(pkgOS), mod(pkgDetector), mod(pkgServer), mod(pkgProfiles)} {
		if hw[bad] {
			t.Errorf("hardware imports %s", bad)
		}
	}
	for _, bad := range []string{mod(pkgHardware), mod(pkgDetector), mod(pkgServer), mod(pkgProfiles), mod(pkgCaps)} {
		if osys[bad] {
			t.Errorf("operating_system imports %s", bad)
		}
	}
	// Positive control: the closure walk does see real imports.
	if !hw[mod(pkgSource)] || !osys[mod(pkgSource)] {
		t.Fatal("the import walk is broken: both packages must import source")
	}
	if !closure(t, pkgDetector)[mod(pkgHardware)] || !closure(t, pkgDetector)[mod(pkgOS)] {
		t.Fatal("the detector must be the package that combines both")
	}
}

// None of the detection packages may depend on the HTTP layer.
func TestArchitectureDetectionDoesNotImportTheServer(t *testing.T) {
	for _, rel := range []string{pkgHardware, pkgOS, pkgCaps, pkgProfiles, pkgSource, pkgDetector} {
		c := closure(t, rel)
		for _, bad := range []string{mod(pkgServer), "net/http", "net"} {
			if c[bad] {
				t.Errorf("%s imports %s", rel, bad)
			}
		}
	}
}

// ADR 0007, ORB-HW-006: detection reads files only. No detection package can run
// a program, and only the source package touches the host file system.
func TestArchitectureDetectionRunsNoProgramsAndOnlySourceTouchesTheHost(t *testing.T) {
	for _, rel := range []string{pkgHardware, pkgOS, pkgCaps, pkgProfiles, pkgSource, pkgDetector} {
		c := closure(t, rel)
		for _, bad := range []string{"os/exec", "syscall", "plugin", "unsafe"} {
			if c[bad] {
				t.Errorf("%s imports %s", rel, bad)
			}
		}
	}
	for _, rel := range []string{pkgHardware, pkgOS, pkgCaps, pkgProfiles, pkgDetector} {
		for _, imp := range importsOf(t, rel) {
			if imp == "os" || imp == "io/ioutil" {
				t.Errorf("%s imports %q directly; read files through the source package", rel, imp)
			}
		}
	}
}

// ORB-HW-005: detection code names no user and no home directory, and does not
// hardcode "pi", "/home" or "raspi-config"/"vcgencmd" (Pflichtenheft 3.5).
func TestArchitectureNoHardcodedUserHomeOrPiTools(t *testing.T) {
	forbidden := []string{`"/home`, `"home/`, `/home/pi`, `"pi"`, "raspi-config", "vcgencmd", "boot/config.txt", "/etc/machine-id", "etc/machine-id", "etc/hostname", "/environ", "sys/class/net"}
	root := filepath.Join(backendDir(t), "internal", "platform")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() && (rel == "testdata" || rel == "platformtest") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for lineNo, line := range strings.Split(string(data), "\n") {
			if t := strings.TrimSpace(line); strings.HasPrefix(t, "//") {
				continue // comments may explain what is not done
			}
			for _, f := range forbidden {
				if strings.Contains(line, f) {
					t.Errorf("%s:%d contains %q", rel, lineNo+1, f)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Go reads a file name ending in _arm64, _linux, _darwin and so on as a build
// constraint, so a file called generic_arm64.go is silently left out of builds
// on other platforms (this happened once: CI on linux/amd64 failed while the
// ARM64 Mac built fine). The detection code must build, and test, everywhere
// the developers and CI run, so no detection file may be constrained.
func TestArchitectureNoPlatformConstrainedFileNames(t *testing.T) {
	var contexts []build.Context
	for _, p := range [][2]string{{"linux", "arm64"}, {"linux", "amd64"}, {"darwin", "arm64"}, {"darwin", "amd64"}} {
		c := build.Default
		c.GOOS, c.GOARCH, c.CgoEnabled = p[0], p[1], false
		contexts = append(contexts, c)
	}
	root := filepath.Join(backendDir(t), "internal", "platform")
	checked := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testdata" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		checked++
		for _, c := range contexts {
			ok, err := c.MatchFile(filepath.Dir(path), d.Name())
			if err != nil {
				return err
			}
			if !ok {
				t.Errorf("%s is excluded from %s/%s builds (file name or build tag constraint)", d.Name(), c.GOOS, c.GOARCH)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 30 {
		t.Fatalf("only %d files checked", checked)
	}
	// Positive control: the check does notice a constrained name.
	c := build.Default
	c.GOOS, c.GOARCH = "linux", "amd64"
	if ok, _ := c.MatchFile(root, "generic_arm64.go"); ok {
		t.Fatal("MatchFile does not apply the _arm64 constraint; this test would be blind")
	}
}
