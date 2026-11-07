#!/usr/bin/env bash
# The update helper's bytes must not depend on the release version or the
# commit (D-C80, ADR 0010 A13). stage compares the helper of a staged release
# with the installed one and answers installer_required when they differ (U5),
# so a helper that carried the release version would make every update need
# the installer.
#
# Builds the helper with scripts/build-helper.sh twice, with two different
# VERSION and COMMIT values in the environment, and fails unless both files are
# identical. scripts/check-reproducible.sh runs it, and so does `make
# check-helper`.
#
# Usage: scripts/check-helper-independent.sh
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
ROOT="$(repo_root)"

TMP="$(mktemp -d)"
trap 'chmod -R u+w "$TMP" 2>/dev/null; rm -rf "$TMP"' EXIT

# Two values of each that no real build uses; the commits are visibly fake.
VERSION=0.0.1-check.1 COMMIT=1111111111111111111111111111111111111111 \
  "$ROOT/scripts/build-helper.sh" -o "$TMP/a" >/dev/null
VERSION=9.9.9-check.2 COMMIT=2222222222222222222222222222222222222222 \
  "$ROOT/scripts/build-helper.sh" -o "$TMP/b" >/dev/null

ha="$(sha256_of "$TMP/a")"; hb="$(sha256_of "$TMP/b")"
echo "helper, VERSION 0.0.1-check.1: $ha"
echo "helper, VERSION 9.9.9-check.2: $hb"
[ "$ha" = "$hb" ] || die "the update helper depends on VERSION or COMMIT (two builds differ); it must not (D-C80)"
echo "INDEPENDENT: the update helper is identical for two release versions and commits ($ha)"
