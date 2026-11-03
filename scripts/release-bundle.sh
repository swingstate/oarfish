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
# Bundle shape (ADR 0010, G6.3b). The default is the COMPONENTS shape: the bundle
# carries the update helper (built here with scripts/build-helper.sh, same VERSION
# and COMMIT) and the four unit files from install/ (the three helper units and the
# recover unit), components protocol 1, and the components key id derived from
# release/oarfish-release.pub (the 16 hex digits of its first line, lower case; the
# method of scripts/check-helper-key.sh). After building, `bundlegen check
# -expect-key-id` must accept the result.
#   OARFISH_BUNDLE_SHAPE=legacy  explicit opt-out, for tests and comparison: the
#                                legacy five-entry bundle, byte for byte what it was
#                                before ADR 0010. Nothing selects it implicitly.
#                                It cannot be combined with OARFISH_COMPONENTS_*.
#   OARFISH_BUNDLE_SHAPE=components   the default
#
# Optional, unset by default. When any OARFISH_COMPONENTS_* variable is set the
# default above is not used at all and these keep their G6.2 meaning: all three or
# none (bundlegen refuses a partial set), nothing is built or derived here.
#   OARFISH_COMPONENTS_DIR       directory holding the four component files
#                                (oarfish-update-helper, oarfish-update-helper.service,
#                                oarfish-update-helper.socket, oarfish-update-recover.service)
#   OARFISH_COMPONENTS_PROTOCOL  highest protocol the delivered helper implements
#   OARFISH_COMPONENTS_KEY_ID    16 lowercase hex digits, key id embedded in that helper
#                                These three are all or nothing: bundlegen refuses a partial set.
# These two apply to both shapes when set:
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

SHAPE="${OARFISH_BUNDLE_SHAPE:-components}"
case "$SHAPE" in
  components|legacy) ;;
  *) die "OARFISH_BUNDLE_SHAPE must be 'components' (the default) or 'legacy', not '$SHAPE'" ;;
esac
EXPLICIT=0
if [ -n "${OARFISH_COMPONENTS_DIR:-}" ] || [ -n "${OARFISH_COMPONENTS_PROTOCOL:-}" ] || [ -n "${OARFISH_COMPONENTS_KEY_ID:-}" ]; then EXPLICIT=1; fi
[ "$SHAPE" = components ] || [ "$EXPLICIT" = 0 ] || die "OARFISH_BUNDLE_SHAPE=legacy cannot be combined with OARFISH_COMPONENTS_*"

# Extra bundlegen flags. In the default components shape the helper is built and
# the key id derived here; explicit OARFISH_COMPONENTS_* variables replace that.
EXPECT_KEY_ID=""
EXTRA=()
if [ "$SHAPE" = components ] && [ "$EXPLICIT" = 0 ]; then
  # The key id of the release key, lower case: the same sed as check-helper-key.sh.
  EXPECT_KEY_ID="$(sed -n 1p "$ROOT/release/oarfish-release.pub" | sed -n 's/^untrusted comment: minisign public key \([0-9A-F]\{16\}\)$/\1/p' | tr 'A-F' 'a-f')"
  [ -n "$EXPECT_KEY_ID" ] || die "cannot read the key id from release/oarfish-release.pub"
  mkdir -p "$WORK/components"
  VERSION="$VERSION" COMMIT="$COMMIT" "$ROOT/scripts/build-helper.sh" -o "$WORK/components/oarfish-update-helper"
  for u in oarfish-update-helper.service oarfish-update-helper.socket oarfish-update-recover.service; do
    cp "$ROOT/install/$u" "$WORK/components/$u"
  done
  OARFISH_COMPONENTS_DIR="$WORK/components"
  OARFISH_COMPONENTS_PROTOCOL=1
  OARFISH_COMPONENTS_KEY_ID="$EXPECT_KEY_ID"
fi
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
if [ -n "$EXPECT_KEY_ID" ]; then
  # The components key id must be the release key's. `check` wants a checksums.txt,
  # which only exists once a release is assembled, so it runs on a throwaway copy of
  # the three files (OUT itself stays without one).
  mkdir "$WORK/check"
  bundle_name="$(basename "$OUT"/oarfish_*_linux_arm64.tar.gz)"
  cp "$OUT/$bundle_name" "$OUT/manifest.json" "$OUT/install.sh" "$WORK/check/"
  "$WORK/bundlegen" checksums -dir "$WORK/check" "$bundle_name" install.sh manifest.json
  "$WORK/bundlegen" check -dir "$WORK/check" -expect-key-id "$EXPECT_KEY_ID"
  echo "==> bundlegen check: components shape, key id $EXPECT_KEY_ID"
fi
echo "==> wrote $OUT"
