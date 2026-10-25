#!/bin/sh
# Runs inside an arm64 container (qemu-user on the CI host). Mounted at /work:
#   /work/release/     a signed throwaway release with the real arm64 binary
#   /work/install.sh   test copy of the installer trusting the throwaway key
#   /work/run.sh, /work/systemctl-stub.sh
# Lines starting with REPORT are collected into the job summary.
set -eu

fail() { echo "CONTAINER TEST FAILED: $*" >&2; exit 1; }
ok() { echo "ok: $*"; }

# shellcheck disable=SC1091
echo "REPORT image: $(. /etc/os-release && echo "$PRETTY_NAME"), $(uname -m), user $(id -un)"
echo "REPORT tools present in the minimal image before anything is installed:"
missing=''
for t in curl openssl tar gzip sha256sum od base64 awk setpriv useradd getent; do
  if command -v "$t" >/dev/null 2>&1; then echo "REPORT   $t: present"; else echo "REPORT   $t: MISSING"; missing="$missing $t"; fi
done
if command -v openssl >/dev/null 2>&1; then echo "REPORT openssl version: $(openssl version)"; fi

# Install only what the image lacks, and say so.
pkgs=''
for t in $missing; do
  case $t in curl) pkgs="$pkgs curl" ;; openssl) pkgs="$pkgs openssl" ;; *) fail "unexpected missing base tool: $t" ;; esac
done
if [ -n "$pkgs" ]; then
  echo "REPORT installing with apt-get because the image lacks them:$pkgs"
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq >/dev/null
  # shellcheck disable=SC2086
  apt-get install -y -qq --no-install-recommends $pkgs >/dev/null
  echo "REPORT openssl version after install: $(openssl version)"
fi

R=/tmp/oarfish-root
mkdir -p "$R/run/systemd/system" "$R/etc"
cp /etc/os-release "$R/etc/os-release"
export OARFISH_TEST_ROOT="$R" OARFISH_SYSTEMCTL=/work/systemctl-stub.sh FAKE_ROOT="$R"
export OARFISH_RELEASE_BASE_URL=file:///work/release OARFISH_HEALTH_TIMEOUT=120

echo "== install"
sh /work/install.sh > /tmp/install.out 2>&1 || { cat /tmp/install.out; fail "installer exited non-zero"; }
cat /tmp/install.out
VER="$(sed -n 's/^OARFISH_STAMPED_VERSION=.\(.*\).$/\1/p' /work/install.sh)"
grep -q "Oarfish $VER is installed and running" /tmp/install.out || fail "no success line"
ok "installer succeeded, /healthz answered through the installer's own check"

echo "== checks"
"$R/opt/oarfish/current/oarfish" -version | grep -q "oarfish $VER " || fail "oarfish -version does not report $VER"
ok "installed binary runs and reports $VER"
code="$(curl -s -o /tmp/healthz.json -w '%{http_code}' http://127.0.0.1:8484/healthz)"
if [ "$code" != 200 ] || ! grep -q '"status":"ok"' /tmp/healthz.json; then fail "/healthz answered $code"; fi
ok "GET /healthz -> 200 $(cat /tmp/healthz.json)"
code="$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8484/)"
[ "$code" = 200 ] || fail "/ answered $code"
ok "GET / -> 200 (embedded frontend)"

id oarfish >/dev/null || fail "user oarfish was not created"
shell="$(getent passwd oarfish | cut -d: -f7)"
case $shell in */nologin|*/false) ok "user oarfish exists with shell $shell" ;; *) fail "user shell is $shell" ;; esac
[ "$(id -u oarfish)" -lt 1000 ] || fail "oarfish is not a system user (uid $(id -u oarfish))"
ok "oarfish is a system user (uid $(id -u oarfish))"

own() { stat -c '%U:%G %a' "$1"; }
[ "$(own "$R/etc/oarfish")" = "root:oarfish 750" ] || fail "/etc/oarfish is $(own "$R/etc/oarfish")"
[ "$(own "$R/etc/oarfish/secrets")" = "oarfish:oarfish 700" ] || fail "secrets is $(own "$R/etc/oarfish/secrets")"
[ "$(own "$R/var/lib/oarfish")" = "oarfish:oarfish 750" ] || fail "/var/lib/oarfish is $(own "$R/var/lib/oarfish")"
[ "$(own "$R/var/cache/oarfish")" = "oarfish:oarfish 750" ] || fail "/var/cache/oarfish is $(own "$R/var/cache/oarfish")"
[ "$(own "$R/opt/oarfish/releases/$VER")" = "root:root 755" ] || fail "release dir is $(own "$R/opt/oarfish/releases/$VER")"
[ "$(own "$R/opt/oarfish/releases/$VER/oarfish")" = "root:root 755" ] || fail "binary is $(own "$R/opt/oarfish/releases/$VER/oarfish")"
ok "ownership and modes of directories and binary"

pid="$(cat /tmp/fake-systemd/pid)"
[ "$(awk '/^Uid:/ {print $2}' "/proc/$pid/status")" = "$(id -u oarfish)" ] || fail "process $pid does not run as oarfish"
ok "the process runs as the oarfish user (not root)"

# 127.0.0.1:8484 is 0100007F:2124 in /proc/net/tcp; state 0A = LISTEN.
awk '$2 == "0100007F:2124" && $4 == "0A" { f = 1 } END { exit !f }' /proc/net/tcp || fail "nothing listens on 127.0.0.1:8484"
if awk '$4 == "0A" && $2 ~ /^0+:2124$/ { f = 1 } END { exit !f }' /proc/net/tcp /proc/net/tcp6 2>/dev/null; then fail "port 8484 is open on all interfaces"; fi
ok "listening on 127.0.0.1:8484 only"

echo "== re-run"
sh /work/install.sh > /tmp/install2.out 2>&1 || { cat /tmp/install2.out; fail "second run failed"; }
grep -q "nothing to restart" /tmp/install2.out || { cat /tmp/install2.out; fail "second run restarted the service"; }
[ "$(cat /tmp/fake-systemd/pid)" = "$pid" ] || fail "the process was replaced by the second run"
ok "second run changed nothing and did not restart the process"

echo "== uninstall"
sh /work/install.sh --uninstall > /tmp/un.out 2>&1 || { cat /tmp/un.out; fail "uninstall failed"; }
if [ -e "$R/opt/oarfish" ] || [ -e "$R/etc/systemd/system/oarfish.service" ]; then fail "uninstall left files"; fi
if [ ! -d "$R/etc/oarfish" ] || [ ! -d "$R/var/lib/oarfish" ]; then fail "uninstall removed user data"; fi
id oarfish >/dev/null 2>&1 || fail "uninstall removed the user without --purge"
ok "uninstall kept data and user"
sh /work/install.sh --uninstall --purge > /tmp/purge.out 2>&1 || { cat /tmp/purge.out; fail "purge failed"; }
if [ -e "$R/etc/oarfish" ] || [ -e "$R/var/lib/oarfish" ]; then fail "purge left data"; fi
if id oarfish >/dev/null 2>&1; then fail "purge left the user"; fi
ok "purge removed data and user"
echo "CONTAINER TEST PASSED"
