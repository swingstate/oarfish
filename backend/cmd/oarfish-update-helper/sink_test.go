package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Under the unit's UMask=0077 every mode must still come out as specified,
// because the code sets it with an explicit chmod and never relies on the
// umask (ADR 0010 U5, U10).
func TestModesWithUmask0077(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)

	v := newTenv(t)
	v.stageable("1.1.0")
	mode := func(path string) os.FileMode {
		fi, err := os.Lstat(path)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			return 0
		}
		return fi.Mode().Perm()
	}
	checked := false
	v.h.wrapFS = func(inner fsops) fsops {
		f := newTestFS(inner)
		f.before = func(op fsOp) error {
			// Just before the release directory is moved into place, look at the staging tree.
			if op.Kind == "rename" && strings.HasSuffix(op.Path, "/release") {
				checked = true
				stage := filepath.Join(v.root, op.Path[:strings.LastIndex(op.Path, "/")])
				want := map[string]os.FileMode{
					stage:                                                                0o700,
					filepath.Join(stage, "release"):                                      0o755,
					filepath.Join(stage, "components"):                                   0o700,
					filepath.Join(stage, "release", "oarfish"):                           0o755,
					filepath.Join(stage, "release", "oarfish.service"):                   0o644,
					filepath.Join(stage, "release", "LICENSE"):                           0o644,
					filepath.Join(stage, "release", "THIRD_PARTY_NOTICES.txt"):           0o644,
					filepath.Join(stage, "components", "oarfish-update-helper"):          0o755,
					filepath.Join(stage, "components", "oarfish-update-helper.socket"):   0o644,
					filepath.Join(stage, "components", "oarfish-update-helper.service"):  0o644,
					filepath.Join(stage, "components", "oarfish-update-recover.service"): 0o644,
				}
				for p, m := range want {
					if got := mode(p); got != m {
						t.Errorf("%s: mode %o, want %o", strings.TrimPrefix(p, v.root), got, m)
					}
				}
			}
			return nil
		}
		v.fs = f
		return f
	}
	wantOK(t, v.stage("1.1.0"))
	if !checked {
		t.Fatal("never looked at the staging tree")
	}
	rel := filepath.Join(v.root, "releases", "1.1.0")
	if got := mode(rel); got != 0o755 {
		t.Errorf("releases/1.1.0: mode %o, want 755", got)
	}
	for name, want := range map[string]os.FileMode{"oarfish": 0o755, "oarfish.service": 0o644, "LICENSE": 0o644, "THIRD_PARTY_NOTICES.txt": 0o644} {
		if got := mode(filepath.Join(rel, name)); got != want {
			t.Errorf("%s: mode %o, want %o", name, got, want)
		}
	}
	// The state file is 0600 and its temporary file was too.
	if got := mode(filepath.Join(v.stateDir, stateFile)); got != 0o600 {
		t.Errorf("state.json: mode %o", got)
	}
}

// The same with umask 0, to show that no mode comes from the umask in either
// direction.
func TestModesWithUmask0(t *testing.T) {
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	v := newTenv(t)
	v.stageable("1.1.0")
	wantOK(t, v.stage("1.1.0"))
	rel := filepath.Join(v.root, "releases", "1.1.0")
	for name, want := range map[string]os.FileMode{"": 0o755, "oarfish": 0o755, "oarfish.service": 0o644, "LICENSE": 0o644} {
		fi, err := os.Stat(filepath.Join(rel, name))
		if err != nil || fi.Mode().Perm() != want {
			t.Errorf("%q: %v %v, want %o", name, fi, err, want)
		}
	}
	if fi, _ := os.Stat(filepath.Join(v.stateDir, stateFile)); fi.Mode().Perm() != 0o600 {
		t.Errorf("state.json: mode %o", fi.Mode().Perm())
	}
}

func TestSinkRules(t *testing.T) {
	v := newTenv(t)
	e, _ := v.h.open(true)
	defer e.close()
	r := &stageRun{h: v.h, e: e, id: "00000000000000aa"}
	os.MkdirAll(filepath.Join(v.root, "releases", ".stage.00000000000000aa", "release"), 0o700)
	os.MkdirAll(filepath.Join(v.root, "releases", ".stage.00000000000000aa", "components"), 0o700)
	sink := newStagingSink(r, "releases/.stage.00000000000000aa")

	for _, name := range []string{"", "x", "../oarfish", "release/oarfish", "oarfish ", "OARFISH", "/etc/passwd", "oarfish-update-helper.timer", "install.sh", "manifest.json"} {
		if _, err := sink.Create(name, 0o644, 1); err == nil {
			t.Errorf("created %q", name)
		}
	}
	for _, mode := range []uint32{0, 0o600, 0o700, 0o777, 0o4755, 0o664, 0o2755} {
		if _, err := sink.Create("LICENSE", mode, 1); err == nil {
			t.Errorf("created with mode %o", mode)
		}
	}
	f, err := sink.Create("LICENSE", 0o644, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sink.Create("LICENSE", 0o644, 3); err == nil {
		t.Error("created the same file twice")
	}
	if n, err := f.Write([]byte("ab")); n != 2 || err != nil {
		t.Fatalf("write: %d %v", n, err)
	}
	if _, err := f.Write([]byte("cd")); err == nil {
		t.Error("wrote beyond the manifest size")
	}
	if err := f.Finish(); err == nil {
		t.Error("finished a file that is shorter than its size")
	}
	sink.abort()
	// Every name of the table goes where the table says.
	got := map[string]string{}
	for n, d := range sinkTargets {
		got[n] = d
	}
	if len(got) != 8 {
		t.Fatalf("table has %d names", len(got))
	}
	for _, n := range []string{"oarfish", "oarfish.service", "LICENSE", "THIRD_PARTY_NOTICES.txt"} {
		if got[n] != dirRelease {
			t.Errorf("%s -> %s", n, got[n])
		}
	}
	for _, n := range componentNames[:4] {
		if got[n] != dirComponents {
			t.Errorf("%s -> %s", n, got[n])
		}
	}
}
