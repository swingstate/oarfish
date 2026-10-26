#!/usr/bin/env bash
# Build the release bundle twice from scratch and compare SHA-256 values
# (bundle, manifest.json and the stamped install.sh).
#
# Usage: scripts/check-reproducible.sh [--rebuild-frontend] [REFERENCE_DIR]
#   --rebuild-frontend  run `npm run build` before each build, so Vite output is
#                       covered too (needs Node.js; CI uses it).
#   REFERENCE_DIR       a directory produced by release-bundle.sh; its bundle,
#                       manifest and install.sh must also match.
# Each build uses an empty Go build cache. VERSION and COMMIT are inherited.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
ROOT="$(repo_root)"

REBUILD=0
REF=""
for arg in "$@"; do
  case "$arg" in
    --rebuild-frontend) REBUILD=1 ;;
    *) REF="$arg" ;;
  esac
done

# Pin the version once so both builds see the same value.
if [ -z "${VERSION:-}" ]; then
  VERSION="0.0.0-dev.$(git -C "$ROOT" rev-parse --short=7 HEAD)"
fi
export VERSION

TMP="$(mktemp -d)"
trap 'chmod -R u+w "$TMP" 2>/dev/null; rm -rf "$TMP"' EXIT

for i in 1 2; do
  [ "$REBUILD" -eq 0 ] || (cd "$ROOT/frontend" && npm run build >/dev/null)
  GOCACHE="$TMP/gocache$i" "$ROOT/scripts/release-bundle.sh" -o "$TMP/build$i" >/dev/null
done

b1="$(ls "$TMP"/build1/*.tar.gz)"; b2="$(ls "$TMP"/build2/*.tar.gz)"
h1="$(sha256_of "$b1")"; h2="$(sha256_of "$b2")"
echo "build 1: $h1  $(basename "$b1")"
echo "build 2: $h2  $(basename "$b2")"
[ "$h1" = "$h2" ] || die "bundle is NOT reproducible (SHA-256 differs between two builds)"
cmp -s "$TMP/build1/manifest.json" "$TMP/build2/manifest.json" || die "manifest.json is NOT reproducible"
cmp -s "$TMP/build1/install.sh" "$TMP/build2/install.sh" || die "install.sh is NOT reproducible"

if [ -n "$REF" ]; then
  hr="$(sha256_of "$REF/$(basename "$b1")")"
  echo "release: $hr  $(basename "$b1") (reference)"
  [ "$hr" = "$h1" ] || die "reference bundle differs from the rebuilt bundle"
  cmp -s "$REF/manifest.json" "$TMP/build1/manifest.json" || die "reference manifest differs from the rebuilt manifest"
  cmp -s "$REF/install.sh" "$TMP/build1/install.sh" || die "reference install.sh differs from the rebuilt install.sh"
fi

echo "REPRODUCIBLE: two clean builds produced identical bundle, manifest and install.sh ($h1)"
summary <<SUM
### Reproducibility
Two clean builds (empty Go cache$([ "$REBUILD" -eq 1 ] && echo ", frontend rebuilt")) of the same commit produced the same bundle.

\`sha256 $h1\`
SUM
