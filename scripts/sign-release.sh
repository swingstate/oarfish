#!/usr/bin/env bash
# Sign DIR/checksums.txt with minisign (Ed25519), writing DIR/checksums.txt.sig.
# Only checksums.txt is signed; it covers every other release file.
#
# Usage:
#   OARFISH_RELEASE_SIGNING_KEY="$(cat key)" scripts/sign-release.sh DIR
#   scripts/sign-release.sh --ephemeral PUBKEY_OUT DIR
#
# The release key comes only from the environment (a GitHub Actions secret). It is
# written to a mode-0600 temporary file, removed on exit, and never printed.
# --ephemeral signs with a throwaway key generated here (dry runs and tests);
# its public half is written to PUBKEY_OUT.
#   OARFISH_SIGN_COMMENT  trusted comment, covered by the signature
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
command -v minisign >/dev/null || die "minisign not found (scripts/install-minisign.sh)"

EPHEMERAL_PUB=""
if [ "${1:-}" = "--ephemeral" ]; then EPHEMERAL_PUB="${2:?--ephemeral needs a PUBKEY_OUT path}"; shift 2; fi
DIR="${1:?usage: $0 [--ephemeral PUBKEY_OUT] DIR}"
[ -f "$DIR/checksums.txt" ] || die "$DIR/checksums.txt not found"

umask 077
KEYDIR="$(mktemp -d)"
trap 'rm -rf "$KEYDIR"' EXIT
KEY="$KEYDIR/key"

if [ -n "$EPHEMERAL_PUB" ]; then
  minisign -G -W -f -p "$EPHEMERAL_PUB" -s "$KEY" >/dev/null
else
  [ -n "${OARFISH_RELEASE_SIGNING_KEY:-}" ] || die "OARFISH_RELEASE_SIGNING_KEY is not set"
  printf '%s\n' "$OARFISH_RELEASE_SIGNING_KEY" > "$KEY"
fi

# stdin from /dev/null: a password-protected key fails instead of hanging.
minisign -S -s "$KEY" -x "$DIR/checksums.txt.sig" \
  -t "${OARFISH_SIGN_COMMENT:-oarfish release}" -m "$DIR/checksums.txt" </dev/null >/dev/null
echo "signed $DIR/checksums.txt -> checksums.txt.sig"
