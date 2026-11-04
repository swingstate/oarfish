package relverify

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// parsed returns the manifest of the release as ParseManifest reads it.
func parsed(t testing.TB, r *release) *Manifest {
	t.Helper()
	b, err := json.Marshal(r.man)
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseManifest(b)
	if err != nil {
		t.Fatalf("ParseManifest: %v (%s)", err, detail(err))
	}
	return m
}

// withEnts builds a bundle from ents and inspects it against the manifest of r.
func inspectEnts(t testing.TB, r *release, ents []tarEnt, sink Sink) error {
	t.Helper()
	return Inspect(bytes.NewReader(gz(makeTar(ents))), parsed(t, r), sink)
}

func entsOf(r *release) []tarEnt { return defaultEnts(r.root, r.files) }

func dropEnt(ents []tarEnt, suffix string) []tarEnt {
	var out []tarEnt
	for _, e := range ents {
		if !strings.HasSuffix(e.name, "/"+suffix) {
			out = append(out, e)
		}
	}
	return out
}

func editEnt(ents []tarEnt, suffix string, f func(e *tarEnt)) []tarEnt {
	out := append([]tarEnt(nil), ents...)
	for i := range out {
		if strings.HasSuffix(out[i].name, "/"+suffix) {
			f(&out[i])
		}
	}
	return out
}

func TestInspectValid(t *testing.T) {
	for _, comps := range []bool{false, true} {
		r := newRelease(t, "0.1.0-test.1", comps)
		s := newMemSink()
		if err := Inspect(bytes.NewReader(r.bundle), parsed(t, r), s); err != nil {
			t.Fatalf("components=%v: %v (%s)", comps, err, detail(err))
		}
		if len(s.files) != len(r.files) {
			t.Fatalf("%d files for %d", len(s.files), len(r.files))
		}
		for _, f := range r.files {
			if !bytes.Equal(s.files[f.name], f.data) {
				t.Errorf("%s: wrong bytes", f.name)
			}
			if s.modes[f.name] != uint32(f.mode) || s.sizes[f.name] != int64(len(f.data)) || !s.finished[f.name] {
				t.Errorf("%s: mode %o size %d finished %v", f.name, s.modes[f.name], s.sizes[f.name], s.finished[f.name])
			}
		}
	}
}

// The sink is called with names, never paths.
func TestInspectSinkSeesPlainNames(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", true)
	s := newMemSink()
	if err := Inspect(bytes.NewReader(r.bundle), parsed(t, r), s); err != nil {
		t.Fatal(err)
	}
	for n := range s.files {
		if strings.ContainsAny(n, "/\\") || n == "" {
			t.Errorf("name %q", n)
		}
	}
}

// The tar header's owner, group and mode are ignored; the manifest's mode is
// what the sink gets.
func TestInspectIgnoresHeaderOwnerGroupMode(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", false)
	ents := entsOf(r)
	for i := range ents {
		ents[i].uid, ents[i].gid, ents[i].mode = 1000+i, 2000+i, 0o4777
	}
	s := newMemSink()
	if err := inspectEnts(t, r, ents, s); err != nil {
		t.Fatalf("%v (%s)", err, detail(err))
	}
	if s.modes["oarfish"] != 0o755 || s.modes["LICENSE"] != 0o644 || s.modes["oarfish.service"] != 0o644 {
		t.Fatalf("modes %v", s.modes)
	}
}

func TestInspectDirSink(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", true)
	dir := t.TempDir()
	s := &dirSink{dir: dir}
	if err := Inspect(bytes.NewReader(r.bundle), parsed(t, r), s); err != nil {
		t.Fatal(err)
	}
	for _, f := range r.files {
		got, err := os.ReadFile(filepath.Join(dir, f.name))
		if err != nil || !bytes.Equal(got, f.data) {
			t.Fatalf("%s: %v", f.name, err)
		}
		st, _ := os.Stat(filepath.Join(dir, f.name))
		if want := os.FileMode(f.mode) &^ 0o022; runtime.GOOS != "windows" && st.Mode().Perm()&^0o022 != want {
			t.Errorf("%s: mode %v, want %v", f.name, st.Mode().Perm(), want)
		}
		if !s.finished[f.name] {
			t.Errorf("%s not finished", f.name)
		}
	}
	// O_EXCL: a second extraction into the same directory fails.
	err := Inspect(bytes.NewReader(r.bundle), parsed(t, r), s)
	wantCode(t, err, CodeIOError)
}

func TestInspectNilSinkVerifiesOnly(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", true)
	if err := Inspect(bytes.NewReader(r.bundle), parsed(t, r), nil); err != nil {
		t.Fatal(err)
	}
	bad := editEnt(entsOf(r), "LICENSE", func(e *tarEnt) { e.body = []byte("tampered\n") })
	wantCode(t, inspectEnts(t, r, bad, nil), CodeChecksumMismatch)
}

func TestInspectRejectedEntryTypes(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", false)
	root := r.root
	for _, tc := range []struct {
		name   string
		mutate func(ents []tarEnt) []tarEnt
		detail string
	}{
		{"symbolic link instead of a file", func(e []tarEnt) []tarEnt {
			return editEnt(e, "oarfish", func(x *tarEnt) { x.typ, x.body, x.link = tar.TypeSymlink, nil, "/bin/sh" })
		}, "symbolic link"},
		{"hard link instead of a file", func(e []tarEnt) []tarEnt {
			return editEnt(e, "oarfish.service", func(x *tarEnt) { x.typ, x.body, x.link = tar.TypeLink, nil, root+"/oarfish" })
		}, "hard link"},
		{"character device", func(e []tarEnt) []tarEnt {
			return editEnt(e, "LICENSE", func(x *tarEnt) { x.typ, x.body = tar.TypeChar, nil })
		}, "device"},
		{"block device", func(e []tarEnt) []tarEnt {
			return editEnt(e, "LICENSE", func(x *tarEnt) { x.typ, x.body = tar.TypeBlock, nil })
		}, "device"},
		{"FIFO", func(e []tarEnt) []tarEnt {
			return editEnt(e, "LICENSE", func(x *tarEnt) { x.typ, x.body = tar.TypeFifo, nil })
		}, "FIFO"},
		{"directory below the top level", func(e []tarEnt) []tarEnt {
			return append(e[:2:2], append([]tarEnt{{name: root + "/sub/", typ: tar.TypeDir, mode: 0o755}}, e[2:]...)...)
		}, "directory below"},
		{"directory in place of a file", func(e []tarEnt) []tarEnt {
			return editEnt(e, "LICENSE", func(x *tarEnt) { x.typ, x.body, x.name = tar.TypeDir, nil, x.name+"/" })
		}, "directory"},
		{"symbolic link as an extra entry", func(e []tarEnt) []tarEnt {
			return append(e, tarEnt{name: root + "/link", typ: tar.TypeSymlink, link: "/etc/passwd"})
		}, "symbolic link"},
		{"pax global header", func(e []tarEnt) []tarEnt {
			return append(e[:1:1], append([]tarEnt{{name: "pax_global_header", typ: tar.TypeXGlobalHeader}}, e[1:]...)...)
		}, "not a regular file"},
		{"contiguous file type", func(e []tarEnt) []tarEnt {
			return editEnt(e, "LICENSE", func(x *tarEnt) { x.typ = tar.TypeCont })
		}, "not a regular file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := inspectEnts(t, r, tc.mutate(entsOf(r)), nil)
			wantDetail(t, err, CodeManifestInvalid, tc.detail)
		})
	}
}

func TestInspectRejectedPaths(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", false)
	root := r.root
	rename := func(name string) func(ents []tarEnt) []tarEnt {
		return func(e []tarEnt) []tarEnt {
			return editEnt(e, "oarfish", func(x *tarEnt) { x.name = name })
		}
	}
	for _, tc := range []struct {
		name   string
		mutate func(ents []tarEnt) []tarEnt
		detail string
	}{
		{"absolute path", rename("/oarfish"), "absolute"},
		{"absolute path into /etc", rename("/etc/cron.d/oarfish"), "absolute"},
		{"parent element at the start", rename("../oarfish"), ".."},
		{"parent element after the directory", rename(root + "/../oarfish"), ".."},
		{"parent element escaping the directory", rename(root + "/../../etc/passwd"), ".."},
		{"dot slash prefix", rename("./" + root + "/oarfish"), "top-level"},
		{"second top-level name", rename("other_1.0.0/oarfish"), "top-level"},
		{"directory name with another version", rename("oarfish_9.9.9_linux_arm64/oarfish"), "top-level"},
		{"bare file name", rename("oarfish"), "top-level"},
		{"name outside the allow-list", rename(root + "/evil"), "allow-list"},
		{"name in a subdirectory", rename(root + "/sub/oarfish"), "allow-list"},
		{"upper-case spelling", rename(root + "/OARFISH"), "allow-list"},
		{"backslash separator", rename(root + `\oarfish`), "top-level"},
		{"double slash", rename(root + "//oarfish"), "allow-list"},
		{"long name", rename(root + "/" + strings.Repeat("a", 5000)), "allow-list"},
		{"hidden file", rename(root + "/.oarfish"), "allow-list"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := inspectEnts(t, r, tc.mutate(entsOf(r)), nil)
			wantDetail(t, err, CodeManifestInvalid, tc.detail)
		})
	}
}

func TestInspectStructure(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", false)
	root := r.root
	t.Run("duplicate file", func(t *testing.T) {
		e := entsOf(r)
		e = append(e, e[2])
		wantDetail(t, inspectEnts(t, r, e, nil), CodeManifestInvalid, "twice")
	})
	t.Run("duplicate file with other content", func(t *testing.T) {
		e := entsOf(r)
		e = append(e, tarEnt{name: e[3].name, body: []byte("evil"), mode: 0o755})
		wantCode(t, inspectEnts(t, r, e, nil), CodeManifestInvalid)
	})
	t.Run("duplicate directory entry", func(t *testing.T) {
		e := entsOf(r)
		e = append(e[:1:1], append([]tarEnt{e[0]}, e[1:]...)...)
		wantCode(t, inspectEnts(t, r, e, nil), CodeManifestInvalid)
	})
	t.Run("no directory entry", func(t *testing.T) {
		wantDetail(t, inspectEnts(t, r, entsOf(r)[1:], nil), CodeManifestInvalid, "first entry")
	})
	t.Run("directory entry after the files", func(t *testing.T) {
		e := entsOf(r)
		e = append(e[1:], e[0])
		wantDetail(t, inspectEnts(t, r, e, nil), CodeManifestInvalid, "first entry")
	})
	t.Run("another name for the directory", func(t *testing.T) {
		e := entsOf(r)
		e[0].name = "oarfish/"
		wantCode(t, inspectEnts(t, r, e, nil), CodeManifestInvalid)
	})
	t.Run("directory without trailing slash", func(t *testing.T) {
		e := entsOf(r)
		e[0].name = root
		wantCode(t, inspectEnts(t, r, e, nil), CodeManifestInvalid)
	})
	t.Run("missing file", func(t *testing.T) {
		for _, f := range r.files {
			wantDetail(t, inspectEnts(t, r, dropEnt(entsOf(r), f.name), nil), CodeManifestInvalid, "missing")
		}
	})
	t.Run("extra file with an allowed name", func(t *testing.T) {
		// The legacy manifest does not list the helper.
		e := append(entsOf(r), tarEnt{name: root + "/oarfish-update-helper", body: elfStub("x"), mode: 0o755})
		wantCode(t, inspectEnts(t, r, e, nil), CodeManifestInvalid)
	})
	t.Run("extra file with another name", func(t *testing.T) {
		e := append(entsOf(r), tarEnt{name: root + "/extra.txt", body: []byte("x"), mode: 0o644})
		wantDetail(t, inspectEnts(t, r, e, nil), CodeManifestInvalid, "allow-list")
	})
	t.Run("only the directory", func(t *testing.T) {
		wantDetail(t, inspectEnts(t, r, entsOf(r)[:1], nil), CodeManifestInvalid, "missing")
	})
	t.Run("empty tar", func(t *testing.T) {
		wantDetail(t, inspectEnts(t, r, nil, nil), CodeManifestInvalid, "no top-level")
	})
	t.Run("too many entries", func(t *testing.T) {
		e := entsOf(r)
		for i := 0; i < 40; i++ {
			e = append(e, tarEnt{name: root + "/LICENSE", body: []byte("x"), mode: 0o644})
		}
		wantCode(t, inspectEnts(t, r, e, nil), CodeManifestInvalid)
	})
}

// A bundle with the shape the manifest does not declare.
func TestInspectShape(t *testing.T) {
	leg := newRelease(t, "0.1.0-test.1", false)
	com := newRelease(t, "0.1.0-test.1", true)
	t.Run("components tarball, legacy manifest", func(t *testing.T) {
		wantCode(t, Inspect(bytes.NewReader(com.bundle), parsed(t, leg), nil), CodeManifestInvalid)
	})
	t.Run("legacy tarball, components manifest", func(t *testing.T) {
		wantDetail(t, Inspect(bytes.NewReader(leg.bundle), parsed(t, com), nil), CodeManifestInvalid, "missing")
	})
	t.Run("three of the four component files", func(t *testing.T) {
		wantCode(t, inspectEnts(t, com, dropEnt(entsOf(com), "oarfish-update-recover.service"), nil), CodeManifestInvalid)
	})
	t.Run("each component file missing", func(t *testing.T) {
		for _, n := range []string{"oarfish-update-helper", "oarfish-update-helper.service", "oarfish-update-helper.socket", "oarfish-update-recover.service"} {
			wantCode(t, inspectEnts(t, com, dropEnt(entsOf(com), n), nil), CodeManifestInvalid)
		}
	})
}

// A Manifest is a plain struct; Inspect does not rely on the caller having
// used ParseManifest.
func TestInspectChecksItsManifest(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", true)
	good := parsed(t, r)
	copyM := func() *Manifest {
		m := *good
		m.Contents = append([]Entry(nil), good.Contents...)
		return &m
	}
	for name, edit := range map[string]func(m *Manifest){
		"mode of a program is 0644":    func(m *Manifest) { m.Contents[2].Mode = 0o644 },
		"mode 0777":                    func(m *Manifest) { m.Contents[0].Mode = 0o777 },
		"path outside the directory":   func(m *Manifest) { m.Contents[0].Path = "other/LICENSE" },
		"path not allow-listed":        func(m *Manifest) { m.Contents[0].Path = r.root + "/evil" },
		"duplicate path":               func(m *Manifest) { m.Contents[1].Path = m.Contents[0].Path },
		"component file, no component": func(m *Manifest) { m.Components = nil },
		"missing entry":                func(m *Manifest) { m.Contents = m.Contents[1:] },
		"size 0":                       func(m *Manifest) { m.Contents[0].Size = 0 },
		"size beyond the limit":        func(m *Manifest) { m.Contents[0].Size = maxContentBytes + 1 },
		"sum beyond the limit": func(m *Manifest) {
			m.Contents[0].Size, m.Contents[1].Size = maxContentBytes, maxContentBytes
		},
		"version not valid": func(m *Manifest) { m.Version = "1.0" },
		"bundle name":       func(m *Manifest) { m.Bundle.File = "x.tar.gz" },
		"no contents":       func(m *Manifest) { m.Contents = nil },
	} {
		t.Run(name, func(t *testing.T) {
			m := copyM()
			edit(m)
			wantCode(t, Inspect(bytes.NewReader(r.bundle), m, nil), CodeInternal)
		})
	}
}

func TestInspectSizeMismatch(t *testing.T) {
	for _, delta := range []int{+1, -1, +1000, -5} {
		for _, comps := range []bool{false, true} {
			r := newRelease(t, "0.1.0-test.1", comps)
			n := len(r.files[0].data)
			setPath(t, r.man, "contents.0.size", n+delta)
			wantCode(t, Inspect(bytes.NewReader(r.bundle), parsed(t, r), newMemSink()), CodeChecksumMismatch)
		}
	}
}

func TestInspectHashMismatch(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", true)
	t.Run("manifest hash differs", func(t *testing.T) {
		r := newRelease(t, "0.1.0-test.1", true)
		setPath(t, r.man, "contents.0.sha256", SumHex([]byte("other")))
		s := newMemSink()
		wantDetail(t, Inspect(bytes.NewReader(r.bundle), parsed(t, r), s), CodeChecksumMismatch, "SHA-256")
		if s.finished["LICENSE"] {
			t.Fatal("a file with a wrong hash was finished")
		}
	})
	t.Run("body changed, same length", func(t *testing.T) {
		for _, f := range r.files {
			e := editEnt(entsOf(r), f.name, func(x *tarEnt) {
				b := append([]byte(nil), x.body...)
				b[len(b)-1] ^= 1
				x.body = b
			})
			wantCode(t, inspectEnts(t, r, e, nil), CodeChecksumMismatch)
		}
	})
	t.Run("a partial file is created, never finished, and nothing follows", func(t *testing.T) {
		e := editEnt(entsOf(r), "LICENSE", func(x *tarEnt) { x.body = []byte("licence TEXT\n") })
		s := newMemSink()
		wantCode(t, inspectEnts(t, r, e, s), CodeChecksumMismatch)
		if len(s.files["LICENSE"]) == 0 || s.finished["LICENSE"] {
			t.Fatal("expected a created, unfinished LICENSE")
		}
		for n := range s.finished {
			if n == "LICENSE" {
				t.Fatal("finished")
			}
		}
		if got := s.order[len(s.order)-1]; got != "LICENSE" {
			t.Fatalf("the sink was called for %s after the failing file", got)
		}
	})
}

func TestInspectELF(t *testing.T) {
	mk := func(mut func(b []byte) []byte) []byte {
		return mut(elfStub("x"))
	}
	bads := map[string][]byte{
		"text":                   []byte("#!/bin/sh\necho hi\n"),
		"empty magic":            make([]byte, 64),
		"too short for a header": []byte("\x7fELF\x02\x01\x01"),
		"19 bytes":               elfStub("")[:19],
		"x86-64":                 mk(func(b []byte) []byte { b[18], b[19] = 0x3e, 0; return b }),
		"arm 32-bit machine":     mk(func(b []byte) []byte { b[18] = 0x28; return b }),
		"machine high byte":      mk(func(b []byte) []byte { b[19] = 1; return b }),
		"32-bit class":           mk(func(b []byte) []byte { b[4] = 1; return b }),
		"big-endian":             mk(func(b []byte) []byte { b[5] = 2; return b }),
		"wrong magic":            mk(func(b []byte) []byte { b[1] = 'e'; return b }),
		"unknown class":          mk(func(b []byte) []byte { b[4] = 0; return b }),
	}
	for _, prog := range []string{"oarfish", "oarfish-update-helper"} {
		for name, content := range bads {
			t.Run(prog+"/"+name, func(t *testing.T) {
				r := newRelease(t, "0.1.0-test.1", true)
				for i := range r.files {
					if r.files[i].name == prog {
						r.files[i].data = content
					}
				}
				r.setBundle(entsOf(r))
				s := newMemSink()
				err := Inspect(bytes.NewReader(r.bundle), parsed(t, r), s)
				wantCode(t, err, CodeRequirementsNotMet)
				if _, created := s.files[prog]; created {
					t.Fatal("the sink got a file that failed the ELF check")
				}
			})
		}
	}
	t.Run("the other files need no ELF header", func(t *testing.T) {
		r := newRelease(t, "0.1.0-test.1", true)
		if err := Inspect(bytes.NewReader(r.bundle), parsed(t, r), nil); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("isELFArm64", func(t *testing.T) {
		if !isELFArm64(elfStub("")) || isELFArm64(nil) {
			t.Fatal()
		}
	})
}

func TestInspectGzipAndTarFraming(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", true)
	m := parsed(t, r)
	good := r.bundle
	t.Run("every strict prefix of the bundle fails", func(t *testing.T) {
		for n := 0; n < len(good); n++ {
			if Inspect(bytes.NewReader(good[:n]), m, nil) == nil {
				t.Fatalf("prefix of %d bytes accepted", n)
			}
		}
	})
	t.Run("truncated gzip is a manifest_invalid, not a panic", func(t *testing.T) {
		wantCode(t, Inspect(bytes.NewReader(good[:len(good)-10]), m, nil), CodeManifestInvalid)
	})
	t.Run("trailing garbage after the gzip stream", func(t *testing.T) {
		wantDetail(t, Inspect(bytes.NewReader(append(append([]byte(nil), good...), "garbage"...)), m, nil), CodeManifestInvalid, "after the gzip stream")
	})
	t.Run("trailing NUL after the gzip stream", func(t *testing.T) {
		wantDetail(t, Inspect(bytes.NewReader(append(append([]byte(nil), good...), 0)), m, nil), CodeManifestInvalid, "after the gzip stream")
	})
	t.Run("a second gzip member", func(t *testing.T) {
		wantCode(t, Inspect(bytes.NewReader(append(append([]byte(nil), good...), gz(nil)...)), m, nil), CodeManifestInvalid)
	})
	t.Run("the same bundle twice", func(t *testing.T) {
		wantCode(t, Inspect(bytes.NewReader(append(append([]byte(nil), good...), good...)), m, nil), CodeManifestInvalid)
	})
	t.Run("corrupted CRC", func(t *testing.T) {
		b := append([]byte(nil), good...)
		b[len(b)-5] ^= 0xff
		wantCode(t, Inspect(bytes.NewReader(b), m, nil), CodeManifestInvalid)
	})
	t.Run("a flipped byte in the compressed data", func(t *testing.T) {
		// A flip is either refused or harmless (a padding bit of the last
		// deflate byte): whatever is accepted is exactly the signed content.
		refused := 0
		for i := 10; i < len(good)-8; i += 7 {
			b := append([]byte(nil), good...)
			b[i] ^= 0x55
			s := newMemSink()
			if Inspect(bytes.NewReader(b), m, s) != nil {
				refused++
				continue
			}
			for _, f := range r.files {
				if !bytes.Equal(s.files[f.name], f.data) {
					t.Fatalf("flip at %d accepted with different content", i)
				}
			}
		}
		if refused < 10 {
			t.Fatalf("only %d flips refused", refused)
		}
	})
	t.Run("not gzip", func(t *testing.T) {
		for _, b := range [][]byte{nil, []byte("x"), []byte("not a gzip file at all"), makeTar(entsOf(r)), bytes.Repeat([]byte{0x1f}, 100)} {
			wantCode(t, Inspect(bytes.NewReader(b), m, nil), CodeManifestInvalid)
		}
	})
	t.Run("non-zero data after the end of the archive", func(t *testing.T) {
		tarb := append(makeTar(entsOf(r)), "X"...)
		wantDetail(t, Inspect(bytes.NewReader(gz(tarb)), m, nil), CodeManifestInvalid, "after the end of the archive")
	})
	t.Run("zero padding after the end of the archive (blocking factor)", func(t *testing.T) {
		tarb := append(makeTar(entsOf(r)), make([]byte, 10240)...)
		if err := Inspect(bytes.NewReader(gz(tarb)), m, nil); err != nil {
			t.Fatalf("%v (%s)", err, detail(err))
		}
	})
	t.Run("gzip header with name and time is fine", func(t *testing.T) {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		zw.Name = "bundle.tar"
		zw.Write(makeTar(entsOf(r)))
		zw.Close()
		if err := Inspect(&buf, m, nil); err != nil {
			t.Fatal(err)
		}
	})
}

// A failing reader is an io_error, a bad archive is not.
type failingReader struct {
	r    io.Reader
	left int
	err  error
}

func (f *failingReader) Read(p []byte) (int, error) {
	if f.left <= 0 {
		return 0, f.err
	}
	if len(p) > f.left {
		p = p[:f.left]
	}
	n, err := f.r.Read(p)
	f.left -= n
	return n, err
}

func TestInspectReaderError(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", true)
	m := parsed(t, r)
	secret := errors.New("read /secret/path: input/output error")
	for _, at := range []int{0, 5, 30, len(r.bundle) / 2, len(r.bundle) - 3} {
		err := Inspect(&failingReader{r: bytes.NewReader(r.bundle), left: at, err: secret}, m, nil)
		wantCode(t, err, CodeIOError)
		if strings.Contains(err.Error(), "secret") || strings.Contains(detail(err), "secret") {
			t.Fatal("an operating-system error text reached the error")
		}
	}
}

func TestInspectSinkErrors(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", false)
	m := parsed(t, r)
	for name, mk := range map[string]func(s *memSink){
		"create": func(s *memSink) { s.failAt = "oarfish" },
		"write":  func(s *memSink) { s.failW = "oarfish" },
		"finish": func(s *memSink) { s.failF = "oarfish" },
	} {
		t.Run(name, func(t *testing.T) {
			s := newMemSink()
			mk(s)
			err := Inspect(bytes.NewReader(r.bundle), m, s)
			wantCode(t, err, CodeIOError)
			if strings.Contains(err.Error()+detail(err), "secret") {
				t.Fatal("sink error text leaked")
			}
			if s.finished["oarfish"] {
				t.Fatal("finished after an error")
			}
		})
	}
}

// Finish is called only for files that matched, in every failure mode.
func TestInspectFinishOnlyForVerifiedFiles(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", true)
	for i, f := range r.files {
		e := editEnt(entsOf(r), f.name, func(x *tarEnt) {
			b := append([]byte(nil), x.body...)
			b[0] ^= 1
			x.body = b
		})
		// the ELF programs fail the header check instead when byte 0 changes
		s := newMemSink()
		if err := inspectEnts(t, r, e, s); err == nil {
			t.Fatalf("file %d accepted", i)
		}
		if s.finished[f.name] {
			t.Errorf("%s was finished although it is bad", f.name)
		}
		for name, ok := range s.finished {
			if ok && !bytes.Equal(s.files[name], dataOf(r, name)) {
				t.Errorf("%s finished with wrong bytes", name)
			}
		}
	}
}

func dataOf(r *release, name string) []byte {
	for _, f := range r.files {
		if f.name == name {
			return f.data
		}
	}
	return nil
}

// A decompression bomb: a valid bundle followed, inside the same gzip member,
// by far more zero bytes than the manifest allows. Zero bytes are legal tar
// padding, so only the expansion limit stops it.
func TestInspectDecompressionBomb(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", true)
	m := parsed(t, r)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(makeTar(entsOf(r)))
	zero := make([]byte, 1<<20)
	const bombMiB = 64
	for i := 0; i < bombMiB; i++ {
		zw.Write(zero)
	}
	zw.Close()
	bomb := buf.Bytes()
	if len(bomb) > 256<<10 {
		t.Fatalf("test setup: the bomb is %d bytes, not small", len(bomb))
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	cr := &countingReader{r: bytes.NewReader(bomb)}
	err := Inspect(cr, m, nil)
	runtime.ReadMemStats(&after)
	wantDetail(t, err, CodeManifestInvalid, "expands beyond")
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 2<<20 {
		t.Fatalf("Inspect allocated %d bytes on a bomb of %d MiB", alloc, bombMiB)
	}
	t.Logf("bomb: %d bytes compressed, %d MiB expanded; read %d bytes before stopping", len(bomb), bombMiB, cr.n)
}

type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// Another bomb: the header of a file claims a huge size.
func TestInspectHugeHeaderSize(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", false)
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: r.root + "/", Mode: 0o755})
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: r.root + "/LICENSE", Mode: 0o644, Size: 1 << 40})
	// Not closed: the header alone is the attack.
	var zbuf bytes.Buffer
	zw := gzip.NewWriter(&zbuf)
	zw.Write(buf.Bytes())
	zw.Write(make([]byte, 4<<20))
	zw.Close()
	wantCode(t, Inspect(bytes.NewReader(zbuf.Bytes()), parsed(t, r), nil), CodeChecksumMismatch)
}

// A big but legitimate file is streamed, not held.
func TestInspectStreamsLargeFiles(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", false)
	big := make([]byte, 24<<20)
	pat := []byte("0123456789abcdef-pattern-block-of-4096-bytes-")
	for i := range big {
		big[i] = pat[i%len(pat)] ^ byte(i>>12)
	}
	for i := range r.files {
		if r.files[i].name == "THIRD_PARTY_NOTICES.txt" {
			r.files[i].data = big
		}
	}
	r.setBundle(entsOf(r))
	m := parsed(t, r)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	err := Inspect(bytes.NewReader(r.bundle), m, discardSink{})
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("%v (%s)", err, detail(err))
	}
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 2<<20 {
		t.Fatalf("Inspect allocated %d bytes for a %d MiB file", alloc, len(big)>>20)
	}
}

type discardSink struct{}
type discardFile struct{}

func (discardSink) Create(string, uint32, int64) (File, error) { return discardFile{}, nil }
func (discardFile) Write(p []byte) (int, error)                { return len(p), nil }
func (discardFile) Finish() error                              { return nil }

// The two limits inside the inspector, tested directly: they back up the checks
// of the header size and of the expansion.
func TestLimitReaderAndFileWriter(t *testing.T) {
	lr := &limitReader{r: bytes.NewReader(make([]byte, 100)), n: 60}
	n, err := io.Copy(io.Discard, lr)
	if n != 60 || !errors.Is(err, errExpansion) {
		t.Fatalf("limitReader: %d %v", n, err)
	}
	lr = &limitReader{r: bytes.NewReader(make([]byte, 60)), n: 60}
	if n, err := io.Copy(io.Discard, lr); n != 60 || err != nil {
		t.Fatalf("limitReader at the exact size: %d %v", n, err)
	}
	fw := &fileWriter{h: discardHash(), limit: 10}
	if _, err := fw.Write(make([]byte, 10)); err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte{1}); !errors.Is(err, errSize) {
		t.Fatalf("fileWriter accepted the byte after the limit: %v", err)
	}
}

// The error text and the detail never contain an attacker-chosen name.
func TestInspectErrorsDoNotEchoNames(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", false)
	const marker = "ATTACKER-CHOSEN-NAME-9f3a"
	for _, name := range []string{r.root + "/" + marker, "/" + marker, marker + "/oarfish", r.root + "/../" + marker} {
		e := append(entsOf(r), tarEnt{name: name, body: []byte("x"), mode: 0o644})
		err := inspectEnts(t, r, e, nil)
		if err == nil {
			t.Fatal("accepted")
		}
		if strings.Contains(err.Error(), marker) || strings.Contains(detail(err), marker) {
			t.Fatalf("name echoed: %q / %q", err.Error(), detail(err))
		}
	}
}
