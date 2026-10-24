#!/usr/bin/env bash
# Generate the CycloneDX SBOMs for a release (ORB-SEC-009, SOLL).
#
# Usage: scripts/make-sbom.sh OUTDIR
# Writes OUTDIR/sbom-backend.cdx.json and OUTDIR/sbom-frontend.cdx.json.
#
# backend:  cyclonedx-gomod, pinned below. It resolves the module graph of the
#           linux/arm64 build and records the Go standard library. Installed with
#           `go install module@version`, so the Go checksum database verifies it.
# frontend: `npm sbom` (built into npm), generated from package-lock.json. All
#           packages are devDependencies; the Svelte runtime ends up inside the
#           shipped JS, the rest is build tooling. The file does not tell them
#           apart, so it is a superset of what ships.
# SBOM files contain timestamps and UUIDs and are not reproducible byte for byte.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
ROOT="$(repo_root)"

CYCLONEDX_GOMOD_VERSION="v1.12.0"
OUT="${1:?usage: $0 OUTDIR}"
mkdir -p "$OUT"
OUT="$(cd "$OUT" && pwd)"

TOOLS="$(mktemp -d)"
trap 'rm -rf "$TOOLS"' EXIT
# Install for the host first; GOOS/GOARCH below describe the target being scanned.
GOBIN="$TOOLS" go install "github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@$CYCLONEDX_GOMOD_VERSION"
(
  cd "$ROOT/backend"
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 GOFLAGS=-mod=readonly \
    "$TOOLS/cyclonedx-gomod" app -json -std -main cmd/oarfish \
    -output "$OUT/sbom-backend.cdx.json"
)
(
  cd "$ROOT/frontend"
  npm sbom --sbom-format cyclonedx --package-lock-only > "$OUT/sbom-frontend.cdx.json"
)
for f in sbom-backend.cdx.json sbom-frontend.cdx.json; do
  [ -s "$OUT/$f" ] || die "$f is empty"
  echo "wrote $OUT/$f ($(wc -c < "$OUT/$f" | tr -d ' ') bytes)"
done
