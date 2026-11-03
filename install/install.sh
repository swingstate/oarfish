#!/bin/sh
# Oarfish installer for Linux ARM64 (Gate G3, ADR 0006).
#
# Recommended use: download, read, then run (do not pipe into a shell):
#
#   curl -fsSLO https://github.com/swingstate/oarfish/releases/download/<tag>/install.sh
#   less install.sh
#   sudo sh install.sh --dry-run        # prints what would happen, changes nothing
#   sudo sh install.sh
#
# What it does, in this order (specification 9.4): checks the system, downloads
# checksums.txt and its signature, VERIFIES THE ED25519 SIGNATURE with the system
# openssl and the public key embedded below, downloads the bundle and manifest,
# verifies their SHA-256 against the signed checksums.txt, and only then creates
# the service user and directories, installs the release to
# /opt/oarfish/releases/<version>, switches /opt/oarfish/current atomically,
# installs the systemd unit, starts the service and checks /healthz.
#
# A release can carry the update helper (ADR 0010, "components" bundle shape): the
# installer then also installs it below /opt/oarfish/components, links it into /usr
# and /etc/systemd/system and locks /var/lib/oarfish-update (waits at most 30 s). A
# release without the helper ("legacy") is installed as before, and refused where the
# helper layout exists.
#
# Nothing is downloaded from anywhere but GitHub Releases over HTTPS, no token is
# used, nothing is sent anywhere. Re-running is safe. Failed downloads or
# verification leave an existing installation untouched.
#
# Options:
#   --version TAG   install this release (v0.1.0-alpha.2 or 0.1.0-alpha.2)
#   --dry-run       check the system and print the plan; change nothing
#   --uninstall     remove service, units, binaries and cache; keep your data
#   --purge         with --uninstall: also remove /etc/oarfish, /var/lib/oarfish,
#                   /var/lib/oarfish-update and the oarfish user
#   --help
#
# Tests only: when OARFISH_TEST_ROOT is set, all paths live below that directory,
# root and a running systemd are not required, and OARFISH_RELEASE_BASE_URL,
# OARFISH_OPENSSL, OARFISH_SYSTEMCTL, OARFISH_HEALTH_URL, OARFISH_HEALTH_TIMEOUT,
# OARFISH_HEALTHCHECK=skip and OARFISH_TEST_USERS=1 (act as root for the user and
# ownership steps, with stub tools on PATH), OARFISH_LOCK_WAIT (seconds, default 30)
# and OARFISH_TEST_ROOT_UID/_GID (the ids that stand for root when the owner of
# /var/lib/oarfish-update is checked; default: the caller's) are honoured. Without
# OARFISH_TEST_ROOT they are ignored.

set -eu
umask 022

# ---- constants -------------------------------------------------------------
OARFISH_REPO='swingstate/oarfish'
# Replaced by scripts/stamp-installer.sh with the release version at build time.
OARFISH_STAMPED_VERSION='@@OARFISH_VERSION@@'
# Public release key, the second line of release/oarfish-release.pub (ADR 0005).
OARFISH_KEY_ID='11F1C52777090BFC'
OARFISH_PUBKEY='RWT8Cwl3J8XxETRSvWsyNWIiVFVSOYnaL9IiJk0ylKyklmYUsRZ+1w6p'

SERVICE_USER='oarfish'
LISTEN_ADDR='127.0.0.1:8484'
MIN_FREE_KB=65536

# ---- paths -----------------------------------------------------------------
TESTMODE=0
ROOT=''
if [ -n "${OARFISH_TEST_ROOT:-}" ]; then
  TESTMODE=1
  ROOT=${OARFISH_TEST_ROOT%/}
fi
OPT_DIR=$ROOT/opt/oarfish
RELEASES_DIR=$OPT_DIR/releases
CURRENT_LINK=$OPT_DIR/current
ETC_DIR=$ROOT/etc/oarfish
STATE_DIR=$ROOT/var/lib/oarfish
CACHE_DIR=$ROOT/var/cache/oarfish
COMP_DIR=$OPT_DIR/components
UPDATE_STATE_DIR=$ROOT/var/lib/oarfish-update
LIBEXEC_DIR=$ROOT/usr/libexec
HELPER_LINK=$LIBEXEC_DIR/oarfish-update-helper
UNIT_DIR=$ROOT/etc/systemd/system
UNIT_PATH=$UNIT_DIR/oarfish.service
SOCKET_UNIT=$UNIT_DIR/oarfish-update-helper.socket
HELPER_UNIT=$UNIT_DIR/oarfish-update-helper.service
RECOVER_UNIT=$UNIT_DIR/oarfish-update-recover.service
MULTI_WANTS=$UNIT_DIR/multi-user.target.wants
SOCKETS_WANTS=$UNIT_DIR/sockets.target.wants
# Link targets are written as on the real system, without the test root.
REAL_OPT=/opt/oarfish
COMP_FILES='oarfish-update-helper oarfish-update-helper.socket oarfish-update-helper.service oarfish-update-recover.service'

OPENSSL=openssl
SYSTEMCTL=systemctl
HEALTH_URL="http://$LISTEN_ADDR/healthz"
HEALTH_TIMEOUT=30
BASE_URL_OVERRIDE=''
ROOT_UID=0
ROOT_GID=0
LOCK_WAIT=30
if [ "$TESTMODE" = 1 ]; then
  ROOT_UID=${OARFISH_TEST_ROOT_UID:-$(id -u)}
  ROOT_GID=${OARFISH_TEST_ROOT_GID:-$(id -g)}
  LOCK_WAIT=${OARFISH_LOCK_WAIT:-$LOCK_WAIT}
  OPENSSL=${OARFISH_OPENSSL:-openssl}
  SYSTEMCTL=${OARFISH_SYSTEMCTL:-systemctl}
  HEALTH_URL=${OARFISH_HEALTH_URL:-$HEALTH_URL}
  HEALTH_TIMEOUT=${OARFISH_HEALTH_TIMEOUT:-$HEALTH_TIMEOUT}
  BASE_URL_OVERRIDE=${OARFISH_RELEASE_BASE_URL:-}
fi

# ---- output ----------------------------------------------------------------
say()  { printf '%s\n' "$*"; }
warn() { printf 'oarfish-install: warning: %s\n' "$*" >&2; }
die()  { printf 'oarfish-install: error: %s\n' "$*" >&2; exit 1; }

WORK=''
STAGE=''
cleanup() {
  if [ -n "$STAGE" ]; then rm -rf "$STAGE"; fi
  if [ -n "$WORK" ]; then rm -rf "$WORK"; fi
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

usage() {
  cat <<'EOF'
Usage: sh install.sh [--version TAG] [--dry-run]
       sh install.sh --uninstall [--purge] [--dry-run]

  --version TAG   install this release (v0.1.0-alpha.2 or 0.1.0-alpha.2)
  --dry-run       check the system and print the plan; change nothing
  --uninstall     remove service, units, binaries and cache; keep your data
  --purge         with --uninstall: also remove /etc/oarfish, /var/lib/oarfish,
                  /var/lib/oarfish-update and the oarfish user
  --help          this text
EOF
}

# ---- small helpers ---------------------------------------------------------
file_sha() { sha256sum "$1" | awk '{print $1}'; }

# True when we may really create users and change ownership.
can_manage_users() { [ "$TESTMODE" = 0 ] || [ "$(id -u)" = 0 ] || [ "${OARFISH_TEST_USERS:-}" = 1 ]; }

valid_version() {
  case $1 in *[!0-9A-Za-z.-]*) return 1 ;; esac
  printf '%s\n' "$1" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'
}

# Remove a path only if it is one of the directories this installer owns.
safe_rm_rf() {
  case $1 in
    "$OPT_DIR"|"$CACHE_DIR"|"$ETC_DIR"|"$STATE_DIR"|"$UPDATE_STATE_DIR") rm -rf "$1" ;;
    *) die "internal error: refusing to remove $1" ;;
  esac
}

# File descriptor 9 holds the lock (take_lock); a child must not inherit it.
systemctl_cmd() { "$SYSTEMCTL" "$@" 9<&-; }

# ---- platform checks (ORB-INS-002) -----------------------------------------
os_release_value() {
  [ -r "$ROOT/etc/os-release" ] || return 0
  sed -n "s/^$1=//p" "$ROOT/etc/os-release" | head -n 1 | sed 's/^"//; s/"$//'
}

check_platform() {
  [ "$(uname -s)" = Linux ] || die "unsupported operating system '$(uname -s)'. Oarfish needs Linux."
  case $(uname -m) in
    aarch64|arm64) ;;
    *) die "unsupported CPU architecture '$(uname -m)'. This release is built for 64-bit ARM (aarch64) only." ;;
  esac
  OS_NAME=$(os_release_value PRETTY_NAME)
  if [ -z "$OS_NAME" ]; then
    warn "no readable /etc/os-release; continuing in generic mode"
    OS_NAME=unknown
  fi
  # /run/systemd/system exists only when systemd is the running init (sd_booted).
  [ -d "$ROOT/run/systemd/system" ] \
    || die "systemd is not the running init system. Oarfish installs a systemd service and cannot continue."
  command -v "$SYSTEMCTL" >/dev/null 2>&1 || die "systemctl was not found."
}

check_tools() {
  missing=''
  for t in curl tar gzip sha256sum od base64 mktemp awk df id mv ln rm mkdir chmod cat cp sed grep tr head tail wc sleep uname readlink dirname ls flock; do
    command -v "$t" >/dev/null 2>&1 || missing="$missing $t"
  done
  if can_manage_users; then
    command -v chown >/dev/null 2>&1 || missing="$missing chown"
    command -v getent >/dev/null 2>&1 || missing="$missing getent"
    command -v useradd >/dev/null 2>&1 || command -v adduser >/dev/null 2>&1 || missing="$missing useradd-or-adduser"
  fi
  command -v "$OPENSSL" >/dev/null 2>&1 || missing="$missing openssl"
  [ -z "$missing" ] || die "required tools are missing:$missing. Install them with your package manager (for example: apt install curl openssl; flock is part of util-linux) and run the installer again."
}

# Only OpenSSL 3.x can verify a raw Ed25519 signature on the command line
# (pkeyutl -rawin). LibreSSL and OpenSSL 1.x cannot (ADR 0006).
check_openssl() {
  ver=$("$OPENSSL" version 2>/dev/null) || die "could not run '$OPENSSL version'."
  # shellcheck disable=SC2086 # splitting into words is intended
  set -- $ver
  [ "${1:-}" = OpenSSL ] || die "'$ver' is not OpenSSL. OpenSSL 3.0 or newer is required to verify the release signature."
  major=${2%%.*}
  case $major in ''|*[!0-9]*) die "cannot read the OpenSSL version from '$ver'." ;; esac
  [ "$major" -ge 3 ] || die "OpenSSL $2 is too old. OpenSSL 3.0 or newer is required to verify the release signature. Use a current Raspberry Pi OS (Bookworm or newer) or Ubuntu Server LTS."
  OPENSSL_VERSION=$ver
}

nearest_existing() {
  d=$1
  while [ ! -d "$d" ] && [ "$d" != / ] && [ -n "$d" ]; do d=$(dirname "$d"); done
  printf '%s\n' "$d"
}

check_space() {
  for d in "$OPT_DIR" "${TMPDIR:-/tmp}"; do
    probe=$(nearest_existing "$d")
    free=$(df -Pk "$probe" | awk 'NR == 2 { print $4 }')
    case $free in ''|*[!0-9]*) die "cannot determine free space on $probe." ;; esac
    [ "$free" -ge "$MIN_FREE_KB" ] || die "not enough free space on $probe: $((free / 1024)) MB free, $((MIN_FREE_KB / 1024)) MB needed."
  done
}

require_root() {
  if [ "$TESTMODE" = 0 ] && [ "$(id -u)" != 0 ]; then
    die "this installer must run as root. Use: sudo sh $0"
  fi
}

# ---- signature verification (ADR 0006) -------------------------------------
# The public key is a constant of this script. The 42-byte minisign key is
# 'Ed' + 8-byte key id + 32-byte Ed25519 key; wrapped in the fixed 12-byte
# SubjectPublicKeyInfo header it becomes a DER key that openssl reads.
prepare_key() {
  printf '%s' "$OARFISH_PUBKEY" | base64 -d > "$WORK/key.bin" 2>/dev/null || die "internal error: embedded key is not valid base64."
  [ "$(wc -c < "$WORK/key.bin" | tr -d ' ')" = 42 ] || die "internal error: embedded key has the wrong length."
  [ "$(od -An -tx1 -N2 "$WORK/key.bin" | tr -d ' \n')" = 4564 ] || die "internal error: embedded key is not an Ed25519 minisign key."
  KEY_ID_LE=$(od -An -tx1 -j2 -N8 "$WORK/key.bin" | tr -d ' \n')
  # minisign prints the key id with the bytes reversed.
  KEY_ID_SHOWN=$(printf '%s' "$KEY_ID_LE" | sed 's/\(..\)\(..\)\(..\)\(..\)\(..\)\(..\)\(..\)\(..\)/\8\7\6\5\4\3\2\1/' | tr 'a-f' 'A-F')
  [ "$KEY_ID_SHOWN" = "$OARFISH_KEY_ID" ] || die "internal error: embedded key does not match key id $OARFISH_KEY_ID."
  { printf '\060\052\060\005\006\003\053\145\160\003\041\000'; tail -c 32 "$WORK/key.bin"; } > "$WORK/key.der"
}

# verify_signature FILE SIGFILE: success only for a legacy-mode ('Ed') minisign
# signature made by the embedded key over exactly the bytes of FILE. The trusted
# comment is ignored, as ADR 0005 requires of a verifier that does not check it.
verify_signature() {
  vdir=$(mktemp -d "$WORK/verify.XXXXXX") || return 1
  sig_b64=$(sed -n 2p "$2")
  [ -n "$sig_b64" ] || return 1
  printf '%s' "$sig_b64" | base64 -d > "$vdir/sig.bin" 2>/dev/null || return 1
  [ "$(wc -c < "$vdir/sig.bin" | tr -d ' ')" = 74 ] || return 1
  [ "$(od -An -tx1 -N2 "$vdir/sig.bin" | tr -d ' \n')" = 4564 ] || return 1
  [ "$(od -An -tx1 -j2 -N8 "$vdir/sig.bin" | tr -d ' \n')" = "$KEY_ID_LE" ] || return 1
  tail -c 64 "$vdir/sig.bin" > "$vdir/sig.raw"
  "$OPENSSL" pkeyutl -verify -pubin -inkey "$WORK/key.der" -keyform DER -rawin \
    -in "$1" -sigfile "$vdir/sig.raw" >/dev/null 2>&1
}

# Negative control: a file with its first byte changed must be rejected. If it
# is not, this openssl does not really verify and nothing it says can be trusted.
verifier_self_test() {
  { printf '#'; tail -c +2 "$1"; } > "$WORK/modified.txt"
  if verify_signature "$WORK/modified.txt" "$2"; then
    die "the signature verifier accepted a modified file. Refusing to continue: this openssl cannot be trusted."
  fi
}

# ---- download --------------------------------------------------------------
fetch() { # fetch FILE_NAME: download one release file into $WORK
  proto='=https'
  if [ "$TESTMODE" = 1 ]; then proto='=https,file'; fi
  curl --proto "$proto" --proto-redir '=https' --tlsv1.2 -fsSL --retry 3 --connect-timeout 20 \
    --max-filesize 100000000 -o "$WORK/$1" "$BASE_URL/$1" \
    || die "download of $BASE_URL/$1 failed. Nothing was changed."
  [ -s "$WORK/$1" ] || die "downloaded $1 is empty. Nothing was changed."
}

hash_listed() { # hash_listed NAME: SHA-256 of NAME from the (verified) checksums.txt
  h=$(awk -v n="$1" '$2 == n { c++; v = $1 } END { if (c == 1) print v }' "$WORK/checksums.txt")
  printf '%s\n' "$h" | grep -Eq '^[0-9a-f]{64}$' || die "checksums.txt does not list $1 exactly once."
  printf '%s\n' "$h"
}

check_listed_file() { # check_listed_file NAME
  want=$(hash_listed "$1")
  [ "$(file_sha "$WORK/$1")" = "$want" ] \
    || die "$1 does not match the SHA-256 in the signed checksums.txt. The download is corrupt or has been tampered with. Nothing was changed."
}

manifest_string() { # top-level string field of manifest.json (two-space indent)
  sed -n "s/^  \"$1\": \"\\([^\"]*\\)\",\\{0,1\\}\$/\\1/p" "$WORK/manifest.json"
}

check_manifest() {
  [ "$(manifest_string name)" = oarfish ] || die "manifest.json: unexpected product name."
  [ "$(manifest_string version)" = "$VERSION" ] || die "manifest.json: version differs from the requested $VERSION."
  [ "$(manifest_string tag)" = "v$VERSION" ] || die "manifest.json: tag differs from v$VERSION."
  [ "$(manifest_string os)" = linux ] || die "manifest.json: not a Linux release."
  [ "$(manifest_string architecture)" = arm64 ] || die "manifest.json: not an arm64 release."
  grep -q '^  "schema_version": 1,$' "$WORK/manifest.json" || die "manifest.json: unsupported schema version."
  grep -q "^    \"file\": \"$BUNDLE\",\$" "$WORK/manifest.json" || die "manifest.json: bundle file name differs from $BUNDLE."
}

# The bundle has exactly one of two shapes, all entries regular files, checked before
# anything is extracted: legacy (the directory, oarfish, oarfish.service, LICENSE,
# THIRD_PARTY_NOTICES.txt: five entries) or components (those plus oarfish-update-helper
# and the helper service, helper socket and recover units: nine). The shape must agree
# with the manifest, which has a "components" object if and only if the bundle does.
# The manifest's key id is not compared with this installer's; the release build checks it.
check_bundle_entries() {
  tar -tvzf "$WORK/$BUNDLE" | awk '{ t = substr($1, 1, 1); if (t != "-" && t != "d") bad = 1 } END { exit bad }' \
    || die "the bundle contains something other than regular files and directories."
  tar -tzf "$WORK/$BUNDLE" > "$WORK/entries" || die "cannot list the bundle."
  n=0
  extra=0
  seen=' '
  while IFS= read -r e; do
    case $e in
      "$BUNDLE_ROOT"/|"$BUNDLE_ROOT"/oarfish|"$BUNDLE_ROOT"/oarfish.service|"$BUNDLE_ROOT"/LICENSE|"$BUNDLE_ROOT"/THIRD_PARTY_NOTICES.txt) ;;
      "$BUNDLE_ROOT"/oarfish-update-helper|"$BUNDLE_ROOT"/oarfish-update-helper.socket|"$BUNDLE_ROOT"/oarfish-update-helper.service|"$BUNDLE_ROOT"/oarfish-update-recover.service) extra=$((extra + 1)) ;;
      *) die "the bundle contains an unexpected entry '$e'." ;;
    esac
    case $seen in *" $e "*) die "the bundle lists the entry '$e' twice." ;; esac
    seen="$seen$e "
    n=$((n + 1))
  done < "$WORK/entries"
  if [ "$n" = 5 ] && [ "$extra" = 0 ]; then
    BUNDLE_SHAPE=legacy
  elif [ "$n" = 9 ] && [ "$extra" = 4 ]; then
    BUNDLE_SHAPE=components
  else
    die "the bundle has neither the legacy shape (five entries) nor the components shape (nine entries)."
  fi
  has_components=0
  if grep -q '^  "components": {$' "$WORK/manifest.json"; then has_components=1; fi
  if [ "$BUNDLE_SHAPE" = components ] && [ "$has_components" = 0 ]; then
    die "the bundle has the components shape but manifest.json has no components object."
  fi
  if [ "$BUNDLE_SHAPE" = legacy ] && [ "$has_components" = 1 ]; then
    die "manifest.json declares components but the bundle does not carry them."
  fi
}

# An ELF64 little-endian executable for machine 0xB7 (AArch64).
check_binary() { # check_binary FILE NAME
  magic=$(od -An -tx1 -N6 "$1" | tr -d ' \n')
  machine=$(od -An -tx1 -j18 -N2 "$1" | tr -d ' \n')
  if [ "$magic" != 7f454c460201 ] || [ "$machine" != b700 ]; then
    die "the $2 binary in the bundle is not a 64-bit ARM (aarch64) executable."
  fi
}

download_and_verify() {
  say "==> downloading and verifying $TAG"
  fetch checksums.txt
  fetch checksums.txt.sig
  prepare_key
  verifier_self_test "$WORK/checksums.txt" "$WORK/checksums.txt.sig"
  verify_signature "$WORK/checksums.txt" "$WORK/checksums.txt.sig" \
    || die "SIGNATURE VERIFICATION FAILED for checksums.txt. This release was not signed with the Oarfish release key $OARFISH_KEY_ID. Nothing was changed."
  say "    signature OK (key $OARFISH_KEY_ID, Ed25519, verified with $OPENSSL_VERSION)"
  fetch manifest.json
  fetch "$BUNDLE"
  check_listed_file manifest.json
  check_listed_file "$BUNDLE"
  say "    SHA-256 of manifest.json and $BUNDLE OK"
  check_manifest
  check_bundle_entries
  mkdir "$WORK/extract"
  tar -xzf "$WORK/$BUNDLE" -C "$WORK/extract"
  check_binary "$WORK/extract/$BUNDLE_ROOT/oarfish" oarfish
  if [ "$BUNDLE_SHAPE" = components ]; then
    check_binary "$WORK/extract/$BUNDLE_ROOT/oarfish-update-helper" oarfish-update-helper
  fi
  say "    bundle contents OK ($BUNDLE_SHAPE shape)"
}

# ---- installation ----------------------------------------------------------
set_owner() { # set_owner OWNER:GROUP PATH...: the owner applies to every path
  owner=$1
  shift
  if can_manage_users; then
    for path in "$@"; do chown "$owner" "$path"; done
  fi
}

make_dir() { # make_dir PATH MODE OWNER:GROUP (mode and owner only for new directories)
  if [ -d "$1" ]; then return 0; fi
  mkdir -p "$1"
  chmod "$2" "$1"
  set_owner "$3" "$1"
}

ensure_user() {
  if ! can_manage_users; then
    say "    (test mode: not creating user $SERVICE_USER)"
    return 0
  fi
  if id -u "$SERVICE_USER" >/dev/null 2>&1; then
    [ "$(id -u "$SERVICE_USER")" != 0 ] || die "the user $SERVICE_USER has uid 0; refusing to use it as service user."
    say "    service user $SERVICE_USER exists"
    return 0
  fi
  nologin=$(command -v nologin 2>/dev/null || true)
  [ -n "$nologin" ] || nologin=/usr/sbin/nologin
  if command -v useradd >/dev/null 2>&1; then
    if getent group "$SERVICE_USER" >/dev/null 2>&1; then grp='--gid'; else grp='--user-group'; fi
    if [ "$grp" = '--gid' ]; then
      useradd --system --gid "$SERVICE_USER" --no-create-home --home-dir /var/lib/oarfish --shell "$nologin" "$SERVICE_USER"
    else
      useradd --system --user-group --no-create-home --home-dir /var/lib/oarfish --shell "$nologin" "$SERVICE_USER"
    fi
  else
    adduser --system --group --no-create-home --home /var/lib/oarfish --shell "$nologin" "$SERVICE_USER"
  fi
  say "    created system user $SERVICE_USER (no login shell)"
}

ensure_dirs() {
  make_dir "$OPT_DIR" 0755 root:root
  make_dir "$RELEASES_DIR" 0755 root:root
  make_dir "$ETC_DIR" 0750 "root:$SERVICE_USER"
  make_dir "$ETC_DIR/secrets" 0700 "$SERVICE_USER:$SERVICE_USER"
  make_dir "$STATE_DIR" 0750 "$SERVICE_USER:$SERVICE_USER"
  make_dir "$CACHE_DIR" 0750 "$SERVICE_USER:$SERVICE_USER"
}

# Move the verified release into place. A release directory is immutable: if the
# version is already installed it must be identical, otherwise we stop.
check_release_dir() {
  src=$WORK/extract/$BUNDLE_ROOT
  RELEASE_DIR=$RELEASES_DIR/$VERSION
  [ -e "$RELEASE_DIR" ] || return 0
  for f in oarfish oarfish.service LICENSE THIRD_PARTY_NOTICES.txt; do
    if [ ! -f "$RELEASE_DIR/$f" ] || [ "$(file_sha "$RELEASE_DIR/$f")" != "$(file_sha "$src/$f")" ]; then
      die "$RELEASE_DIR already exists and differs from the verified release (file $f). Releases are immutable; remove that directory if it is damaged and run the installer again. Nothing was changed."
    fi
  done
}

install_release() {
  check_release_dir
  if [ -e "$RELEASE_DIR" ]; then
    say "    release $VERSION is already installed and identical"
    return 0
  fi
  # Copy next to the destination (same file system), then rename: the release
  # directory appears complete or not at all.
  STAGE=$(mktemp -d "$RELEASES_DIR/.stage.XXXXXX") || die "cannot create a staging directory below $RELEASES_DIR."
  cp -R "$src" "$STAGE/release"
  chmod 0755 "$STAGE/release" "$STAGE/release/oarfish"
  chmod 0644 "$STAGE/release/oarfish.service" "$STAGE/release/LICENSE" "$STAGE/release/THIRD_PARTY_NOTICES.txt"
  set_owner root:root "$STAGE/release" "$STAGE/release/oarfish" "$STAGE/release/oarfish.service" "$STAGE/release/LICENSE" "$STAGE/release/THIRD_PARTY_NOTICES.txt"
  mv "$STAGE/release" "$RELEASE_DIR"
  rm -rf "$STAGE"
  STAGE=''
  say "    installed $RELEASE_DIR"
}

install_unit() { # install_unit SRC: replace the unit only if it differs
  mkdir -p "$(dirname "$UNIT_PATH")"
  if [ -f "$UNIT_PATH" ] && [ "$(file_sha "$UNIT_PATH")" = "$(file_sha "$1")" ]; then return 0; fi
  tmp=$UNIT_PATH.new.$$
  had_unit=0
  if [ -e "$UNIT_PATH" ]; then had_unit=1; fi
  cp "$1" "$tmp"
  chmod 0644 "$tmp"
  mv -f "$tmp" "$UNIT_PATH"
  UNIT_CHANGED=1
  if [ "$had_unit" = 1 ]; then
    say "    replaced the existing $UNIT_PATH (local changes belong in a drop-in: systemctl edit oarfish)"
  else
    say "    installed $UNIT_PATH"
  fi
}

# Atomic switch: build the new symlink beside the old one, then rename over it.
# GNU mv -T never follows the destination; mv -h is the BSD spelling, only used
# on developer machines.
switch_current() { # switch_current VERSION
  tmp=$OPT_DIR/.current.new.$$
  rm -f "$tmp"
  ln -s "releases/$1" "$tmp"
  mv -T "$tmp" "$CURRENT_LINK" 2>/dev/null || mv -fh "$tmp" "$CURRENT_LINK" \
    || { rm -f "$tmp"; die "could not switch $CURRENT_LINK."; }
}

# LINK points to TARGET afterwards, replaced in one rename; left alone if it already does.
replace_link() { # replace_link TARGET LINK
  LINK_CHANGED=0
  if [ -L "$2" ] && [ "$(readlink "$2")" = "$1" ]; then return 0; fi
  tmp=$2.new.$$
  rm -f "$tmp"
  ln -s "$1" "$tmp"
  mv -T "$tmp" "$2" 2>/dev/null || mv -fh "$tmp" "$2" \
    || { rm -f "$tmp"; die "could not replace $2."; }
  LINK_CHANGED=1
}

link_step() { # link_step TARGET LINK
  replace_link "$1" "$2"
  if [ "$LINK_CHANGED" = 1 ]; then
    LAYOUT_CHANGED=1
    say "    $2 -> $1"
  fi
}

# ---- lock (ADR 0010, U7, U8, T14) -------------------------------------------
# The installer and, from G6.5, the helper take an exclusive flock on the directory
# /var/lib/oarfish-update, so an installer run and an update cannot overlap.
check_state_dir() {
  if [ -L "$UPDATE_STATE_DIR" ] || [ ! -d "$UPDATE_STATE_DIR" ]; then
    die "$UPDATE_STATE_DIR exists and is not a directory (it holds the update lock and state). Nothing was changed."
  fi
  # shellcheck disable=SC2012 # numeric owner and mode of one directory; find has no portable spelling
  info=$(ls -ldn "$UPDATE_STATE_DIR" | awk '{ print substr($1, 1, 10), $3, $4 }')
  if [ "$info" != "drwx------ $ROOT_UID $ROOT_GID" ]; then
    die "$UPDATE_STATE_DIR must be owned by root:root with mode 0700 (it is security relevant), but permissions, uid and gid are '$info'. Fix or remove it. Nothing was changed."
  fi
}

ensure_state_dir() {
  if [ -e "$UPDATE_STATE_DIR" ] || [ -L "$UPDATE_STATE_DIR" ]; then check_state_dir; return 0; fi
  (umask 077; mkdir -p "$UPDATE_STATE_DIR")
  chmod 0700 "$UPDATE_STATE_DIR"
  set_owner root:root "$UPDATE_STATE_DIR"
  say "    created $UPDATE_STATE_DIR (root:root, mode 0700)"
}

# Held until the run ends: descriptor 9 closes when the shell exits, however it exits.
take_lock() {
  command -v flock >/dev/null 2>&1 || die "flock (util-linux) was not found, but $UPDATE_STATE_DIR has to be locked. Nothing was changed."
  check_state_dir
  [ -r "$UPDATE_STATE_DIR" ] || die "cannot open $UPDATE_STATE_DIR to lock it. Nothing was changed."
  exec 9< "$UPDATE_STATE_DIR"
  if ! flock -x -w "$LOCK_WAIT" 9; then
    die "another update or installer run is in progress (waited ${LOCK_WAIT} s for the lock on $UPDATE_STATE_DIR). Nothing was changed. Run the installer again when it has finished."
  fi
}

# ---- the update helper layout (ADR 0010, U5) ---------------------------------
helper_layout_exists() {
  for p in "$COMP_DIR" "$HELPER_LINK" "$SOCKET_UNIT" "$HELPER_UNIT" "$RECOVER_UNIT"; do
    if [ -e "$p" ] || [ -L "$p" ]; then HELPER_LAYOUT_PATH=$p; return 0; fi
  done
  return 1
}

# Read-only check of a legacy-shape run: going back to a release without the helper
# is not supported (its units and links would stay behind).
preflight_legacy() {
  if helper_layout_exists; then
    die "this device has the update helper layout ($HELPER_LAYOUT_PATH), but $TAG is a legacy bundle without the helper. Going back to a release without the helper is not supported: install a release with the helper, or remove everything first with: sh install.sh --uninstall. Nothing was changed."
  fi
}

check_unit_link() { # check_unit_link PATH TARGET: absent, or a link to TARGET
  if [ -L "$1" ]; then
    [ "$(readlink "$1")" = "$2" ] || die "$1 is a link to '$(readlink "$1")', not to $2. Nothing was changed."
  elif [ -e "$1" ]; then
    die "$1 exists and is not a link to $2 (a foreign file). Nothing was changed."
  fi
}

check_wants_link() { # a wants link may be absent or any symbolic link, never a file or directory
  if [ -e "$1" ] && [ ! -L "$1" ]; then die "$1 exists and is not a symbolic link. Nothing was changed."; fi
}

comp_same() { # comp_same DIR SRC: the four component files in DIR are regular files equal to those in SRC
  for f in $COMP_FILES; do
    if [ ! -f "$1/$f" ] || [ -L "$1/$f" ] || [ "$(file_sha "$1/$f")" != "$(file_sha "$2/$f")" ]; then return 1; fi
  done
  return 0
}

# Read-only checks of a components-shape run and the decisions that follow. Run before
# the lock, so that a refusal changes nothing, and again under it.
preflight_components() {
  src=$WORK/extract/$BUNDLE_ROOT
  for d in "$LIBEXEC_DIR" "$UNIT_DIR" "$MULTI_WANTS" "$SOCKETS_WANTS"; do
    if { [ -e "$d" ] || [ -L "$d" ]; } && [ ! -d "$d" ]; then die "$d exists and is not a directory. Nothing was changed."; fi
  done
  if [ -L "$UNIT_PATH" ]; then
    [ "$(readlink "$UNIT_PATH")" = "$REAL_OPT/current/oarfish.service" ] \
      || die "$UNIT_PATH is a link to '$(readlink "$UNIT_PATH")', not to $REAL_OPT/current/oarfish.service. Nothing was changed."
  elif [ -e "$UNIT_PATH" ] && [ ! -f "$UNIT_PATH" ]; then
    die "$UNIT_PATH is neither a regular file nor the expected link. Nothing was changed."
  fi
  check_unit_link "$HELPER_LINK" "$REAL_OPT/components/active/oarfish-update-helper"
  check_unit_link "$SOCKET_UNIT" "$REAL_OPT/components/active/oarfish-update-helper.socket"
  check_unit_link "$HELPER_UNIT" "$REAL_OPT/components/active/oarfish-update-helper.service"
  check_unit_link "$RECOVER_UNIT" "$REAL_OPT/components/known-good/oarfish-update-recover.service"
  check_wants_link "$MULTI_WANTS/oarfish.service"
  check_wants_link "$SOCKETS_WANTS/oarfish-update-helper.socket"
  check_wants_link "$MULTI_WANTS/oarfish-update-recover.service"
  if [ -e "$UPDATE_STATE_DIR" ] || [ -L "$UPDATE_STATE_DIR" ]; then check_state_dir; fi

  if [ -L "$COMP_DIR" ] || { [ -e "$COMP_DIR" ] && [ ! -d "$COMP_DIR" ]; }; then
    die "$COMP_DIR exists and is not a directory. Nothing was changed."
  fi
  ACTIVE_ID=''
  KNOWN_ID=''
  for l in active known-good; do
    pth=$COMP_DIR/$l
    if [ -L "$pth" ]; then
      id=$(readlink "$pth")
      if ! valid_version "$id" || [ ! -d "$COMP_DIR/$id" ] || [ -L "$COMP_DIR/$id" ]; then
        die "$pth points to '$id', which is not a component directory. Nothing was changed."
      fi
      if [ "$l" = active ]; then ACTIVE_ID=$id; else KNOWN_ID=$id; fi
    elif [ -e "$pth" ]; then
      die "$pth exists and is not a symbolic link. Nothing was changed."
    fi
  done

  # <id> is the version of the release that first delivered these exact bytes.
  COMP_DIR_EXISTS=0
  HELPER_CHANGED=1
  if [ -n "$ACTIVE_ID" ] && comp_same "$COMP_DIR/active" "$src"; then
    COMP_NEW_ID=$ACTIVE_ID
    COMP_ACTIVE_MOVES=0
    HELPER_CHANGED=0
  else
    COMP_NEW_ID=$VERSION
    COMP_ACTIVE_MOVES=1
    if [ -e "$COMP_DIR/$VERSION" ] || [ -L "$COMP_DIR/$VERSION" ]; then
      if [ -L "$COMP_DIR/$VERSION" ] || ! comp_same "$COMP_DIR/$VERSION" "$src"; then
        die "$COMP_DIR/$VERSION already exists and differs from the verified release. Component sets are immutable; remove it if it is damaged. Nothing was changed."
      fi
      COMP_DIR_EXISTS=1
    fi
    # A running socket keeps its text: only a changed helper or socket unit needs a restart.
    if [ -n "$ACTIVE_ID" ] \
      && [ "$(file_sha "$COMP_DIR/active/oarfish-update-helper")" = "$(file_sha "$src/oarfish-update-helper")" ] \
      && [ "$(file_sha "$COMP_DIR/active/oarfish-update-helper.socket")" = "$(file_sha "$src/oarfish-update-helper.socket")" ]; then
      HELPER_CHANGED=0
    fi
  fi
}

ensure_comp_dirs() {
  make_dir "$COMP_DIR" 0755 root:root
  make_dir "$LIBEXEC_DIR" 0755 root:root
  make_dir "$UNIT_DIR" 0755 root:root
  make_dir "$MULTI_WANTS" 0755 root:root
  make_dir "$SOCKETS_WANTS" 0755 root:root
}

# The four component files below components/<id>, through a staging directory and one
# rename, only when these bytes are new.
install_components() {
  src=$WORK/extract/$BUNDLE_ROOT
  if [ "$COMP_ACTIVE_MOVES" = 0 ] || [ "$COMP_DIR_EXISTS" = 1 ]; then
    say "    components $COMP_NEW_ID are already installed and identical"
    return 0
  fi
  STAGE=$(mktemp -d "$COMP_DIR/.stage.XXXXXX") || die "cannot create a staging directory below $COMP_DIR."
  mkdir "$STAGE/set"
  for f in $COMP_FILES; do cp "$src/$f" "$STAGE/set/$f"; done
  chmod 0755 "$STAGE/set" "$STAGE/set/oarfish-update-helper"
  chmod 0644 "$STAGE/set/oarfish-update-helper.socket" "$STAGE/set/oarfish-update-helper.service" "$STAGE/set/oarfish-update-recover.service"
  set_owner root:root "$STAGE/set"
  for f in $COMP_FILES; do set_owner root:root "$STAGE/set/$f"; done
  mv "$STAGE/set" "$COMP_DIR/$VERSION"
  rm -rf "$STAGE"
  STAGE=''
  say "    installed $COMP_DIR/$VERSION"
}

# All links of the layout, each created or replaced atomically.
install_layout() {
  if [ -z "$ACTIVE_ID" ]; then
    link_step "$COMP_NEW_ID" "$COMP_DIR/active"
    if [ -z "$KNOWN_ID" ]; then link_step "$COMP_NEW_ID" "$COMP_DIR/known-good"; KNOWN_ID=$COMP_NEW_ID; fi
  else
    if [ "$COMP_ACTIVE_MOVES" = 1 ]; then
      ACTIVE_MOVED_FROM=$ACTIVE_ID
      link_step "$COMP_NEW_ID" "$COMP_DIR/active"
    fi
    # known-good moves after the health check (move_known_good); a missing one is created.
    if [ -z "$KNOWN_ID" ]; then link_step "$ACTIVE_ID" "$COMP_DIR/known-good"; KNOWN_ID=$ACTIVE_ID; fi
  fi
  PREV_UNIT_SHA=''
  if [ -L "$UNIT_PATH" ]; then
    # A link into current: the text of the release current points to.
    if [ -n "$PREV" ] && [ -f "$RELEASES_DIR/$PREV/oarfish.service" ]; then PREV_UNIT_SHA=$(file_sha "$RELEASES_DIR/$PREV/oarfish.service"); fi
  elif [ -f "$UNIT_PATH" ]; then
    PREV_UNIT_SHA=$(file_sha "$UNIT_PATH")
  fi
  if [ "$PREV_UNIT_SHA" != "$(file_sha "$RELEASE_DIR/oarfish.service")" ]; then UNIT_TEXT_CHANGED=1; fi

  if [ "$PREV" != "$VERSION" ]; then
    switch_current "$VERSION"
    SWITCHED=1
    say "    $CURRENT_LINK -> releases/$VERSION"
  else
    say "    $CURRENT_LINK already points to releases/$VERSION"
  fi

  link_step "$REAL_OPT/components/active/oarfish-update-helper" "$HELPER_LINK"
  if [ -e "$UNIT_PATH" ] && [ ! -L "$UNIT_PATH" ]; then
    say "    replacing the regular file $UNIT_PATH by a link into the active release (local changes belong in a drop-in, which is kept: systemctl edit oarfish)"
  fi
  link_step "$REAL_OPT/current/oarfish.service" "$UNIT_PATH"
  link_step "$REAL_OPT/components/active/oarfish-update-helper.socket" "$SOCKET_UNIT"
  link_step "$REAL_OPT/components/active/oarfish-update-helper.service" "$HELPER_UNIT"
  link_step "$REAL_OPT/components/known-good/oarfish-update-recover.service" "$RECOVER_UNIT"
  # Relative wants links; systemctl enable would link the resolved release path (A5).
  link_step ../oarfish.service "$MULTI_WANTS/oarfish.service"
  link_step ../oarfish-update-helper.socket "$SOCKETS_WANTS/oarfish-update-helper.socket"
  link_step ../oarfish-update-recover.service "$MULTI_WANTS/oarfish-update-recover.service"
}

# A socket cannot be (re)started while its service is active (ADR 0010, A9): stop that first.
restart_helper_socket() {
  systemctl_cmd stop oarfish-update-helper.service || return 1
  systemctl_cmd restart oarfish-update-helper.socket || return 1
}

# known-good moves only once the new release and helper have proven healthy.
move_known_good() {
  [ "$BUNDLE_SHAPE" = components ] || return 0
  if [ "$KNOWN_ID" = "$COMP_NEW_ID" ]; then return 0; fi
  old_recover=$COMP_DIR/$KNOWN_ID/oarfish-update-recover.service
  link_step "$COMP_NEW_ID" "$COMP_DIR/known-good"
  if [ ! -f "$old_recover" ] || [ "$(file_sha "$old_recover")" != "$(file_sha "$COMP_DIR/$COMP_NEW_ID/oarfish-update-recover.service")" ]; then
    systemctl_cmd daemon-reload
  fi
  KNOWN_ID=$COMP_NEW_ID
}

health_check() {
  if [ "$TESTMODE" = 1 ] && [ "${OARFISH_HEALTHCHECK:-}" = skip ]; then
    say "    health check skipped (test mode)"
    return 0
  fi
  i=0
  while [ "$i" -lt "$HEALTH_TIMEOUT" ]; do
    if body=$(curl -fsS --max-time 2 "$HEALTH_URL" 2>/dev/null); then
      case $body in
        *'"status":"ok"'*)
          if systemctl_cmd is-active --quiet oarfish.service; then
            # socket activated and idle-exiting: the socket must be active, not the service
            if [ "$BUNDLE_SHAPE" != components ] || systemctl_cmd is-active --quiet oarfish-update-helper.socket; then return 0; fi
          fi ;;
      esac
    fi
    i=$((i + 1))
    sleep 1
  done
  return 1
}

# Switch back to the previous release after a failed health check and prove that
# it works. Always ends the run with an error.
roll_back() {
  warn "rolling back to $PREV"
  UNIT_CHANGED=0
  if [ "$BUNDLE_SHAPE" = components ]; then
    # The unit is a link into current and moves with it. The layout stays; known-good is untouched.
    switch_current "$PREV"
    if [ -n "$ACTIVE_MOVED_FROM" ]; then replace_link "$ACTIVE_MOVED_FROM" "$COMP_DIR/active"; fi
    systemctl_cmd daemon-reload
    if [ -n "$ACTIVE_MOVED_FROM" ]; then restart_helper_socket || warn "could not restart oarfish-update-helper.socket"; fi
  else
    if [ -f "$RELEASES_DIR/$PREV/oarfish.service" ]; then install_unit "$RELEASES_DIR/$PREV/oarfish.service"; fi
    switch_current "$PREV"
    if [ "$UNIT_CHANGED" = 1 ]; then systemctl_cmd daemon-reload; fi
  fi
  # The failed start counts against systemd's start limit; clear it so the restart is not refused.
  systemctl_cmd reset-failed oarfish.service || true
  systemctl_cmd restart oarfish.service || true
  if health_check; then
    die "installation of $VERSION failed its health check; the previous version $PREV is active again and healthy. See: journalctl -u oarfish"
  fi
  die "installation of $VERSION failed its health check AND the rollback to $PREV did not become healthy either. The service is probably not running. See: journalctl -u oarfish and systemctl status oarfish"
}

do_install() {
  UNIT_CHANGED=0
  UNIT_TEXT_CHANGED=0
  LAYOUT_CHANGED=0
  ACTIVE_MOVED_FROM=''
  PREV=''

  # Read-only checks first, then the lock, then the checks again under it.
  if [ "$BUNDLE_SHAPE" = components ]; then
    preflight_components
    ensure_state_dir
    take_lock
    preflight_components
    check_release_dir
  else
    preflight_legacy
    if [ -e "$UPDATE_STATE_DIR" ] || [ -L "$UPDATE_STATE_DIR" ]; then
      take_lock
      preflight_legacy
    fi
  fi

  if [ -L "$CURRENT_LINK" ]; then
    PREV=$(readlink "$CURRENT_LINK")
    PREV=${PREV#releases/}
  elif [ -e "$CURRENT_LINK" ]; then
    die "$CURRENT_LINK exists and is not a symlink; refusing to touch it."
  fi

  say "==> installing"
  ensure_user
  ensure_dirs
  if [ "$BUNDLE_SHAPE" = components ]; then ensure_comp_dirs; fi
  install_release
  SWITCHED=0
  if [ "$BUNDLE_SHAPE" = components ]; then
    install_components
    install_layout
  else
    install_unit "$RELEASE_DIR/oarfish.service"
    if [ "$PREV" != "$VERSION" ]; then
      switch_current "$VERSION"
      SWITCHED=1
      say "    $CURRENT_LINK -> releases/$VERSION"
    else
      say "    $CURRENT_LINK already points to releases/$VERSION"
    fi
  fi

  # One daemon-reload makes systemd read every unit and link written above.
  if [ "$BUNDLE_SHAPE" = components ]; then
    if [ "$LAYOUT_CHANGED" = 1 ] || [ "$UNIT_TEXT_CHANGED" = 1 ]; then systemctl_cmd daemon-reload; fi
    if [ "$HELPER_CHANGED" = 1 ] || ! systemctl_cmd is-active --quiet oarfish-update-helper.socket; then
      restart_helper_socket || die "could not (re)start oarfish-update-helper.socket. See: systemctl status oarfish-update-helper.socket and journalctl -u oarfish-update-helper"
      say "    update helper socket (re)started"
    fi
    if [ "$SWITCHED" = 1 ] || [ "$UNIT_TEXT_CHANGED" = 1 ]; then
      UNIT_CHANGED=1
    fi
  else
    if [ "$UNIT_CHANGED" = 1 ]; then systemctl_cmd daemon-reload; fi
    systemctl_cmd is-enabled --quiet oarfish.service 2>/dev/null || systemctl_cmd enable oarfish.service
  fi
  if [ "$SWITCHED" = 1 ] || [ "$UNIT_CHANGED" = 1 ]; then
    systemctl_cmd restart oarfish.service
    say "    service (re)started"
  elif ! systemctl_cmd is-active --quiet oarfish.service; then
    systemctl_cmd start oarfish.service
    say "    service started"
  else
    say "    service already running, nothing to restart"
  fi

  say "==> checking $HEALTH_URL"
  if ! health_check; then
    warn "the service did not become healthy within ${HEALTH_TIMEOUT}s."
    if [ -n "$PREV" ] && [ "$SWITCHED" = 1 ] && [ -d "$RELEASES_DIR/$PREV" ]; then
      roll_back
    fi
    if [ -z "$PREV" ]; then
      # First installation: nothing to go back to. Do not leave a failing service
      # restarting in the background or starting at the next boot; keep the files.
      systemctl_cmd disable --now oarfish.service || warn "could not disable oarfish.service"
      if [ "$BUNDLE_SHAPE" = components ]; then
        systemctl_cmd disable --now oarfish-update-helper.socket || warn "could not disable oarfish-update-helper.socket"
        systemctl_cmd stop oarfish-update-helper.service || warn "could not stop oarfish-update-helper.service"
        systemctl_cmd disable oarfish-update-recover.service || warn "could not disable oarfish-update-recover.service"
        die "Oarfish $VERSION is installed but its service did not become healthy (or the helper socket is not active), so oarfish.service, the helper socket and the recover unit were stopped and disabled. The files and links are kept in $OPT_DIR. See: journalctl -u oarfish and systemctl status oarfish oarfish-update-helper.socket. Run this installer again to start everything."
      fi
      die "Oarfish $VERSION is installed but its service did not become healthy, so it was stopped and disabled. The files are kept in $OPT_DIR. See: journalctl -u oarfish and systemctl status oarfish. Start it again with: systemctl enable --now oarfish"
    fi
    if [ "$BUNDLE_SHAPE" = components ]; then
      die "the service did not become healthy, or the update helper socket is not active. See: systemctl status oarfish oarfish-update-helper.socket and journalctl -u oarfish"
    fi
    die "the service did not become healthy. See: systemctl status oarfish and journalctl -u oarfish"
  fi
  say "    healthy"
  move_known_good

  say ""
  say "Oarfish $VERSION is installed and running."
  say "  Open the GUI on this device:  http://$LISTEN_ADDR"
  say "  From another computer (the service listens on loopback only), open an SSH tunnel:"
  say "      ssh -L 8484:$LISTEN_ADDR <user>@<this-device>     then browse http://127.0.0.1:8484"
  if [ ! -f "$STATE_DIR/auth/account.json" ]; then
    say "  No administrator account exists yet. The GUI asks for the setup token; show it on this device with:"
    say "      sudo cat /var/lib/oarfish/setup-token"
  fi
  if [ "$BUNDLE_SHAPE" = components ]; then
    say "  Update helper: components $COMP_NEW_ID, socket oarfish-update-helper.socket is active"
  fi
  say "  Release key checked: $OARFISH_KEY_ID"
}

# ---- uninstall -------------------------------------------------------------
remove_link_into_opt() { # remove_link_into_opt PATH: remove PATH only if it is a link into /opt/oarfish
  if [ -L "$1" ]; then
    t=$(readlink "$1")
    case $t in
      "$REAL_OPT"/*) rm -f "$1"; say "    removed $1"; return 0 ;;
    esac
    say "    left $1 alone: it is a link to '$t', not into $REAL_OPT"
  elif [ -e "$1" ]; then
    say "    left $1 alone: it is not a link into $REAL_OPT"
  fi
}

remove_wants_link() { # remove_wants_link PATH UNIT: only a link that names the unit
  if [ -L "$1" ]; then
    t=$(readlink "$1")
    if [ "${t##*/}" = "$2" ]; then rm -f "$1"; say "    removed $1"; return 0; fi
    say "    left $1 alone: it is a link to '$t', not to $2"
  elif [ -e "$1" ]; then
    say "    left $1 alone: it is not a symbolic link"
  fi
}

do_uninstall() {
  say "==> uninstalling"
  # The same lock as an installer run takes; without the directory there is nothing to lock.
  if [ -e "$UPDATE_STATE_DIR" ] || [ -L "$UPDATE_STATE_DIR" ]; then
    take_lock
  fi
  RELOAD=0
  if [ -f "$UNIT_PATH" ] || [ -L "$UNIT_PATH" ] || systemctl_cmd cat oarfish.service >/dev/null 2>&1; then
    systemctl_cmd stop oarfish.service 2>/dev/null || warn "could not stop oarfish.service (not running?)"
    systemctl_cmd disable oarfish.service 2>/dev/null || warn "could not disable oarfish.service (not enabled?)"
  fi
  for u in oarfish-update-helper.socket oarfish-update-helper.service oarfish-update-recover.service; do
    if [ -e "$UNIT_DIR/$u" ] || [ -L "$UNIT_DIR/$u" ]; then
      systemctl_cmd stop "$u" 2>/dev/null || warn "could not stop $u (not running?)"
      systemctl_cmd disable "$u" 2>/dev/null || warn "could not disable $u (not enabled?)"
    fi
  done
  remove_wants_link "$MULTI_WANTS/oarfish.service" oarfish.service
  remove_wants_link "$SOCKETS_WANTS/oarfish-update-helper.socket" oarfish-update-helper.socket
  remove_wants_link "$MULTI_WANTS/oarfish-update-recover.service" oarfish-update-recover.service
  for u in "$SOCKET_UNIT" "$HELPER_UNIT" "$RECOVER_UNIT"; do
    if [ -e "$u" ] || [ -L "$u" ]; then remove_link_into_opt "$u"; RELOAD=1; fi
  done
  remove_link_into_opt "$HELPER_LINK"
  if [ -L "$UNIT_PATH" ]; then
    remove_link_into_opt "$UNIT_PATH"
    RELOAD=1
  elif [ -f "$UNIT_PATH" ]; then
    rm -f "$UNIT_PATH"
    RELOAD=1
    say "    removed $UNIT_PATH"
  fi
  if [ "$RELOAD" = 1 ]; then systemctl_cmd daemon-reload; fi
  for d in "$OPT_DIR" "$CACHE_DIR"; do
    if [ -e "$d" ] || [ -L "$d" ]; then safe_rm_rf "$d"; say "    removed $d"; fi
  done
  if [ "$PURGE" = 1 ]; then
    for d in "$ETC_DIR" "$STATE_DIR" "$UPDATE_STATE_DIR"; do
      if [ -e "$d" ] || [ -L "$d" ]; then safe_rm_rf "$d"; say "    removed $d"; fi
    done
    if can_manage_users && id -u "$SERVICE_USER" >/dev/null 2>&1; then
      if userdel "$SERVICE_USER" 2>/dev/null; then say "    removed user $SERVICE_USER"; else warn "could not remove user $SERVICE_USER"; fi
    fi
  else
    say "    kept your data: $ETC_DIR, $STATE_DIR and the user $SERVICE_USER (use --purge to remove them)"
    if [ -e "$UPDATE_STATE_DIR" ]; then
      say "    kept the update state and lock directory $UPDATE_STATE_DIR (use --purge to remove it)"
    fi
  fi
  say "Oarfish is uninstalled."
}

# ---- plan ------------------------------------------------------------------
print_plan_install() {
  say "Oarfish installer"
  say "  system:      $OS_NAME ($(uname -m)), kernel $(uname -r)"
  say "  openssl:     $OPENSSL_VERSION"
  say "  release:     $TAG"
  say "  source:      $BASE_URL/ (HTTPS, anonymous)"
  say "  release key: $OARFISH_KEY_ID (compare with release/oarfish-release.pub, see docs/INSTALL.md)"
  say ""
  say "It will, in this order:"
  say "  1. download checksums.txt and checksums.txt.sig, verify the Ed25519 signature"
  say "  2. download manifest.json and $BUNDLE, verify their SHA-256 against the signed checksums.txt"
  say "     and check the bundle shape (legacy, or components with the update helper) against the manifest"
  say "  3. only then: create system user '$SERVICE_USER' (no login shell) if missing"
  say "  4. create $OPT_DIR, $ETC_DIR, $STATE_DIR, $CACHE_DIR (and /run/oarfish via systemd)"
  say "  5. install the release to $RELEASES_DIR/$VERSION"
  say "  6. install $UNIT_PATH, switch $CURRENT_LINK atomically"
  say "  7. enable and start oarfish.service, check $HEALTH_URL"
  say "The service listens on $LISTEN_ADDR only. Nothing is opened to the network."
  say ""
  say "The bundle shape is only known after the download. For the components shape (with the update helper):"
  say "  3. create $UPDATE_STATE_DIR (root:root, 0700) and take an exclusive lock on it (at most 30 s wait)"
  say "  5. install the release and the helper set below $COMP_DIR/<id>"
  say "  6. replace atomically: $CURRENT_LINK, $COMP_DIR/active and known-good, $HELPER_LINK, $UNIT_PATH"
  say "     (a regular file there is replaced; oarfish.service.d is kept), the helper units, and the relative"
  say "     wants links of the three units (systemctl enable is not used)"
  say "  7. one daemon-reload; if the helper or its socket changed: stop the helper service, restart its socket;"
  say "     restart oarfish.service when something changed; the recover unit is enabled, not started"
  say "  8. check $HEALTH_URL and the helper socket; only then move components/known-good"
  say "A legacy-shape release is installed as before, and is refused on a device that has the helper layout."
  say ""
}

print_plan_uninstall() {
  say "Oarfish uninstall"
  say "  will remove: oarfish.service and $UNIT_PATH, $OPT_DIR, $CACHE_DIR"
  say "  update helper: if present, takes the lock on $UPDATE_STATE_DIR, stops and disables its socket, service and"
  say "                 recover unit, removes their links, the wants links and $HELPER_LINK where they link"
  say "                 into $REAL_OPT (anything else stays and is reported)"
  if [ "$PURGE" = 1 ]; then
    say "  --purge:     will also remove $ETC_DIR, $STATE_DIR, $UPDATE_STATE_DIR and the user $SERVICE_USER"
  else
    say "  will keep:   $ETC_DIR, $STATE_DIR, $UPDATE_STATE_DIR and the user $SERVICE_USER"
  fi
  say ""
}

# ---- main ------------------------------------------------------------------
main() {
  MODE=install
  DRY_RUN=0
  PURGE=0
  REQ_VERSION=''
  while [ $# -gt 0 ]; do
    case $1 in
      --version) [ $# -ge 2 ] || die "--version needs a value."; REQ_VERSION=$2; shift 2 ;;
      --version=*) REQ_VERSION=${1#--version=}; shift ;;
      --dry-run) DRY_RUN=1; shift ;;
      --uninstall) MODE=uninstall; shift ;;
      --purge) PURGE=1; shift ;;
      -h|--help) usage; exit 0 ;;
      *) die "unknown option '$1'. Try --help." ;;
    esac
  done
  [ "$PURGE" = 0 ] || [ "$MODE" = uninstall ] || die "--purge only makes sense together with --uninstall."

  if [ "$TESTMODE" = 1 ]; then
    [ -d "$ROOT" ] || die "OARFISH_TEST_ROOT is not a directory."
    warn "TEST MODE: working below $ROOT, not touching the real system"
  fi

  if [ "$MODE" = uninstall ]; then
    print_plan_uninstall
    if [ "$PURGE" = 1 ] && can_manage_users && ! command -v userdel >/dev/null 2>&1; then
      die "userdel was not found, but --purge has to remove the user $SERVICE_USER. Nothing was changed."
    fi
    if [ "$DRY_RUN" = 1 ]; then say "Dry run: nothing was changed."; exit 0; fi
    require_root
    do_uninstall
    exit 0
  fi

  # Which release? The one this script was stamped with, unless --version says otherwise.
  case $OARFISH_STAMPED_VERSION in @@*@@) STAMPED='' ;; *) STAMPED=$OARFISH_STAMPED_VERSION ;; esac
  VERSION=${REQ_VERSION#v}
  if [ -z "$VERSION" ]; then
    [ -n "$STAMPED" ] || die "this copy of install.sh is not tied to a release (it is a repository or local copy), so it does not know what to install. Pass the release explicitly, for example: sh install.sh --version v0.1.0-alpha.2. Or download install.sh from the release page of the version you want."
    VERSION=$STAMPED
  elif [ -n "$STAMPED" ] && [ "$VERSION" != "$STAMPED" ]; then
    warn "this installer belongs to release $STAMPED; installing $VERSION as requested."
  fi
  valid_version "$VERSION" || die "'$VERSION' is not a valid version (expected for example 0.1.0 or v0.1.0-alpha.2)."
  TAG=v$VERSION
  BUNDLE_ROOT=oarfish_${VERSION}_linux_arm64
  BUNDLE=$BUNDLE_ROOT.tar.gz
  BASE_URL=${BASE_URL_OVERRIDE:-https://github.com/$OARFISH_REPO/releases/download/$TAG}

  # Tell a non-root user straight away; a dry run only reads, so it needs no root.
  if [ "$DRY_RUN" = 0 ]; then require_root; fi
  check_platform
  check_tools
  check_openssl
  check_space
  print_plan_install
  if [ "$DRY_RUN" = 1 ]; then say "Dry run: nothing was downloaded or changed."; exit 0; fi

  WORK=$(mktemp -d "${TMPDIR:-/tmp}/oarfish-install.XXXXXX") || die "cannot create a temporary directory."
  download_and_verify
  do_install
}

main "$@"
