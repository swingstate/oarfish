package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// The release content after v0.1.0-alpha.11 (G6.10 closeout, row F24 of U13,
// owner decision D-C85). The release after v0.1.0-alpha.11 is meant to differ
// from it in exactly two of the five files of the closed list: the helper
// binary (new bytes from a behaviour-neutral change of one start-up message)
// and the recover unit (another value of Description=). The other three files
// stay byte-identical. A GUI update to that release runs the component
// transaction with a new helper and a new unit text, as U13 F24 asks. Class C
// only: nothing here ran on a device.
//
// The test fails when a third file differs from v0.1.0-alpha.11, or when the
// helper or the recover unit no longer differs. Then the release plan has
// changed, and this file is changed with it.

const alpha11UnitDir = "testdata/units-v0.1.0-alpha.11/"

// The SHA-256 of the unit files of the tag v0.1.0-alpha.11 (`git show
// v0.1.0-alpha.11:install/<name>`), so that the copies in testdata cannot drift.
var alpha11UnitSHA256 = map[string]string{
	unitService:       "a68d5eddd1c98166c4252ff0aa2e8f64331b27d1feb92feb8c55ab044812f178",
	unitHelperSocket:  "fe2e27ee7af6165f709f19f50c4f7893f75d6cfe2fbd066793d5f2281aacc634",
	unitHelperService: "fabe9f4b0857746e75ff5ab463a2f430fd1be3d71b1cc61b899f87a9081fae28",
	unitRecover:       "735d1339758b355b9a38dbd05f4ebe58f4e4df6bf770c3c1bd6061b48cb4460e",
}

// The SHA-256 of oarfish-update-helper in the published manifest of
// v0.1.0-alpha.11 (built by scripts/build-helper.sh at the tag commit).
const alpha11HelperSHA256 = "f0b21c6176b97d03c4703c9b6bc40bceed2b80c05e39cd614de1e2e46cb35362"

// The text of the change that gives the helper new bytes (main.go, serve).
const alpha11HelperChange = "cannot determine the uid of the service user oarfish"

// releaseHelperBuild builds the helper of this tree as scripts/build-helper.sh
// does for a release, and checks that the script still uses these flags.
func releaseHelperBuild(t *testing.T) []byte {
	t.Helper()
	script, err := os.ReadFile("../../../scripts/build-helper.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		`LDFLAGS="-s -w"`,
		`CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOFLAGS=-mod=readonly`,
		`go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$1" ./cmd/oarfish-update-helper`,
	} {
		if !bytes.Contains(script, []byte(s)) {
			t.Fatalf("scripts/build-helper.sh no longer contains %q; adapt releaseHelperBuild", s)
		}
	}
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goTool); err != nil {
		goTool = "go"
	}
	out := filepath.Join(t.TempDir(), "oarfish-update-helper")
	cmd := exec.Command(goTool, "build", "-trimpath", "-buildvcs=false", "-ldflags", "-s -w", "-o", out, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=arm64", "GOFLAGS=-mod=readonly")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, b)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestReleaseAfterAlpha11ChangesTheHelperAndTheRecoverUnit(t *testing.T) {
	units := []string{unitHelperSocket, unitHelperService, unitRecover, unitService}
	old, repo := map[string][]byte{}, map[string][]byte{}
	var differ []string

	// The helper binary: built from this tree with the release flags.
	helper := releaseHelperBuild(t)
	if sumHex(helper) != alpha11HelperSHA256 {
		differ = append(differ, "oarfish-update-helper")
	}

	// The four units: install/ against the copies of the tag.
	for _, n := range units {
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
	wantDiffer := []string{"oarfish-update-helper", unitRecover}
	if !slices.Equal(differ, wantDiffer) {
		t.Fatalf("this tree differs from v0.1.0-alpha.11 in %v, want %v", differ, wantDiffer)
	}
	if !bytes.Contains(helper, []byte(alpha11HelperChange)) {
		t.Fatalf("the helper of this tree lacks %q: its bytes differ for another reason than D-C85", alpha11HelperChange)
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

	t.Run("stage reports two files and the transaction replaces them", func(t *testing.T) {
		v := newTenv(t)
		// The installed tree: the units of v0.1.0-alpha.11 and the fixture
		// helper. The release: the units of install/ and other helper bytes.
		// The fixture cannot run the real helper (it embeds the release key),
		// so a stand-in with other bytes plays the new helper.
		for _, n := range units {
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
		newHelper := elfStub("helper " + newVersion)
		r := v.release(newVersion)
		r.setFile("oarfish-update-helper", newHelper)
		for _, n := range units {
			r.setFile(n, repo[n])
		}
		v.place(newVersion, r.build())

		got := wantOK(t, v.stage(newVersion))
		if !got.ComponentsDiffer || !slices.Equal(got.Differs, wantDiffer) {
			t.Fatalf("stage answer: components_differ %v, differs %v; want true and %v", got.ComponentsDiffer, got.Differs, wantDiffer)
		}
		st, _ := v.readState()
		if st.Txn.ComponentsTo != newVersion || st.Txn.ComponentsHelperSHA256 != sumHex(newHelper) || !slices.Equal(st.Txn.Differs, wantDiffer) {
			t.Fatalf("no component set with the new helper was staged: %+v", st.Txn)
		}
		// F32 for this release: without the second confirmation nothing happens.
		wantErrCode(t, v.activate(newVersion), codeConfirmationRequired)

		s := &supervisor{v: v}
		wantOK(t, v.confirm(newVersion))
		s.settle()
		if len(s.restartAt) != 1 || s.recovers != 0 {
			t.Fatalf("restarts %v, recover runs %d", s.restartAt, s.recovers)
		}
		fin := checkSettled(t, v, false)
		if v.current() != newVersion || v.link(linkActive) != newVersion || v.link(linkKnownGood) != newVersion ||
			fin.Last == nil || fin.Last.Outcome != outcomeSuccess {
			t.Fatalf("current %s active %s known-good %s state %+v", v.current(), v.link(linkActive), v.link(linkKnownGood), fin)
		}
		// The set that known-good serves has the new helper and the new recover
		// unit (D2); the three other files are those of v0.1.0-alpha.11; the
		// socket flag of D4 is not set.
		want := map[string][]byte{"oarfish-update-helper": newHelper}
		for _, n := range units {
			want[n] = old[n]
		}
		want[unitRecover] = repo[unitRecover]
		for n, w := range want {
			p := filepath.Join(v.root, "components", linkKnownGood, n)
			if n == unitService {
				p = filepath.Join(v.root, "current", n)
			}
			data, err := os.ReadFile(p)
			if err != nil || !bytes.Equal(data, w) {
				t.Fatalf("after the update %s is not the expected text (%v)", n, err)
			}
		}
		if sr := v.status(); sr.SocketRestartPending {
			t.Fatal("socket_restart_pending set for a change of the helper and the recover unit")
		}
		// The two systemd calls of a component set without oarfish.service:
		// reload by the old helper, reload after the known-good switch (D2),
		// then the restart of the service for the new release.
		if got := strings.Join(v.sd.acting(), ","); got != "daemon-reload,daemon-reload,restart oarfish.service" {
			t.Fatalf("systemctl calls %s", got)
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
