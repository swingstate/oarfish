#!/usr/bin/env bash
# The tooling check of the two-step rule (ADR 0010 U15 rule 3, amendment A15,
# D-C83 item D3): the unit texts of a release must pass the unit lint of the
# update helper of the directly preceding release, because that helper lints
# them when a device stages the release and answers unit_rejected for a text it
# does not know.
#
# The lint of the preceding release is not downloaded and not run as a binary.
# Its source is read from the repository history at the preceding tag
# (backend/ at that tag: the helper package and the two packages it links),
# the helper's own tests are left out, and one test is added that calls that
# source's lintUnit for every unit in that source's lintedUnits, on the unit
# files of the bundle in RELEASE_DIR, as stage does. It is built and run with
# the Go toolchain of this repository, offline (GOPROXY=off, an empty module cache). So:
#   - a change in comments, blank lines, Description= or Documentation= passes,
#     because the lint normalises them away;
#   - a unit text that equals an accepted form of that helper (for
#     oarfish.service: passes its rule table) passes;
#   - any other unit text fails, as it would fail on a device.
# Units of the release that the preceding helper does not lint (for example the
# broker units for a helper before G7.3a) are listed and not judged: that
# helper does not stage them as units. A preceding helper without the lint
# (before G6.10 step a) answers installer_required for every difference; the
# check prints that and passes. A preceding source that cannot be extracted or
# built, or a unit the preceding helper lints that the release lacks, fails
# the check (fail closed).
#
# Usage: scripts/check-unit-forms.sh [--exceptions FILE] [--repo DIR] RELEASE_DIR
#   --exceptions FILE  the owner's exceptions (check unit-forms); the release
#                      workflow passes release/release-exceptions.txt
#   --repo DIR         the repository whose tags and history are read
#                      (default: this repository)
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

REPO="$(repo_root)"
EXCEPTIONS=""
REL=""
while [ $# -gt 0 ]; do
  case "$1" in
    --exceptions) EXCEPTIONS="$2"; shift 2 ;;
    --repo) REPO="$2"; shift 2 ;;
    -*) die "unknown option $1" ;;
    *) [ -z "$REL" ] || die "one RELEASE_DIR only"; REL="$1"; shift ;;
  esac
done
[ -n "$REL" ] || die "usage: $0 [--exceptions FILE] [--repo DIR] RELEASE_DIR"
[ -f "$REL/manifest.json" ] || die "$REL/manifest.json not found"

TAG="$(manifest_tag "$REL/manifest.json")"
VER="${TAG#v}"
BUNDLE="$REL/oarfish_${VER}_linux_arm64.tar.gz"
[ -f "$BUNDLE" ] || die "$BUNDLE not found"
EARLIER="$(release_tags_before "$REPO" "$TAG")"
[ -n "$EARLIER" ] || die "no release tag before $TAG in $REPO"
PREV="$(printf '%s\n' "$EARLIER" | tail -n 1)"
HELPER_DIR=backend/cmd/oarfish-update-helper

echo "unit-forms: $TAG, directly preceding release: $PREV"
if ! git -C "$REPO" cat-file -e "$PREV:$HELPER_DIR/unitlint.go" 2>/dev/null; then
  echo "unit-forms: the helper of $PREV has no unit lint (before G6.10 step a); it answers installer_required for every difference, so the two-step rule does not apply"
  echo "unit-forms: PASS (not comparable)"
  exit 0
fi

TMP="$(mktemp -d)"
trap 'chmod -R u+w "$TMP" 2>/dev/null; rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bundle" "$TMP/src"
tar -xzf "$BUNDLE" -C "$TMP/bundle"
UNITS="$TMP/bundle/oarfish_${VER}_linux_arm64"
[ -d "$UNITS" ] || die "$BUNDLE has no directory oarfish_${VER}_linux_arm64"

git -C "$REPO" archive --format=tar "$PREV" backend | tar -xf - -C "$TMP/src" \
  || die "cannot read backend/ at $PREV"
rm -f "$TMP/src/$HELPER_DIR"/*_test.go
cat > "$TMP/src/$HELPER_DIR/zz_release_units_test.go" <<'GO'
package main

// Added by scripts/check-unit-forms.sh to the source of the preceding
// release's update helper. It runs that helper's own unit lint over the unit
// files of the release being built, in the order and with the function that
// stage uses (lintedUnits, lintUnit), and writes one line per unit.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseUnitsPassThePrecedingLint(t *testing.T) {
	dir := os.Getenv("OARFISH_RELEASE_UNITS")
	var b strings.Builder
	linted := map[string]bool{}
	for _, name := range lintedUnits {
		linted[name] = true
		text, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			fmt.Fprintf(&b, "missing %s\n", name)
			continue
		}
		if res := lintUnit(name, text); res.Accepted {
			fmt.Fprintf(&b, "accepted %s\n", name)
		} else {
			fmt.Fprintf(&b, "rejected %s %s\n", name, res.Reason)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		n := e.Name()
		if (strings.HasSuffix(n, ".service") || strings.HasSuffix(n, ".socket")) && !linted[n] {
			fmt.Fprintf(&b, "unlinted %s\n", n)
		}
	}
	if err := os.WriteFile(os.Getenv("OARFISH_LINT_RESULT"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}
GO

# The test writes the result file as its last step, so a result file means the
# lint ran over every unit; without one the source did not build or the test
# did not finish.
# The Go toolchain is the one this repository's backend/ selects (go.mod), so
# the old source is built as the helper of this release is; GOTOOLCHAIN=local
# keeps it from downloading another one.
GO="$(cd "$(repo_root)/backend" && go env GOROOT)/bin/go"
[ -x "$GO" ] || die "no Go toolchain for $(repo_root)/backend"
RESULT="$TMP/result.txt"
(cd "$TMP/src/backend" && GOWORK=off GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=readonly \
  GOMODCACHE="$TMP/modcache" OARFISH_RELEASE_UNITS="$UNITS" OARFISH_LINT_RESULT="$RESULT" \
  "$GO" test -count=1 -run '^TestReleaseUnitsPassThePrecedingLint$' "./${HELPER_DIR#backend/}") > "$TMP/go.log" 2>&1 || true
if [ ! -f "$RESULT" ]; then
  tail -n 20 "$TMP/go.log" >&2
  die "unit-forms: FAIL: cannot build or run the unit lint of the helper of $PREV from the repository history (the check fails closed)"
fi

bad=()
while read -r what name reason; do
  case "$what" in
    accepted) echo "unit-forms: $name: accepted by the unit lint of the helper of $PREV" ;;
    rejected) echo "unit-forms: $name: REJECTED by the unit lint of the helper of $PREV ($reason)"; bad+=("$name ($reason)") ;;
    missing) echo "unit-forms: $name: MISSING from $TAG, but the helper of $PREV lints it"; bad+=("$name (missing)") ;;
    unlinted) echo "unit-forms: $name: not linted by the helper of $PREV (not a unit of its component set), not judged" ;;
    *) die "unit-forms: FAIL: unexpected result line '$what $name $reason'" ;;
  esac
done < "$RESULT"
grep -q '^accepted \|^rejected \|^missing ' "$RESULT" \
  || die "unit-forms: FAIL: the helper of $PREV linted no unit (the check fails closed)"

if [ ${#bad[@]} -eq 0 ]; then
  echo "unit-forms: PASS: every unit that the helper of $PREV lints passes its lint"
  exit 0
fi
if [ -n "$EXCEPTIONS" ]; then
  release_exception "$EXCEPTIONS" "$TAG" unit-forms
  if [ -n "$EXCEPTION_REASON" ]; then
    echo "unit-forms: EXCEPTION: ${bad[*]} would be refused (unit_rejected) by the helper of $PREV; passed only because $EXCEPTIONS names $TAG: $EXCEPTION_REASON (a device on $PREV needs one installer run; the release notes must say so)"
    exit 0
  fi
fi
die "unit-forms: FAIL: ${bad[*]} of $TAG would be refused by the helper of $PREV (unit_rejected on a device that stages it). Two-step rule (ADR 0010 U15 rule 3, A15, D-C83): first release a helper that lists the new form in unitForms (or the row in serviceRules), then change the unit (docs/RELEASING.md)"
