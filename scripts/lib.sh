# shellcheck shell=bash
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

# Signature algorithm of a minisign .sig file: "Ed" (legacy, plain Ed25519) or
# "ED" (prehashed with BLAKE2b). It is the first two bytes of the line-2 payload.
sig_algorithm() {
  sed -n 2p "$1" | { base64 -d 2>/dev/null || base64 -D; } | head -c 2
}

# ---- Release history (D-C92 and the two-step rule) ---------------------------
# Shared by scripts/check-component-history.sh and scripts/check-unit-forms.sh.

# The tag rule of release.yml.
OARFISH_TAG_RE='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'

# The tag a manifest written by bundlegen names (its one "tag" line).
manifest_tag() {
  local t
  t="$(sed -n 's/^  "tag": "\(.*\)",$/\1/p' "$1")"
  [[ "$t" =~ $OARFISH_TAG_RE ]] || die "cannot read the tag of $1"
  printf '%s\n' "$t"
}

# The release tags of repository $1 in release order, oldest first: every tag
# that matches the tag rule, sorted by creation date (the tagger date of an
# annotated tag, the commit date of a lightweight one), ties by name. A shallow
# clone or a clone without tags fails, because it would look like a repository
# without releases and every check would pass.
release_tags() {
  local tags
  [ "$(git -C "$1" rev-parse --is-shallow-repository)" = false ] \
    || die "the clone in $1 is shallow; the release checks need every tag (fetch-depth: 0, or git fetch --unshallow --tags)"
  tags="$(git -C "$1" for-each-ref --sort=refname --sort=creatordate --format='%(refname:lstrip=2)' 'refs/tags/v*' \
    | grep -E "$OARFISH_TAG_RE" || true)"
  [ -n "$tags" ] || die "no release tag in $1; the release checks need the tags (git fetch --tags)"
  printf '%s\n' "$tags"
}

# The release tags of repository $1 created before tag $2, oldest first; all of
# them when $2 is not a tag of the repository (a dry run). The last line is the
# directly preceding release. Prints nothing when $2 is the first tag.
release_tags_before() {
  local tags
  tags="$(release_tags "$1")" || exit 1
  printf '%s\n' "$tags" | awk -v t="$2" '$0 == t { exit } { print }'
}

# release_exception FILE TAG CHECK: sets EXCEPTION_REASON to the reason of the
# line "TAG CHECK reason..." of FILE, or to "" when FILE names no exception for
# TAG and CHECK. CHECK is component-set or unit-forms. Blank lines and lines
# that start with # are ignored. A malformed line, an unknown check, a missing
# reason or a second line for the same TAG and CHECK ends the script: a file
# that cannot be read exactly is not an exception (fail closed). Call it
# directly, not in $(...), so that its die ends the calling script.
release_exception() {
  local file="$1" tag="$2" check="$3" line t c r n=0
  EXCEPTION_REASON=""
  [ -f "$file" ] || die "the exceptions file $file does not exist"
  while IFS= read -r line || [ -n "$line" ]; do
    n=$((n + 1))
    case "$line" in '' | '#'*) continue ;; esac
    read -r t c r <<< "$line"
    [[ "$t" =~ $OARFISH_TAG_RE ]] || die "$file:$n: '$t' is not a release tag"
    case "$c" in component-set | unit-forms) ;; *) die "$file:$n: unknown check '$c' (component-set or unit-forms)" ;; esac
    [ -n "$r" ] || die "$file:$n: an exception needs a reason"
    if [ "$t" = "$tag" ] && [ "$c" = "$check" ]; then
      [ -z "$EXCEPTION_REASON" ] || die "$file:$n: a second exception for $tag $check"
      EXCEPTION_REASON="$r"
    fi
  done < "$file"
}
