package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Each of the five names differing alone, several together, none differing.
func TestComponentComparison(t *testing.T) {
	changed := func(name string) []byte {
		if strings.HasSuffix(name, "helper") {
			return elfStub("changed " + name)
		}
		return []byte("[Unit]\n# changed " + name + "\n")
	}
	for i, name := range componentNames {
		t.Run("only "+name, func(t *testing.T) {
			v := newTenv(t)
			r := v.release("1.1.0")
			r.setFile(name, changed(name))
			v.place("1.1.0", r.build())
			before := snapshot(t, v.root)
			got := wantErrCode(t, v.stage("1.1.0"), codeInstallerRequired)
			if len(got.Differs) != 1 || got.Differs[0] != componentNames[i] {
				t.Fatalf("differs %v, want [%s]", got.Differs, name)
			}
			wantNothingLeft(t, v, before) // nothing has been moved into releases/
		})
	}
	t.Run("several together, in the fixed order", func(t *testing.T) {
		v := newTenv(t)
		r := v.release("1.1.0")
		for _, n := range []string{"oarfish.service", "oarfish-update-recover.service", "oarfish-update-helper"} { // not in order
			r.setFile(n, changed(n))
		}
		v.place("1.1.0", r.build())
		got := wantErrCode(t, v.stage("1.1.0"), codeInstallerRequired)
		want := []string{"oarfish-update-helper", "oarfish-update-recover.service", "oarfish.service"}
		if strings.Join(got.Differs, ",") != strings.Join(want, ",") {
			t.Fatalf("differs %v, want %v", got.Differs, want)
		}
	})
	t.Run("all five", func(t *testing.T) {
		v := newTenv(t)
		r := v.release("1.1.0")
		for _, n := range componentNames {
			r.setFile(n, changed(n))
		}
		v.place("1.1.0", r.build())
		got := wantErrCode(t, v.stage("1.1.0"), codeInstallerRequired)
		if strings.Join(got.Differs, ",") != strings.Join(componentNames, ",") {
			t.Fatalf("differs %v", got.Differs)
		}
	})
	t.Run("none differs", func(t *testing.T) {
		v := newTenv(t)
		v.stageable("1.1.0")
		r := wantOK(t, v.stage("1.1.0"))
		if r.ComponentsDiffer {
			t.Fatal("components_differ")
		}
	})
	t.Run("a difference in a file the comparison does not cover is not a difference", func(t *testing.T) {
		v := newTenv(t)
		r := v.release("1.1.0")
		r.setFile("LICENSE", []byte("another licence\n"))
		v.place("1.1.0", r.build())
		wantOK(t, v.stage("1.1.0"))
	})
}

// The installed oarfish.service is read through current, the helper files
// through components/active.
func TestComponentComparisonReadsTheInstalledSide(t *testing.T) {
	t.Run("the installed service unit is the one in current", func(t *testing.T) {
		v := newTenv(t)
		os.WriteFile(filepath.Join(v.root, "releases", "1.0.0", "oarfish.service"), []byte("[Service]\n# edited\n"), 0o644)
		v.stageable("1.1.0")
		got := wantErrCode(t, v.stage("1.1.0"), codeInstallerRequired)
		if strings.Join(got.Differs, ",") != "oarfish.service" {
			t.Fatalf("%v", got.Differs)
		}
	})
	t.Run("the helper files are the ones active points to", func(t *testing.T) {
		v := newTenv(t)
		v.stageable("1.1.0")
		// known-good and an unreferenced set differ; active is what counts.
		os.MkdirAll(filepath.Join(v.root, "components", "0.9.0"), 0o755)
		for n := range baseComponents {
			os.WriteFile(filepath.Join(v.root, "components", "0.9.0", n), []byte("other"), 0o755)
		}
		os.Remove(filepath.Join(v.root, "components", "known-good"))
		os.Symlink("0.9.0", filepath.Join(v.root, "components", "known-good"))
		wantOK(t, v.stage("1.1.0"))
	})
}

func TestComponentsDamaged(t *testing.T) {
	damage := map[string]func(v *tenv){
		"active link missing": func(v *tenv) { os.Remove(filepath.Join(v.root, "components", "active")) },
		"active link dangling": func(v *tenv) {
			os.Remove(filepath.Join(v.root, "components", "active"))
			os.Symlink("9.9.9", filepath.Join(v.root, "components", "active"))
		},
		"components missing": func(v *tenv) { os.RemoveAll(filepath.Join(v.root, "components")) },
		"active link leaves root": func(v *tenv) {
			os.Remove(filepath.Join(v.root, "components", "active"))
			os.Symlink("../../..", filepath.Join(v.root, "components", "active"))
		},
		"active link is absolute": func(v *tenv) {
			os.Remove(filepath.Join(v.root, "components", "active"))
			os.Symlink("/etc", filepath.Join(v.root, "components", "active"))
		},
		"one file is a directory": func(v *tenv) {
			p := filepath.Join(v.root, "components", "1.0.0", "oarfish-update-helper.socket")
			os.Remove(p)
			os.Mkdir(p, 0o755)
		},
		"one file is a FIFO": func(v *tenv) {
			p := filepath.Join(v.root, "components", "1.0.0", "oarfish-update-helper")
			os.Remove(p)
			syscallMkfifo(p)
		},
		"one file is a link": func(v *tenv) {
			p := filepath.Join(v.root, "components", "1.0.0", "oarfish-update-helper.service")
			os.Rename(p, p+".real")
			os.Symlink("oarfish-update-helper.service.real", p)
		},
		"each file missing (recover unit)": func(v *tenv) {
			os.Remove(filepath.Join(v.root, "components", "1.0.0", "oarfish-update-recover.service"))
		},
		"each file missing (helper)":      func(v *tenv) { os.Remove(filepath.Join(v.root, "components", "1.0.0", "oarfish-update-helper")) },
		"service unit missing in current": func(v *tenv) { os.Remove(filepath.Join(v.root, "releases", "1.0.0", "oarfish.service")) },
	}
	for name, f := range damage {
		t.Run(name, func(t *testing.T) {
			v := newTenv(t)
			v.stageable("1.1.0")
			f(v)
			before := snapshot(t, v.root)
			wantErrCode(t, v.stageWithin2s("1.1.0"), codeComponentsDamaged)
			after := snapshot(t, v.root)
			if d := diffSnapshots(before, after); len(d) != 0 {
				t.Fatalf("changed %v", d)
			}
			if st, ok := v.readState(); ok && (st.Txn.Phase != phaseIdle || st.Last != nil) {
				t.Fatalf("state %+v", st)
			}
			if !lockFree(t, v.stateDir) {
				t.Fatal("lock held")
			}
		})
	}
}

// Damage is reported before a difference.
func TestDamagedBeatsDifferent(t *testing.T) {
	v := newTenv(t)
	r := v.release("1.1.0")
	r.setFile("oarfish-update-helper", elfStub("changed"))
	v.place("1.1.0", r.build())
	os.Remove(filepath.Join(v.root, "components", "1.0.0", "oarfish-update-recover.service"))
	wantErrCode(t, v.stage("1.1.0"), codeComponentsDamaged)
}

// A bundle without a components object (the legacy shape) can not be applied
// while the helper runs (G1).
func TestLegacyBundleIsRequirementsNotMet(t *testing.T) {
	v := newTenv(t)
	v.place("1.1.0", newRelease(t, v.key, "1.1.0", false).build())
	before := snapshot(t, v.root)
	wantErrCode(t, v.stage("1.1.0"), codeRequirementsNotMet)
	wantNothingLeft(t, v, before)
}

// The list of names is the closed list of the ADR (U2, "Replaceable component
// files") plus oarfish.service, in that order.
func TestComponentNamesMatchTheADR(t *testing.T) {
	adr, err := os.ReadFile("../../../docs/adr/0010-update-helper.md")
	if err != nil {
		t.Fatal(err)
	}
	var row string
	for _, l := range strings.Split(string(adr), "\n") {
		if strings.HasPrefix(l, "  | Replaceable component files |") {
			row = l
		}
	}
	if row == "" {
		t.Fatal("row not found")
	}
	var want []string
	for _, m := range regexp.MustCompile("`([a-z.-]+)`").FindAllStringSubmatch(row, -1) {
		want = append(want, m[1])
	}
	// The row lists the four files and names oarfish.service as the one that travels with the release.
	if strings.Join(want, ",") != strings.Join(componentNames, ",") {
		t.Fatalf("ADR %v, code %v", want, componentNames)
	}
}
