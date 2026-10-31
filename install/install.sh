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
# Nothing is downloaded from anywhere but GitHub Releases over HTTPS, no token is
# used, nothing is sent anywhere. Re-running is safe. Failed downloads or
# verification leave an existing installation untouched.
#
# Options:
#   --version TAG   install this release (v0.1.0-alpha.2 or 0.1.0-alpha.2)
#   --dry-run       check the system and print the plan; change nothing
#   --uninstall     remove service, unit, binaries and cache; keep your data
#   --purge         with --uninstall: also remove /etc/oarfish, /var/lib/oarfish
#                   and the oarfish user
#   --help
#
# Tests only: when OARFISH_TEST_ROOT is set, all paths live below that directory,
# root and a running systemd are not required, and OARFISH_RELEASE_BASE_URL,
# OARFISH_OPENSSL, OARFISH_SYSTEMCTL, OARFISH_HEALTH_URL, OARFISH_HEALTH_TIMEOUT,
# OARFISH_HEALTHCHECK=skip and OARFISH_TEST_USERS=1 (act as root for the user and
# ownership steps, with stub tools on PATH) are honoured. Without it they are ignored.

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
UNIT_PATH=$ROOT/etc/systemd/system/oarfish.service

OPENSSL=openssl
SYSTEMCTL=systemctl
HEALTH_URL="http://$LISTEN_ADDR/healthz"
HEALTH_TIMEOUT=30
BASE_URL_OVERRIDE=''
if [ "$TESTMODE" = 1 ]; then
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
  --uninstall     remove service, unit, binaries and cache; keep your data
  --purge         with --uninstall: also remove /etc/oarfish, /var/lib/oarfish
                  and the oarfish user
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
    "$OPT_DIR"|"$CACHE_DIR"|"$ETC_DIR"|"$STATE_DIR") rm -rf "$1" ;;
    *) die "internal error: refusing to remove $1" ;;
  esac
}

systemctl_cmd() { "$SYSTEMCTL" "$@"; }

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
  for t in curl tar gzip sha256sum od base64 mktemp awk df id mv ln rm mkdir chmod cat cp sed grep tr head tail wc sleep uname readlink dirname; do
    command -v "$t" >/dev/null 2>&1 || missing="$missing $t"
  done
  if can_manage_users; then
    command -v chown >/dev/null 2>&1 || missing="$missing chown"
    command -v getent >/dev/null 2>&1 || missing="$missing getent"
    command -v useradd >/dev/null 2>&1 || command -v adduser >/dev/null 2>&1 || missing="$missing useradd-or-adduser"
  fi
  command -v "$OPENSSL" >/dev/null 2>&1 || missing="$missing openssl"
  [ -z "$missing" ] || die "required tools are missing:$missing. Install them with your package manager (for example: apt install curl openssl) and run the installer again."
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

# The bundle holds exactly these entries, all regular files (no links, no
# absolute or parent paths). Checked before anything is extracted.
check_bundle_entries() {
  tar -tvzf "$WORK/$BUNDLE" | awk '{ t = substr($1, 1, 1); if (t != "-" && t != "d") bad = 1 } END { exit bad }' \
    || die "the bundle contains something other than regular files and directories."
  n=0
  for e in $(tar -tzf "$WORK/$BUNDLE"); do
    case $e in
      "$BUNDLE_ROOT"/|"$BUNDLE_ROOT"/oarfish|"$BUNDLE_ROOT"/oarfish.service|"$BUNDLE_ROOT"/LICENSE|"$BUNDLE_ROOT"/THIRD_PARTY_NOTICES.txt) n=$((n + 1)) ;;
      *) die "the bundle contains an unexpected entry '$e'." ;;
    esac
  done
  [ "$n" = 5 ] || die "the bundle does not contain the expected five entries."
}

# An ELF64 little-endian executable for machine 0xB7 (AArch64).
check_binary() {
  magic=$(od -An -tx1 -N6 "$1" | tr -d ' \n')
  machine=$(od -An -tx1 -j18 -N2 "$1" | tr -d ' \n')
  if [ "$magic" != 7f454c460201 ] || [ "$machine" != b700 ]; then
    die "the oarfish binary in the bundle is not a 64-bit ARM (aarch64) executable."
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
  check_binary "$WORK/extract/$BUNDLE_ROOT/oarfish"
  say "    bundle contents OK"
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
install_release() {
  src=$WORK/extract/$BUNDLE_ROOT
  RELEASE_DIR=$RELEASES_DIR/$VERSION
  if [ -e "$RELEASE_DIR" ]; then
    for f in oarfish oarfish.service LICENSE THIRD_PARTY_NOTICES.txt; do
      if [ ! -f "$RELEASE_DIR/$f" ] || [ "$(file_sha "$RELEASE_DIR/$f")" != "$(file_sha "$src/$f")" ]; then
        die "$RELEASE_DIR already exists and differs from the verified release (file $f). Releases are immutable; remove that directory if it is damaged and run the installer again. Nothing was changed."
      fi
    done
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
          systemctl_cmd is-active --quiet oarfish.service && return 0 ;;
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
  if [ -f "$RELEASES_DIR/$PREV/oarfish.service" ]; then install_unit "$RELEASES_DIR/$PREV/oarfish.service"; fi
  switch_current "$PREV"
  if [ "$UNIT_CHANGED" = 1 ]; then systemctl_cmd daemon-reload; fi
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
  PREV=''
  if [ -L "$CURRENT_LINK" ]; then
    PREV=$(readlink "$CURRENT_LINK")
    PREV=${PREV#releases/}
  elif [ -e "$CURRENT_LINK" ]; then
    die "$CURRENT_LINK exists and is not a symlink; refusing to touch it."
  fi

  say "==> installing"
  ensure_user
  ensure_dirs
  install_release
  install_unit "$RELEASE_DIR/oarfish.service"
  SWITCHED=0
  if [ "$PREV" != "$VERSION" ]; then
    switch_current "$VERSION"
    SWITCHED=1
    say "    $CURRENT_LINK -> releases/$VERSION"
  else
    say "    $CURRENT_LINK already points to releases/$VERSION"
  fi

  if [ "$UNIT_CHANGED" = 1 ]; then systemctl_cmd daemon-reload; fi
  systemctl_cmd is-enabled --quiet oarfish.service 2>/dev/null || systemctl_cmd enable oarfish.service
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
      die "Oarfish $VERSION is installed but its service did not become healthy, so it was stopped and disabled. The files are kept in $OPT_DIR. See: journalctl -u oarfish and systemctl status oarfish. Start it again with: systemctl enable --now oarfish"
    fi
    die "the service did not become healthy. See: systemctl status oarfish and journalctl -u oarfish"
  fi
  say "    healthy"

  say ""
  say "Oarfish $VERSION is installed and running."
  say "  Open the GUI on this device:  http://$LISTEN_ADDR"
  say "  From another computer (the service listens on loopback only), open an SSH tunnel:"
  say "      ssh -L 8484:$LISTEN_ADDR <user>@<this-device>     then browse http://127.0.0.1:8484"
  if [ ! -f "$STATE_DIR/auth/account.json" ]; then
    say "  No administrator account exists yet. The GUI asks for the setup token; show it on this device with:"
    say "      sudo cat /var/lib/oarfish/setup-token"
  fi
  say "  Release key checked: $OARFISH_KEY_ID"
}

# ---- uninstall -------------------------------------------------------------
do_uninstall() {
  say "==> uninstalling"
  if [ -f "$UNIT_PATH" ] || systemctl_cmd cat oarfish.service >/dev/null 2>&1; then
    systemctl_cmd stop oarfish.service 2>/dev/null || warn "could not stop oarfish.service (not running?)"
    systemctl_cmd disable oarfish.service 2>/dev/null || warn "could not disable oarfish.service (not enabled?)"
  fi
  if [ -f "$UNIT_PATH" ]; then
    rm -f "$UNIT_PATH"
    systemctl_cmd daemon-reload
    say "    removed $UNIT_PATH"
  fi
  for d in "$OPT_DIR" "$CACHE_DIR"; do
    if [ -e "$d" ]; then safe_rm_rf "$d"; say "    removed $d"; fi
  done
  if [ "$PURGE" = 1 ]; then
    for d in "$ETC_DIR" "$STATE_DIR"; do
      if [ -e "$d" ]; then safe_rm_rf "$d"; say "    removed $d"; fi
    done
    if can_manage_users && id -u "$SERVICE_USER" >/dev/null 2>&1; then
      if userdel "$SERVICE_USER" 2>/dev/null; then say "    removed user $SERVICE_USER"; else warn "could not remove user $SERVICE_USER"; fi
    fi
  else
    say "    kept your data: $ETC_DIR, $STATE_DIR and the user $SERVICE_USER (use --purge to remove them)"
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
  say "  3. only then: create system user '$SERVICE_USER' (no login shell) if missing"
  say "  4. create $OPT_DIR, $ETC_DIR, $STATE_DIR, $CACHE_DIR (and /run/oarfish via systemd)"
  say "  5. install the release to $RELEASES_DIR/$VERSION"
  say "  6. install $UNIT_PATH, switch $CURRENT_LINK atomically"
  say "  7. enable and start oarfish.service, check $HEALTH_URL"
  say "The service listens on $LISTEN_ADDR only. Nothing is opened to the network."
  say ""
}

print_plan_uninstall() {
  say "Oarfish uninstall"
  say "  will remove: oarfish.service and $UNIT_PATH, $OPT_DIR, $CACHE_DIR"
  if [ "$PURGE" = 1 ]; then
    say "  --purge:     will also remove $ETC_DIR, $STATE_DIR and the user $SERVICE_USER"
  else
    say "  will keep:   $ETC_DIR, $STATE_DIR and the user $SERVICE_USER"
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
