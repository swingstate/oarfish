package main

import (
	"crypto/sha256"
	"io"
	"os"
)

// The component comparison (ADR 0010 U5 "releases that change the helper or a
// unit", U2 closed list, A20). A release whose component files (the helper,
// the broker and their units) differ from the installed ones brings a component
// set and is applied by the component transaction of U15; a difference of
// oarfish.service alone needs the second confirmation and no component set
// (D6). Since G6.10 step b stage no longer answers installer_required for a
// difference (A16, E1). The staged file is read from the staging tree; the
// installed component files are read through components/active and the
// installed oarfish.service through current/oarfish.service.

// componentFile is one file of the component set: its name from the closed
// list of U2 and whether it is a unit text that the lint checks (U15 rule 3) or
// a program, which is compared by its digest only.
type componentFile struct {
	name string
	unit bool
}

// componentSet is the closed, ordered list of the replaceable component files
// of U2 (A20): the update helper and its three units, then the privilege
// broker and its two units (A19.1). The order is the order of the reference
// hash (E2) and of the record of D6.
var componentSet = []componentFile{
	{helperBinary, false},
	{unitHelperSocket, true},
	{unitHelperService, true},
	{unitRecover, true},
	{brokerBinary, false},
	{unitBrokerSocket, true},
	{unitBrokerService, true},
}

// The two programs of the component set.
const (
	helperBinary = "oarfish-update-helper"
	brokerBinary = "oarfish-broker"
)

// componentNames is the closed, ordered list of the compared files of U5: the
// seven files of the component set and oarfish.service, which travels with the
// release.
var componentNames = func() []string {
	out := make([]string, 0, len(componentSet)+1)
	for _, f := range componentSet {
		out = append(out, f.name)
	}
	return append(out, unitService)
}()

// digest is the SHA-256 of one file.
type digest = [sha256.Size]byte

const maxCompareBytes = 128 << 20

// fileDigest streams the SHA-256 of a regular file below root through a
// bounded buffer. Opening is O_NONBLOCK|O_NOFOLLOW so that a FIFO can not
// block it and a final link is not followed.
func fileDigest(root *os.Root, name string) (digest, error) {
	var sum digest
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

// comparison is the result of compareComponents.
type comparison struct {
	// differs are the differing names, in the fixed order of the closed list.
	differs []string
	// set: one of the files of the component set differs, so the release
	// brings a component set (U15 rule 2); oarfish.service alone is not one (D6).
	set bool
	// staged are the digests of the staged files of the component set in the
	// order of componentSet, helper the digest of the staged helper binary
	// (D1) and service the digest of the staged oarfish.service.
	staged  []digest
	helper  digest
	service digest
}

// compareComponents compares the eight files of componentNames. It returns the
// differing names in the fixed order, or components_damaged when an installed
// file can not be read (the active set is unreadable or incomplete; a set of
// the four-file shape before A20 is incomplete). Reading the installed side
// first means a damaged set is reported before a difference.
func (r *stageRun) compareComponents(stageDir string) (comparison, errorCode) {
	var c comparison
	installed := make([]digest, len(componentNames))
	for i, n := range componentNames {
		path := "components/active/" + n
		if n == unitService {
			path = "current/" + n
		}
		sum, err := fileDigest(r.e.opt, path)
		if err != nil {
			return c, codeComponentsDamaged
		}
		installed[i] = sum
	}
	for i, n := range componentNames {
		path := stageDir + "/" + dirComponents + "/" + n
		if n == unitService {
			path = stageDir + "/" + dirRelease + "/" + n
		}
		sum, err := fileDigest(r.e.opt, path)
		if err != nil {
			return c, codeIOError
		}
		inSet := n != unitService
		if n == helperBinary {
			c.helper = sum
		}
		if inSet {
			c.staged = append(c.staged, sum)
		} else {
			c.service = sum
		}
		if sum != installed[i] {
			c.differs = append(c.differs, n)
			c.set = c.set || inSet
		}
	}
	return c, ""
}
