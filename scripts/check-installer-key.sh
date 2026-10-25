#!/usr/bin/env bash
# The public key embedded in install.sh must be the committed release key
# (release/oarfish-release.pub, ADR 0005/0006): same key line, same key id.
#
# Usage: scripts/check-installer-key.sh [INSTALL_SH] [PUBKEY_FILE]
#   defaults: install/install.sh, release/oarfish-release.pub
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
ROOT="$(repo_root)"
SH="${1:-$ROOT/install/install.sh}"
PUB="${2:-$ROOT/release/oarfish-release.pub}"

want_key="$(sed -n 2p "$PUB")"
want_id="$(sed -n 1p "$PUB" | sed -n 's/^untrusted comment: minisign public key \([0-9A-F]\{16\}\)$/\1/p')"
[ -n "$want_key" ] && [ -n "$want_id" ] || die "cannot read key and key id from $PUB"

have_key="$(sed -n "s/^OARFISH_PUBKEY='\\(.*\\)'\$/\\1/p" "$SH")"
have_id="$(sed -n "s/^OARFISH_KEY_ID='\\(.*\\)'\$/\\1/p" "$SH")"
[ "$have_key" = "$want_key" ] || die "$SH embeds a different public key than $PUB"
[ "$have_id" = "$want_id" ] || die "$SH embeds key id '$have_id', $PUB has '$want_id'"
echo "install.sh embeds the release key $want_id (matches $(basename "$PUB"))"
