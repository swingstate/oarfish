#!/usr/bin/env bash
# Write a TEST copy of install.sh that trusts a throwaway public key instead of the
# release key, stamped with VERSION (or left unstamped with "none"). Used by the
# installer tests only; the copy must never be published. The production script
# has no way to swap its key, which is why tests need a modified copy.
#
# Usage: scripts/make-test-installer.sh VERSION|none PUBKEY_FILE OUTFILE
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
ROOT="$(repo_root)"
VER="${1:?usage: $0 VERSION|none PUBKEY_FILE OUTFILE}"
PUB="${2:?usage: $0 VERSION|none PUBKEY_FILE OUTFILE}"
OUT="${3:?usage: $0 VERSION|none PUBKEY_FILE OUTFILE}"

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
if [ "$VER" = none ]; then cp "$ROOT/install/install.sh" "$TMP/stamped.sh"
else "$ROOT/scripts/stamp-installer.sh" "$VER" "$TMP/stamped.sh" >/dev/null; fi

key="$(sed -n 2p "$PUB")"
id="$(sed -n 's/^untrusted comment: minisign public key \([0-9A-F]*\)$/\1/p' "$PUB")"
[ -n "$key" ] && [ -n "$id" ] || die "cannot read key and key id from $PUB"
sed -e "s|^OARFISH_PUBKEY='.*'\$|OARFISH_PUBKEY='$key'|" -e "s|^OARFISH_KEY_ID='.*'\$|OARFISH_KEY_ID='$id'|" "$TMP/stamped.sh" > "$OUT"
[ "$(grep -c -F "$key" "$OUT")" = 1 ] || die "could not substitute the test key"
