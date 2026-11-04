package main

import (
	"os"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

const hexMarker = "decafbad0badc0de" // a correlation id that can be found again

// Nothing the helper sends, logs or stores may contain a path, an
// operating-system error string, an address or a host name (ADR 0010 U8,
// ORB-SEC-007). The whole flow runs with a marker as correlation id and
// temporary directories whose names are known.
func TestRedaction(t *testing.T) {
	host, _ := os.Hostname()
	osErrors := []string{
		"no such file", "permission denied", "invalid argument", "operation not permitted", "file exists",
		"not a directory", "is a directory", "input/output", "read-only file system", "no space left",
		"too many", "errno", "syscall", "openat", "lstat", "rename ", "mkdir ", "unlinkat", "fstatat", "open ", "/", "\\",
		"injected failure", "file system stopped", "deadline exceeded",
	}
	ip := regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b|[0-9a-f:]{2,}::[0-9a-f:]+`)

	type flow struct {
		name string
		run  func(v *tenv) [][]byte
	}
	failWith := func(f func(v *tenv)) func(v *tenv) [][]byte {
		return func(v *tenv) [][]byte {
			f(v)
			return [][]byte{v.request(stageLine("1.1.0", hexMarker))}
		}
	}
	flows := []flow{
		{"success, status, discard", func(v *tenv) [][]byte {
			v.stageable("1.1.0")
			out := [][]byte{v.request(stageLine("1.1.0", hexMarker)), v.request([]byte(okStatus)), v.request(discardLine("1.1.0")), v.request([]byte(okStatus))}
			return out
		}},
		{"expiry", func(v *tenv) [][]byte {
			v.stageable("1.1.0")
			out := [][]byte{v.request(stageLine("1.1.0", hexMarker))}
			v.uptime.Store(1000 + stageLifetimeS)
			return append(out, v.request([]byte(okStatus)))
		}},
		{"not_found", failWith(func(v *tenv) {})},
		{"cache directory is a link", failWith(func(v *tenv) {
			v.stageable("1.1.0")
			os.Rename(v.cacheDir+"/update/1.1.0", v.cacheDir+"/moved")
			os.Symlink("../moved", v.cacheDir+"/update/1.1.0")
		})},
		{"signature_invalid", failWith(func(v *tenv) { r := v.release("1.1.0"); r.key = newTestKey(t, "other"); v.place("1.1.0", r.build()) })},
		{"checksum_mismatch", failWith(func(v *tenv) {
			b := v.release("1.1.0").build()
			b.Bundle = append([]byte(nil), b.Bundle...)
			b.Bundle[10] ^= 1
			v.place("1.1.0", b)
		})},
		{"manifest_invalid", failWith(func(v *tenv) {
			r := v.release("1.1.0")
			r.bundle = []byte("not a bundle")
			r.man = r.defaultManifest()
			v.place("1.1.0", r.build())
		})},
		{"downgrade", func(v *tenv) [][]byte {
			v.place("0.9.0", v.release("0.9.0").build())
			return [][]byte{v.request(stageLine("0.9.0", hexMarker))}
		}},
		{"installer_required", failWith(func(v *tenv) {
			r := v.release("1.1.0")
			r.setFile("oarfish-update-helper", elfStub("x"))
			v.place("1.1.0", r.build())
		})},
		{"components_damaged", failWith(func(v *tenv) { v.stageable("1.1.0"); os.RemoveAll(v.root + "/components") })},
		{"no_space", failWith(func(v *tenv) { v.stageable("1.1.0"); v.h.freeBytes = func(*os.File) (uint64, error) { return 1, nil } })},
		{"io_error and a failed cleanup", failWith(func(v *tenv) {
			v.stageable("1.1.0")
			v.h.wrapFS = func(inner fsops) fsops {
				f := newTestFS(inner)
				f.before = func(op fsOp) error {
					if op.Kind == "write" && strings.Contains(op.Path, "/release/") || op.Kind == "removeall" && strings.HasPrefix(op.Path, "releases/.stage.") {
						return &os.PathError{Op: "write", Path: v.root + "/secret/path", Err: os.ErrPermission}
					}
					return nil
				}
				return f
			}
		})},
		{"corrupt state", failWith(func(v *tenv) { os.WriteFile(v.stateDir+"/"+stateFile, []byte(v.root+hexMarker), 0o600) })},
	}
	for _, fl := range flows {
		t.Run(fl.name, func(t *testing.T) {
			v := newTenv(t)
			answers := fl.run(v)
			var blobs []string
			for _, a := range answers {
				blobs = append(blobs, string(a))
			}
			blobs = append(blobs, v.journal.String())
			if data, err := os.ReadFile(v.stateDir + "/" + stateFile); err == nil && fl.name != "corrupt state" {
				blobs = append(blobs, string(data))
			}
			ents, _ := os.ReadDir(v.stateDir)
			for _, e := range ents {
				blobs = append(blobs, e.Name())
			}
			for _, b := range blobs {
				low := strings.ToLower(b)
				for _, bad := range append([]string{v.root, v.stateDir, v.cacheDir, os.TempDir(), "/tmp", "/var/", host, runtime.GOOS + "/"}, osErrors...) {
					if bad != "" && strings.Contains(low, strings.ToLower(bad)) {
						t.Errorf("%q appears in %q", bad, b)
					}
				}
				if ip.MatchString(b) {
					t.Errorf("an address appears in %q", b)
				}
			}
			// The marker may appear where it belongs: as the correlation id in the
			// journal, the state file and the last result of a status answer, never
			// in the answer to a request that carried it.
			for _, a := range answers {
				if strings.Contains(string(a), hexMarker) && !strings.Contains(string(a), `"last_result":{`) {
					t.Errorf("an answer echoes the correlation id: %s", a)
				}
			}
		})
	}
}

// Every journal line has exactly the shape of U8.
func TestJournalLineShape(t *testing.T) {
	v := newTenv(t)
	v.stageable("1.1.0")
	wantOK(t, v.request(stageLine("1.1.0", hexMarker)))
	wantOK(t, v.request(discardLine("1.1.0")))
	r := v.release("1.2.0")
	r.setFile("oarfish-update-helper", elfStub("x"))
	v.place("1.2.0", r.build())
	v.stage("1.2.0")
	re := regexp.MustCompile(`^phase=(idle|staging|staged) to=[0-9A-Za-z.-]+ from=[0-9A-Za-z.-]+ cid=[0-9a-f]{16} result=[a-z_]+$`)
	lines := strings.Split(strings.TrimSuffix(v.journal.String(), "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("%d lines:\n%s", len(lines), v.journal.String())
	}
	for _, l := range lines {
		if !re.MatchString(l) {
			t.Errorf("journal line %q", l)
		}
	}
	want := []string{
		"phase=staging to=1.1.0 from=1.0.0 cid=decafbad0badc0de result=ok",
		"phase=staged to=1.1.0 from=1.0.0 cid=decafbad0badc0de result=ok",
		"phase=idle to=1.1.0 from=1.0.0 cid=decafbad0badc0de result=discarded",
		"phase=staging to=1.2.0 from=1.0.0 cid=0123456789abcdef result=ok",
		"phase=idle to=1.2.0 from=1.0.0 cid=0123456789abcdef result=installer_required",
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d = %q, want %q", i, lines[i], w)
		}
	}
}
