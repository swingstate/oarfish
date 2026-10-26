#!/usr/bin/env bash
# Write the release's install.sh: install/install.sh with the version placeholder
# replaced by VERSION. Deterministic (no timestamps, no host data), so the stamped
# file is reproducible and its SHA-256 can go into the signed checksums.txt.
#
# Usage: scripts/stamp-installer.sh VERSION OUTFILE
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
ROOT="$(repo_root)"

VERSION="${1:?usage: $0 VERSION OUTFILE}"
OUT="${2:?usage: $0 VERSION OUTFILE}"
# Strict SemVer without a leading v; also guarantees the sed replacement below is safe.
[[ "$VERSION" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$ ]] \
  || die "invalid version '$VERSION'"

SRC="$ROOT/install/install.sh"
PLACEHOLDER="OARFISH_STAMPED_VERSION='@@OARFISH_VERSION@@'"
[ "$(grep -c -x -F "$PLACEHOLDER" "$SRC")" = 1 ] || die "$SRC must contain the placeholder line exactly once"

# Only that whole line is replaced, so the placeholder test further down the script is untouched.
TMP="$(mktemp)"; trap 'rm -f "$TMP"' EXIT
sed "s/^OARFISH_STAMPED_VERSION='@@OARFISH_VERSION@@'\$/OARFISH_STAMPED_VERSION='$VERSION'/" "$SRC" > "$TMP"
[ "$(grep -c -x -F "OARFISH_STAMPED_VERSION='$VERSION'" "$TMP")" = 1 ] || die "stamping failed"
[ "$(diff "$SRC" "$TMP" | grep -c '^>')" = 1 ] || die "stamping changed more than one line"
install -m 0644 "$TMP" "$OUT"
echo "stamped $OUT with version $VERSION"
