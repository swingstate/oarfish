#!/usr/bin/env bash
# The privilege broker's bytes must not depend on the release version or the
# commit (D-C80 applied to the broker, ADR 0011 B1). Under layout L1 the
# broker travels in the helper's component set (ADR 0010 A19, G7.3), so a
# broker that carried the release version would make every release change a
# privileged file.
#
# Builds the broker with scripts/build-broker.sh twice, with two different
# VERSION and COMMIT values in the environment, and fails unless both files
# are identical. `make check-broker` runs it.
#
# Usage: scripts/check-broker-independent.sh
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
ROOT="$(repo_root)"

TMP="$(mktemp -d)"
trap 'chmod -R u+w "$TMP" 2>/dev/null; rm -rf "$TMP"' EXIT

# Two values of each that no real build uses; the commits are visibly fake.
VERSION=0.0.1-check.1 COMMIT=1111111111111111111111111111111111111111 \
  "$ROOT/scripts/build-broker.sh" -o "$TMP/a" >/dev/null
VERSION=9.9.9-check.2 COMMIT=2222222222222222222222222222222222222222 \
  "$ROOT/scripts/build-broker.sh" -o "$TMP/b" >/dev/null

ha="$(sha256_of "$TMP/a")"; hb="$(sha256_of "$TMP/b")"
echo "broker, VERSION 0.0.1-check.1: $ha"
echo "broker, VERSION 9.9.9-check.2: $hb"
[ "$ha" = "$hb" ] || die "the privilege broker depends on VERSION or COMMIT (two builds differ); it must not (D-C80)"
echo "INDEPENDENT: the privilege broker is identical for two release versions and commits ($ha)"
