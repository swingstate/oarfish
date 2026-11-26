#!/usr/bin/env bash
# Fixture tests of the two release history checks: scripts/check-component-history.sh
# (D-C92) and scripts/check-unit-forms.sh (the two-step rule). The comparison
# rule of D-C92 has its own Go tests (scripts/bundlegen/history_test.go); this
# script tests what the shell adds: the release order from the tags, the
# exceptions file, the download path, and the lint of the preceding tag read
# from the history.
#
# Everything is synthetic: throwaway git repositories in a temporary
# directory, fake manifests with digests of labels, and a fake helper package
# whose lint accepts listed texts. Nothing is downloaded, except one attempt
# against a closed local port, which must fail the check.
#
# Usage: scripts/test-release-history.sh
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
ROOT="$(repo_root)"

TMP="$(mktemp -d)"
trap 'chmod -R u+w "$TMP" 2>/dev/null; rm -rf "$TMP"' EXIT
unset GH_TOKEN GITHUB_TOKEN

pass=0
fail=0
ok() { echo "ok   $*"; pass=$((pass + 1)); }
bad() { echo "FAIL $*"; fail=$((fail + 1)); }

# expect pass|fail NAME PATTERN CMD...: runs CMD, checks the exit status and
# that the output (stdout and stderr) contains PATTERN (a fixed string).
expect() {
  local want="$1" name="$2" pattern="$3" out status=0
  shift 3
  out="$("$@" 2>&1)" || status=$?
  if { [ "$want" = pass ] && [ "$status" -ne 0 ]; } || { [ "$want" = fail ] && [ "$status" -eq 0 ]; }; then
    bad "$name: exit status $status, want $want"; printf '%s\n' "$out" | sed 's/^/     /'; return
  fi
  if ! grep -qF -- "$pattern" <<< "$out"; then
    bad "$name: output lacks '$pattern'"; printf '%s\n' "$out" | sed 's/^/     /'; return
  fi
  ok "$name"
}

hex_of() { printf '%s' "$1" > "$TMP/label"; sha256_of "$TMP/label"; }

# fake_manifest FILE TAG SHAPE SET: a manifest in the format bundlegen writes
# (two-space indent, the "tag" line), with SHAPE legacy, four or seven, and
# component digests derived from SET.
fake_manifest() {
  local file="$1" tag="$2" shape="$3" set="$4" root names="" n first=1
  root="oarfish_${tag#v}_linux_arm64"
  case "$shape" in
    four) names="oarfish-update-helper oarfish-update-helper.socket oarfish-update-helper.service oarfish-update-recover.service" ;;
    seven) names="oarfish-update-helper oarfish-update-helper.socket oarfish-update-helper.service oarfish-update-recover.service oarfish-broker oarfish-broker.socket oarfish-broker.service" ;;
  esac
  mkdir -p "$(dirname "$file")"
  {
    printf '{\n  "schema_version": 1,\n  "tag": "%s",\n  "contents": [\n' "$tag"
    for n in oarfish oarfish.service $names; do
      [ $first -eq 1 ] || printf ',\n'
      first=0
      printf '    {"path": "%s/%s", "mode": "0644", "size": 1, "sha256": "%s"}' "$root" "$n" "$(hex_of "$set $n")"
    done
    printf '\n  ]'
    [ "$shape" = legacy ] || printf ',\n  "components": {"key_id": "0123456789abcdef", "protocol": 1}'
    printf '\n}\n'
  } > "$file"
}

gitq() { git -C "$1" -c user.name=test -c user.email=test@example.invalid "${@:2}" >/dev/null; }

# new_repo DIR: an empty repository with one commit.
new_repo() {
  mkdir -p "$1"
  git -C "$1" init -q
  GIT_COMMITTER_DATE="2026-01-01T00:00:00Z" GIT_AUTHOR_DATE="2026-01-01T00:00:00Z" \
    gitq "$1" commit -q --allow-empty -m init
}

# tag_at DIR TAG N: a commit dated N minutes after the first, tagged TAG
# (lightweight, so the creation date is the commit date).
tag_at() {
  local d
  d="$(printf '2026-01-01T00:%02d:00Z' "$3")"
  GIT_COMMITTER_DATE="$d" GIT_AUTHOR_DATE="$d" gitq "$1" commit -q --allow-empty -m "$2"
  gitq "$1" tag "$2"
}

echo "== component set (D-C92)"
CH="$ROOT/scripts/check-component-history.sh"
R="$TMP/repo-b"
M="$TMP/manifests"
new_repo "$R"
tag_at "$R" v0.1.0 1; fake_manifest "$M/v0.1.0/manifest.json" v0.1.0 legacy ""
tag_at "$R" v0.1.1 2; fake_manifest "$M/v0.1.1/manifest.json" v0.1.1 four A
tag_at "$R" v0.1.9 3; fake_manifest "$M/v0.1.9/manifest.json" v0.1.9 seven X
# Created after v0.1.9, sorted before it by name: the order is the creation.
tag_at "$R" v0.1.10 4; fake_manifest "$M/v0.1.10/manifest.json" v0.1.10 seven Y
# Same set as v0.1.10 (unchanged).
tag_at "$R" v0.1.11 5; fake_manifest "$M/v0.1.11/manifest.json" v0.1.11 seven Y

rel() { mkdir -p "$TMP/rel/$1"; fake_manifest "$TMP/rel/$1/manifest.json" "$1" seven "$2"; echo "$TMP/rel/$1"; }
EX="$TMP/exceptions.txt"
printf '# none\n' > "$EX"

expect pass "dry run, new set" "PASS: v0.2.0 brings a set that no earlier release brought (3 compared)" \
  "$CH" --repo "$R" --manifests-dir "$M" --exceptions "$EX" "$(rel v0.2.0 Z)"
expect pass "dry run, the set of the preceding release" "PASS: v0.2.1 brings the set of the preceding release v0.1.11 unchanged" \
  "$CH" --repo "$R" --manifests-dir "$M" "$(rel v0.2.1 Y)"
expect pass "legacy and four-file releases are skipped with a line" "v0.1.1: skipped, four-file component set" \
  "$CH" --repo "$R" --manifests-dir "$M" "$(rel v0.2.2 Z)"
expect fail "the set of an older release fails" "FAIL: v0.2.3 brings the component set of v0.1.9, which is not the directly preceding release v0.1.11" \
  "$CH" --repo "$R" --manifests-dir "$M" --exceptions "$EX" "$(rel v0.2.3 X)"
# The release being built is a tag of the repository: only the tags before it
# count, and the order is the creation (v0.1.10 was created after v0.1.9 and
# sorts before it by name).
expect pass "a tagged release compares only with the tags before it" "directly preceding release: v0.1.9" \
  "$CH" --repo "$R" --manifests-dir "$M" "$(rel v0.1.10 X)"
expect pass "the preceding release is the one created last (v0.1.10, not v0.1.9)" "PASS: v0.1.11 brings the set of the preceding release v0.1.10 unchanged" \
  "$CH" --repo "$R" --manifests-dir "$M" "$(rel v0.1.11 Y)"
expect fail "a tagged release with the set of an older one fails" "is not the directly preceding release v0.1.10" \
  "$CH" --repo "$R" --manifests-dir "$M" "$(rel v0.1.11 X)"

printf 'v0.1.11 component-set revert of a faulty helper fix, owner approval in chat 2026-01-02\n' > "$TMP/ex-b.txt"
expect pass "the exception for this tag passes and is printed" "EXCEPTION: v0.1.11 brings the set of v0.1.9" \
  "$CH" --repo "$R" --manifests-dir "$M" --exceptions "$TMP/ex-b.txt" "$(rel v0.1.11 X)"
expect pass "the reason of the exception is printed" "exception for v0.1.11 in $TMP/ex-b.txt: revert of a faulty helper fix" \
  "$CH" --repo "$R" --manifests-dir "$M" --exceptions "$TMP/ex-b.txt" "$(rel v0.1.11 X)"
printf 'v0.1.11 unit-forms another check\nv0.1.12 component-set another tag\n' > "$TMP/ex-other.txt"
expect fail "an exception for another check or another tag does not apply" "FAIL: v0.1.11 brings the component set of v0.1.9" \
  "$CH" --repo "$R" --manifests-dir "$M" --exceptions "$TMP/ex-other.txt" "$(rel v0.1.11 X)"
expect fail "the exception is not on without the exceptions file" "FAIL" \
  "$CH" --repo "$R" --manifests-dir "$M" "$(rel v0.1.11 X)"
for line in 'latest component-set reason' 'v0.1.11 component-sets reason' 'v0.1.11 component-set' \
  '  # an indented comment'; do
  printf '%s\n' "$line" > "$TMP/ex-bad.txt"
  expect fail "malformed exceptions line '$line' fails closed" "$TMP/ex-bad.txt:1:" \
    "$CH" --repo "$R" --manifests-dir "$M" --exceptions "$TMP/ex-bad.txt" "$(rel v0.2.0 Z)"
done
printf 'v0.1.11 component-set one\nv0.1.11 component-set two\n' > "$TMP/ex-dup.txt"
expect fail "a second exception for the same tag and check fails closed" "a second exception for v0.1.11 component-set" \
  "$CH" --repo "$R" --manifests-dir "$M" --exceptions "$TMP/ex-dup.txt" "$(rel v0.1.11 X)"
expect fail "a missing exceptions file fails closed" "does not exist" \
  "$CH" --repo "$R" --manifests-dir "$M" --exceptions "$TMP/none.txt" "$(rel v0.2.0 Z)"

cp -R "$M" "$TMP/m-broken"; printf '<html>' > "$TMP/m-broken/v0.1.1/manifest.json"
expect fail "a malformed earlier manifest fails closed" "the manifest of v0.1.1 is malformed" \
  "$CH" --repo "$R" --manifests-dir "$TMP/m-broken" "$(rel v0.2.0 Z)"
cp -R "$M" "$TMP/m-missing"; rm "$TMP/m-missing/v0.1.0/manifest.json"
expect fail "an unreadable earlier manifest fails closed" "cannot read the manifest of v0.1.0" \
  "$CH" --repo "$R" --manifests-dir "$TMP/m-missing" "$(rel v0.2.0 Z)"
expect fail "a network failure fails closed" "cannot read the manifest of v0.1.0" \
  env OARFISH_RELEASES_URL=https://127.0.0.1:1 "$CH" --repo "$R" "$(rel v0.2.0 Z)"

# Creation order against version order: v1.0.0 was created first, v0.9.0 later,
# so v0.9.0 is the directly preceding release (by version it would be v1.0.0).
R2="$TMP/repo-order"
M2="$TMP/manifests-order"
new_repo "$R2"
tag_at "$R2" v1.0.0 1; fake_manifest "$M2/v1.0.0/manifest.json" v1.0.0 seven X
tag_at "$R2" v0.9.0 2; fake_manifest "$M2/v0.9.0/manifest.json" v0.9.0 seven Y
expect pass "the preceding release is the one created last, not the highest version" "PASS: v2.0.0 brings the set of the preceding release v0.9.0 unchanged" \
  "$CH" --repo "$R2" --manifests-dir "$M2" "$(rel v2.0.0 Y)"

new_repo "$TMP/repo-none"
expect fail "a repository without tags fails closed" "no release tag in" \
  "$CH" --repo "$TMP/repo-none" --manifests-dir "$M" "$(rel v0.2.0 Z)"
git clone -q --depth 1 "file://$R" "$TMP/repo-shallow" 2>/dev/null
expect fail "a shallow clone fails closed" "is shallow" \
  "$CH" --repo "$TMP/repo-shallow" --manifests-dir "$M" "$(rel v0.2.0 Z)"
expect fail "the first tag has nothing before it" "no release tag before v0.1.0" \
  "$CH" --repo "$R" --manifests-dir "$M" "$(rel v0.1.0 Z)"

echo "== unit forms (two-step rule)"
UF="$ROOT/scripts/check-unit-forms.sh"
U="$TMP/repo-c"
HD="backend/cmd/oarfish-update-helper"
new_repo "$U"

# write_helper FORM_A FORM_B...: a fake helper package with a lint that accepts
# the listed texts of a.service (comment lines and blank lines removed first)
# and exactly "S" for oarfish.service, and a test file that does not build (the
# check must leave the helper's own tests out).
write_helper() {
  local forms="" f
  for f in "$@"; do forms="$forms\"$f\", "; done
  mkdir -p "$U/$HD"
  printf 'module example.invalid/fake\n\ngo 1.22\n' > "$U/backend/go.mod"
  printf 'package main\n\nfunc main() {}\n' > "$U/$HD/main.go"
  cat > "$U/$HD/unitlint.go" <<GO
package main

import "strings"

type lintResult struct {
	Accepted bool
	Reason   string
}

var lintedUnits = []string{"a.service", "oarfish.service"}

var forms = map[string][]string{"a.service": {$forms}, "oarfish.service": {"S"}}

func lintUnit(name string, text []byte) lintResult {
	var kept []string
	for _, l := range strings.Split(string(text), "\n") {
		if l != "" && !strings.HasPrefix(l, "#") {
			kept = append(kept, l)
		}
	}
	for _, f := range forms[name] {
		if strings.Join(kept, "\n") == f {
			return lintResult{Accepted: true}
		}
	}
	return lintResult{Reason: "form_mismatch"}
}
GO
  printf 'package main\n\nvar _ = notDefinedAnywhere\n' > "$U/$HD/old_test.go"
  gitq "$U" add -A
}

# v0.1.0: no lint. v0.1.1: form A. v0.1.2: forms A and B. v0.1.3: a lint that does not build.
mkdir -p "$U/$HD"; printf 'package main\n\nfunc main() {}\n' > "$U/$HD/main.go"
printf 'module example.invalid/fake\n\ngo 1.22\n' > "$U/backend/go.mod"
gitq "$U" add -A; tag_at "$U" v0.1.0 1
write_helper A; tag_at "$U" v0.1.1 2
write_helper A B; tag_at "$U" v0.1.2 3
write_helper A B; printf 'package main\n\nvar broken =\n' > "$U/$HD/broken.go"; gitq "$U" add -A; tag_at "$U" v0.1.3 4

# unit_release TAG A_TEXT [EXTRA_UNIT]: a release directory with a bundle that
# holds a.service with A_TEXT and oarfish.service "S" (A_TEXT "-" leaves
# a.service out).
unit_release() {
  local d="$TMP/urel/$1" root="oarfish_${1#v}_linux_arm64"
  rm -rf "$d"; mkdir -p "$d/$root"
  [ "$2" = - ] || printf '%b\n' "$2" > "$d/$root/a.service"
  printf 'S\n' > "$d/$root/oarfish.service"
  [ -z "${3:-}" ] || printf 'x\n' > "$d/$root/$3"
  (cd "$d" && tar -czf "$root.tar.gz" "$root" && rm -rf "$root")
  fake_manifest "$d/manifest.json" "$1" seven Q
  echo "$d"
}

expect pass "a form the preceding helper knows passes" "a.service: accepted by the unit lint of the helper of v0.1.1" \
  "$UF" --repo "$U" "$(unit_release v0.1.2 'A')"
expect pass "a comment and a blank line more pass" "PASS: every unit that the helper of v0.1.1 lints passes its lint" \
  "$UF" --repo "$U" "$(unit_release v0.1.2 '# a comment\n\nA')"
expect fail "a form the preceding helper does not know fails" "a.service (form_mismatch) of v0.1.2 would be refused by the helper of v0.1.1" \
  "$UF" --repo "$U" --exceptions "$EX" "$(unit_release v0.1.2 'B')"
expect pass "step two: the form that the preceding helper lists passes" "a.service: accepted by the unit lint of the helper of v0.1.2" \
  "$UF" --repo "$U" "$(unit_release v0.1.3 'B')"
printf 'v0.1.2 unit-forms the owner accepted one installer run, 2026-01-02\n' > "$TMP/ex-c.txt"
expect pass "the exception for this tag passes and is printed" "EXCEPTION: a.service (form_mismatch) would be refused (unit_rejected) by the helper of v0.1.1" \
  "$UF" --repo "$U" --exceptions "$TMP/ex-c.txt" "$(unit_release v0.1.2 'B')"
printf 'v0.1.2 component-set another check\n' > "$TMP/ex-c2.txt"
expect fail "an exception for the other check does not apply" "FAIL: a.service (form_mismatch)" \
  "$UF" --repo "$U" --exceptions "$TMP/ex-c2.txt" "$(unit_release v0.1.2 'B')"
expect pass "a preceding helper without the lint is not comparable" "the helper of v0.1.0 has no unit lint" \
  "$UF" --repo "$U" "$(unit_release v0.1.1 'B')"
expect fail "a unit the preceding helper lints is missing" "a.service: MISSING from v0.1.2" \
  "$UF" --repo "$U" "$(unit_release v0.1.2 -)"
expect pass "a unit the preceding helper does not lint is listed, not judged" "b.socket: not linted by the helper of v0.1.1" \
  "$UF" --repo "$U" "$(unit_release v0.1.2 'A' b.socket)"
expect fail "a preceding lint that does not build fails closed" "cannot build or run the unit lint of the helper of v0.1.3" \
  "$UF" --repo "$U" "$(unit_release v0.2.0 'A')"
expect fail "a repository without tags fails closed" "no release tag in" \
  "$UF" --repo "$TMP/repo-none" "$(unit_release v0.2.0 'A')"

echo
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
