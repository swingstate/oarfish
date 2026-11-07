#!/usr/bin/env bash
# Build the update helper (ADR 0010, U1) for linux/arm64 with the flags of the
# main binary in scripts/release-bundle.sh, which puts it into the default
# (components) bundle.
#
# The helper's bytes depend only on its code, the packages it links and the
# toolchain, never on the release version or the commit (D-C80, ADR 0010 A13):
# no -X flag is passed, and VERSION and COMMIT are not read. A release whose
# helper code is unchanged therefore carries the same helper as the release
# before it, and stage does not answer installer_required for it (U5).
# scripts/check-helper-independent.sh checks this.
#
# Usage: scripts/build-helper.sh [-o FILE] [--twice]
#   -o FILE   output file (default: bin/oarfish-update-helper-linux-arm64)
#   --twice   build a second time with an empty Go cache into a temporary
#             file and fail unless both files are identical
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
mkdir -p "$(dirname "$OUT")"
OUT="$(cd "$(dirname "$OUT")" && pwd)/$(basename "$OUT")"

# The flags of the main binary without its -X flags: -trimpath and
# -buildvcs=false keep checkout paths and VCS state out, -mod=readonly forbids
# silent go.mod changes.
LDFLAGS="-s -w"
build() { # output [GOCACHE]
  (
    cd backend
    [ -z "${2:-}" ] || export GOCACHE="$2"
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOFLAGS=-mod=readonly \
      go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$1" ./cmd/oarfish-update-helper
  )
}

echo "==> oarfish-update-helper (independent of the release version)"
build "$OUT"
if [ "$TWICE" -eq 1 ]; then
  TMP="$(mktemp -d)"
  trap 'chmod -R u+w "$TMP" 2>/dev/null; rm -rf "$TMP"' EXIT
  build "$TMP/second" "$TMP/gocache"
  cmp -s "$OUT" "$TMP/second" || die "the update helper is NOT reproducible (two builds differ)"
  echo "==> reproducible: two clean builds are identical"
fi
echo "==> $(basename "$OUT"): $(wc -c < "$OUT" | tr -d ' ') bytes, sha256 $(sha256_of "$OUT")"
