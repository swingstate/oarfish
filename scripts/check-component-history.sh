#!/usr/bin/env bash
# The release check of D-C92 (path A): no release brings an older component set.
#
# Compares the component set of the release in RELEASE_DIR (the seven files of
# ADR 0010 A20.1, from the per-file digests of its manifest.json) with the sets
# of all earlier published releases. The set may equal the set of the directly
# preceding release; it must not equal the set of any other earlier release.
# The rule, what is compared and what is skipped are stated in
# scripts/bundlegen/history.go; the procedure is in docs/RELEASING.md.
#
# Earlier releases: the release tags of the repository created before the tag
# of the release (all of them in a dry run, whose tag is not a tag of the
# repository), oldest first, so the last one is the directly preceding release.
# Their manifest.json is downloaded anonymously over HTTPS from
# OARFISH_RELEASES_URL (default: the release downloads of the public
# repository). No token is used. A download or a manifest that fails fails this
# check (fail closed).
#
# Usage: scripts/check-component-history.sh [--exceptions FILE] [--repo DIR]
#                                            [--manifests-dir DIR] RELEASE_DIR
#   --exceptions FILE   the owner's exceptions; the release workflow passes
#                       release/release-exceptions.txt. Without it no exception
#                       applies.
#   --repo DIR          the repository whose tags list the earlier releases
#                       (default: this repository)
#   --manifests-dir DIR read DIR/<tag>/manifest.json instead of downloading
#                       (fixtures, scripts/test-release-history.sh)
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

REPO="$(repo_root)"
EXCEPTIONS=""
MANIFESTS=""
REL=""
while [ $# -gt 0 ]; do
  case "$1" in
    --exceptions) EXCEPTIONS="$2"; shift 2 ;;
    --repo) REPO="$2"; shift 2 ;;
    --manifests-dir) MANIFESTS="$2"; shift 2 ;;
    -*) die "unknown option $1" ;;
    *) [ -z "$REL" ] || die "one RELEASE_DIR only"; REL="$1"; shift ;;
  esac
done
[ -n "$REL" ] || die "usage: $0 [--exceptions FILE] [--repo DIR] [--manifests-dir DIR] RELEASE_DIR"
[ -f "$REL/manifest.json" ] || die "$REL/manifest.json not found"
BASE="${OARFISH_RELEASES_URL:-https://github.com/swingstate/oarfish/releases/download}"

TAG="$(manifest_tag "$REL/manifest.json")"
EARLIER="$(release_tags_before "$REPO" "$TAG")"
[ -n "$EARLIER" ] || die "no release tag before $TAG in $REPO"

args=(-manifest "$REL/manifest.json")
if [ -n "$EXCEPTIONS" ]; then
  release_exception "$EXCEPTIONS" "$TAG" component-set
  if [ -n "$EXCEPTION_REASON" ]; then
    echo "component-set: exception for $TAG in $EXCEPTIONS: $EXCEPTION_REASON"
    args+=(-exception "$TAG")
  fi
fi
if [ -n "$MANIFESTS" ]; then args+=(-manifests-dir "$MANIFESTS"); else args+=(-base-url "$BASE"); fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
build_bundlegen "$TMP/bundlegen"
# shellcheck disable=SC2086 # one tag per word
"$TMP/bundlegen" history "${args[@]}" $EARLIER
