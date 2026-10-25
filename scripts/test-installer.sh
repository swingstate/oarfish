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
# systemctl stand-in: records calls and keeps enabled/active state in files.
cat > "$TMP/shim/systemctl-stub" <<'EOF'
#!/bin/sh
echo "$*" >> "$FAKE_SYSTEMD_STATE/calls.log"
case "$1" in
  is-enabled) [ -f "$FAKE_SYSTEMD_STATE/enabled" ] ;;
  enable)     : > "$FAKE_SYSTEMD_STATE/enabled" ;;
  disable)    rm -f "$FAKE_SYSTEMD_STATE/enabled" ;;
  is-active)  [ -f "$FAKE_SYSTEMD_STATE/active" ] ;;
  start|restart) : > "$FAKE_SYSTEMD_STATE/active" ;;
  stop)       rm -f "$FAKE_SYSTEMD_STATE/active" ;;
  cat)        [ -f "$FAKE_ROOT/etc/systemd/system/oarfish.service" ] ;;
  daemon-reload) ;;
  *) exit 1 ;;
esac
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
    "$SH" "$inst" "$@" > "$TMP/out" 2>&1
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

expect_ok "upgrade to the next version" "$R" "$TMP/inst-v2.sh" "$REL2"
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
  for t in curl tar gzip sha256sum od base64 mktemp awk df id mv ln rm mkdir chmod cat cp sed grep tr head tail wc sleep readlink dirname find; do
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
tamper_case "tampered checksums.txt (a hash changed)" "SIGNATURE VERIFICATION FAILED" sed -i.bak '1s/^./0/;1s/^0/1/' checksums.txt
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

echo "== health check failure"
R="$(new_root)"
INST_HEALTHCHECK='' INST_ENV="OARFISH_HEALTH_URL=http://127.0.0.1:9/healthz OARFISH_HEALTH_TIMEOUT=2" \
  expect_reject "first install that never becomes healthy" "did not become healthy" "$R" "$TMP/inst-v1.sh" "$REL1"
expect_ok "install v1 (health skipped)" "$(new_root)" "$TMP/inst-v1.sh" "$REL1"
R="$(new_root)"; expect_ok "install v1 again" "$R" "$TMP/inst-v1.sh" "$REL1"
INST_HEALTHCHECK='' INST_ENV="OARFISH_HEALTH_URL=http://127.0.0.1:9/healthz OARFISH_HEALTH_TIMEOUT=2" \
  expect_reject "upgrade that never becomes healthy is rolled back" "rolling back" "$R" "$TMP/inst-v2.sh" "$REL2"
assert "current points to the previous release again" test "$(readlink "$R/opt/oarfish/current")" = "releases/$V1"
grep -q "installation of $V2 failed" "$TMP/out" || fail "rollback message"

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

echo "== real mode refuses a non-root user (Linux with systemd only)"
if [ "$(uname -s)" = Linux ] && [ -d /run/systemd/system ]; then
  set +e
  env -i PATH="$PATH" "$SH" "$TMP/prod/install.sh" > "$TMP/out" 2>&1; RC=$?
  set -e
  [ "$RC" != 0 ] && grep -q "must run as root" "$TMP/out" || fail "non-root real run: exit $RC"
  pass "non-root real run refused"
else
  echo "SKIP  real-mode root check needs Linux with systemd; covered by the CI job"
fi

echo "ALL $N INSTALLER CHECKS PASSED"
