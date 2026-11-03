#!/usr/bin/env bash
# Build the update helper (ADR 0010, U1) for linux/arm64 with the flags of the
# main binary in scripts/release-bundle.sh. The helper is NOT part of any bundle
# in G6.3a; this script only builds it, for tests, the smoke test and the size
# report.
#
# Usage: scripts/build-helper.sh [-o FILE] [--twice]
#   -o FILE   output file (default: bin/oarfish-update-helper-linux-arm64)
#   --twice   build a second time with an empty Go cache into a temporary
#             file and fail unless both files are identical
#   VERSION   default 0.0.0-dev.<short commit>, ".dirty" when tracked files changed
#   COMMIT    default HEAD
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
ROOT="$(repo_root)"
OUT="$ROOT/bin/oarfish-update-helper-linux-arm64"
TWICE=0

while [ $# -gt 0 ]; do
  case "$1" in
    -o) [ $# -ge 2 ] || die "usage: $0 [-o FILE] [--twice]"; OUT="$2"; shift 2 ;;
    --twice) TWICE=1; shift ;;
    *) die "usage: $0 [-o FILE] [--twice]" ;;
  esac
done

cd "$ROOT"
COMMIT="${COMMIT:-$(git rev-parse HEAD)}"
if [ -z "${VERSION:-}" ]; then
  VERSION="0.0.0-dev.$(git rev-parse --short=7 "$COMMIT")"
  [ -z "$(git status --porcelain --untracked-files=no)" ] || VERSION="$VERSION.dirty"
fi
mkdir -p "$(dirname "$OUT")"
OUT="$(cd "$(dirname "$OUT")" && pwd)/$(basename "$OUT")"

# The same flags as the main binary: -trimpath and -buildvcs=false keep checkout
# paths and VCS state out, -mod=readonly forbids silent go.mod changes.
LDFLAGS="-s -w \
 -X github.com/swingstate/oarfish/backend/internal/version.Version=$VERSION \
 -X github.com/swingstate/oarfish/backend/internal/version.Commit=$COMMIT"
build() { # output [GOCACHE]
  (
    cd backend
    [ -z "${2:-}" ] || export GOCACHE="$2"
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOFLAGS=-mod=readonly \
      go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$1" ./cmd/oarfish-update-helper
  )
}

echo "==> oarfish-update-helper $VERSION ($COMMIT)"
build "$OUT"
if [ "$TWICE" -eq 1 ]; then
  TMP="$(mktemp -d)"
  trap 'chmod -R u+w "$TMP" 2>/dev/null; rm -rf "$TMP"' EXIT
  build "$TMP/second" "$TMP/gocache"
  cmp -s "$OUT" "$TMP/second" || die "the update helper is NOT reproducible (two builds differ)"
  echo "==> reproducible: two clean builds are identical"
fi
echo "==> $(basename "$OUT"): $(wc -c < "$OUT" | tr -d ' ') bytes, sha256 $(sha256_of "$OUT")"
