#!/usr/bin/env bash
# Verify a release directory the way an installer must: signature first, then
# hashes, then manifest and tarball contents.
#
# Usage: scripts/verify-release.sh DIR PUBKEY
#   DIR     holds checksums.txt, checksums.txt.sig, manifest.json, the bundle, ...
#   PUBKEY  minisign public key file (release/orbit-release.pub)
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
command -v minisign >/dev/null || die "minisign not found"

DIR="${1:?usage: $0 DIR PUBKEY}"
PUB="${2:?usage: $0 DIR PUBKEY}"
[ -f "$PUB" ] || die "public key $PUB not found"
for f in checksums.txt checksums.txt.sig manifest.json; do
  [ -f "$DIR/$f" ] || die "$DIR/$f is missing"
done

echo "== 1/3 signature of checksums.txt"
minisign -V -p "$PUB" -x "$DIR/checksums.txt.sig" -m "$DIR/checksums.txt" \
  || die "signature verification FAILED"

echo "== 2/3 file hashes (system sha256 tool)"
(cd "$DIR" && sha256_check checksums.txt) || die "hash check FAILED"

echo "== 3/3 manifest and tarball contents (bundlegen check)"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
build_bundlegen "$TMP/bundlegen"
"$TMP/bundlegen" check -dir "$DIR" || die "manifest/content check FAILED"

echo "VERIFIED: $DIR"
