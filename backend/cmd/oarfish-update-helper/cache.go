package main

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"syscall"

	"github.com/swingstate/oarfish/backend/internal/relverify"
)

// Cache access, ADR 0010 U3 step 1. The cache belongs to the unprivileged
// service, so everything in it is hostile input: the helper opens it through
// os.Root (a link can not lead out of the cache), opens each file with
// O_NONBLOCK|O_NOFOLLOW|O_CLOEXEC (a FIFO can not block the open) after an
// Lstat that refuses a link (openRegular explains why O_NOFOLLOW alone is not
// enough) and requires a regular file on the opened descriptor.

const (
	cacheUpdateDir = "update"
	// maxSmallFile is the cap for checksums.txt, checksums.txt.sig and
	// manifest.json (U3).
	maxSmallFile = 64 << 10
	// maxBundleFile is the cap for the bundle while the manifest is not known
	// (U3, ORB-NFR-005).
	maxBundleFile = 40_000_000

	nameChecksums = "checksums.txt"
	nameSignature = "checksums.txt.sig"
	nameManifest  = relverify.ManifestName
)

// cacheFile is one opened and type-checked cache file.
type cacheFile struct {
	name string
	f    *os.File
	size int64
	cap  int64
}

// cacheSet is the four files of a release, opened.
type cacheSet struct {
	checksums, signature, manifest, bundle cacheFile
}

func (c *cacheSet) all() []*cacheFile {
	return []*cacheFile{&c.checksums, &c.signature, &c.manifest, &c.bundle}
}

func (c *cacheSet) close() {
	for _, f := range c.all() {
		if f.f != nil {
			f.f.Close()
			f.f = nil
		}
	}
}

// totalSize is the sum of the four sizes found by fstat.
func (c *cacheSet) totalSize() int64 {
	var n int64
	for _, f := range c.all() {
		n += f.size
	}
	return n
}

// cacheOpenError maps a failure of the traversal or of the open to an error
// code. A file that is missing, not a regular file, a link (also ELOOP and an
// escape from the root), a directory, a device or a socket (ENXIO, ENODEV, EOPNOTSUPP), and a
// path element that is not a directory are not_found; everything else is
// io_error.
func cacheOpenError(err error) errorCode {
	switch {
	case errors.Is(err, errNotRegular), errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR), errors.Is(err, syscall.ELOOP),
		errors.Is(err, syscall.EISDIR), errors.Is(err, syscall.ENXIO), errors.Is(err, syscall.ENODEV),
		errors.Is(err, syscall.EOPNOTSUPP), errors.Is(err, syscall.EMLINK):
		return codeNotFound
	}
	// os.Root reports an escape from the root with an error whose text, not its
	// type, says so; the text is only compared here and never returned.
	if strings.Contains(err.Error(), "path escapes from parent") {
		return codeNotFound
	}
	return codeIOError
}

// openCache opens the four files of a version. All four are opened and
// checked before anything is written (U3). On any error nothing stays open.
func openCache(cacheDir, version string) (*cacheSet, errorCode) {
	top, err := os.OpenRoot(cacheDir)
	if err != nil {
		return nil, cacheOpenError(err)
	}
	defer top.Close()
	// os.Root follows a link that stays inside the root, so the two directory
	// levels are checked with Lstat as well: a link there is refused whatever it
	// points to.
	if code := requireDir(top, cacheUpdateDir); code != "" {
		return nil, code
	}
	upd, err := top.OpenRoot(cacheUpdateDir)
	if err != nil {
		return nil, cacheOpenError(err)
	}
	defer upd.Close()
	if code := requireDir(upd, version); code != "" {
		return nil, code
	}
	v, err := relverify.ParseVersion(version)
	if err != nil {
		return nil, codeBadRequest
	}
	set := &cacheSet{
		checksums: cacheFile{name: nameChecksums, cap: maxSmallFile},
		signature: cacheFile{name: nameSignature, cap: maxSmallFile},
		manifest:  cacheFile{name: nameManifest, cap: maxSmallFile},
		bundle:    cacheFile{name: relverify.BundleFileName(v), cap: maxBundleFile},
	}
	for _, cf := range set.all() {
		if code := openCacheFile(upd, version, cf); code != "" {
			set.close()
			return nil, code
		}
	}
	return set, ""
}

func requireDir(r *os.Root, name string) errorCode {
	fi, err := r.Lstat(name)
	if err != nil {
		return cacheOpenError(err)
	}
	if !fi.IsDir() {
		return codeNotFound
	}
	return ""
}

func openCacheFile(upd *os.Root, version string, cf *cacheFile) errorCode {
	f, fi, err := openRegular(upd, version+"/"+cf.name)
	if err != nil {
		return cacheOpenError(err)
	}
	if fi.Size() > cf.cap {
		f.Close()
		return codeChecksumMismatch
	}
	cf.f, cf.size = f, fi.Size()
	return ""
}
