package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The release content after v0.1.0-alpha.11 (G6.10 closeout, row F24 of U13).
// The release after v0.1.0-alpha.11 is meant to differ from it in exactly one
// of the five files of the closed list: the value of Description= in the
// recover unit. The helper bytes stay the same. A GUI update to that release
// runs the component transaction with the production key, but it does not
// prove a new helper binary (U13 F24 asks for both). Class C only: nothing here
// ran on a device.
//
// The test fails when another unit of install/ differs from v0.1.0-alpha.11.
// Then the release plan has changed, and this file is changed with it.

const alpha11UnitDir = "testdata/units-v0.1.0-alpha.11/"

// The SHA-256 of the unit files of the tag v0.1.0-alpha.11 (`git show
// v0.1.0-alpha.11:install/<name>`), so that the copies in testdata cannot drift.
var alpha11UnitSHA256 = map[string]string{
	unitService:       "a68d5eddd1c98166c4252ff0aa2e8f64331b27d1feb92feb8c55ab044812f178",
	unitHelperSocket:  "fe2e27ee7af6165f709f19f50c4f7893f75d6cfe2fbd066793d5f2281aacc634",
	unitHelperService: "fabe9f4b0857746e75ff5ab463a2f430fd1be3d71b1cc61b899f87a9081fae28",
	unitRecover:       "735d1339758b355b9a38dbd05f4ebe58f4e4df6bf770c3c1bd6061b48cb4460e",
}

func TestReleaseAfterAlpha11ChangesOnlyTheRecoverUnit(t *testing.T) {
	names := []string{unitHelperSocket, unitHelperService, unitRecover, unitService}
	old, repo := map[string][]byte{}, map[string][]byte{}
	var differ []string
	for _, n := range names {
		data, err := os.ReadFile(alpha11UnitDir + n)
		if err != nil {
			t.Fatal(err)
		}
		if got := sumHex(data); got != alpha11UnitSHA256[n] {
			t.Fatalf("%s%s has SHA-256 %s, not the one of v0.1.0-alpha.11", alpha11UnitDir, n, got)
		}
		old[n], repo[n] = data, []byte(installUnit(t, n))
		if !bytes.Equal(old[n], repo[n]) {
			differ = append(differ, n)
		}
	}
	if !slices.Equal(differ, []string{unitRecover}) {
		t.Fatalf("install/ differs from v0.1.0-alpha.11 in %v, want %s alone", differ, unitRecover)
	}

	t.Run("the lint of the installed helper accepts the change", func(t *testing.T) {
		wantAccepted(t, unitRecover, string(repo[unitRecover]))
		// The difference is inside what the lint ignores (comments,
		// Description=, Documentation=): the normalised texts are equal, so the
		// unit still matches the same accepted form, and a helper that knows
		// only that form accepts it.
		lo, ro := mustParse(t, unitRecover, old[unitRecover]), mustParse(t, unitRecover, repo[unitRecover])
		if normalisedUnit(lo) != normalisedUnit(ro) {
			t.Fatal("the recover unit differs in more than comments, Description= or Documentation=")
		}
		if normalisedUnit(ro) != helperForms[unitRecover][0] {
			t.Fatal("the recover unit is not the first accepted form")
		}
	})

	t.Run("stage reports one file and the transaction replaces it", func(t *testing.T) {
		v := newTenv(t)
		// The installed tree: the units of v0.1.0-alpha.11 and the fixture
		// helper. The release: the units of install/ and the same helper bytes.
		for _, n := range names {
			p := filepath.Join(v.root, "components", v.installed, n)
			if n == unitService {
				p = filepath.Join(v.root, "releases", v.installed, n)
			}
			if err := os.WriteFile(p, old[n], 0o644); err != nil {
				t.Fatal(err)
			}
		}
		v.installOarfish()
		v.fakeSystemd()
		v.h.wrapFS = withoutSync
		r := v.release(newVersion)
		for _, n := range names {
			r.setFile(n, repo[n])
		}
		v.place(newVersion, r.build())

		got := wantOK(t, v.stage(newVersion))
		if !got.ComponentsDiffer || !slices.Equal(got.Differs, []string{unitRecover}) {
			t.Fatalf("stage answer: components_differ %v, differs %v; want true and [%s]", got.ComponentsDiffer, got.Differs, unitRecover)
		}
		if st, _ := v.readState(); st.Txn.ComponentsTo != newVersion || st.Txn.ComponentsHelperSHA256 != sumHex(baseComponents["oarfish-update-helper"]) {
			t.Fatalf("no component set with the unchanged helper was staged: %+v", st.Txn)
		}
		// F32 for this release: without the second confirmation nothing happens.
		wantErrCode(t, v.activate(newVersion), codeConfirmationRequired)

		s := &supervisor{v: v}
		wantOK(t, v.confirm(newVersion))
		s.settle()
		if len(s.restartAt) != 1 || s.recovers != 0 {
			t.Fatalf("restarts %v, recover runs %d", s.restartAt, s.recovers)
		}
		// checkSettled assumes that a new set has new helper bytes, so the
		// invariant is checked here with the parts that apply.
		checkDisk(t, v)
		fin, _ := v.readState()
		if fin.Txn.Phase != phaseIdle || v.current() != newVersion || v.link(linkActive) != newVersion || v.link(linkKnownGood) != newVersion ||
			fin.Last == nil || fin.Last.Outcome != outcomeSuccess || !fin.Last.ComponentsUpdated ||
			fin.Txn.KnownGoodID != newVersion || fin.Txn.KnownGoodSHA256 != diskSetDigest(t, v, newVersion) || fin.ComponentsPrevious != v.installed {
			t.Fatalf("current %s active %s known-good %s state %+v", v.current(), v.link(linkActive), v.link(linkKnownGood), fin)
		}
		// The recover unit that known-good serves is the new text (D2); the
		// helper is the same bytes; the socket flag of D4 is not set.
		for n, want := range map[string][]byte{
			unitRecover:             repo[unitRecover],
			"oarfish-update-helper": baseComponents["oarfish-update-helper"],
		} {
			data, err := os.ReadFile(filepath.Join(v.root, "components", linkKnownGood, n))
			if err != nil || !bytes.Equal(data, want) {
				t.Fatalf("known-good %s is not the release's (%v)", n, err)
			}
		}
		if sr := v.status(); sr.SocketRestartPending {
			t.Fatal("socket_restart_pending set for a change of the recover unit")
		}
	})
}

func mustParse(t *testing.T, name string, text []byte) []unitLine {
	t.Helper()
	lines, reason := parseUnit(unitKinds[name], text)
	if reason != "" {
		t.Fatalf("%s: %s", name, reason)
	}
	return lines
}
