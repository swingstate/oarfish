#!/usr/bin/env bash
# Positive and negative tests for signing and verification.
#
# Everything uses throwaway minisign keys generated inside this script. The
# release key is never needed and never touched. Each negative case must be
# rejected for the right reason, otherwise the test fails.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
ROOT="$(repo_root)"
command -v minisign >/dev/null || die "minisign not found (scripts/install-minisign.sh, or brew install minisign)"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
build_bundlegen "$TMP/bundlegen"

COMMIT=0123456789abcdef0123456789abcdef01234567
make_release() { # make_release DIR VERSION
  mkdir -p "$1"
  printf 'not a real binary %s\n' "$2" > "$TMP/fake-oarfish"
  "$TMP/bundlegen" bundle -binary "$TMP/fake-oarfish" -license "$ROOT/LICENSE" -notices "$ROOT/THIRD_PARTY_NOTICES.txt" \
    -version "$2" -commit "$COMMIT" -epoch 1700000000 -out "$1" >/dev/null
  printf '{"fixture":true}\n' > "$1/sbom.json"
  "$TMP/bundlegen" checksums -dir "$1" "oarfish_$2_linux_arm64.tar.gz" manifest.json sbom.json
}

N=0
expect_ok()   { N=$((N+1)); local name="$1"; shift; if "$@" >"$TMP/out" 2>&1; then echo "PASS  [$N] $name"; else cat "$TMP/out"; die "[$N] $name: expected success"; fi; }
# expect_reject NAME PATTERN CMD...: must fail and mention PATTERN.
expect_reject() {
  N=$((N+1)); local name="$1" pat="$2"; shift 2
  if "$@" >"$TMP/out" 2>&1; then cat "$TMP/out"; die "[$N] $name: ACCEPTED, must be rejected"; fi
  grep -qi "$pat" "$TMP/out" || { cat "$TMP/out"; die "[$N] $name: rejected, but not for the expected reason ($pat)"; }
  echo "PASS  [$N] $name: rejected ($pat)"
}
flip_byte() { # flip_byte FILE: invert one byte in the middle
  local f="$1" size; size="$(wc -c < "$f" | tr -d ' ')"
  printf '\377' | dd of="$f" bs=1 seek=$((size / 2)) count=1 conv=notrunc 2>/dev/null
}

REL="$TMP/rel"; make_release "$REL" 0.1.0-test.1
OARFISH_SIGN_COMMENT="oarfish test" "$ROOT/scripts/sign-release.sh" --ephemeral "$TMP/a.pub" "$REL" >/dev/null
VERIFY=("$ROOT/scripts/verify-release.sh")

expect_ok "valid release verifies" "${VERIFY[@]}" "$REL" "$TMP/a.pub"

cp -R "$REL" "$TMP/t1"; echo "extra line" >> "$TMP/t1/checksums.txt"
expect_reject "tampered checksums.txt" "signature verification" "${VERIFY[@]}" "$TMP/t1" "$TMP/a.pub"

cp -R "$REL" "$TMP/t2"; flip_byte "$TMP/t2/oarfish_0.1.0-test.1_linux_arm64.tar.gz"
expect_reject "tampered bundle (signature and checksums intact)" "hash check failed" "${VERIFY[@]}" "$TMP/t2" "$TMP/a.pub"

cp -R "$REL" "$TMP/t3"; echo " " >> "$TMP/t3/manifest.json"
expect_reject "tampered manifest.json" "hash check failed" "${VERIFY[@]}" "$TMP/t3" "$TMP/a.pub"

cp -R "$REL" "$TMP/t4"; flip_byte "$TMP/t4/sbom.json"
expect_reject "tampered SBOM" "hash check failed" "${VERIFY[@]}" "$TMP/t4" "$TMP/a.pub"

cp -R "$REL" "$TMP/t5"; flip_byte "$TMP/t5/checksums.txt.sig"
expect_reject "tampered signature file" "signature verification" "${VERIFY[@]}" "$TMP/t5" "$TMP/a.pub"

cp -R "$REL" "$TMP/t6"; rm "$TMP/t6/checksums.txt.sig"
expect_reject "missing signature" "missing" "${VERIFY[@]}" "$TMP/t6" "$TMP/a.pub"

# A well-formed release signed by a different key must not verify under the trusted key.
cp -R "$REL" "$TMP/t7"; rm "$TMP/t7/checksums.txt.sig"
"$ROOT/scripts/sign-release.sh" --ephemeral "$TMP/b.pub" "$TMP/t7" >/dev/null
expect_ok     "other key's release verifies under its own key" "${VERIFY[@]}" "$TMP/t7" "$TMP/b.pub"
expect_reject "release signed by another key" "signature verification" "${VERIFY[@]}" "$TMP/t7" "$TMP/a.pub"

# Replay: a valid signature from release 1 must not validate release 2's checksums.
make_release "$TMP/rel2" 0.1.0-test.2
cp "$REL/checksums.txt.sig" "$TMP/rel2/checksums.txt.sig"
expect_reject "signature replayed from another release" "signature verification" "${VERIFY[@]}" "$TMP/rel2" "$TMP/a.pub"

# Format is pinned to legacy Ed25519: a valid prehashed (ED) signature from the
# trusted key must not be accepted by verify-release.sh.
cp -R "$REL" "$TMP/t8"; rm "$TMP/t8/checksums.txt.sig"
minisign -G -W -f -p "$TMP/c.pub" -s "$TMP/c.key" >/dev/null
minisign -S -s "$TMP/c.key" -x "$TMP/t8/checksums.txt.sig" -m "$TMP/t8/checksums.txt" </dev/null >/dev/null
expect_ok     "prehashed signature is valid for minisign itself" minisign -V -p "$TMP/c.pub" -x "$TMP/t8/checksums.txt.sig" -m "$TMP/t8/checksums.txt"
expect_reject "prehashed (ED) signature format" "legacy (Ed) format" "${VERIFY[@]}" "$TMP/t8" "$TMP/c.pub"
[ "$(sig_algorithm "$REL/checksums.txt.sig")" = "Ed" ] || die "release signature is not legacy format"
N=$((N+1)); echo "PASS  [$N] sign-release.sh produces legacy (Ed) signatures"

echo "ALL $N SIGNING CHECKS PASSED"
