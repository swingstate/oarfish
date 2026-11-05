#!/usr/bin/env bash
# The release helper must contain no fault hooks (ADR 0010 U13, class T). The
# hooks are compiled only with the build tag faulthooks (faults_on.go) and read
# OARFISH_FAULT_AT and OARFISH_FAULT_MODE. This builds the helper for
# linux/arm64 without the tag and with it, into a temporary directory, and
# fails unless the release binary has none of the hooks' strings and the hooked
# binary has all of them (so that the check itself can fail).
#
# Usage: scripts/check-helper-hooks.sh [RELEASE_BINARY]
#   RELEASE_BINARY  an already built helper to check instead of building one
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
ROOT="$(repo_root)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
MARKERS=(OARFISH_FAULT_AT OARFISH_FAULT_MODE "injected fault")

build() { # build TAGS OUT
  (cd "$ROOT/backend" && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath ${1:+-tags "$1"} -o "$2" ./cmd/oarfish-update-helper)
}

contains() { grep -qF -- "$2" "$1"; }

RELEASE="${1:-}"
if [ -z "$RELEASE" ]; then
  RELEASE="$TMP/release"
  build "" "$RELEASE"
fi
build faulthooks "$TMP/hooked"

for m in "${MARKERS[@]}"; do
  contains "$TMP/hooked" "$m" || die "the check can not fail: the faulthooks build lacks \"$m\""
  if contains "$RELEASE" "$m"; then
    die "the release helper contains the fault hook string \"$m\""
  fi
done
echo "the release helper has no fault hooks (the faulthooks build has all ${#MARKERS[@]} markers)"
