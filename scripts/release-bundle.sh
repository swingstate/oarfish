#!/usr/bin/env bash
# Build the unsigned Linux ARM64 release bundle and its manifest.
#
# This is the only place that packages a release; `make release-bundle`, ci.yml
# and release.yml all call it, so local and CI builds cannot diverge.
#
# Usage: scripts/release-bundle.sh [-o OUTDIR]
#   VERSION  SemVer without leading "v" (default: 0.0.0-dev.<short commit>)
#   COMMIT   full commit hash (default: HEAD); build_date and tar mtimes are the
#            timestamp of that commit, which makes the output reproducible.
#
# Requires the frontend to be built already (backend/internal/web/dist). Output:
#   OUTDIR/oarfish_<version>_linux_arm64.tar.gz and OUTDIR/manifest.json
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
ROOT="$(repo_root)"
OUT="$ROOT/dist/release"

while getopts "o:" opt; do
  case "$opt" in
    o) OUT="$(mkdir -p "$OPTARG" && cd "$OPTARG" && pwd)" ;;
    *) die "usage: $0 [-o OUTDIR]" ;;
  esac
done

cd "$ROOT"
COMMIT="${COMMIT:-$(git rev-parse HEAD)}"
EPOCH="$(git log -1 --format=%ct "$COMMIT")"
if [ -z "${VERSION:-}" ]; then
  VERSION="0.0.0-dev.$(git rev-parse --short=7 "$COMMIT")"
  [ -z "$(git status --porcelain --untracked-files=no)" ] || VERSION="$VERSION.dirty"
fi
[ -f backend/internal/web/dist/index.html ] || die "frontend is not built; run 'make frontend-build' first"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "==> oarfish $VERSION ($COMMIT, commit time $EPOCH)"

# -trimpath and -buildvcs=false keep checkout paths and VCS state out of the
# binary; -mod=readonly forbids silent go.mod changes.
LDFLAGS="-s -w \
 -X github.com/swingstate/oarfish/backend/internal/version.Version=$VERSION \
 -X github.com/swingstate/oarfish/backend/internal/version.Commit=$COMMIT"
(
  cd backend
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOFLAGS=-mod=readonly \
    go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$WORK/oarfish" ./cmd/oarfish
)

build_bundlegen "$WORK/bundlegen"
mkdir -p "$OUT"
"$WORK/bundlegen" bundle \
  -binary "$WORK/oarfish" -license "$ROOT/LICENSE" -notices "$ROOT/THIRD_PARTY_NOTICES.txt" \
  -version "$VERSION" -commit "$COMMIT" -epoch "$EPOCH" -out "$OUT"
echo "==> wrote $OUT"
