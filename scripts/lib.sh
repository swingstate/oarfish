# Shared helpers, sourced by the other scripts. Not executable on its own.
# Scripts never enable `set -x`: signing scripts handle secret material.

# Resolved once, when sourced, so later `cd` calls cannot break relative paths.
OARFISH_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repo_root() { echo "$OARFISH_ROOT"; }

die() { echo "error: $*" >&2; exit 1; }

# sha256 of a file, with the tool the platform has.
sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
  else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

# sha256 -c equivalent, run in the current directory.
sha256_check() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum -c "$1"
  else shasum -a 256 -c "$1"; fi
}

# Build scripts/bundlegen into $1 (a path to the binary).
build_bundlegen() {
  (cd "$(repo_root)/scripts/bundlegen" && CGO_ENABLED=0 go build -trimpath -o "$1" .)
}

# Append to the GitHub job summary when running in Actions, else print.
summary() {
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then cat >> "$GITHUB_STEP_SUMMARY"; else cat; fi
}
