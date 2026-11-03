#!/usr/bin/env bash
# Start the linux/arm64 binary from a release bundle and check it answers.
# On an x86-64 host the binary runs under qemu-user (no self-hosted runner, D-C12,
# ORB-CICD-004). Nothing is deployed anywhere; the server binds to loopback only.
#
# Usage: scripts/smoke-test-arm64.sh BUNDLE.tar.gz [HELPER_BINARY]
#   HELPER_BINARY  the linux/arm64 update helper (scripts/build-helper.sh). It is not
#                  part of the bundle in G6.3a; with it the test also checks that
#                  `recover` exits 0 and that `serve` without socket activation
#                  exits non-zero.
#   OARFISH_SMOKE_EMULATOR  command prefix; default qemu-aarch64 unless the host is
#                         linux/arm64. Set to "none" to run the binary directly.
#   OARFISH_SMOKE_PORT      default 18484
#   EXPECTED_VERSION      if set, `oarfish -version` must contain it
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

BUNDLE="${1:?usage: $0 BUNDLE.tar.gz [HELPER_BINARY]}"
HELPER="${2:-}"
PORT="${OARFISH_SMOKE_PORT:-18484}"
BASE="http://127.0.0.1:$PORT"

emu="${OARFISH_SMOKE_EMULATOR:-}"
if [ -z "$emu" ]; then
  case "$(uname -s)/$(uname -m)" in
    Linux/aarch64|Linux/arm64) emu=none ;;
    *) emu=qemu-aarch64 ;;
  esac
fi
if [ "$emu" = none ]; then run=(env); else
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
BIN="$(ls "$TMP"/oarfish_*/oarfish)"
echo "binary: $(file -b "$BIN")"
file -b "$BIN" | grep -q 'ELF 64-bit.*ARM aarch64' || die "bundle binary is not an aarch64 ELF"

version_out="$("${run[@]}" "$BIN" -version)"
echo "version: $version_out"
[ -z "${EXPECTED_VERSION:-}" ] || grep -qF "oarfish $EXPECTED_VERSION " <<<"$version_out" \
  || die "version output does not contain $EXPECTED_VERSION"

# The service keeps its account, setup token and audit in the state directory
# (systemd passes STATE_DIRECTORY); a fresh one starts in setup mode.
mkdir -m 0700 "$TMP/state"
STATE_DIRECTORY="$TMP/state" "${run[@]}" "$BIN" -listen "127.0.0.1:$PORT" >"$TMP/server.log" 2>&1 &
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
grep -q '<div id="app">' "$TMP/root.html" || fail "/ does not look like the Oarfish frontend"
grep -qi '^content-type: text/html' "$TMP/root.hdr" || fail "/ is not served as text/html"
grep -qi '^content-security-policy:' "$TMP/root.hdr" || fail "/ lacks a Content-Security-Policy header"
echo "GET /         -> 200 text/html, embedded frontend"

asset="$(grep -oE 'src="/assets/[^"]+\.js"' "$TMP/root.html" | head -1 | sed -E 's/^src="(.*)"$/\1/')"
[ -n "$asset" ] || fail "index.html references no script asset"
code="$(curl -s -o "$TMP/asset.js" -w '%{http_code}' "$BASE$asset")"
[ "$code" = 200 ] && [ -s "$TMP/asset.js" ] || fail "$asset answered $code"
echo "GET $asset -> 200 ($(wc -c < "$TMP/asset.js" | tr -d ' ') bytes)"

# Access control (ADR 0009): a fresh state directory is in setup mode, the API
# needs a session, and the setup token file exists but is not in the log.
code="$(curl -s -o "$TMP/session.json" -w '%{http_code}' "$BASE/api/v1/session")"
if [ "$code" != 200 ] || ! grep -q '"state":"setup_required"' "$TMP/session.json"; then fail "/api/v1/session answered $code $(cat "$TMP/session.json")"; fi
echo "GET /api/v1/session -> 200 setup_required"
code="$(curl -s -o "$TMP/device.json" -w '%{http_code}' "$BASE/api/v1/device")"
if [ "$code" != 401 ] || ! grep -q '"setup_required"' "$TMP/device.json"; then fail "/api/v1/device answered $code without a session"; fi
echo "GET /api/v1/device  -> 401 setup_required (no data without an account)"
[ -s "$TMP/state/setup-token" ] || fail "no setup token file in the state directory"
if grep -qF "$(cat "$TMP/state/setup-token")" "$TMP/server.log"; then fail "the setup token is in the log"; fi
echo "setup token written to the state directory, not to the log"

# The update helper stub (ADR 0010 U1, G6.3a). Not started by systemd here, so
# `serve` must refuse: it never binds a socket itself.
helper_summary=""
if [ -n "$HELPER" ]; then
  echo "helper: $(file -b "$HELPER")"
  file -b "$HELPER" | grep -q 'ELF 64-bit.*ARM aarch64' || fail "the update helper is not an aarch64 ELF"
  mkdir "$TMP/helper-cwd"
  helper() { (cd "$TMP/helper-cwd" && env -u LISTEN_PID -u LISTEN_FDS -u LISTEN_FDNAMES "${run[@]}" "$HELPER" "$@"); }

  rc=0; out="$(helper recover 2>&1)" || rc=$?
  [ "$rc" = 0 ] || fail "helper recover exited $rc"
  [ -z "$out" ] || fail "helper recover printed: $out"
  echo "helper recover        -> exit 0, no output"

  rc=0; out="$(helper serve 2>&1)" || rc=$?
  [ "$rc" != 0 ] || fail "helper serve without socket activation exited 0"
  [ "$rc" = 1 ] || fail "helper serve without socket activation exited $rc, expected 1"
  [ "$out" = "oarfish-update-helper: not started by socket activation" ] || fail "helper serve printed: $out"
  echo "helper serve          -> exit 1 without socket activation"

  rc=0; out="$(cd "$TMP/helper-cwd" && env LISTEN_PID=1 LISTEN_FDS=1 "${run[@]}" "$HELPER" serve 2>&1)" || rc=$?
  [ "$rc" = 1 ] || fail "helper serve with another LISTEN_PID exited $rc, expected 1"
  echo "helper serve (LISTEN_PID of another process) -> exit 1"

  rc=0; out="$(helper status 2>&1)" || rc=$?
  [ "$rc" = 2 ] || fail "helper with an unknown subcommand exited $rc, expected 2"
  [ "$out" = "usage: oarfish-update-helper serve|recover" ] || fail "helper usage line: $out"
  echo "helper status         -> exit 2, usage line"

  [ -z "$(ls -A "$TMP/helper-cwd")" ] || fail "the helper created files"
  helper_summary=" The update helper stub (not shipped) answered as specified: \`recover\` exit 0, \`serve\` without socket activation exit 1, unknown subcommand exit 2."
fi

echo "SMOKE TEST PASSED (${emu})"
summary <<SUM
### ARM64 smoke test
\`$(basename "$BUNDLE")\` extracted, started $([ "$emu" = none ] && echo "natively" || echo "under \`$emu\`"): \`/healthz\` returned 200, \`/\` served the embedded frontend, a fresh state directory was in setup mode and \`/api/v1/device\` answered 401 without a session.$helper_summary
SUM
