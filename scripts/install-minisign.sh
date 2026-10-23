#!/usr/bin/env bash
# Install the pinned minisign release on a Linux CI runner.
#
# minisign is the only signing tool (ADR 0005). The release is pinned by version
# and SHA-256. The checksum below was computed from the upstream archive after
# verifying its minisign signature (.minisig) against the upstream public key
# RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3 (from
# https://github.com/jedisct1/minisign). To bump: verify the new archive the same
# way, then change both values.
#
# Usage: scripts/install-minisign.sh [INSTALL_DIR]
# Adds INSTALL_DIR to $GITHUB_PATH when running in Actions.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

VERSION="0.12"
SHA256="9a599b48ba6eb7b1e80f12f36b94ceca7c00b7a5173c95c3efc88d9822957e73"
URL="https://github.com/jedisct1/minisign/releases/download/$VERSION/minisign-$VERSION-linux.tar.gz"

[ "$(uname -s)" = Linux ] || die "this installer is for Linux runners; install minisign with your package manager locally"
case "$(uname -m)" in
  x86_64) sub=x86_64 ;;
  aarch64|arm64) sub=aarch64 ;;
  *) die "unsupported architecture $(uname -m)" ;;
esac

DEST="${1:-${RUNNER_TEMP:-$(mktemp -d)}/minisign}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

curl --proto '=https' --tlsv1.2 -fsSL --retry 3 -o "$TMP/minisign.tar.gz" "$URL"
got="$(sha256_of "$TMP/minisign.tar.gz")"
[ "$got" = "$SHA256" ] || die "minisign archive checksum mismatch (got $got, want $SHA256)"

mkdir -p "$DEST"
tar -xzf "$TMP/minisign.tar.gz" -C "$TMP"
install -m 0755 "$TMP/minisign-linux/$sub/minisign" "$DEST/minisign"
[ -z "${GITHUB_PATH:-}" ] || echo "$DEST" >> "$GITHUB_PATH"
"$DEST/minisign" -v
