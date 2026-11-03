#!/usr/bin/env bash
# The release public key embedded in the update helper (go:embed cannot reach
# outside the Go module, so the helper package holds a copy) must be the
# committed release key, byte for byte. Same rule as check-installer-key.sh
# for install.sh (ADR 0010, U4).
#
# Usage: scripts/check-helper-key.sh [EMBEDDED_COPY] [PUBKEY_FILE]
#   defaults: backend/internal/trustkey/oarfish-release.pub, release/oarfish-release.pub
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
ROOT="$(repo_root)"
COPY="${1:-$ROOT/backend/internal/trustkey/oarfish-release.pub}"
PUB="${2:-$ROOT/release/oarfish-release.pub}"

[ -f "$COPY" ] || die "$COPY is missing"
want_id="$(sed -n 1p "$PUB" | sed -n 's/^untrusted comment: minisign public key \([0-9A-F]\{16\}\)$/\1/p')"
[ -n "$(sed -n 2p "$PUB")" ] && [ -n "$want_id" ] || die "cannot read key and key id from $PUB"

cmp -s "$COPY" "$PUB" || die "$COPY differs from $PUB (copy the release key into the helper package, or restore the copy)"
echo "update helper embeds the release key $want_id (byte-identical to $(basename "$PUB"))"
