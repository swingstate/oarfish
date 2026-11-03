#!/usr/bin/env bash
# Tests of install/install.sh against a fake root directory (no root, no systemd,
# no network). A throwaway minisign key signs fake releases that are served
# through file:// URLs; the installer's key constant is replaced by that key in a
# test copy. The production file is only used where a test says so.
#
# OpenSSL: the installer is told which openssl to use through OARFISH_OPENSSL and
# the test refuses to run unless that is a real OpenSSL 3.x. macOS ships LibreSSL
# as /usr/bin/openssl, which cannot do this; never let a test pass with it.
#   OARFISH_TEST_OPENSSL   path to OpenSSL 3 (default: /opt/homebrew/bin/openssl on
#                          macOS, `openssl` from PATH elsewhere)
#   OARFISH_TEST_SH        shell that runs the installer (default: dash if present,
#                          the Debian/Ubuntu/Raspberry Pi OS /bin/sh; else /bin/sh)
#
# Usage: scripts/test-installer.sh        (needs minisign, Go, and the tools above)
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
ROOT="$(repo_root)"

[ "$(id -u)" != 0 ] || die "do not run the installer tests as root"
command -v minisign >/dev/null || die "minisign not found (scripts/install-minisign.sh, or brew install minisign)"

if [ -n "${OARFISH_TEST_OPENSSL:-}" ]; then OPENSSL_BIN="$OARFISH_TEST_OPENSSL"
elif [ "$(uname -s)" = Darwin ]; then OPENSSL_BIN=/opt/homebrew/bin/openssl
else OPENSSL_BIN="$(command -v openssl || true)"; fi
[ -x "$OPENSSL_BIN" ] || die "openssl not found at '$OPENSSL_BIN'; set OARFISH_TEST_OPENSSL to an OpenSSL 3.x binary"
case "$("$OPENSSL_BIN" version)" in
  "OpenSSL 3."*) ;;
  *) die "$OPENSSL_BIN is '$("$OPENSSL_BIN" version)', not OpenSSL 3.x. Set OARFISH_TEST_OPENSSL." ;;
esac
echo "openssl for the tests: $OPENSSL_BIN ($("$OPENSSL_BIN" version))"

if [ -n "${OARFISH_TEST_SH:-}" ]; then SH="$OARFISH_TEST_SH"
elif [ -x /bin/dash ]; then SH=/bin/dash
else SH=/bin/sh; fi
echo "shell for the installer: $SH"

TMP="$(mktemp -d)"
trap 'chmod -R u+w "$TMP" 2>/dev/null; rm -rf "$TMP"' EXIT
build_bundlegen "$TMP/bundlegen"
mkdir -p "$TMP/shim" "$TMP/tmp" "$TMP/roots" "$TMP/rel" "$TMP/state"

COMMIT=0123456789abcdef0123456789abcdef01234567
V1=0.1.0-test.1
V2=0.1.0-test.2

# ---- shims ---------------------------------------------------------------
# uname: Linux/aarch64 unless FAKE_UNAME_S / FAKE_UNAME_M say otherwise.
cat > "$TMP/shim/uname" <<'EOF'
#!/bin/sh
case "${1:-}" in
  -s) echo "${FAKE_UNAME_S:-Linux}" ;;
  -m) echo "${FAKE_UNAME_M:-aarch64}" ;;
  -r) echo "6.1.0-test" ;;
  *) exec /usr/bin/uname "$@" ;;
esac
EOF
# sha256sum for macOS; Linux has its own.
if ! command -v sha256sum >/dev/null 2>&1; then
  printf '#!/bin/sh\nexec shasum -a 256 "$@"\n' > "$TMP/shim/sha256sum"
fi
# systemctl stand-in: records calls and keeps enabled/active state in files, per unit
# (oarfish.service: enabled, active; any other unit: enabled.UNIT, active.UNIT).
# Health is a file ($state/healthz, read by the installer through a file:// URL):
# start/restart writes it, unless the version that "current" points to is listed
# in $state/unhealthy, in which case it is removed, as a service that fails to come up.
# Like systemd (ADR 0010 A9, device observation), it refuses to start or restart the
# helper socket while the helper service is active. A file $state/block-daemon-reload
# makes daemon-reload wait (until $state/proceed exists) and announce itself with
# $state/blocked, so a test can hold an installer run while it holds the lock.
cat > "$TMP/shim/systemctl-stub" <<'EOF'
#!/bin/sh
S="$FAKE_SYSTEMD_STATE"
echo "$*" >> "$S/calls.log"
cmd="$1"; shift
unit=''
for a in "$@"; do case $a in -*) ;; *) unit=$a ;; esac; done
case $unit in oarfish.service|'') sfx='' ;; *) sfx=".$unit" ;; esac
set_health() {
  cur="$(readlink "$FAKE_ROOT/opt/oarfish/current" 2>/dev/null || true)"
  cur="${cur#releases/}"
  if [ -f "$S/unhealthy" ] && grep -qx "$cur" "$S/unhealthy"; then rm -f "$S/healthz"
  else echo '{"status":"ok"}' > "$S/healthz"; fi
}
case "$cmd" in
  is-enabled) [ -f "$S/enabled$sfx" ] ;;
  enable)     : > "$S/enabled$sfx" ;;
  disable)    rm -f "$S/enabled$sfx"
              case " $* " in *" --now "*) rm -f "$S/active$sfx"; [ -n "$sfx" ] || rm -f "$S/healthz" ;; esac ;;
  is-active)  [ -f "$S/active$sfx" ] ;;
  start|restart)
              if [ "$unit" = oarfish-update-helper.socket ] && [ -f "$S/active.oarfish-update-helper.service" ]; then
                echo "Socket service oarfish-update-helper.service already active, refusing." >&2; exit 1
              fi
              [ -f "$S/fail-start$sfx" ] && exit 1
              : > "$S/active$sfx"; [ -n "$sfx" ] || set_health ;;
  stop)       rm -f "$S/active$sfx"; [ -n "$sfx" ] || rm -f "$S/healthz" ;;
  reset-failed) ;;
  cat)        [ -f "$FAKE_ROOT/etc/systemd/system/oarfish.service" ] || [ -L "$FAKE_ROOT/etc/systemd/system/oarfish.service" ] ;;
  daemon-reload)
              if [ -f "$S/block-daemon-reload" ]; then
                : > "$S/blocked"
                i=0; while [ ! -f "$S/proceed" ] && [ "$i" -lt 300 ]; do sleep 0.1; i=$((i+1)); done
              fi ;;
  *) exit 1 ;;
esac
EOF
# flock stand-in for hosts without util-linux (macOS): flock -x -w SECONDS FD, on an
# inherited descriptor, like the real tool. perl ships with macOS.
if ! command -v flock >/dev/null 2>&1; then
  cat > "$TMP/shim/flock" <<'EOF'
#!/usr/bin/perl
use strict; use warnings; use Fcntl qw(:flock); use Time::HiRes qw(sleep time);
my ($wait, $fd) = (0, undef);
while (@ARGV) { my $a = shift @ARGV; if ($a eq '-x') { next } elsif ($a eq '-w') { $wait = shift @ARGV } else { $fd = $a } }
open(my $fh, '<&=', $fd) or exit 64;
my $end = time() + $wait;
while (1) { exit 0 if flock($fh, LOCK_EX | LOCK_NB); exit 1 if time() >= $end; sleep(0.05); }
EOF
fi
# User and ownership tools for the tests that run with OARFISH_TEST_USERS=1: record
# their arguments instead of touching the machine. "id -u oarfish" follows useradd/userdel.
REAL_ID="$(command -v id)"
cat > "$TMP/shim/chown" <<'EOF'
#!/bin/sh
echo "$*" >> "$FAKE_SYSTEMD_STATE/chown.log"
EOF
cat > "$TMP/shim/useradd" <<'EOF'
#!/bin/sh
echo "useradd $*" >> "$FAKE_SYSTEMD_STATE/users.log"
: > "$FAKE_SYSTEMD_STATE/user-exists"
EOF
cat > "$TMP/shim/userdel" <<'EOF'
#!/bin/sh
echo "userdel $*" >> "$FAKE_SYSTEMD_STATE/users.log"
rm -f "$FAKE_SYSTEMD_STATE/user-exists"
EOF
cat > "$TMP/shim/getent" <<'EOF'
#!/bin/sh
exit 2
EOF
cat > "$TMP/shim/id" <<EOF
#!/bin/sh
if [ "\$1" = -u ] && [ "\${2:-}" = oarfish ]; then
  if [ -f "\$FAKE_SYSTEMD_STATE/user-exists" ]; then echo 998; exit 0; fi
  exit 1
fi
exec "$REAL_ID" "\$@"
EOF
chmod +x "$TMP/shim/"*
# The real tools that point to a different binary on this machine.
[ -x /usr/bin/uname ] || sed -i.bak 's#/usr/bin/uname#/bin/uname#' "$TMP/shim/uname"

# ---- helpers -------------------------------------------------------------
N=0
pass() { N=$((N+1)); echo "PASS  [$N] $1"; }
fail() { echo "--- last output ---"; cat "$TMP/out" 2>/dev/null || true; die "$1"; }

echo 0 > "$TMP/nr"
new_root() { # new_root: prints a fresh fake root with systemd marker and os-release (called in $(...), so the counter lives in a file)
  local n; n=$(( $(cat "$TMP/nr") + 1 )); echo "$n" > "$TMP/nr"; local r="$TMP/roots/r$n"
  mkdir -p "$r/run/systemd/system" "$r/etc" "$TMP/state/r$n"
  printf 'PRETTY_NAME="Test Linux 1"\nID=testlinux\nVERSION_ID="1"\n' > "$r/etc/os-release"
  snapshot "$r" > "$TMP/roots/r$n.snap"
  echo "$r"
}

# run_inst ROOT INSTALLER REL_DIR [args...]: sets RC, writes $TMP/out.
run_inst() {
  local root="$1" inst="$2" rel="$3"; shift 3
  local state; state="$TMP/state/$(basename "$root")"; mkdir -p "$state"
  set +e
  env -i HOME="$TMP" PATH="${INST_PATH:-$TMP/shim:$PATH}" TMPDIR="$TMP/tmp" \
    OARFISH_TEST_ROOT="$root" OARFISH_OPENSSL="${INST_OPENSSL:-$OPENSSL_BIN}" \
    OARFISH_SYSTEMCTL="$TMP/shim/systemctl-stub" FAKE_SYSTEMD_STATE="$state" FAKE_ROOT="$root" \
    OARFISH_RELEASE_BASE_URL="file://$rel" OARFISH_HEALTHCHECK="${INST_HEALTHCHECK-skip}" \
    ${INST_ENV:-} \
    FAKE_UNAME_S="${FAKE_UNAME_S:-Linux}" FAKE_UNAME_M="${FAKE_UNAME_M:-aarch64}" \
    "$SH" "$inst" "$@" > "${INST_OUT:-$TMP/out}" 2>&1
  RC=$?
  set -e
}
expect_ok()   { N=$((N+1)); local name="$1"; shift; run_inst "$@"; [ "$RC" = 0 ] || fail "[$N] $name: expected success, exit $RC"; echo "PASS  [$N] $name"; }
# expect_reject NAME PATTERN ROOT INSTALLER REL [args]: must exit non-zero, mention PATTERN.
expect_reject() {
  N=$((N+1)); local name="$1" pat="$2"; shift 2; run_inst "$@"
  [ "$RC" != 0 ] || fail "[$N] $name: ACCEPTED, must be rejected"
  grep -qi -- "$pat" "$TMP/out" || fail "[$N] $name: rejected, but not for the expected reason ($pat)"
  echo "PASS  [$N] $name: rejected ($pat)"
}
assert() { N=$((N+1)); local name="$1"; shift; "$@" >/dev/null 2>&1 || fail "[$N] $name"; echo "PASS  [$N] $name"; }
out_has() { grep -qF -- "$1" "$TMP/out"; }
out_lacks() { ! grep -qF -- "$1" "$TMP/out"; }

# A stable description of everything below a root (hashes, modes, inodes, symlink targets).
snapshot() {
  (cd "$1" && find . | LC_ALL=C sort | while IFS= read -r p; do
    if [ -L "$p" ]; then printf '%s -> %s\n' "$p" "$(readlink "$p")"
    elif [ -f "$p" ]; then printf '%s %s %s\n' "$p" "$(ls -ldi "$p" | awk '{print $1,$2,$6,$7,$8,$9}')" "$(sha256_of "$p")"
    else printf '%s %s\n' "$p" "$(ls -ld "$p" | awk '{print $1}')"; fi
  done)
}
no_leftovers() { # no staging dirs in the root, no temp dirs in TMPDIR
  [ -z "$(find "$1" -name '.stage.*' -o -name '.current.new.*' -o -name '*.new.*' 2>/dev/null)" ] \
    && [ -z "$(ls -A "$TMP/tmp")" ]
}
# Each fake root records its initial state; untouched ROOT fails if anything differs.
untouched() {
  local n; n="$(basename "$1")"
  cmp -s <(snapshot "$1") "$TMP/roots/$n.snap" || { diff <(snapshot "$1") "$TMP/roots/$n.snap" || true; fail "a refused run modified the root $n"; }
}
perm_of() { ls -ld "$1" | awk '{print substr($1,2,9)}'; }

# ELF header of a 64-bit little-endian executable for machine MACHINE_LO MACHINE_HI (octal).
fake_elf() { # fake_elf OUT TEXT [machine bytes, default AArch64 = \267\000]
  printf '\177ELF\002\001\001\000\000\000\000\000\000\000\000\000\002\000%b' "${3:-\\267\\000}" > "$1"
  printf '\001\000\000\000fake oarfish: %s\n' "$2" >> "$1"
}

# Keys: A is the key the test installers trust, B is an unrelated key.
minisign -G -W -f -p "$TMP/a.pub" -s "$TMP/a.key" >/dev/null
minisign -G -W -f -p "$TMP/b.pub" -s "$TMP/b.key" >/dev/null
sign_with() { # sign_with KEYFILE DIR [extra minisign args]: legacy-mode signature through the release script
  local key="$1" dir="$2"; shift 2
  OARFISH_RELEASE_SIGNING_KEY="$(cat "$key")" OARFISH_SIGN_COMMENT="oarfish test" "$ROOT/scripts/sign-release.sh" "$dir" >/dev/null
}
sign_release_dir() { # (re)create checksums.txt and the signature in DIR with key A
  local dir="$1"; shift
  rm -f "$dir/checksums.txt" "$dir/checksums.txt.sig"
  "$TMP/bundlegen" checksums -dir "$dir" "$@"
  sign_with "$TMP/a.key" "$dir"
}

# make_release DIR VERSION: complete signed release; the fake binary differs per version.
make_release() {
  local dir="$1" ver="$2"; mkdir -p "$dir"
  fake_elf "$TMP/fake-oarfish" "$ver"
  "$TMP/bundlegen" bundle -binary "$TMP/fake-oarfish" -license "$ROOT/LICENSE" \
    -notices "$ROOT/THIRD_PARTY_NOTICES.txt" -unit "$ROOT/install/oarfish.service" \
    -version "$ver" -commit "$COMMIT" -epoch 1700000000 -out "$dir" >/dev/null
  "$ROOT/scripts/stamp-installer.sh" "$ver" "$dir/install.sh" >/dev/null
  sign_release_dir "$dir" "oarfish_${ver}_linux_arm64.tar.gz" install.sh manifest.json
}

# make_installer VERSION OUT [PUBFILE]: test copy of install.sh that trusts PUBFILE's key.
make_installer() { "$ROOT/scripts/make-test-installer.sh" "$1" "${3:-$TMP/a.pub}" "$2"; }

echo "== setup"
"$ROOT/scripts/check-installer-key.sh"
make_release "$TMP/rel/v1" "$V1"
make_release "$TMP/rel/v2" "$V2"
make_installer "$V1" "$TMP/inst-v1.sh"
make_installer "$V2" "$TMP/inst-v2.sh"
make_installer none "$TMP/inst-unstamped.sh"
REL1="$TMP/rel/v1"; REL2="$TMP/rel/v2"
mkdir -p "$TMP/rel/empty"

echo "== install, layout, idempotence"
R="$(new_root)"
expect_ok "fresh install" "$R" "$TMP/inst-v1.sh" "$REL1"
out_has "Oarfish $V1 is installed and running" || fail "summary missing"
out_has "sudo cat /var/lib/oarfish/setup-token" || fail "no setup-token hint on a device without an account"
pass "the summary names the setup-token command while no account exists (ADR 0009)"
assert "binary installed and executable" test -x "$R/opt/oarfish/releases/$V1/oarfish"
assert "current is a relative symlink to the release" test "$(readlink "$R/opt/oarfish/current")" = "releases/$V1"
assert "unit installed and identical to the bundle's unit" cmp -s "$ROOT/install/oarfish.service" "$R/etc/systemd/system/oarfish.service"
assert "LICENSE and third-party notices installed" test -f "$R/opt/oarfish/releases/$V1/LICENSE" -a -f "$R/opt/oarfish/releases/$V1/THIRD_PARTY_NOTICES.txt"
assert "/etc/oarfish is 0750" test "$(perm_of "$R/etc/oarfish")" = rwxr-x---
assert "/etc/oarfish/secrets is 0700" test "$(perm_of "$R/etc/oarfish/secrets")" = rwx------
assert "/var/lib/oarfish is 0750" test "$(perm_of "$R/var/lib/oarfish")" = rwxr-x---
assert "/var/cache/oarfish exists" test -d "$R/var/cache/oarfish"
assert "release dir is not group/world writable" test "$(perm_of "$R/opt/oarfish/releases/$V1")" = rwxr-xr-x
CALLS="$TMP/state/$(basename "$R")/calls.log"
grep -q '^daemon-reload' "$CALLS" && grep -q '^enable oarfish.service' "$CALLS" && grep -q '^restart oarfish.service' "$CALLS" \
  || fail "systemctl was not driven as expected: $(cat "$CALLS")"
pass "daemon-reload, enable and restart were issued"
assert "no staging or temp leftovers" no_leftovers "$R"
out_has "signature OK (key " || fail "no signature line in output"

snapshot "$R" > "$TMP/snap1"; : > "$CALLS"
expect_ok "re-run of the same version" "$R" "$TMP/inst-v1.sh" "$REL1"
snapshot "$R" > "$TMP/snap2"
cmp -s "$TMP/snap1" "$TMP/snap2" || { diff "$TMP/snap1" "$TMP/snap2" || true; fail "re-run changed the installation"; }
pass "re-run changed nothing (files, modes, inodes, symlink identical)"
grep -qE '^(restart|start|enable|daemon-reload)' "$CALLS" && fail "re-run restarted or re-enabled the service: $(cat "$CALLS")"
pass "re-run did not restart, enable or reload anything"
out_has "nothing to restart" || fail "re-run output"
assert "no leftovers after re-run" no_leftovers "$R"

mkdir -p "$R/var/lib/oarfish/auth" && echo '{}' > "$R/var/lib/oarfish/auth/account.json"
expect_ok "upgrade to the next version" "$R" "$TMP/inst-v2.sh" "$REL2"
if out_has "setup-token"; then fail "the setup-token hint is shown although an account exists"; fi
pass "no setup-token hint once an account exists"
assert "the account file is kept by the upgrade" test -f "$R/var/lib/oarfish/auth/account.json"
assert "current points to the new release" test "$(readlink "$R/opt/oarfish/current")" = "releases/$V2"
assert "previous release is kept" test -x "$R/opt/oarfish/releases/$V1/oarfish"
grep -q '^restart oarfish.service' "$CALLS" || fail "upgrade did not restart"
pass "upgrade restarted the service"

echo "== refusals before anything is touched"
R="$(new_root)"
for arch in x86_64 armv7l i686; do
  FAKE_UNAME_M=$arch expect_reject "architecture $arch refused" "unsupported CPU architecture" "$R" "$TMP/inst-v1.sh" "$REL1"
done
FAKE_UNAME_S=Darwin expect_reject "non-Linux refused" "unsupported operating system" "$R" "$TMP/inst-v1.sh" "$REL1"
R2="$(new_root)"; rmdir "$R2/run/systemd/system"
expect_reject "missing systemd refused" "systemd is not the running init" "$R2" "$TMP/inst-v1.sh" "$REL1"
untouched "$R"
pass "refused runs left the root untouched"

# Tools: a PATH that lacks one required tool.
tool_path() { # tool_path EXCLUDE: a PATH dir with every needed tool except EXCLUDE
  local d="$TMP/path-$1"; mkdir -p "$d"
  for t in curl tar gzip sha256sum od base64 mktemp awk df id mv ln rm mkdir chmod cat cp sed grep tr head tail wc sleep readlink dirname ls flock find chown useradd userdel getent; do
    [ "$t" != "$1" ] || continue
    src="$(PATH="$TMP/shim:$PATH" command -v "$t")" || die "test host lacks $t"
    ln -sf "$src" "$d/$t"
  done
  cp "$TMP/shim/uname" "$d/uname"
  echo "$d"
}
R="$(new_root)"
INST_PATH="$(tool_path curl)" INST_OPENSSL="$OPENSSL_BIN" expect_reject "missing curl refused" "required tools are missing: curl" "$R" "$TMP/inst-v1.sh" "$REL1"
INST_PATH="$(tool_path awk)" expect_reject "missing awk refused" "required tools are missing: awk" "$R" "$TMP/inst-v1.sh" "$REL1"
INST_OPENSSL=openssl INST_PATH="$(tool_path openssl)" expect_reject "missing openssl refused" "required tools are missing: openssl" "$R" "$TMP/inst-v1.sh" "$REL1"

fake_openssl() { # fake_openssl NAME VERSION-LINE [pkeyutl exit code]
  cat > "$TMP/shim/$1" <<EOF
#!/bin/sh
if [ "\$1" = version ]; then echo "$2"; exit 0; fi
exit ${3:-1}
EOF
  chmod +x "$TMP/shim/$1"
}
fake_openssl openssl-libre "LibreSSL 3.3.6"
fake_openssl openssl-old "OpenSSL 1.1.1w  11 Sep 2023"
fake_openssl openssl-liar "OpenSSL 3.0.13 30 Jan 2024" 0
INST_OPENSSL="$TMP/shim/openssl-libre" expect_reject "LibreSSL refused" "is not OpenSSL" "$R" "$TMP/inst-v1.sh" "$REL1"
INST_OPENSSL="$TMP/shim/openssl-old" expect_reject "OpenSSL 1.1.1 refused" "too old" "$R" "$TMP/inst-v1.sh" "$REL1"
if [ -x /usr/bin/openssl ] && /usr/bin/openssl version | grep -q LibreSSL; then
  INST_OPENSSL=/usr/bin/openssl expect_reject "the real macOS LibreSSL is refused" "is not OpenSSL" "$R" "$TMP/inst-v1.sh" "$REL1"
fi
INST_OPENSSL="$TMP/shim/openssl-liar" expect_reject "negative control: a verifier that accepts everything is refused" "accepted a modified file" "$R" "$TMP/inst-v1.sh" "$REL1"
untouched "$R"

expect_reject "unstamped installer refuses without --version" "not tied to a release" "$R" "$TMP/inst-unstamped.sh" "$REL1"
out_has "--version" || fail "refusal does not explain how to proceed"
for bad in '../x' '1.0' '1.0.0 foo' 'v01.0.0' '1.0.0/../../etc' "$(printf '1.0.0\n../x')" ''; do
  expect_reject "invalid version '$(printf '%s' "$bad" | tr '\n' '~')' refused" "not a valid version\|needs a value\|not tied" "$R" "$TMP/inst-unstamped.sh" "$REL1" --version "$bad"
done
untouched "$R"
expect_reject "unknown option refused" "unknown option" "$R" "$TMP/inst-v1.sh" "$REL1" --frobnicate
expect_reject "--purge without --uninstall refused" "only makes sense" "$R" "$TMP/inst-v1.sh" "$REL1" --purge

echo "== tampering is rejected, nothing is installed"
tamper_case() { # tamper_case NAME PATTERN COMMAND-ON-COPY...
  local name="$1" pat="$2"; shift 2
  local copy="$TMP/rel/t-$N"; rm -rf "$copy"; cp -R "$REL1" "$copy"
  ( cd "$copy" && "$@" )
  local r; r="$(new_root)"
  expect_reject "$name" "$pat" "$r" "$TMP/inst-v1.sh" "$copy"
  untouched "$r"
  no_leftovers "$r" || fail "[$N] $name: leftovers"
}
flip() { local f="$1" size; size="$(wc -c < "$f" | tr -d ' ')"; printf '\377' | dd of="$f" bs=1 seek=$((size / 2)) count=1 conv=notrunc 2>/dev/null; }
# The first digit must really change, whatever it is (a swap through 0 and 1 is a no-op for a hash that starts with 1).
tamper_case "tampered checksums.txt (a hash changed)" "SIGNATURE VERIFICATION FAILED" sed -i.bak -e '1s/^0/1/' -e t -e '1s/^./0/' checksums.txt
tamper_case "tampered checksums.txt (line appended)" "SIGNATURE VERIFICATION FAILED" sh -c 'echo "$(printf "0%.0s" $(seq 1 64))  evil" >> checksums.txt'
tamper_case "tampered signature (byte flipped)" "SIGNATURE VERIFICATION FAILED" flip checksums.txt.sig
tamper_case "signature file truncated" "SIGNATURE VERIFICATION FAILED" sh -c 'head -c 60 checksums.txt.sig > s && mv s checksums.txt.sig'
tamper_case "signature line empty" "SIGNATURE VERIFICATION FAILED" sh -c 'printf "untrusted comment: x\n\n" > checksums.txt.sig'
tamper_case "signature made by another key" "SIGNATURE VERIFICATION FAILED" sh -c "OARFISH_RELEASE_SIGNING_KEY=\"\$(cat '$TMP/b.key')\" '$ROOT/scripts/sign-release.sh' . >/dev/null"
tamper_case "valid prehashed (ED) signature of the trusted key" "SIGNATURE VERIFICATION FAILED" sh -c "rm checksums.txt.sig && minisign -S -s '$TMP/a.key' -x checksums.txt.sig -m checksums.txt </dev/null >/dev/null"
tamper_case "bundle tampered, checksums and signature intact" "does not match the SHA-256" flip "oarfish_${V1}_linux_arm64.tar.gz"
tamper_case "bundle truncated, checksums and signature intact" "does not match the SHA-256" sh -c "head -c 1000 oarfish_${V1}_linux_arm64.tar.gz > b && mv b oarfish_${V1}_linux_arm64.tar.gz"
tamper_case "manifest tampered, checksums and signature intact" "does not match the SHA-256" sh -c 'echo " " >> manifest.json'
tamper_case "bundle missing (download fails)" "download of" rm "oarfish_${V1}_linux_arm64.tar.gz"
tamper_case "signature missing (download fails)" "download of" rm checksums.txt.sig
tamper_case "signature replayed from another release" "SIGNATURE VERIFICATION FAILED" cp "$REL2/checksums.txt.sig" checksums.txt.sig

# Signed by the trusted key, but not what the installer expects.
custom_release() { # custom_release NAME BUILD-FUNCTION: signed release whose bundle is built by the function
  local dir="$TMP/rel/c-$1"; rm -rf "$dir"; mkdir -p "$dir"
  cp "$REL1/manifest.json" "$dir/"
  local work="$TMP/work-$1"; rm -rf "$work"; mkdir -p "$work/oarfish_${V1}_linux_arm64"
  cp -R "$TMP/bundle-src/"* "$work/oarfish_${V1}_linux_arm64/"
  "$2" "$work/oarfish_${V1}_linux_arm64"
  (cd "$work" && COPYFILE_DISABLE=1 tar -czf "$dir/oarfish_${V1}_linux_arm64.tar.gz" "oarfish_${V1}_linux_arm64")
  sign_release_dir "$dir" "oarfish_${V1}_linux_arm64.tar.gz" manifest.json
  echo "$dir"
}
mkdir -p "$TMP/bundle-src"
fake_elf "$TMP/bundle-src/oarfish" good; chmod 755 "$TMP/bundle-src/oarfish"
cp "$ROOT/install/oarfish.service" "$ROOT/LICENSE" "$ROOT/THIRD_PARTY_NOTICES.txt" "$TMP/bundle-src/"
add_extra() { echo evil > "$1/extra"; }
add_link()  { ln -s /etc/passwd "$1/evil-link"; }
wrong_arch() { fake_elf "$1/oarfish" x86 '\076\000'; }
x86=$(custom_release extra add_extra); R="$(new_root)"
expect_reject "signed bundle with an unexpected file" "unexpected entry" "$R" "$TMP/inst-v1.sh" "$x86"
x86=$(custom_release link add_link)
expect_reject "signed bundle with a symlink" "regular files" "$R" "$TMP/inst-v1.sh" "$x86"
x86=$(custom_release arch wrong_arch)
expect_reject "signed bundle with an x86-64 binary" "not a 64-bit ARM" "$R" "$TMP/inst-v1.sh" "$x86"
untouched "$R"
pass "none of the signed-but-wrong bundles touched the root"

echo "== production installer (real key) rejects a release signed by the test key"
mkdir -p "$TMP/prod"; "$ROOT/scripts/stamp-installer.sh" "$V1" "$TMP/prod/install.sh" >/dev/null
R="$(new_root)"
expect_reject "release signed with another key under the production key" "SIGNATURE VERIFICATION FAILED" "$R" "$TMP/prod/install.sh" "$REL1"
out_has "11F1C52777090BFC" || fail "refusal does not name the expected key id"
untouched "$R"

echo "== failed or interrupted downloads leave an existing installation alone"
R="$(new_root)"
expect_ok "install v1" "$R" "$TMP/inst-v1.sh" "$REL1"
snapshot "$R" > "$TMP/snap-before"
# curl that dies halfway through the bundle (exit 18 = partial file)
cat > "$TMP/shim-curl-dir.sh" <<EOF
#!/bin/sh
for a in "\$@"; do last="\$a"; done
case "\$last" in
  *.tar.gz) real_out=''; prev=''; for a in "\$@"; do [ "\$prev" = -o ] && real_out="\$a"; prev="\$a"; done
            head -c 500 "\${last#file://}" > "\$real_out"; exit 18 ;;
esac
exec "$(command -v curl)" "\$@"
EOF
mkdir -p "$TMP/curlpath"; cp "$TMP/shim-curl-dir.sh" "$TMP/curlpath/curl"; chmod +x "$TMP/curlpath/curl"
INST_PATH="$TMP/curlpath:$TMP/shim:$PATH" expect_reject "download interrupted during the bundle" "download of .* failed" "$R" "$TMP/inst-v2.sh" "$REL2"
snapshot "$R" > "$TMP/snap-after"
cmp -s "$TMP/snap-before" "$TMP/snap-after" || { diff "$TMP/snap-before" "$TMP/snap-after" || true; fail "interrupted download changed the installation"; }
assert "interrupted download left no staging files" no_leftovers "$R"
pass "old installation intact after the interrupted download"
tc="$TMP/rel/t-corrupt"; rm -rf "$tc"; cp -R "$REL2" "$tc"; head -c 1000 "$REL2/oarfish_${V2}_linux_arm64.tar.gz" > "$tc/oarfish_${V2}_linux_arm64.tar.gz"
expect_reject "truncated bundle with a correct signature" "does not match the SHA-256" "$R" "$TMP/inst-v2.sh" "$tc"
cmp -s "$TMP/snap-before" <(snapshot "$R") || fail "corrupt download changed the installation"
pass "old installation intact after the corrupt download"
expect_reject "release that does not exist" "download of" "$R" "$TMP/inst-v2.sh" "$TMP/rel/empty"
cmp -s "$TMP/snap-before" <(snapshot "$R") || fail "missing release changed the installation"
pass "old installation intact after the missing release"

echo "== existing release directory must be identical"
R="$(new_root)"; mkdir -p "$R/opt/oarfish/releases/$V1"; echo different > "$R/opt/oarfish/releases/$V1/oarfish"
expect_reject "release dir with different content" "already exists and differs" "$R" "$TMP/inst-v1.sh" "$REL1"
assert "current was not created" test ! -e "$R/opt/oarfish/current"
R="$(new_root)"; mkdir -p "$R/opt/oarfish/current"
expect_reject "current is a real directory" "not a symlink" "$R" "$TMP/inst-v1.sh" "$REL1"

echo "== health check failure: rollback, and the first installation"
# The stub systemctl writes the health file on start/restart unless the active version is listed in $state/unhealthy.
state_of() { echo "$TMP/state/$(basename "$1")"; }
HEALTH_ENV() { echo "OARFISH_HEALTH_URL=file://$(state_of "$1")/healthz OARFISH_HEALTH_TIMEOUT=2"; }
line_of() { grep -n -- "$1" "$2" | head -n "${3:-1}" | tail -n 1 | cut -d: -f1; }

R="$(new_root)"; ST="$(state_of "$R")"
INST_HEALTHCHECK='' INST_ENV="$(HEALTH_ENV "$R")" expect_ok "install v1 with the health check on" "$R" "$TMP/inst-v1.sh" "$REL1"
out_has "healthy" || fail "no healthy line"
echo "$V2" > "$ST/unhealthy"; : > "$ST/calls.log"
INST_HEALTHCHECK='' INST_ENV="$(HEALTH_ENV "$R")" expect_reject "upgrade that fails its health check: rollback is healthy" "previous version $V1 is active again and healthy" "$R" "$TMP/inst-v2.sh" "$REL2"
assert "current points to the previous release again" test "$(readlink "$R/opt/oarfish/current")" = "releases/$V1"
assert "reset-failed was called" grep -q '^reset-failed oarfish.service' "$ST/calls.log"
rs="$(line_of '^restart oarfish.service' "$ST/calls.log" 2)"; rf="$(line_of '^reset-failed oarfish.service' "$ST/calls.log")"
[ -n "$rs" ] && [ -n "$rf" ] && [ "$rf" -lt "$rs" ] || fail "reset-failed must come before the restart of the rollback (log: $(tr '\n' ';' < "$ST/calls.log"))"
pass "reset-failed comes before the rollback's restart"
assert "the service is healthy again after the rollback" test -f "$ST/healthz"
out_lacks "AND the rollback" || fail "a healthy rollback must not report a failed rollback"

printf '%s\n%s\n' "$V1" "$V2" > "$ST/unhealthy"; : > "$ST/calls.log"
INST_HEALTHCHECK='' INST_ENV="$(HEALTH_ENV "$R")" expect_reject "upgrade that fails and whose rollback is unhealthy too" "rollback to $V1 did not become healthy either" "$R" "$TMP/inst-v2.sh" "$REL2"
out_has "journalctl -u oarfish" || fail "no journalctl hint"
out_lacks "is active again and healthy" || fail "an unhealthy rollback must not claim the old version is healthy"
assert "reset-failed was called for the failed rollback too" grep -q '^reset-failed oarfish.service' "$ST/calls.log"
assert "current points to the previous release" test "$(readlink "$R/opt/oarfish/current")" = "releases/$V1"

R="$(new_root)"; ST="$(state_of "$R")"; echo "$V1" > "$ST/unhealthy"
INST_HEALTHCHECK='' INST_ENV="$(HEALTH_ENV "$R")" expect_reject "first installation that never becomes healthy" "stopped and disabled" "$R" "$TMP/inst-v1.sh" "$REL1"
out_has "journalctl -u oarfish" || fail "no journalctl hint"
assert "disable --now was issued" grep -q '^disable --now oarfish.service' "$ST/calls.log"
assert "the stub service is disabled and stopped" test ! -e "$ST/enabled" -a ! -e "$ST/active"
assert "the files are kept" test -x "$R/opt/oarfish/releases/$V1/oarfish" -a "$(readlink "$R/opt/oarfish/current")" = "releases/$V1" -a -f "$R/etc/systemd/system/oarfish.service"
out_lacks "rolling back" || fail "a first installation has nothing to roll back to"

echo "== ownership and users (stub chown, useradd, userdel, getent, id)"
R="$(new_root)"; ST="$(state_of "$R")"
INST_ENV="OARFISH_TEST_USERS=1" expect_ok "install acting as root with stub user tools" "$R" "$TMP/inst-v1.sh" "$REL1"
grep -q '^useradd --system --user-group --no-create-home --home-dir /var/lib/oarfish --shell [^ ]*nologin oarfish$' "$ST/users.log" \
  || fail "useradd was not called as expected: $(cat "$ST/users.log" 2>/dev/null)"
pass "useradd: system user, own group, no home, nologin shell"
for f in "" /oarfish /oarfish.service /LICENSE /THIRD_PARTY_NOTICES.txt; do
  grep -q "^root:root .*/release$f\$" "$ST/chown.log" || fail "chown root:root was not applied to the release path '...${f:-/release}' (chown.log: $(tr '\n' ';' < "$ST/chown.log"))"
done
pass "chown root:root was applied to every one of the five release paths"
grep -q "^root:oarfish $R/etc/oarfish\$" "$ST/chown.log" && grep -q "^oarfish:oarfish $R/etc/oarfish/secrets\$" "$ST/chown.log" \
  && grep -q "^oarfish:oarfish $R/var/lib/oarfish\$" "$ST/chown.log" && grep -q "^oarfish:oarfish $R/var/cache/oarfish\$" "$ST/chown.log" \
  || fail "directory ownership: $(tr '\n' ';' < "$ST/chown.log")"
pass "directories got their owners"
: > "$ST/users.log"
INST_ENV="OARFISH_TEST_USERS=1" expect_ok "re-run with the user present" "$R" "$TMP/inst-v1.sh" "$REL1"
[ ! -s "$ST/users.log" ] || fail "a re-run must not create the user again: $(cat "$ST/users.log")"
out_has "service user oarfish exists" || fail "no 'exists' line"
pass "re-run did not call useradd"

echo '{"keep":"me"}' > "$R/etc/oarfish/config.json"
INST_ENV="OARFISH_TEST_USERS=1" INST_PATH="$(tool_path userdel)" expect_reject "--uninstall --purge needs userdel" "userdel was not found" "$R" "$TMP/inst-v1.sh" "$REL1" --uninstall --purge
INST_ENV="OARFISH_TEST_USERS=1" INST_PATH="$(tool_path userdel)" expect_reject "--uninstall --purge --dry-run warns about userdel too" "userdel was not found" "$R" "$TMP/inst-v1.sh" "$REL1" --uninstall --purge --dry-run
assert "the refusal changed nothing" test -f "$R/etc/oarfish/config.json" -a -x "$R/opt/oarfish/releases/$V1/oarfish"
INST_ENV="OARFISH_TEST_USERS=1" INST_PATH="$(tool_path userdel)" expect_ok "--uninstall without --purge does not need userdel" "$R" "$TMP/inst-v1.sh" "$REL1" --uninstall
R="$(new_root)"; ST="$(state_of "$R")"
INST_ENV="OARFISH_TEST_USERS=1" expect_ok "install again" "$R" "$TMP/inst-v1.sh" "$REL1"
INST_ENV="OARFISH_TEST_USERS=1" expect_ok "--uninstall --purge removes the user" "$R" "$TMP/inst-v1.sh" "$REL1" --uninstall --purge
grep -q '^userdel oarfish$' "$ST/users.log" || fail "userdel was not called: $(cat "$ST/users.log")"
pass "userdel oarfish was called"

echo "== an existing unit is replaced, and the output says so"
R="$(new_root)"; mkdir -p "$R/etc/systemd/system"; echo "[Service]" > "$R/etc/systemd/system/oarfish.service"
expect_ok "install over a unit that differs" "$R" "$TMP/inst-v1.sh" "$REL1"
out_has "replaced the existing" || fail "no 'replaced' message"
out_has "drop-in" || fail "no drop-in hint"
assert "the unit is the release's unit" cmp -s "$ROOT/install/oarfish.service" "$R/etc/systemd/system/oarfish.service"

echo "== the unit and the installer agree on the listen address"
unit_listen="$(sed -n 's/^ExecStart=.* -listen \([^ ]*\).*$/\1/p' "$ROOT/install/oarfish.service")"
inst_listen="$(sed -n "s/^LISTEN_ADDR='\(.*\)'\$/\1/p" "$ROOT/install/install.sh")"
[ -n "$unit_listen" ] && [ "$unit_listen" = "$inst_listen" ] || fail "unit -listen '$unit_listen' vs installer LISTEN_ADDR '$inst_listen'"
pass "ExecStart -listen $unit_listen equals the installer's LISTEN_ADDR"
case "$unit_listen" in 127.0.0.1:*) pass "the unit listens on loopback only" ;; *) fail "unit listens on $unit_listen" ;; esac

echo "== dry run"
R="$(new_root)"
expect_ok "dry run" "$R" "$TMP/prod/install.sh" "$TMP/rel/empty" --dry-run
for s in "11F1C52777090BFC" "oarfish-release.pub" "127.0.0.1:8484" "Dry run: nothing was downloaded or changed" "Test Linux 1"; do
  out_has "$s" || fail "dry run output lacks '$s'"
done
pass "dry run names the key id, listen address and system, and does not download"
untouched "$R"
pass "dry run changed nothing"
FAKE_UNAME_M=x86_64 expect_reject "dry run still refuses a wrong architecture" "unsupported CPU architecture" "$R" "$TMP/prod/install.sh" "$TMP/rel/empty" --dry-run

echo "== uninstall"
R="$(new_root)"
expect_ok "install" "$R" "$TMP/inst-v1.sh" "$REL1"
echo '{"keep":"me"}' > "$R/etc/oarfish/config.json"; echo state > "$R/var/lib/oarfish/state.db"
snapshot "$R" > "$TMP/snap-installed"
expect_ok "uninstall dry run" "$R" "$TMP/inst-v1.sh" "$REL1" --uninstall --dry-run
cmp -s <(snapshot "$R") "$TMP/snap-installed" || fail "uninstall dry run changed something"
pass "uninstall --dry-run changed nothing"
: > "$TMP/state/$(basename "$R")/calls.log"
expect_ok "uninstall" "$R" "$TMP/inst-v1.sh" "$REL1" --uninstall
assert "service files and binaries removed" test ! -e "$R/opt/oarfish" -a ! -e "$R/etc/systemd/system/oarfish.service"
assert "cache removed" test ! -e "$R/var/cache/oarfish"
assert "configuration kept" test -f "$R/etc/oarfish/config.json"
assert "state kept" test -f "$R/var/lib/oarfish/state.db"
grep -q '^stop oarfish.service' "$TMP/state/$(basename "$R")/calls.log" && grep -q '^disable oarfish.service' "$TMP/state/$(basename "$R")/calls.log" \
  || fail "uninstall did not stop and disable the service"
pass "uninstall stopped and disabled the service"
expect_ok "uninstall twice" "$R" "$TMP/inst-v1.sh" "$REL1" --uninstall
assert "data still kept after the second uninstall" test -f "$R/etc/oarfish/config.json"
expect_ok "reinstall after uninstall" "$R" "$TMP/inst-v1.sh" "$REL1"
assert "reinstall found the old data" test -f "$R/var/lib/oarfish/state.db"
expect_ok "uninstall --purge" "$R" "$TMP/inst-v1.sh" "$REL1" --uninstall --purge
assert "purge removed configuration and state" test ! -e "$R/etc/oarfish" -a ! -e "$R/var/lib/oarfish"
left="$(find "$R" \( -type f -o -type l \) ! -path "$R/etc/os-release")"
[ -z "$left" ] || fail "files remain after purge: $left"
pass "no installed file or link remains after purge"


# =========================================================================
# Components shape (ADR 0010, G6.3b): the update helper layout, the lock, the
# link layout, the order of systemctl calls. Nothing here runs systemd; the stub
# proves the installer's logic and order, not systemd behaviour (docs/INSTALL.md).
# =========================================================================
CKEY="$(sed -n 1p "$ROOT/release/oarfish-release.pub" | sed -n 's/^untrusted comment: minisign public key \([0-9A-F]\{16\}\)$/\1/p' | tr 'A-F' 'a-f')"
CV1=0.2.0-test.1; CV2=0.2.0-test.2; CV3=0.2.0-test.3
UNITS3="oarfish-update-helper.service oarfish-update-helper.socket oarfish-update-recover.service"

# make_crelease DIR VERSION [HELPER_TAG] [SOCKET_COMMENT]: signed components release (throwaway key A).
make_crelease() {
  local dir="$1" ver="$2" tag="${3:-a}" sock="${4:-}" ud u
  ud="$TMP/cunits-$ver-$tag-${sock:-none}"; mkdir -p "$dir" "$ud"
  fake_elf "$TMP/fake-oarfish" "$ver"; fake_elf "$TMP/fake-helper" "helper $tag"
  for u in $UNITS3; do cp "$ROOT/install/$u" "$ud/$u"; done
  [ -z "$sock" ] || printf '# %s\n' "$sock" >> "$ud/oarfish-update-helper.socket"
  "$TMP/bundlegen" bundle -binary "$TMP/fake-oarfish" -license "$ROOT/LICENSE" \
    -notices "$ROOT/THIRD_PARTY_NOTICES.txt" -unit "$ROOT/install/oarfish.service" \
    -version "$ver" -commit "$COMMIT" -epoch 1700000000 -out "$dir" \
    -helper "$TMP/fake-helper" -helper-service "$ud/oarfish-update-helper.service" \
    -helper-socket "$ud/oarfish-update-helper.socket" -recover-service "$ud/oarfish-update-recover.service" \
    -components-protocol 1 -components-key-id "$CKEY" >/dev/null
  "$ROOT/scripts/stamp-installer.sh" "$ver" "$dir/install.sh" >/dev/null
  sign_release_dir "$dir" "oarfish_${ver}_linux_arm64.tar.gz" install.sh manifest.json
}

echo "== components shape: setup"
CREL1="$TMP/rel/c1"; CREL2="$TMP/rel/c2"; CREL3="$TMP/rel/c3"; CREL3S="$TMP/rel/c3s"
make_crelease "$CREL1" "$CV1" a
make_crelease "$CREL2" "$CV2" a                 # same helper bytes as c1
make_crelease "$CREL3" "$CV3" b                 # different helper bytes
make_crelease "$CREL3S" "$CV3" a socket-text-changed   # same helper, different socket text
make_installer "$CV1" "$TMP/inst-c1.sh"; make_installer "$CV2" "$TMP/inst-c2.sh"; make_installer "$CV3" "$TMP/inst-c3.sh"
tar -tzf "$CREL1/oarfish_${CV1}_linux_arm64.tar.gz" | sed 's#^[^/]*/##' | grep -v '^$' | LC_ALL=C sort | tr '\n' ' ' > "$TMP/c1-entries"
[ "$(cat "$TMP/c1-entries")" = "LICENSE THIRD_PARTY_NOTICES.txt oarfish oarfish-update-helper oarfish-update-helper.service oarfish-update-helper.socket oarfish-update-recover.service oarfish.service " ] \
  || fail "the components fixture has unexpected entries: $(cat "$TMP/c1-entries")"
pass "the components fixture holds the nine entries of the contract"

# resolve ROOT PATH: follow links the way the device would, absolute targets inside ROOT.
resolve() {
  local r="$1" p="$2" n=0 t
  while [ -L "$p" ]; do
    t="$(readlink "$p")"
    case $t in /*) p="$r$t" ;; *) p="$(dirname "$p")/$t" ;; esac
    n=$((n + 1)); [ "$n" -le 20 ] || return 1
  done
  [ -e "$p" ] && echo "$p"
}
link_is() { [ -L "$1" ] && [ "$(readlink "$1")" = "$2" ]; } # link_is PATH TARGET
mode_is() { [ "$(perm_of "$1")" = "$2" ]; }
calls_of() { tr '\n' ';' < "$(state_of "$1")/calls.log"; }
# True when the lock on DIR can be taken at once (nobody holds it).
lock_is_free() { perl -e 'use Fcntl ":flock"; open(F, "<", $ARGV[0]) or exit 2; exit(flock(F, LOCK_EX | LOCK_NB) ? 0 : 1)' "$1"; }
hold_lock() { # hold_lock DIR SECONDS: a background process that holds an exclusive flock on DIR
  rm -f "$TMP/holder.out"
  perl -e '$| = 1; use Fcntl ":flock"; open(F, "<", $ARGV[0]) or die; flock(F, LOCK_EX) or die; print "locked\n"; sleep $ARGV[1];' "$1" "$2" > "$TMP/holder.out" &
  HOLDER=$!
  for _ in $(seq 1 50); do [ -s "$TMP/holder.out" ] && break; sleep 0.1; done
  [ -s "$TMP/holder.out" ] || fail "the test could not take the lock"
}
release_lock() { kill "$HOLDER" 2>/dev/null || true; wait "$HOLDER" 2>/dev/null || true; }
print_layout() { # the link layout as the test sees it
  (cd "$1" && find opt etc usr var -not -path 'opt/oarfish/releases/*' -not -path 'etc/oarfish*' -not -path 'var/lib/oarfish/*' -not -path 'var/cache/*' -not -path 'opt/oarfish/components/[0-9]*/*' \
    | LC_ALL=C sort | while IFS= read -r p; do
      if [ -L "$p" ]; then printf '  %s -> %s\n' "$p" "$(readlink "$p")"; else printf '  %s\n' "$p"; fi
    done)
}

echo "== bundle shapes: exactly two are accepted, everything else is rejected before a change"
mkdir -p "$TMP/cbundle-src"
fake_elf "$TMP/cbundle-src/oarfish" good; chmod 755 "$TMP/cbundle-src/oarfish"
fake_elf "$TMP/cbundle-src/oarfish-update-helper" helper; chmod 755 "$TMP/cbundle-src/oarfish-update-helper"
cp "$ROOT/install/oarfish.service" "$ROOT/LICENSE" "$ROOT/THIRD_PARTY_NOTICES.txt" "$TMP/cbundle-src/"
for u in $UNITS3; do cp "$ROOT/install/$u" "$TMP/cbundle-src/"; done
make_release "$TMP/rel/lcv1" "$CV1"             # a legacy release (and manifest) with the components test version
custom_bundle() { # custom_bundle NAME VERSION SRCDIR MANIFEST_DIR BUILD-FUNCTION: signed release with a hand-made bundle
  local name="$1" ver="$2" src="$3" mdir="$4" fn="$5" dir work
  dir="$TMP/rel/x-$name"; rm -rf "$dir"; mkdir -p "$dir"
  cp "$mdir/manifest.json" "$dir/"
  work="$TMP/work-x-$name"; rm -rf "$work"; mkdir -p "$work/oarfish_${ver}_linux_arm64"
  cp -R "$src/"* "$work/oarfish_${ver}_linux_arm64/"
  "$fn" "$work/oarfish_${ver}_linux_arm64"
  (cd "$work" && COPYFILE_DISABLE=1 tar -czf "$dir/oarfish_${ver}_linux_arm64.tar.gz" "oarfish_${ver}_linux_arm64")
  sign_release_dir "$dir" "oarfish_${ver}_linux_arm64.tar.gz" manifest.json
  echo "$dir"
}
nothing() { :; }
c_extra() { echo evil > "$1/extra"; }
c_missing_unit() { rm "$1/oarfish-update-recover.service"; }
c_missing_helper() { rm "$1/oarfish-update-helper"; }
c_helper_link() { rm "$1/oarfish-update-helper"; ln -s /etc/passwd "$1/oarfish-update-helper"; }
c_helper_hardlink() { rm "$1/oarfish-update-helper"; ln "$1/oarfish" "$1/oarfish-update-helper"; }
c_helper_text() { echo "not an executable" > "$1/oarfish-update-helper"; }
c_helper_x86() { fake_elf "$1/oarfish-update-helper" x86 '\076\000'; }
c_helper_32bit() { printf '\177ELF\001\001\001\000\000\000\000\000\000\000\000\000\002\000\267\000' > "$1/oarfish-update-helper"; }
l_missing() { rm "$1/LICENSE"; }
l_helper_only() { cp "$TMP/cbundle-src/oarfish-update-helper" "$1/"; }

R="$(new_root)"
x="$(custom_bundle plain "$CV1" "$TMP/cbundle-src" "$CREL1" nothing)"
expect_ok "a hand-made components bundle that follows the contract is accepted" "$R" "$TMP/inst-c1.sh" "$x"
R="$(new_root)"
x="$(custom_bundle c-extra "$CV1" "$TMP/cbundle-src" "$CREL1" c_extra)";          expect_reject "components bundle with an extra entry" "unexpected entry" "$R" "$TMP/inst-c1.sh" "$x"
x="$(custom_bundle c-missing "$CV1" "$TMP/cbundle-src" "$CREL1" c_missing_unit)"; expect_reject "components bundle without the recover unit" "neither the legacy shape" "$R" "$TMP/inst-c1.sh" "$x"
x="$(custom_bundle c-nohelper "$CV1" "$TMP/cbundle-src" "$CREL1" c_missing_helper)"; expect_reject "components bundle without the helper" "neither the legacy shape" "$R" "$TMP/inst-c1.sh" "$x"
x="$(custom_bundle c-link "$CV1" "$TMP/cbundle-src" "$CREL1" c_helper_link)";     expect_reject "components bundle whose helper is a symlink" "regular files" "$R" "$TMP/inst-c1.sh" "$x"
x="$(custom_bundle c-hard "$CV1" "$TMP/cbundle-src" "$CREL1" c_helper_hardlink)"; expect_reject "components bundle whose helper is a hard link" "regular files" "$R" "$TMP/inst-c1.sh" "$x"
x="$(custom_bundle c-text "$CV1" "$TMP/cbundle-src" "$CREL1" c_helper_text)";     expect_reject "components bundle with a helper that is not an ELF file" "oarfish-update-helper binary in the bundle is not a 64-bit ARM" "$R" "$TMP/inst-c1.sh" "$x"
x="$(custom_bundle c-x86 "$CV1" "$TMP/cbundle-src" "$CREL1" c_helper_x86)";       expect_reject "components bundle with an x86-64 helper" "oarfish-update-helper binary in the bundle is not a 64-bit ARM" "$R" "$TMP/inst-c1.sh" "$x"
x="$(custom_bundle c-32 "$CV1" "$TMP/cbundle-src" "$CREL1" c_helper_32bit)";      expect_reject "components bundle with a 32-bit ELF helper" "oarfish-update-helper binary in the bundle is not a 64-bit ARM" "$R" "$TMP/inst-c1.sh" "$x"
x="$(custom_bundle c-legacy-manifest "$CV1" "$TMP/cbundle-src" "$TMP/rel/lcv1" nothing)"; expect_reject "components bundle with a manifest without a components object" "no components object" "$R" "$TMP/inst-c1.sh" "$x"
x="$(custom_bundle l-comp-manifest "$CV1" "$TMP/bundle-src" "$CREL1" nothing)";   expect_reject "legacy bundle with a manifest that declares components" "does not carry them" "$R" "$TMP/inst-c1.sh" "$x"
x="$(custom_bundle l-missing "$CV1" "$TMP/bundle-src" "$TMP/rel/lcv1" l_missing)"; expect_reject "legacy bundle with a missing entry" "neither the legacy shape" "$R" "$TMP/inst-c1.sh" "$x"
x="$(custom_bundle l-helper "$CV1" "$TMP/bundle-src" "$TMP/rel/lcv1" l_helper_only)"; expect_reject "legacy bundle plus only the helper (six entries)" "neither the legacy shape" "$R" "$TMP/inst-c1.sh" "$x"
x="$(custom_bundle l-extra "$CV1" "$TMP/bundle-src" "$TMP/rel/lcv1" c_extra)";    expect_reject "legacy bundle with an extra entry" "unexpected entry" "$R" "$TMP/inst-c1.sh" "$x"
x="$(custom_bundle l-link "$CV1" "$TMP/bundle-src" "$TMP/rel/lcv1" c_helper_link)"; expect_reject "legacy bundle with a symlink named like the helper" "regular files" "$R" "$TMP/inst-c1.sh" "$x"
untouched "$R"
no_leftovers "$R" || fail "leftovers after the rejected bundles"
pass "none of the rejected bundles of either shape changed the root (no lock directory, no layout, no user)"

echo "== components shape: fresh installation, layout, modes, call order"
R="$(new_root)"; CALLS="$(state_of "$R")/calls.log"
expect_ok "fresh installation of a components release" "$R" "$TMP/inst-c1.sh" "$CREL1"
out_has "components shape" || fail "the output does not name the bundle shape"
link_is "$R/opt/oarfish/current" "releases/$CV1" || fail "current"
link_is "$R/opt/oarfish/components/active" "$CV1" || fail "components/active"
link_is "$R/opt/oarfish/components/known-good" "$CV1" || fail "components/known-good"
pass "components/active and components/known-good are relative links to <id> (first installation: both the new id)"
link_is "$R/usr/libexec/oarfish-update-helper" "/opt/oarfish/components/active/oarfish-update-helper" || fail "libexec link"
link_is "$R/etc/systemd/system/oarfish.service" "/opt/oarfish/current/oarfish.service" || fail "oarfish.service link"
link_is "$R/etc/systemd/system/oarfish-update-helper.socket" "/opt/oarfish/components/active/oarfish-update-helper.socket" || fail "socket link"
link_is "$R/etc/systemd/system/oarfish-update-helper.service" "/opt/oarfish/components/active/oarfish-update-helper.service" || fail "helper service link"
link_is "$R/etc/systemd/system/oarfish-update-recover.service" "/opt/oarfish/components/known-good/oarfish-update-recover.service" || fail "recover link"
pass "the helper link and the four unit links have the exact targets of ADR 0010 U5"
link_is "$R/etc/systemd/system/multi-user.target.wants/oarfish.service" "../oarfish.service" || fail "wants oarfish"
link_is "$R/etc/systemd/system/sockets.target.wants/oarfish-update-helper.socket" "../oarfish-update-helper.socket" || fail "wants socket"
link_is "$R/etc/systemd/system/multi-user.target.wants/oarfish-update-recover.service" "../oarfish-update-recover.service" || fail "wants recover"
pass "every wants link is relative and points to the unit in /etc/systemd/system (A5)"
for l in usr/libexec/oarfish-update-helper etc/systemd/system/oarfish.service etc/systemd/system/oarfish-update-helper.socket etc/systemd/system/oarfish-update-helper.service \
  etc/systemd/system/oarfish-update-recover.service etc/systemd/system/multi-user.target.wants/oarfish.service etc/systemd/system/sockets.target.wants/oarfish-update-helper.socket \
  etc/systemd/system/multi-user.target.wants/oarfish-update-recover.service; do
  resolve "$R" "$R/$l" >/dev/null || fail "$l does not resolve"
done
pass "every link resolves to a file"
cmp -s "$(resolve "$R" "$R/etc/systemd/system/oarfish-update-recover.service")" "$ROOT/install/oarfish-update-recover.service" || fail "the recover unit is not the repository's"
cmp -s "$(resolve "$R" "$R/etc/systemd/system/multi-user.target.wants/oarfish-update-recover.service")" "$ROOT/install/oarfish-update-recover.service" || fail "the recover wants link does not reach the unit"
pass "the units reached through the links are the bytes of the bundle"
mode_is "$R/opt/oarfish/components" rwxr-xr-x && mode_is "$R/opt/oarfish/components/$CV1" rwxr-xr-x || fail "component directory modes"
mode_is "$R/opt/oarfish/components/$CV1/oarfish-update-helper" rwxr-xr-x || fail "helper mode"
for u in $UNITS3; do mode_is "$R/opt/oarfish/components/$CV1/$u" rw-r--r-- || fail "$u mode"; done
mode_is "$R/usr/libexec" rwxr-xr-x || fail "/usr/libexec mode"
mode_is "$R/var/lib/oarfish-update" rwx------ || fail "state directory mode"
pass "modes: components 0755, helper 0755, units 0644, /usr/libexec 0755, /var/lib/oarfish-update 0700"
assert "the helper in components/<id> equals the bundle's helper" cmp -s "$R/opt/oarfish/components/$CV1/oarfish-update-helper" "$TMP/fake-helper"
assert "an extra component directory was not created" test "$(find "$R/opt/oarfish/components" -mindepth 1 -maxdepth 1 | wc -l | tr -d ' ')" = 3
assert "no staging or temp leftovers" no_leftovers "$R"
[ "$(calls_of "$R")" = "daemon-reload;stop oarfish-update-helper.service;restart oarfish-update-helper.socket;restart oarfish.service;" ] \
  || fail "unexpected systemctl calls on a fresh installation: $(calls_of "$R")"
pass "systemctl calls (fresh): daemon-reload; stop helper service; restart helper socket; restart oarfish.service"
grep -qE '^(enable|disable|is-enabled)' "$CALLS" && fail "systemctl enable was used"
pass "no enable call: the wants links enable the units"
grep -qE 'oarfish-update-recover' "$CALLS" && fail "the recover unit was started"
pass "the recover unit is enabled by its wants link and was not started"
echo "-- layout after a fresh installation (as printed by the test) --"; print_layout "$R"

echo "== components shape: identical re-run changes nothing"
snapshot "$R" > "$TMP/snap1"; : > "$CALLS"
expect_ok "re-run of the same components release" "$R" "$TMP/inst-c1.sh" "$CREL1"
snapshot "$R" > "$TMP/snap2"
cmp -s "$TMP/snap1" "$TMP/snap2" || { diff "$TMP/snap1" "$TMP/snap2" || true; fail "the re-run changed the installation"; }
pass "re-run changed nothing (files, modes, inodes, link targets)"
grep -qE '^(restart|start|stop|enable|disable|daemon-reload)' "$CALLS" && fail "the re-run issued: $(calls_of "$R")"
pass "re-run restarted, stopped, enabled and reloaded nothing (calls: $(calls_of "$R"))"
out_has "nothing to restart" || fail "re-run output"

echo "== components shape: a helper stopped from the stub makes a second run restart the socket"
rm -f "$(state_of "$R")/active.oarfish-update-helper.socket"; : > "$CALLS"
expect_ok "re-run with the helper socket not active" "$R" "$TMP/inst-c1.sh" "$CREL1"
[ "$(calls_of "$R")" = "is-active --quiet oarfish-update-helper.socket;stop oarfish-update-helper.service;restart oarfish-update-helper.socket;is-active --quiet oarfish.service;" ] \
  || fail "unexpected calls for an inactive socket: $(calls_of "$R")"
pass "an inactive socket is started again (stop service, restart socket), oarfish.service untouched: $(calls_of "$R")"

echo "== components shape: upgrade with identical helper bytes creates no new component set"
: > "$CALLS"
expect_ok "upgrade c1 -> c2 (same helper and units)" "$R" "$TMP/inst-c2.sh" "$CREL2"
link_is "$R/opt/oarfish/current" "releases/$CV2" || fail "current"
link_is "$R/opt/oarfish/components/active" "$CV1" && link_is "$R/opt/oarfish/components/known-good" "$CV1" || fail "active must stay"
assert "no components/$CV2 directory" test ! -e "$R/opt/oarfish/components/$CV2"
[ "$(calls_of "$R")" = "is-active --quiet oarfish-update-helper.socket;restart oarfish.service;" ] || fail "calls: $(calls_of "$R")"
pass "identical helper bytes: active stays on $CV1, only oarfish.service restarts (calls: $(calls_of "$R"))"

echo "== components shape: upgrade with different helper bytes, health passes"
ST="$(state_of "$R")"; : > "$CALLS"; : > "$ST/active.oarfish-update-helper.service"
INST_HEALTHCHECK='' INST_ENV="$(HEALTH_ENV "$R")" expect_ok "upgrade c2 -> c3 (new helper bytes), the helper service is running" "$R" "$TMP/inst-c3.sh" "$CREL3"
assert "a new component set components/$CV3 exists" test -x "$R/opt/oarfish/components/$CV3/oarfish-update-helper"
link_is "$R/opt/oarfish/components/active" "$CV3" && link_is "$R/opt/oarfish/components/known-good" "$CV3" || fail "active and known-good after a healthy upgrade"
assert "the old component set is kept" test -x "$R/opt/oarfish/components/$CV1/oarfish-update-helper"
[ "$(calls_of "$R")" = "daemon-reload;stop oarfish-update-helper.service;restart oarfish-update-helper.socket;restart oarfish.service;is-active --quiet oarfish.service;is-active --quiet oarfish-update-helper.socket;" ] \
  || fail "unexpected calls for an upgrade with new helper bytes: $(calls_of "$R")"
pass "systemctl calls (upgrade, new helper bytes): daemon-reload; stop service; restart socket; restart oarfish; health needs oarfish.service and the socket active"
pass "the socket restart was accepted because the stub's helper service had been stopped first (the stub refuses otherwise, as systemd does, A9)"
echo "-- layout after the upgrade --"; print_layout "$R"

echo "== components shape: after old release and component directories are removed, the links still resolve (A5, fake-root half)"
rm -rf "$R/opt/oarfish/releases/$CV1" "$R/opt/oarfish/releases/$CV2" "$R/opt/oarfish/components/$CV1"
for l in etc/systemd/system/oarfish.service etc/systemd/system/oarfish-update-helper.socket etc/systemd/system/oarfish-update-helper.service etc/systemd/system/oarfish-update-recover.service \
  etc/systemd/system/multi-user.target.wants/oarfish.service etc/systemd/system/sockets.target.wants/oarfish-update-helper.socket etc/systemd/system/multi-user.target.wants/oarfish-update-recover.service usr/libexec/oarfish-update-helper; do
  resolve "$R" "$R/$l" >/dev/null || fail "$l no longer resolves after the old directories were removed"
done
pass "wants links and unit links resolve with only the active release and set present (the on-device half is the owner stop)"

echo "== components shape: upgrade whose health check fails is rolled back, known-good stays"
R="$(new_root)"; ST="$(state_of "$R")"; CALLS="$ST/calls.log"
INST_HEALTHCHECK='' INST_ENV="$(HEALTH_ENV "$R")" expect_ok "install c1 with the health check on" "$R" "$TMP/inst-c1.sh" "$CREL1"
echo "$CV3" > "$ST/unhealthy"; : > "$CALLS"
INST_HEALTHCHECK='' INST_ENV="$(HEALTH_ENV "$R")" expect_reject "c3 fails its health check" "previous version $CV1 is active again and healthy" "$R" "$TMP/inst-c3.sh" "$CREL3"
link_is "$R/opt/oarfish/current" "releases/$CV1" || fail "current must be restored"
link_is "$R/opt/oarfish/components/active" "$CV1" || fail "components/active must be restored"
link_is "$R/opt/oarfish/components/known-good" "$CV1" || fail "known-good must not have moved"
pass "rollback: current and components/active are back on $CV1, known-good never moved"
assert "the layout created in this run stays (components/$CV3 kept)" test -x "$R/opt/oarfish/components/$CV3/oarfish-update-helper"
[ "$(calls_of "$R")" = "daemon-reload;stop oarfish-update-helper.service;restart oarfish-update-helper.socket;restart oarfish.service;daemon-reload;stop oarfish-update-helper.service;restart oarfish-update-helper.socket;reset-failed oarfish.service;restart oarfish.service;is-active --quiet oarfish.service;is-active --quiet oarfish-update-helper.socket;" ] \
  || fail "unexpected calls for the rollback: $(calls_of "$R")"
pass "systemctl calls (rollback): daemon-reload; stop service; restart socket; reset-failed; restart oarfish; health on the restored release"

echo "== components shape: first installation whose health check fails"
R="$(new_root)"; ST="$(state_of "$R")"; echo "$CV1" > "$ST/unhealthy"
INST_HEALTHCHECK='' INST_ENV="$(HEALTH_ENV "$R")" expect_reject "first components installation never becomes healthy" "stopped and disabled" "$R" "$TMP/inst-c1.sh" "$CREL1"
for c in "disable --now oarfish.service" "disable --now oarfish-update-helper.socket" "disable oarfish-update-recover.service"; do
  grep -qx "$c" "$ST/calls.log" || fail "missing call: $c (calls: $(tr '\n' ';' < "$ST/calls.log"))"
done
pass "oarfish.service, the helper socket and the recover unit were disabled"
assert "the files and links are kept" test -x "$R/opt/oarfish/releases/$CV1/oarfish" -a -L "$R/opt/oarfish/components/active" -a -L "$R/etc/systemd/system/oarfish.service"
assert "the lock is free after a failed run" lock_is_free "$R/var/lib/oarfish-update"

echo "== components shape: migration from an alpha-style installation"
R="$(new_root)"; ST="$(state_of "$R")"
expect_ok "alpha-style install (legacy bundle)" "$R" "$TMP/inst-v1.sh" "$REL1"
assert "the unit is a regular file" test -f "$R/etc/systemd/system/oarfish.service" -a ! -L "$R/etc/systemd/system/oarfish.service"
mkdir -p "$R/etc/systemd/system/oarfish.service.d" "$R/etc/systemd/system/multi-user.target.wants"
printf '[Service]\nEnvironment=KEEP=me\n' > "$R/etc/systemd/system/oarfish.service.d/override.conf"
ln -s /etc/systemd/system/oarfish.service "$R/etc/systemd/system/multi-user.target.wants/oarfish.service"
dropin_sha="$(sha256_of "$R/etc/systemd/system/oarfish.service.d/override.conf")"; : > "$ST/calls.log"
expect_ok "upgrade the alpha-style installation with a components release" "$R" "$TMP/inst-c1.sh" "$CREL1"
link_is "$R/etc/systemd/system/oarfish.service" "/opt/oarfish/current/oarfish.service" || fail "the unit must now be a link"
[ "$(sha256_of "$R/etc/systemd/system/oarfish.service.d/override.conf")" = "$dropin_sha" ] || fail "the drop-in changed"
pass "the regular unit file became a link; the drop-in is untouched"
link_is "$R/etc/systemd/system/multi-user.target.wants/oarfish.service" "../oarfish.service" || fail "the old absolute wants link must be replaced by the relative one"
link_is "$R/opt/oarfish/current" "releases/$CV1" && link_is "$R/opt/oarfish/components/active" "$CV1" && link_is "$R/etc/systemd/system/sockets.target.wants/oarfish-update-helper.socket" "../oarfish-update-helper.socket" || fail "layout incomplete"
pass "layout complete, wants links relative, current switched"
assert "the previous release is kept" test -x "$R/opt/oarfish/releases/$V1/oarfish"
out_has "replacing the regular file" || fail "the output does not say that the file is replaced"
grep -qE '^(enable|is-enabled)' "$ST/calls.log" && fail "systemctl enable was used in a migration"

echo "== components shape: migration without a version change"
R="$(new_root)"; ST="$(state_of "$R")"
make_crelease "$TMP/rel/cv1" "$V1" a; make_installer "$V1" "$TMP/inst-cv1.sh"
expect_ok "legacy install of $V1" "$R" "$TMP/inst-v1.sh" "$REL1"; : > "$ST/calls.log"
expect_ok "components release with the same version" "$R" "$TMP/inst-cv1.sh" "$TMP/rel/cv1"
link_is "$R/opt/oarfish/components/active" "$V1" && link_is "$R/etc/systemd/system/oarfish.service" "/opt/oarfish/current/oarfish.service" || fail "layout not created"
[ "$(calls_of "$R")" = "daemon-reload;stop oarfish-update-helper.service;restart oarfish-update-helper.socket;is-active --quiet oarfish.service;" ] || fail "calls: $(calls_of "$R")"
pass "same version: the layout is created, oarfish.service is not restarted (calls: $(calls_of "$R"))"

echo "== components shape: the socket text changed, the helper did not"
R="$(new_root)"; ST="$(state_of "$R")"
expect_ok "install c1" "$R" "$TMP/inst-c1.sh" "$CREL1"; : > "$ST/calls.log"
expect_ok "upgrade to a release whose socket unit text differs" "$R" "$TMP/inst-c3.sh" "$CREL3S"
link_is "$R/opt/oarfish/components/active" "$CV3" || fail "a changed socket unit is a new component set"
grep -qx "restart oarfish-update-helper.socket" "$ST/calls.log" && grep -qx "stop oarfish-update-helper.service" "$ST/calls.log" || fail "the socket must be restarted: $(calls_of "$R")"
pass "a changed socket text alone makes the installer stop the helper service and restart the socket"

echo "== components shape: refusals before anything is changed"
R="$(new_root)"
expect_ok "install c1" "$R" "$TMP/inst-c1.sh" "$CREL1"
snapshot "$R" > "$TMP/roots/$(basename "$R").snap"
rm "$R/etc/systemd/system/oarfish-update-recover.service"; echo foreign > "$R/etc/systemd/system/oarfish-update-recover.service"
snapshot "$R" > "$TMP/roots/$(basename "$R").snap"
expect_reject "a foreign regular file at a helper unit path" "foreign file" "$R" "$TMP/inst-c3.sh" "$CREL3"; untouched "$R"
rm "$R/etc/systemd/system/oarfish-update-recover.service"; ln -s /opt/elsewhere/recover.service "$R/etc/systemd/system/oarfish-update-recover.service"
snapshot "$R" > "$TMP/roots/$(basename "$R").snap"
expect_reject "a helper unit link to something else" "not to /opt/oarfish/components/known-good" "$R" "$TMP/inst-c3.sh" "$CREL3"; untouched "$R"
rm "$R/etc/systemd/system/oarfish-update-recover.service"; ln -s /opt/oarfish/components/known-good/oarfish-update-recover.service "$R/etc/systemd/system/oarfish-update-recover.service"
rm "$R/etc/systemd/system/oarfish.service"; ln -s /somewhere/else.service "$R/etc/systemd/system/oarfish.service"
snapshot "$R" > "$TMP/roots/$(basename "$R").snap"
expect_reject "oarfish.service is a link to something else" "not to /opt/oarfish/current/oarfish.service" "$R" "$TMP/inst-c3.sh" "$CREL3"; untouched "$R"
rm "$R/etc/systemd/system/oarfish.service"; ln -s /opt/oarfish/current/oarfish.service "$R/etc/systemd/system/oarfish.service"
rm "$R/usr/libexec/oarfish-update-helper"; echo foreign > "$R/usr/libexec/oarfish-update-helper"
snapshot "$R" > "$TMP/roots/$(basename "$R").snap"
expect_reject "a foreign file at /usr/libexec/oarfish-update-helper" "foreign file" "$R" "$TMP/inst-c3.sh" "$CREL3"; untouched "$R"
rm "$R/usr/libexec/oarfish-update-helper"; ln -s /opt/oarfish/components/active/oarfish-update-helper "$R/usr/libexec/oarfish-update-helper"
rm "$R/etc/systemd/system/multi-user.target.wants/oarfish.service"; echo foreign > "$R/etc/systemd/system/multi-user.target.wants/oarfish.service"
snapshot "$R" > "$TMP/roots/$(basename "$R").snap"
expect_reject "a regular file where a wants link belongs" "not a symbolic link" "$R" "$TMP/inst-c3.sh" "$CREL3"; untouched "$R"
rm "$R/etc/systemd/system/multi-user.target.wants/oarfish.service"; ln -s ../oarfish.service "$R/etc/systemd/system/multi-user.target.wants/oarfish.service"
mkdir "$R/opt/oarfish/components/$CV3"; echo different > "$R/opt/oarfish/components/$CV3/oarfish-update-helper"
snapshot "$R" > "$TMP/roots/$(basename "$R").snap"
expect_reject "components/<version> exists with other bytes (immutable)" "already exists and differs" "$R" "$TMP/inst-c3.sh" "$CREL3"; untouched "$R"
rmdir_c3() { rm -rf "$R/opt/oarfish/components/$CV3"; }; rmdir_c3
rm "$R/opt/oarfish/components/active"; ln -s ../../elsewhere "$R/opt/oarfish/components/active"
snapshot "$R" > "$TMP/roots/$(basename "$R").snap"
expect_reject "components/active points to something that is not a component set" "not a component directory" "$R" "$TMP/inst-c3.sh" "$CREL3"; untouched "$R"

echo "== components shape: the state directory"
R="$(new_root)"; mkdir -m 0755 "$R/var" "$R/var/lib" 2>/dev/null || true; mkdir "$R/var/lib/oarfish-update"; chmod 0755 "$R/var/lib/oarfish-update"
snapshot "$R" > "$TMP/roots/$(basename "$R").snap"
expect_reject "a state directory with mode 0755 is refused" "must be owned by root:root with mode 0700" "$R" "$TMP/inst-c1.sh" "$CREL1"; untouched "$R"
chmod 0700 "$R/var/lib/oarfish-update"; snapshot "$R" > "$TMP/roots/$(basename "$R").snap"
INST_ENV="OARFISH_TEST_ROOT_UID=$(( $(id -u) + 1 ))" expect_reject "a state directory owned by someone else is refused" "must be owned by root:root" "$R" "$TMP/inst-c1.sh" "$CREL1"; untouched "$R"
INST_ENV="OARFISH_TEST_ROOT_GID=$(( $(id -g) + 1 ))" expect_reject "a state directory with another group is refused" "must be owned by root:root" "$R" "$TMP/inst-c1.sh" "$CREL1"; untouched "$R"
rmdir "$R/var/lib/oarfish-update"; ln -s /tmp "$R/var/lib/oarfish-update"; snapshot "$R" > "$TMP/roots/$(basename "$R").snap"
expect_reject "a symbolic link as state directory is refused" "is not a directory" "$R" "$TMP/inst-c1.sh" "$CREL1"; untouched "$R"
pass "the state directory is refused for wrong mode, owner, group and type, and nothing is changed"
R="$(new_root)"
expect_ok "an existing state directory with 0700 is accepted" "$R" "$TMP/inst-c1.sh" "$CREL1"
snapshot "$R" > "$TMP/roots/$(basename "$R").snap"
expect_ok "a second run accepts the directory it created" "$R" "$TMP/inst-c1.sh" "$CREL1"

echo "== components shape: the lock"
R="$(new_root)"; mkdir -p "$R/var/lib"; mkdir -m 0700 "$R/var/lib/oarfish-update"; snapshot "$R" > "$TMP/roots/$(basename "$R").snap"
hold_lock "$R/var/lib/oarfish-update" 30
t0=$(date +%s)
INST_ENV="OARFISH_LOCK_WAIT=2" expect_reject "another process holds the lock" "in progress" "$R" "$TMP/inst-c1.sh" "$CREL1"
t1=$(date +%s)
[ $((t1 - t0)) -ge 1 ] && [ $((t1 - t0)) -le 15 ] || fail "the installer waited $((t1 - t0)) s with a wait of 2 s"
out_has "Nothing was changed" || fail "the message does not say that nothing was changed"
untouched "$R"
pass "a held lock stops the installer after the shortened wait ($((t1 - t0)) s), nothing changed"
INST_ENV="OARFISH_LOCK_WAIT=1" expect_reject "the lock also blocks --uninstall" "in progress" "$R" "$TMP/inst-c1.sh" "$CREL1" --uninstall
release_lock
assert "the lock is free once the holder ended" lock_is_free "$R/var/lib/oarfish-update"
expect_ok "the same run succeeds once the lock is free" "$R" "$TMP/inst-c1.sh" "$CREL1"
assert "the lock is free after a successful run" lock_is_free "$R/var/lib/oarfish-update"

echo "== components shape: a second installer while the first runs"
R="$(new_root)"; ST="$(state_of "$R")"; : > "$ST/block-daemon-reload"; rm -f "$ST/blocked" "$ST/proceed" "$TMP/bg.rc"
( INST_OUT="$TMP/out-bg" run_inst "$R" "$TMP/inst-c1.sh" "$CREL1"; echo "$RC" > "$TMP/bg.rc" ) &
BGPID=$!
for _ in $(seq 1 100); do [ -f "$ST/blocked" ] && break; sleep 0.1; done
[ -f "$ST/blocked" ] || fail "the first installer never reached daemon-reload"
snapshot "$R" > "$TMP/snap-first-running"
INST_ENV="OARFISH_LOCK_WAIT=2" expect_reject "a second installer while the first runs" "in progress" "$R" "$TMP/inst-c1.sh" "$CREL1"
cmp -s "$TMP/snap-first-running" <(snapshot "$R") || fail "the second installer changed the root"
pass "the second installer failed with the same message and changed nothing"
: > "$ST/proceed"; wait "$BGPID" || true
[ "$(cat "$TMP/bg.rc")" = 0 ] || { cat "$TMP/out-bg"; fail "the first installer failed"; }
pass "the first installer finished normally"
assert "the lock is free after both runs" lock_is_free "$R/var/lib/oarfish-update"
rm -f "$ST/block-daemon-reload"

echo "== components shape: flock is required"
R="$(new_root)"
INST_PATH="$(tool_path flock)" expect_reject "missing flock refused" "required tools are missing: flock" "$R" "$TMP/inst-c1.sh" "$CREL1"; untouched "$R"

echo "== legacy shape and the helper layout"
R="$(new_root)"
expect_ok "legacy install on a clean device" "$R" "$TMP/inst-v1.sh" "$REL1"
assert "a legacy installation takes no lock directory and creates none" test ! -e "$R/var/lib/oarfish-update" -a ! -e "$R/opt/oarfish/components"
assert "a legacy installation creates no helper link" test ! -e "$R/usr/libexec/oarfish-update-helper" -a ! -L "$R/etc/systemd/system/oarfish-update-helper.socket"
R="$(new_root)"
expect_ok "components install" "$R" "$TMP/inst-c1.sh" "$CREL1"
snapshot "$R" > "$TMP/roots/$(basename "$R").snap"
expect_reject "a legacy release on a device with the helper layout" "not supported" "$R" "$TMP/inst-v2.sh" "$REL2"
out_has "--uninstall" || fail "the refusal does not name --uninstall"
untouched "$R"
R="$(new_root)"; mkdir -p "$R/var/lib"; mkdir -m 0700 "$R/var/lib/oarfish-update"; snapshot "$R" > "$TMP/roots/$(basename "$R").snap"
expect_ok "a legacy release with only the state directory left over" "$R" "$TMP/inst-v1.sh" "$REL1"
hold_lock "$R/var/lib/oarfish-update" 30
INST_ENV="OARFISH_LOCK_WAIT=1" expect_reject "a legacy release waits for the lock when the directory exists" "in progress" "$R" "$TMP/inst-v2.sh" "$REL2"
release_lock

echo "== components shape: the source of the lock timeout is the test mode only"
awk '/^if \[ "\$TESTMODE" = 1 \]; then$/ { in_t = 1 } in_t && /OARFISH_LOCK_WAIT/ { ok = 1 } /^fi$/ { in_t = 0 } /OARFISH_LOCK_WAIT/ && !in_t && !/^#/ { bad = 1 } END { exit !(ok && !bad) }' "$ROOT/install/install.sh" \
  || fail "OARFISH_LOCK_WAIT must be read only inside the test-mode block"
pass "OARFISH_LOCK_WAIT is read only in test mode"

echo "== components shape: ownership of the new paths"
R="$(new_root)"; ST="$(state_of "$R")"
INST_ENV="OARFISH_TEST_USERS=1" expect_ok "components install acting as root with stub user tools" "$R" "$TMP/inst-c1.sh" "$CREL1"
for pth in "/var/lib/oarfish-update" "/opt/oarfish/components" "/usr/libexec"; do
  grep -q "^root:root $R$pth\$" "$ST/chown.log" || fail "chown root:root was not applied to $pth (chown.log: $(tr '\n' ';' < "$ST/chown.log"))"
done
# The component files are owned while they are in the staging directory, before the rename.
for f in "" /oarfish-update-helper /oarfish-update-helper.socket /oarfish-update-helper.service /oarfish-update-recover.service; do
  grep -q "^root:root .*/components/\.stage\.[^/]*/set$f\$" "$ST/chown.log" || fail "chown root:root was not applied to the component set path '...set$f' (chown.log: $(tr '\n' ';' < "$ST/chown.log"))"
done
pass "chown root:root was applied to the state directory, /usr/libexec, the component directory and every component file"

echo "== components shape: dry run and uninstall"
R="$(new_root)"
expect_ok "dry run names the lock, the layout and the shape check" "$R" "$TMP/prod/install.sh" "$TMP/rel/empty" --dry-run
for sx in "components" "legacy" "$R/var/lib/oarfish-update" "exclusive lock" "wants links" "known-good" "after the download" "Dry run: nothing was downloaded or changed"; do
  out_has "$sx" || fail "the dry run output lacks '$sx'"
done
untouched "$R"; pass "the plan lists the new steps and the dry run changed nothing"
R="$(new_root)"; ST="$(state_of "$R")"
expect_ok "install c1" "$R" "$TMP/inst-c1.sh" "$CREL1"
echo '{"keep":"me"}' > "$R/etc/oarfish/config.json"; echo state > "$R/var/lib/oarfish-update/state.json"
snapshot "$R" > "$TMP/snap-installed"
expect_ok "uninstall dry run" "$R" "$TMP/inst-c1.sh" "$CREL1" --uninstall --dry-run
cmp -s <(snapshot "$R") "$TMP/snap-installed" || fail "uninstall dry run changed something"
out_has "$R/var/lib/oarfish-update" || fail "the uninstall plan does not name the lock directory"
pass "uninstall --dry-run lists the helper layout and changes nothing"
# a foreign regular file at a unit path and a foreign link are left alone and reported
rm "$R/etc/systemd/system/oarfish-update-helper.service"; echo foreign > "$R/etc/systemd/system/oarfish-update-helper.service"
rm "$R/usr/libexec/oarfish-update-helper"; ln -s /usr/bin/env "$R/usr/libexec/oarfish-update-helper"
mkdir -p "$R/etc/systemd/system/oarfish.service.d"; echo '[Service]' > "$R/etc/systemd/system/oarfish.service.d/o.conf"
: > "$ST/calls.log"
expect_ok "uninstall of the components layout" "$R" "$TMP/inst-c1.sh" "$CREL1" --uninstall
out_has "left $R/etc/systemd/system/oarfish-update-helper.service alone" || fail "the foreign file is not reported"
out_has "left $R/usr/libexec/oarfish-update-helper alone" || fail "the foreign link is not reported"
assert "the foreign regular file is still there" test -f "$R/etc/systemd/system/oarfish-update-helper.service" -a "$(cat "$R/etc/systemd/system/oarfish-update-helper.service")" = foreign
assert "the foreign link is still there" link_is "$R/usr/libexec/oarfish-update-helper" /usr/bin/env
assert "the drop-in directory is untouched" test -f "$R/etc/systemd/system/oarfish.service.d/o.conf"
for l in etc/systemd/system/oarfish.service etc/systemd/system/oarfish-update-helper.socket etc/systemd/system/oarfish-update-recover.service \
  etc/systemd/system/multi-user.target.wants/oarfish.service etc/systemd/system/sockets.target.wants/oarfish-update-helper.socket etc/systemd/system/multi-user.target.wants/oarfish-update-recover.service; do
  [ ! -e "$R/$l" ] && [ ! -L "$R/$l" ] || fail "$l was not removed"
done
pass "the unit links and the three wants links into /opt/oarfish were removed"
assert "/opt/oarfish and the cache are gone" test ! -e "$R/opt/oarfish" -a ! -e "$R/var/cache/oarfish"
assert "the state directory and configuration are kept without --purge" test -f "$R/var/lib/oarfish-update/state.json" -a -f "$R/etc/oarfish/config.json"
out_has "kept the update state and lock directory" || fail "the message does not say that the state directory is kept"
for c in "stop oarfish.service" "stop oarfish-update-helper.socket" "stop oarfish-update-helper.service" "stop oarfish-update-recover.service" "disable oarfish-update-helper.socket" "disable oarfish-update-recover.service"; do
  grep -qx "$c" "$ST/calls.log" || fail "missing call: $c (calls: $(tr '\n' ';' < "$ST/calls.log"))"
done
grep -qx daemon-reload "$ST/calls.log" || fail "no daemon-reload after removing the links"
pass "uninstall stopped and disabled all four units and reloaded systemd"
assert "the lock is free after the uninstall" lock_is_free "$R/var/lib/oarfish-update"
expect_ok "uninstall --purge" "$R" "$TMP/inst-c1.sh" "$CREL1" --uninstall --purge
assert "purge removed the update state directory" test ! -e "$R/var/lib/oarfish-update" -a ! -e "$R/etc/oarfish" -a ! -e "$R/var/lib/oarfish"
echo "-- layout after the uninstall and purge (what remains of the helper layout) --"; print_layout "$R"

echo "== components shape: a clean full cycle leaves nothing behind"
R="$(new_root)"
expect_ok "install" "$R" "$TMP/inst-c1.sh" "$CREL1"
expect_ok "uninstall --purge" "$R" "$TMP/inst-c1.sh" "$CREL1" --uninstall --purge
left="$(find "$R" \( -type f -o -type l \) ! -path "$R/etc/os-release")"
[ -z "$left" ] || fail "files or links remain after purge: $left"
pass "no installed file or link remains after install and purge of the components layout"

echo "== components shape: the installed link layout passes systemd-analyze verify"
R="$(new_root)"
expect_ok "install c1 for the unit verification" "$R" "$TMP/inst-c1.sh" "$CREL1"
if command -v systemd-analyze >/dev/null 2>&1; then
  "$ROOT/scripts/verify-units.sh" --installed "$R" || fail "systemd-analyze verify rejected the installed layout"
  pass "systemd-analyze verify accepts oarfish.service and the three helper units as installed links"
else
  echo "SKIP  systemd-analyze is not available on this host; CI runs it (scripts/verify-units.sh --installed)"
fi

echo "== real mode (no OARFISH_TEST_ROOT) refuses a non-root user before doing anything"
# Not in test mode, so it would touch the real system if it got past the root check;
# the root check comes first, and the suite itself refuses to run as root.
set +e
env -i PATH="$TMP/shim:$PATH" "$SH" "$TMP/prod/install.sh" > "$TMP/out" 2>&1; RC=$?
set -e
if [ "$RC" = 0 ] || ! grep -q "must run as root" "$TMP/out"; then fail "non-root real run: exit $RC"; fi
pass "non-root real run refused: must run as root"
set +e
env -i PATH="$TMP/shim:$PATH" FAKE_UNAME_M=x86_64 "$SH" "$TMP/prod/install.sh" --dry-run > "$TMP/out" 2>&1; RC=$?
set -e
if [ "$RC" = 0 ] || ! grep -q "unsupported CPU architecture" "$TMP/out"; then fail "real-mode dry run on a wrong architecture: exit $RC"; fi
pass "real-mode dry run needs no root and refuses a wrong architecture"

echo "ALL $N INSTALLER CHECKS PASSED"
