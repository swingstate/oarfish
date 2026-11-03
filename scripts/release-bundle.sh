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
# Optional, unset by default (the output is then the legacy bundle, byte for byte
# what it was before ADR 0010). Neither workflow sets any of these until G6.3.
#   OARFISH_COMPONENTS_DIR       directory holding the four component files
#                                (oarfish-update-helper, oarfish-update-helper.service,
#                                oarfish-update-helper.socket, oarfish-update-recover.service)
#   OARFISH_COMPONENTS_PROTOCOL  highest protocol the delivered helper implements
#   OARFISH_COMPONENTS_KEY_ID    16 lowercase hex digits, key id embedded in that helper
#                                These three are all or nothing: bundlegen refuses a partial set.
#   OARFISH_MIN_INSTALLED_VERSION     manifest requirements.min_installed_version
#   OARFISH_REQUIRES_HELPER_PROTOCOL  manifest requirements.helper_protocol
#
# Requires the frontend to be built already (backend/internal/web/dist). Output:
#   OUTDIR/oarfish_<version>_linux_arm64.tar.gz, OUTDIR/manifest.json and
#   OUTDIR/install.sh (install/install.sh stamped with the version)
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

# Extra bundlegen flags, only for variables the caller set explicitly. An unset
# or empty variable adds nothing, so the default stays the legacy bundle.
EXTRA=()
if [ -n "${OARFISH_COMPONENTS_DIR:-}" ]; then
  EXTRA+=(-helper "$OARFISH_COMPONENTS_DIR/oarfish-update-helper"
    -helper-service "$OARFISH_COMPONENTS_DIR/oarfish-update-helper.service"
    -helper-socket "$OARFISH_COMPONENTS_DIR/oarfish-update-helper.socket"
    -recover-service "$OARFISH_COMPONENTS_DIR/oarfish-update-recover.service")
fi
[ -z "${OARFISH_COMPONENTS_PROTOCOL:-}" ] || EXTRA+=(-components-protocol "$OARFISH_COMPONENTS_PROTOCOL")
[ -z "${OARFISH_COMPONENTS_KEY_ID:-}" ] || EXTRA+=(-components-key-id "$OARFISH_COMPONENTS_KEY_ID")
[ -z "${OARFISH_MIN_INSTALLED_VERSION:-}" ] || EXTRA+=(-min-installed-version "$OARFISH_MIN_INSTALLED_VERSION")
[ -z "${OARFISH_REQUIRES_HELPER_PROTOCOL:-}" ] || EXTRA+=(-requires-helper-protocol "$OARFISH_REQUIRES_HELPER_PROTOCOL")

build_bundlegen "$WORK/bundlegen"
mkdir -p "$OUT"
# ${EXTRA[@]+...} keeps an empty array valid under `set -u` in older bash.
"$WORK/bundlegen" bundle \
  -binary "$WORK/oarfish" -license "$ROOT/LICENSE" -notices "$ROOT/THIRD_PARTY_NOTICES.txt" \
  -unit "$ROOT/install/oarfish.service" \
  -version "$VERSION" -commit "$COMMIT" -epoch "$EPOCH" -out "$OUT" \
  ${EXTRA[@]+"${EXTRA[@]}"}
"$ROOT/scripts/stamp-installer.sh" "$VERSION" "$OUT/install.sh" >/dev/null
echo "==> wrote $OUT"
