#!/usr/bin/env bash
# Build the privilege broker (ADR 0011, B1) for linux/arm64 with the flags of
# the update helper (scripts/build-helper.sh). G7.2: the broker is not in any
# release bundle yet; packaging is step G7.3.
#
# The broker's bytes depend only on its code and the toolchain, never on the
# release version or the commit (D-C80 applied to the broker, ADR 0011 B1):
# no -X flag is passed, and VERSION and COMMIT are not read. It imports only
# the standard library (TestImports). scripts/check-broker-independent.sh
# checks the independence.
#
# Usage: scripts/build-broker.sh [-o FILE] [--twice]
#   -o FILE   output file (default: bin/oarfish-broker-linux-arm64)
#   --twice   build a second time with an empty Go cache into a temporary
#             file and fail unless both files are identical
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
ROOT="$(repo_root)"
OUT="$ROOT/bin/oarfish-broker-linux-arm64"
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

# -trimpath and -buildvcs=false keep checkout paths and VCS state out,
# -mod=readonly forbids silent go.mod changes. No build tag: the broker has
# none, and its test seams exist only in the options that tests construct.
LDFLAGS="-s -w"
build() { # output [GOCACHE]
  (
    cd backend
    [ -z "${2:-}" ] || export GOCACHE="$2"
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOFLAGS=-mod=readonly \
      go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$1" ./cmd/oarfish-broker
  )
}

echo "==> oarfish-broker (independent of the release version)"
build "$OUT"
if [ "$TWICE" -eq 1 ]; then
  TMP="$(mktemp -d)"
  trap 'chmod -R u+w "$TMP" 2>/dev/null; rm -rf "$TMP"' EXIT
  build "$TMP/second" "$TMP/gocache"
  cmp -s "$OUT" "$TMP/second" || die "the privilege broker is NOT reproducible (two builds differ)"
  echo "==> reproducible: two clean builds are identical"
fi
echo "==> $(basename "$OUT"): $(wc -c < "$OUT" | tr -d ' ') bytes, sha256 $(sha256_of "$OUT")"
