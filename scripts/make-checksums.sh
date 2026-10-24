#!/usr/bin/env bash
# Write DIR/checksums.txt covering every release file in DIR (bundle, manifest,
# SBOMs). checksums.txt and its signature are never listed in themselves.
#
# Usage: scripts/make-checksums.sh DIR
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

DIR="${1:?usage: $0 DIR}"
cd "$DIR"
files=()
for f in *; do
  [ -f "$f" ] || continue
  case "$f" in checksums.txt|checksums.txt.sig) continue ;; esac
  files+=("$f")
done
[ "${#files[@]}" -gt 0 ] || die "no release files in $DIR"

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
build_bundlegen "$TMP/bundlegen"
"$TMP/bundlegen" checksums -dir . "${files[@]}"
echo "checksums.txt:"; cat checksums.txt
