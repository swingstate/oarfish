#!/usr/bin/env bash
# Start the linux/arm64 binary from a release bundle and check it answers.
# On an x86-64 host the binary runs under qemu-user (no self-hosted runner, D-C12,
# ORB-CICD-004). Nothing is deployed anywhere; the server binds to loopback only.
#
# Usage: scripts/smoke-test-arm64.sh BUNDLE.tar.gz
#   ORBIT_SMOKE_EMULATOR  command prefix; default qemu-aarch64 unless the host is
#                         linux/arm64. Set to "none" to run the binary directly.
#   ORBIT_SMOKE_PORT      default 18484
#   EXPECTED_VERSION      if set, `orbit -version` must contain it
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

BUNDLE="${1:?usage: $0 BUNDLE.tar.gz}"
PORT="${ORBIT_SMOKE_PORT:-18484}"
BASE="http://127.0.0.1:$PORT"

emu="${ORBIT_SMOKE_EMULATOR:-}"
if [ -z "$emu" ]; then
  case "$(uname -s)/$(uname -m)" in
    Linux/aarch64|Linux/arm64) emu=none ;;
    *) emu=qemu-aarch64 ;;
  esac
fi
if [ "$emu" = none ]; then run=(); else
  command -v "$emu" >/dev/null || die "$emu not found (apt-get install qemu-user)"
  run=("$emu")
fi

TMP="$(mktemp -d)"
pid=""
cleanup() {
  [ -z "$pid" ] || { kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; }
  rm -rf "$TMP"
}
trap cleanup EXIT

tar -xzf "$BUNDLE" -C "$TMP"
BIN="$(ls "$TMP"/orbit_*/orbit)"
echo "binary: $(file -b "$BIN")"
file -b "$BIN" | grep -q 'ELF 64-bit.*ARM aarch64' || die "bundle binary is not an aarch64 ELF"

version_out="$("${run[@]}" "$BIN" -version)"
echo "version: $version_out"
[ -z "${EXPECTED_VERSION:-}" ] || grep -qF "orbit $EXPECTED_VERSION " <<<"$version_out" \
  || die "version output does not contain $EXPECTED_VERSION"

"${run[@]}" "$BIN" -listen "127.0.0.1:$PORT" >"$TMP/server.log" 2>&1 &
pid=$!

fail() { echo "SMOKE TEST FAILED: $*" >&2; echo "--- server log ---" >&2; cat "$TMP/server.log" >&2; exit 1; }

# Emulated startup is slow; allow up to 60 s.
code=000
for _ in $(seq 1 120); do
  kill -0 "$pid" 2>/dev/null || fail "server exited during startup"
  code="$(curl -s -o "$TMP/healthz.json" -w '%{http_code}' "$BASE/healthz" || true)"
  [ "$code" = 200 ] && break
  sleep 0.5
done
[ "$code" = 200 ] || fail "/healthz answered $code"
grep -q '"status":"ok"' "$TMP/healthz.json" || fail "/healthz body: $(cat "$TMP/healthz.json")"
echo "GET /healthz  -> 200 $(tr -d '\n' < "$TMP/healthz.json")"

code="$(curl -s -D "$TMP/root.hdr" -o "$TMP/root.html" -w '%{http_code}' "$BASE/")"
[ "$code" = 200 ] || fail "/ answered $code"
grep -q '<div id="app">' "$TMP/root.html" || fail "/ does not look like the Orbit frontend"
grep -qi '^content-type: text/html' "$TMP/root.hdr" || fail "/ is not served as text/html"
grep -qi '^content-security-policy:' "$TMP/root.hdr" || fail "/ lacks a Content-Security-Policy header"
echo "GET /         -> 200 text/html, embedded frontend"

asset="$(grep -oE 'src="/assets/[^"]+\.js"' "$TMP/root.html" | head -1 | sed -E 's/^src="(.*)"$/\1/')"
[ -n "$asset" ] || fail "index.html references no script asset"
code="$(curl -s -o "$TMP/asset.js" -w '%{http_code}' "$BASE$asset")"
[ "$code" = 200 ] && [ -s "$TMP/asset.js" ] || fail "$asset answered $code"
echo "GET $asset -> 200 ($(wc -c < "$TMP/asset.js" | tr -d ' ') bytes)"

echo "SMOKE TEST PASSED (${emu})"
summary <<SUM
### ARM64 smoke test
\`$(basename "$BUNDLE")\` extracted, started $([ "$emu" = none ] && echo "natively" || echo "under \`$emu\`"): \`/healthz\` returned 200, \`/\` served the embedded frontend.
SUM
