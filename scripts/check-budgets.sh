#!/usr/bin/env bash
# Enforce and report the size budgets.
#   ORB-NFR-005: ARM64 release including frontend stays under 35 MB
#   ORB-NFR-006: compressed initial frontend bundle stays under 750 KB
# Sizes use decimal units (1 MB = 1,000,000 bytes), which is the stricter reading.
# MAX_BUNDLE_BYTES / MAX_FRONTEND_GZIP_BYTES override the limits (used to test failure).
#
# Usage: scripts/check-budgets.sh BUNDLE.tar.gz
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
ROOT="$(repo_root)"
MAX_BUNDLE_BYTES="${MAX_BUNDLE_BYTES:-35000000}"
MAX_FRONTEND_GZIP_BYTES="${MAX_FRONTEND_GZIP_BYTES:-750000}"

BUNDLE="${1:?usage: $0 BUNDLE.tar.gz}"
DIST="$ROOT/backend/internal/web/dist"
[ -f "$DIST/index.html" ] || die "frontend is not built: $DIST/index.html missing"

bundle_bytes="$(wc -c < "$BUNDLE" | tr -d ' ')"

# Initial load = index.html plus every local script/stylesheet it references.
gz() { gzip -9 -n -c "$1" | wc -c | tr -d ' '; }
refs="$(grep -oE '(src|href)="/[^"/][^"]*"' "$DIST/index.html" | sed -E 's/^(src|href)="\/([^"]*)"$/\2/' | sort -u)"
frontend_gz="$(gz "$DIST/index.html")"
frontend_raw="$(wc -c < "$DIST/index.html" | tr -d ' ')"
rows="| index.html | $frontend_raw | $frontend_gz |"
while IFS= read -r ref; do
  [ -n "$ref" ] || continue
  [ -f "$DIST/$ref" ] || die "index.html references $ref, which is not in the build"
  r="$(wc -c < "$DIST/$ref" | tr -d ' ')"; g="$(gz "$DIST/$ref")"
  frontend_raw=$((frontend_raw + r)); frontend_gz=$((frontend_gz + g))
  rows="$rows"$'\n'"| $ref | $r | $g |"
done <<< "$refs"

status=0
bv="within budget"; fv="within budget"
if [ "$bundle_bytes" -gt "$MAX_BUNDLE_BYTES" ]; then bv="OVER BUDGET"; status=1; fi
if [ "$frontend_gz" -gt "$MAX_FRONTEND_GZIP_BYTES" ]; then fv="OVER BUDGET"; status=1; fi

summary <<SUM
### Size budgets
| Item | Size (bytes) | Budget (bytes) | Result |
|---|---|---|---|
| Release bundle \`$(basename "$BUNDLE")\` (ORB-NFR-005) | $bundle_bytes | $MAX_BUNDLE_BYTES | $bv |
| Initial frontend, gzip -9 (ORB-NFR-006) | $frontend_gz | $MAX_FRONTEND_GZIP_BYTES | $fv |

| Initial frontend file | Raw (bytes) | gzip -9 (bytes) |
|---|---|---|
$rows
SUM
echo "bundle:   $bundle_bytes bytes (budget $MAX_BUNDLE_BYTES): $bv"
echo "frontend: $frontend_gz bytes gzip, $frontend_raw bytes raw (budget $MAX_FRONTEND_GZIP_BYTES): $fv"
exit "$status"
