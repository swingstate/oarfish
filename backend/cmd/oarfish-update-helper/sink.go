package main

import (
	"errors"
	"os"

	"github.com/swingstate/oarfish/backend/internal/relverify"
)

// The staging sink implements relverify.Sink (ADR 0010 U5 extraction rules).
// It writes below .stage.<id>: the four release files into release/, the four
// component files into components/, by a fixed table of names. Everything it
// writes is unusable after any error of Inspect; the caller removes the whole
// staging directory.

const (
	dirRelease    = "release"
	dirComponents = "components"
)

// sinkTargets is the fixed table: file name to directory below .stage.<id>.
// It is the allow-list of ADR 0010 U5 (and relverify's layout).
var sinkTargets = map[string]string{
	"oarfish":                 dirRelease,
	"oarfish.service":         dirRelease,
	"LICENSE":                 dirRelease,
	"THIRD_PARTY_NOTICES.txt": dirRelease,

	"oarfish-update-helper":          dirComponents,
	"oarfish-update-helper.socket":   dirComponents,
	"oarfish-update-helper.service":  dirComponents,
	"oarfish-update-recover.service": dirComponents,
}

var (
	errSinkName = errors.New("name not in the staging table")
	errSinkMode = errors.New("mode is not 0644 or 0755")
	errSinkSize = errors.New("file larger than its manifest size")
	errSinkDup  = errors.New("file created twice")
	errDeadline = errors.New("request deadline exceeded")
)

type stagingSink struct {
	r       *stageRun
	dir     string // releases/.stage.<id>
	created map[string]bool
	open    *sinkFile
}

func newStagingSink(r *stageRun, dir string) *stagingSink {
	return &stagingSink{r: r, dir: dir, created: map[string]bool{}}
}

// Create makes the file with O_EXCL and the manifest mode, set by fchmod and
// never left to the umask. The name must be in the fixed table.
func (s *stagingSink) Create(name string, mode uint32, size int64) (relverify.File, error) {
	sub, ok := sinkTargets[name]
	if !ok {
		return nil, errSinkName
	}
	if mode != 0o644 && mode != 0o755 {
		return nil, errSinkMode
	}
	if s.created[name] {
		return nil, errSinkDup
	}
	if s.r.expired() {
		return nil, errDeadline
	}
	f, err := s.r.e.fs.Create(treeOpt, s.dir+"/"+sub+"/"+name, os.FileMode(mode))
	if err != nil {
		return nil, err
	}
	s.created[name] = true
	sf := &sinkFile{s: s, f: f, size: size}
	s.open = sf
	return sf, nil
}

// abort closes the file that was open when Inspect failed. It does not sync.
func (s *stagingSink) abort() {
	if s.open != nil && s.open.f != nil {
		s.open.f.Close()
		s.open.f = nil
	}
}

type sinkFile struct {
	s    *stagingSink
	f    wfile
	size int64
	n    int64
}

// Write counts the bytes and never writes more than the manifest size.
func (f *sinkFile) Write(p []byte) (int, error) {
	if f.s.r.expired() {
		return 0, errDeadline
	}
	if f.n+int64(len(p)) > f.size {
		return 0, errSinkSize
	}
	n, err := f.f.Write(p)
	f.n += int64(n)
	return n, err
}

// Finish syncs and closes. Inspect calls it only for a file whose size and
// hash match the manifest.
func (f *sinkFile) Finish() error {
	if f.n != f.size {
		return errSinkSize
	}
	if err := f.f.Sync(); err != nil {
		return err
	}
	err := f.f.Close()
	f.f = nil
	return err
}
