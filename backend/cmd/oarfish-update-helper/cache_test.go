package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func openFDs(t *testing.T) int {
	t.Helper()
	ents, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Skip("no /dev/fd")
	}
	return len(ents)
}

// within2s runs stage in a goroutine and fails the test when it does not end
// within two seconds (a FIFO must not hang the helper).
func (v *tenv) stageWithin2s(version string) []byte {
	v.t.Helper()
	done := make(chan []byte, 1)
	go func() { done <- v.stage(version) }()
	select {
	case out := <-done:
		return out
	case <-time.After(2 * time.Second):
		v.t.Fatal("stage did not return within 2 seconds")
		return nil
	}
}

// replaceWith removes path and puts a thing of the given kind there.
func replaceWith(t *testing.T, cacheDir, version, name, kind string) {
	t.Helper()
	dir := filepath.Join(cacheDir, "update", version)
	path := filepath.Join(dir, name)
	outside := shortDir(t)
	os.WriteFile(filepath.Join(outside, "valid"), []byte("a valid file outside the cache"), 0o644)
	data, _ := os.ReadFile(path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	switch kind {
	case "fifo":
		if err := syscall.Mkfifo(path, 0o644); err != nil {
			t.Fatal(err)
		}
	case "symlink inside":
		if err := os.WriteFile(filepath.Join(dir, "real-"+name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("real-"+name, path); err != nil {
			t.Fatal(err)
		}
	case "symlink outside":
		if err := os.WriteFile(filepath.Join(outside, "copy"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(outside, "copy"), path); err != nil {
			t.Fatal(err)
		}
	case "symlink up":
		os.WriteFile(filepath.Join(dir, "..", "..", "..", "up-"+filepath.Base(outside)), data, 0o644)
		if err := os.Symlink("../../../up-"+filepath.Base(outside), path); err != nil {
			t.Fatal(err)
		}
	case "directory":
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	case "socket":
		wd, _ := os.Getwd()
		os.Chdir(dir)
		ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: name, Net: "unix"})
		os.Chdir(wd)
		if err != nil {
			t.Skipf("cannot bind a socket here: %v", err)
		}
		ln.SetUnlinkOnClose(false)
		t.Cleanup(func() { ln.Close() })
	case "missing":
	default:
		t.Fatal(kind)
	}
}

func TestCacheFileKinds(t *testing.T) {
	names := func(version string) []string {
		return []string{nameChecksums, nameSignature, nameManifest, "oarfish_" + version + "_linux_arm64.tar.gz"}
	}
	kinds := []string{"fifo", "symlink inside", "symlink outside", "symlink up", "directory", "socket", "missing"}
	for _, name := range names("1.1.0") {
		for _, kind := range kinds {
			t.Run(name+"/"+kind, func(t *testing.T) {
				v := newTenv(t)
				v.stageable("1.1.0")
				replaceWith(t, v.cacheDir, "1.1.0", name, kind)
				before := openFDs(t)
				wantErrCode(t, v.stageWithin2s("1.1.0"), codeNotFound)
				if after := openFDs(t); after != before {
					t.Fatalf("descriptors: %d before, %d after", before, after)
				}
				v.wantClean()
				if _, ok := v.readState(); ok {
					t.Fatal("the state file was written")
				}
				if ops := v.fs.Ops(); len(ops) != 0 {
					t.Fatalf("mutating calls before the cache check ended: %v", ops)
				}
			})
		}
	}
}

func TestCacheDirectoryLevels(t *testing.T) {
	setup := map[string]func(v *tenv){
		"version directory is a symbolic link to a directory outside": func(v *tenv) {
			outside := shortDir(t)
			real := filepath.Join(v.cacheDir, "update", "1.1.0")
			os.Rename(real, filepath.Join(outside, "1.1.0"))
			os.Symlink(filepath.Join(outside, "1.1.0"), real)
		},
		"version directory is a symbolic link to a directory inside": func(v *tenv) {
			real := filepath.Join(v.cacheDir, "update", "1.1.0")
			os.Rename(real, filepath.Join(v.cacheDir, "update", "other"))
			os.Symlink("other", real)
		},
		"version directory is a relative link up": func(v *tenv) {
			real := filepath.Join(v.cacheDir, "update", "1.1.0")
			os.Rename(real, filepath.Join(v.cacheDir, "moved"))
			os.Symlink("../moved", real)
		},
		"version directory is a regular file": func(v *tenv) {
			real := filepath.Join(v.cacheDir, "update", "1.1.0")
			os.RemoveAll(real)
			os.WriteFile(real, []byte("x"), 0o644)
		},
		"version directory is missing": func(v *tenv) { os.RemoveAll(filepath.Join(v.cacheDir, "update", "1.1.0")) },
		"update directory is a link outside": func(v *tenv) {
			outside := shortDir(t)
			os.Rename(filepath.Join(v.cacheDir, "update"), filepath.Join(outside, "update"))
			os.Symlink(filepath.Join(outside, "update"), filepath.Join(v.cacheDir, "update"))
		},
		"update directory is a link inside": func(v *tenv) {
			os.Rename(filepath.Join(v.cacheDir, "update"), filepath.Join(v.cacheDir, "real"))
			os.Symlink("real", filepath.Join(v.cacheDir, "update"))
		},
		"update directory is a regular file": func(v *tenv) {
			os.RemoveAll(filepath.Join(v.cacheDir, "update"))
			os.WriteFile(filepath.Join(v.cacheDir, "update"), nil, 0o644)
		},
		"update directory is missing": func(v *tenv) { os.RemoveAll(filepath.Join(v.cacheDir, "update")) },
		"cache directory is missing":  func(v *tenv) { os.RemoveAll(v.cacheDir) },
	}
	for name, f := range setup {
		t.Run(name, func(t *testing.T) {
			v := newTenv(t)
			v.stageable("1.1.0")
			f(v)
			before := openFDs(t)
			wantErrCode(t, v.stageWithin2s("1.1.0"), codeNotFound)
			if after := openFDs(t); after != before {
				t.Fatalf("descriptors: %d before, %d after", before, after)
			}
			v.wantClean()
			if _, ok := v.readState(); ok || len(v.fs.Ops()) != 0 {
				t.Fatal("something was written")
			}
		})
	}
}

func TestCacheSizeCaps(t *testing.T) {
	t.Run("small files", func(t *testing.T) {
		for _, name := range []string{nameChecksums, nameSignature, nameManifest} {
			v := newTenv(t)
			v.stageable("1.1.0")
			p := filepath.Join(v.cacheDir, "update", "1.1.0", name)
			if err := os.Truncate(p, maxSmallFile+1); err != nil {
				t.Fatal(err)
			}
			wantErrCode(t, v.stage("1.1.0"), codeChecksumMismatch)
			v.wantClean()
			if len(v.fs.Ops()) != 0 {
				t.Fatalf("%s: wrote before the cap check", name)
			}
		}
	})
	t.Run("a small file of exactly the cap is read", func(t *testing.T) {
		v := newTenv(t)
		v.stageable("1.1.0")
		p := filepath.Join(v.cacheDir, "update", "1.1.0", nameChecksums)
		if err := os.Truncate(p, maxSmallFile); err != nil { // zero-filled, so the signature check fails
			t.Fatal(err)
		}
		wantErrCode(t, v.stage("1.1.0"), codeSignatureInvalid)
		v.wantClean()
	})
	t.Run("bundle", func(t *testing.T) {
		v := newTenv(t)
		_, b := v.stageable("1.1.0")
		p := filepath.Join(v.cacheDir, "update", "1.1.0", b.BundleName)
		if err := os.Truncate(p, maxBundleFile+1); err != nil { // sparse
			t.Fatal(err)
		}
		wantErrCode(t, v.stage("1.1.0"), codeChecksumMismatch)
		v.wantClean()
		if len(v.fs.Ops()) != 0 {
			t.Fatal("wrote before the cap check")
		}
	})
}

// bigRelease is a release whose bundle is larger than two chunks, so that a
// test can act between two chunks of the bundle.
func (v *tenv) bigRelease(version string) (*release, built) {
	r := v.release(version)
	r.setFile("oarfish", append(elfStub("big"), incompressible(100<<10)...))
	b := r.build()
	if len(b.Bundle) < 3*chunk {
		v.t.Fatalf("bundle of %d bytes is too small", len(b.Bundle))
	}
	v.place(version, b)
	return r, b
}

// A file that grows beyond the signed size ends in checksum_mismatch, whether
// the growth happens before the first chunk or between two chunks.
func TestCacheFileGrowsDuringTheCopy(t *testing.T) {
	for _, tt := range []struct {
		file string
		at   int64
		add  int
		want errorCode
	}{
		// Growth that stays under the 64 KiB cap changes the signed content: the
		// signature (checksums.txt, its signature file) or the listed hash (the
		// manifest) fails. Growth over the cap is the size error.
		{nameChecksums, 0, 5, codeSignatureInvalid}, {nameSignature, 0, 5, codeSignatureInvalid}, {nameManifest, 0, 5, codeChecksumMismatch},
		{nameChecksums, 0, maxSmallFile, codeChecksumMismatch}, {nameSignature, 0, maxSmallFile, codeChecksumMismatch},
		{nameManifest, 0, maxSmallFile, codeChecksumMismatch},
		{"bundle", 0, 5, codeChecksumMismatch}, {"bundle", chunk, 5, codeChecksumMismatch}, {"bundle", 2 * chunk, 5, codeChecksumMismatch},
		{"bundle", chunk, 1, codeChecksumMismatch},
	} {
		t.Run(fmt.Sprintf("%s@%d+%d", tt.file, tt.at, tt.add), func(t *testing.T) {
			v := newTenv(t)
			_, b := v.bigRelease("1.1.0")
			name := tt.file
			if name == "bundle" {
				name = b.BundleName
			}
			path := filepath.Join(v.cacheDir, "update", "1.1.0", name)
			v.h.onChunk = func(file string, copied int64) {
				if file == name && copied == tt.at {
					f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
					if err != nil {
						t.Error(err)
						return
					}
					f.Write(make([]byte, tt.add))
					f.Close()
				}
			}
			wantErrCode(t, v.stage("1.1.0"), tt.want)
			v.wantClean()
			if _, ok := v.readState(); ok && v.phase() != phaseIdle {
				t.Fatalf("state %s", v.phase())
			}
			assertNoStageLeft(t, v)
		})
	}
}

func assertNoStageLeft(t *testing.T, v *tenv) {
	t.Helper()
	for _, n := range v.releasesEntries() {
		if strings.HasPrefix(n, ".stage.") || strings.HasPrefix(n, ".trash.") {
			t.Fatalf("left behind: %s", n)
		}
	}
}

// A file that is shortened during the copy fails the hash.
func TestCacheFileShrinksDuringTheCopy(t *testing.T) {
	v := newTenv(t)
	_, b := v.bigRelease("1.1.0")
	path := filepath.Join(v.cacheDir, "update", "1.1.0", b.BundleName)
	v.h.onChunk = func(file string, copied int64) {
		if file == b.BundleName && copied == chunk {
			os.Truncate(path, chunk+5)
		}
	}
	wantErrCode(t, v.stage("1.1.0"), codeChecksumMismatch)
	v.wantClean()
	assertNoStageLeft(t, v)
}

// A file replaced by rename while it is being copied: the helper holds the
// descriptor of the file it opened, so every staged byte is that file's. The
// replacement is never used.
func TestCacheFileReplacedDuringTheCopy(t *testing.T) {
	for _, tt := range []struct {
		file string
		at   int64
	}{
		{nameChecksums, 0}, {nameSignature, 0}, {nameManifest, 0},
		{"bundle", 0}, {"bundle", chunk},
	} {
		t.Run(tt.file, func(t *testing.T) {
			v := newTenv(t)
			rel, b := v.bigRelease("1.1.0")
			name := tt.file
			if name == "bundle" {
				name = b.BundleName
			}
			dir := filepath.Join(v.cacheDir, "update", "1.1.0")
			v.h.onChunk = func(file string, copied int64) {
				if file == name && copied == tt.at {
					repl := filepath.Join(dir, "replacement")
					os.WriteFile(repl, []byte("the replacement, which is something else"), 0o644)
					if err := os.Rename(repl, filepath.Join(dir, name)); err != nil {
						t.Error(err)
					}
				}
			}
			raw := v.stage("1.1.0")
			wantOK(t, raw)
			for _, f := range rel.files {
				if f.name != "oarfish" {
					continue
				}
				got, err := os.ReadFile(filepath.Join(v.root, "releases", "1.1.0", "oarfish"))
				if err != nil || string(got) != string(f.data) {
					t.Fatalf("staged bytes differ from the signed ones: %v", err)
				}
			}
			assertNoStageLeft(t, v)
		})
	}
}

// The cache is hostile only in the ways above: an unreadable file is io_error.
func TestCacheUnreadableFileIsIOError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	v := newTenv(t)
	v.stageable("1.1.0")
	os.Chmod(filepath.Join(v.cacheDir, "update", "1.1.0", nameManifest), 0)
	wantErrCode(t, v.stage("1.1.0"), codeIOError)
	v.wantClean()
}

func TestCacheOpenErrorMapping(t *testing.T) {
	for _, tt := range []struct {
		err  error
		want errorCode
	}{
		{os.ErrNotExist, codeNotFound}, {syscall.ENOENT, codeNotFound}, {syscall.ELOOP, codeNotFound}, {syscall.ENOTDIR, codeNotFound},
		{syscall.EISDIR, codeNotFound}, {syscall.ENXIO, codeNotFound}, {syscall.ENODEV, codeNotFound}, {syscall.EOPNOTSUPP, codeNotFound},
		{errNotRegular, codeNotFound}, {syscall.EACCES, codeIOError}, {syscall.EIO, codeIOError}, {syscall.EMFILE, codeIOError},
	} {
		if got := cacheOpenError(&os.PathError{Op: "openat", Path: "/secret/path", Err: tt.err}); got != tt.want {
			t.Errorf("%v -> %s, want %s", tt.err, got, tt.want)
		}
	}
}

// swapAfterLstat installs hookAfterLstat so that swap runs once, between the
// Lstat and the open of the cache file with the given name, and returns a
// function that reports whether it ran. swap gets the path of the file and what
// Lstat saw. The hook is removed when the test ends.
func swapAfterLstat(t *testing.T, name string, swap func(path string, orig os.FileInfo)) (ran func() bool) {
	t.Helper()
	done := false
	hookAfterLstat = func(root *os.Root, n string) {
		if !strings.HasSuffix(n, "/"+name) || done {
			return
		}
		done = true
		orig, err := root.Lstat(n)
		if err != nil {
			t.Error(err)
			return
		}
		swap(filepath.Join(root.Name(), n), orig)
	}
	t.Cleanup(func() { hookAfterLstat = nil })
	return func() bool { return done }
}

// The name is swapped between the Lstat and the open: a FIFO must not block
// the open (O_NONBLOCK), and neither a link to another file nor another regular
// file is read (the descriptor must be the file that Lstat saw).
//
// The original is kept alive (renamed to a side name) while the replacement is
// created, so the replacement can never get the original's inode: on ext4 a
// removed file's inode is reused at once by the next file created, and the
// same-file check cannot tell the two apart (see
// TestCacheFileSwappedForOtherContent). The test checks that the identity
// really differs before it relies on it.
func TestCacheFileSwappedBetweenLstatAndOpen(t *testing.T) {
	for _, kind := range []string{"fifo", "symlink to another file", "another regular file"} {
		t.Run(kind, func(t *testing.T) {
			v := newTenv(t)
			v.stageable("1.1.0")
			dir := filepath.Join(v.cacheDir, "update", "1.1.0")
			ran := swapAfterLstat(t, nameManifest, func(path string, orig os.FileInfo) {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Error(err)
					return
				}
				if err := os.Rename(path, filepath.Join(dir, "kept")); err != nil {
					t.Error(err)
					return
				}
				switch kind {
				case "fifo":
					if err := syscall.Mkfifo(path, 0o644); err != nil {
						t.Error(err)
					}
				case "symlink to another file":
					if err := os.WriteFile(filepath.Join(dir, "other"), data, 0o644); err != nil {
						t.Error(err)
					}
					if err := os.Symlink("other", path); err != nil {
						t.Error(err)
					}
				case "another regular file":
					if err := os.WriteFile(path, data, 0o644); err != nil {
						t.Error(err)
					}
				}
				now, err := os.Stat(path) // follows the link: what the open will reach
				if err != nil {
					t.Error(err)
					return
				}
				if os.SameFile(orig, now) {
					t.Errorf("the test did not produce a different identity: %s", kind)
				}
			})
			before := openFDs(t)
			wantErrCode(t, v.stageWithin2s("1.1.0"), codeNotFound)
			if !ran() {
				t.Fatal("the hook did not run")
			}
			if after := openFDs(t); after != before {
				t.Fatalf("descriptors: %d before, %d after", before, after)
			}
			v.wantClean()
			if len(v.fs.Ops()) != 0 {
				t.Fatal("wrote after a refused cache file")
			}
		})
	}
}

// The name is swapped between the Lstat and the open for a file with other
// content, by remove and create. The kernel may give the new file the inode of
// the removed one (ext4 does, APFS usually does not), and then the same-file
// check passes: it is defence in depth and cannot detect inode reuse. The test
// therefore asserts no inode relation and no particular error code. What must
// hold on every file system is that the other bytes never become a staged
// release: they are verified against the signed values after the copy.
func TestCacheFileSwappedForOtherContent(t *testing.T) {
	for _, which := range []string{nameChecksums, nameSignature, nameManifest, "bundle"} {
		t.Run(which, func(t *testing.T) {
			v := newTenv(t)
			_, b := v.stageable("1.1.0")
			name := which
			if name == "bundle" {
				name = b.BundleName
			}
			ran := swapAfterLstat(t, name, func(path string, orig os.FileInfo) {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Error(err)
					return
				}
				other := append([]byte(nil), data...)
				other[0] ^= 0xff // same size, other content
				if err := os.Remove(path); err != nil {
					t.Error(err)
					return
				}
				if err := os.WriteFile(path, other, 0o644); err != nil {
					t.Error(err)
				}
			})
			r := parseReply(t, v.stageWithin2s("1.1.0"))
			if !ran() {
				t.Fatal("the hook did not run")
			}
			if r.OK {
				t.Fatal("a swapped file with other content was staged")
			}
			v.wantClean()
			assertNoStageLeft(t, v)
		})
	}
}

// The name is swapped for a FIFO by remove and create. Where the FIFO gets the
// inode of the removed file (ext4 may do that) the same-file check passes and
// only the type check on the opened descriptor refuses it; where it does not,
// the same-file check refuses it. Either way the answer is not_found and the
// open does not block.
func TestCacheFileSwappedForFifoByRemoveAndCreate(t *testing.T) {
	v := newTenv(t)
	v.stageable("1.1.0")
	ran := swapAfterLstat(t, nameManifest, func(path string, orig os.FileInfo) {
		if err := os.Remove(path); err != nil {
			t.Error(err)
			return
		}
		if err := syscall.Mkfifo(path, 0o644); err != nil {
			t.Error(err)
		}
	})
	before := openFDs(t)
	wantErrCode(t, v.stageWithin2s("1.1.0"), codeNotFound)
	if !ran() {
		t.Fatal("the hook did not run")
	}
	if after := openFDs(t); after != before {
		t.Fatalf("descriptors: %d before, %d after", before, after)
	}
	v.wantClean()
}

// A link inside the cache that leads to the very file that was checked is
// accepted, by design: the opened descriptor is the file Lstat saw, so the
// bytes read are the ones that are then verified, and where the name points
// does not matter. The original is renamed (the inode stays) and a link takes
// its place.
func TestCacheLinkToTheCheckedFileIsAccepted(t *testing.T) {
	v := newTenv(t)
	v.stageable("1.1.0")
	dir := filepath.Join(v.cacheDir, "update", "1.1.0")
	ran := swapAfterLstat(t, nameManifest, func(path string, orig os.FileInfo) {
		if err := os.Rename(path, filepath.Join(dir, "real")); err != nil {
			t.Error(err)
			return
		}
		if err := os.Symlink("real", path); err != nil {
			t.Error(err)
			return
		}
		now, err := os.Stat(path)
		if err != nil || !os.SameFile(orig, now) {
			t.Errorf("the link does not lead to the checked file: %v", err)
		}
	})
	wantOK(t, v.stageWithin2s("1.1.0"))
	if !ran() {
		t.Fatal("the hook did not run")
	}
}
