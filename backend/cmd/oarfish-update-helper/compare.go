package main

import (
	"crypto/sha256"
	"io"
	"os"
)

// The component comparison (ADR 0010 U5 "releases that change the helper or a
// unit", U2 closed list). In G6.5 a release whose helper files or
// oarfish.service differ from the installed ones is not applied: stage answers
// installer_required with the names. The staged file is read from the staging
// tree; the installed helper files are read through components/active and the
// installed oarfish.service through current/oarfish.service.

// componentNames is the closed, ordered list of U2: the four replaceable
// component files and oarfish.service, which travels with the release.
var componentNames = []string{
	"oarfish-update-helper",
	"oarfish-update-helper.socket",
	"oarfish-update-helper.service",
	"oarfish-update-recover.service",
	"oarfish.service",
}

const maxCompareBytes = 128 << 20

// fileDigest streams the SHA-256 of a regular file below root through a
// bounded buffer. Opening is O_NONBLOCK|O_NOFOLLOW so that a FIFO can not
// block it and a final link is not followed.
func fileDigest(root *os.Root, name string) ([sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	f, fi, err := openRegular(root, name)
	if err != nil {
		return sum, err
	}
	defer f.Close()
	if fi.Size() > maxCompareBytes {
		return sum, errNotRegular
	}
	h := sha256.New()
	buf := make([]byte, chunk)
	if _, err := io.CopyBuffer(h, io.LimitReader(f, maxCompareBytes+1), buf); err != nil {
		return sum, err
	}
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

// compareComponents compares the five files. It returns the differing names in
// the fixed order, or components_damaged when an installed file can not be
// read (the active set is unreadable or incomplete). Reading the installed
// side first means a damaged set is reported before a difference.
func (r *stageRun) compareComponents(stageDir string) ([]string, errorCode) {
	installed := make([][sha256.Size]byte, len(componentNames))
	for i, n := range componentNames {
		path := "components/active/" + n
		if n == "oarfish.service" {
			path = "current/" + n
		}
		sum, err := fileDigest(r.e.opt, path)
		if err != nil {
			return nil, codeComponentsDamaged
		}
		installed[i] = sum
	}
	var differs []string
	for i, n := range componentNames {
		path := stageDir + "/" + dirComponents + "/" + n
		if n == "oarfish.service" {
			path = stageDir + "/" + dirRelease + "/" + n
		}
		sum, err := fileDigest(r.e.opt, path)
		if err != nil {
			return nil, codeIOError
		}
		if sum != installed[i] {
			differs = append(differs, n)
		}
	}
	return differs, ""
}
