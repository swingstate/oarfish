#!/bin/sh
# Builds the throwaway probe for the G6.1 device spike on the development machine
# (never on the Raspberry Pi). Static linux/arm64 binary, standard library only,
# built outside every Go module. Writes build/oarfish-spike-probe (not committed)
# and probe.sha256 (committed, so that kitcheck on the device can compare).
#
# The checksum is only reproducible with the same Go toolchain: see README.md.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$here/build"
out="$here/build/oarfish-spike-probe"
build() {
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 GOFLAGS='' \
    go build -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o "$1" "$here/probe.go"
}
sha() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'; else shasum -a 256 "$1" | awk '{print $1}'; fi; }
build "$out"
tmp="$out.second"
build "$tmp"
a=$(sha "$out")
b=$(sha "$tmp")
rm -f "$tmp"
[ "$a" = "$b" ] || { echo "build-probe: two builds differ ($a, $b): not reproducible" >&2; exit 1; }
printf '%s  oarfish-spike-probe\n' "$a" > "$here/probe.sha256"
echo "go: $(go version)"
echo "sha256: $a"
echo "size: $(wc -c <"$out" | tr -d ' ') bytes"
