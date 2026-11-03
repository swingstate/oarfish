#!/bin/sh
# Runs inside an arm64 container (qemu-user on the CI host). Mounted at /work:
#   /work/release/     a signed throwaway release with the real arm64 binary
#   /work/install.sh   test copy of the installer trusting the throwaway key
#   /work/run.sh, /work/systemctl-stub.sh
# Optional (present when the host script could build one): /work/legacy-release and
# /work/legacy-install.sh, a legacy-shape release of the same commit and its test
# installer. It is installed first, so that the main release is an upgrade of an
# alpha-style installation (a migration to the update helper layout, G6.3b).
# Lines starting with REPORT are collected into the job summary.
set -eu

fail() { echo "CONTAINER TEST FAILED: $*" >&2; exit 1; }
ok() { echo "ok: $*"; }

# shellcheck disable=SC1091
echo "REPORT image: $(. /etc/os-release && echo "$PRETTY_NAME"), $(uname -m), user $(id -un)"
echo "REPORT tools present in the minimal image before anything is installed:"
missing=''
for t in curl openssl tar gzip sha256sum od base64 awk setpriv useradd getent flock; do
  if command -v "$t" >/dev/null 2>&1; then echo "REPORT   $t: present"; else echo "REPORT   $t: MISSING"; missing="$missing $t"; fi
done
if command -v openssl >/dev/null 2>&1; then echo "REPORT openssl version: $(openssl version)"; fi

# Install only what the image lacks, and say so.
pkgs=''
for t in $missing; do
  case $t in curl) pkgs="$pkgs curl" ;; openssl) pkgs="$pkgs openssl" ;; flock) pkgs="$pkgs util-linux" ;; *) fail "unexpected missing base tool: $t" ;; esac
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

SHAPE=legacy
if grep -q '^  "components": {$' /work/release/manifest.json; then SHAPE=components; fi
echo "REPORT release shape: $SHAPE"
HAVE_LEGACY=0
if [ -f /work/legacy-install.sh ]; then
  HAVE_LEGACY=1
  echo "== legacy installation first (alpha-style), then the main release over it"
  OARFISH_RELEASE_BASE_URL=file:///work/legacy-release sh /work/legacy-install.sh > /tmp/legacy.out 2>&1 || { cat /tmp/legacy.out; fail "the legacy installation failed"; }
  if [ ! -f "$R/etc/systemd/system/oarfish.service" ] || [ -L "$R/etc/systemd/system/oarfish.service" ]; then fail "the legacy unit is not a regular file"; fi
  if [ -e "$R/opt/oarfish/components" ] || [ -e "$R/var/lib/oarfish-update" ] || [ -L "$R/etc/systemd/system/oarfish-update-helper.socket" ]; then fail "a legacy installation created the update helper layout"; fi
  ok "a legacy-shape release installs as before: a regular unit file, no helper layout, no lock directory"
  mkdir -p "$R/etc/systemd/system/oarfish.service.d" "$R/etc/systemd/system/multi-user.target.wants"
  printf '[Service]\nEnvironment=KEEP=me\n' > "$R/etc/systemd/system/oarfish.service.d/override.conf"
  ln -s /etc/systemd/system/oarfish.service "$R/etc/systemd/system/multi-user.target.wants/oarfish.service"
  dropin_sum="$(sha256sum "$R/etc/systemd/system/oarfish.service.d/override.conf")"
  LEGACY_VER="$(sed -n 's/^OARFISH_STAMPED_VERSION=.\(.*\).$/\1/p' /work/legacy-install.sh)"
  [ "$(readlink "$R/opt/oarfish/current")" = "releases/$LEGACY_VER" ] || fail "the legacy release is not current"
fi

echo "== install"
sh /work/install.sh > /tmp/install.out 2>&1 || { cat /tmp/install.out; fail "installer exited non-zero"; }
cat /tmp/install.out
VER="$(sed -n 's/^OARFISH_STAMPED_VERSION=.\(.*\).$/\1/p' /work/install.sh)"
grep -q "Oarfish $VER is installed and running" /tmp/install.out || fail "no success line"
ok "installer succeeded, /healthz answered through the installer's own check"

echo "== checks"
"$R/opt/oarfish/current/oarfish" -version | grep -q "oarfish $VER " || fail "oarfish -version does not report $VER"
ok "installed binary runs and reports $VER"

# The service was started with the arguments of the unit's ExecStart (see the stub);
# the address to check comes from the same line, not from a copy of it here.
UNIT="$R/etc/systemd/system/oarfish.service"
# In a components installation the unit is a link whose absolute target resolves only below $R: read it through current.
if [ -L "$UNIT" ]; then UNIT="$R/opt/oarfish/current/oarfish.service"; fi
cmp -s "$UNIT" "$R/opt/oarfish/releases/$VER/oarfish.service" || fail "installed unit differs from the release's unit"
grep -q '^ExecStart=/opt/oarfish/current/oarfish ' "$UNIT" || fail "unexpected ExecStart in the unit"
listen="$(sed -n 's/^ExecStart=.* -listen \([^ ]*\).*$/\1/p' "$UNIT")"
[ -n "$listen" ] || fail "the unit's ExecStart has no -listen argument"
[ "$listen" = "$(sed -n "s/^LISTEN_ADDR='\(.*\)'\$/\1/p" /work/install.sh)" ] || fail "unit -listen $listen differs from the installer's LISTEN_ADDR"
grep -q "^ExecStart arguments: .*-listen $listen" /tmp/fake-systemd/calls.log || fail "the process was not started with the unit's arguments"
ok "the process was started with the ExecStart arguments of the installed unit (-listen $listen)"
code="$(curl -s -o /tmp/healthz.json -w '%{http_code}' "http://$listen/healthz")"
if [ "$code" != 200 ] || ! grep -q '"status":"ok"' /tmp/healthz.json; then fail "/healthz answered $code"; fi
ok "GET /healthz -> 200 $(cat /tmp/healthz.json)"
code="$(curl -s -o /dev/null -w '%{http_code}' "http://$listen/")"
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
for f in oarfish.service LICENSE THIRD_PARTY_NOTICES.txt; do
  [ "$(own "$R/opt/oarfish/releases/$VER/$f")" = "root:root 644" ] || fail "$f is $(own "$R/opt/oarfish/releases/$VER/$f")"
done
ok "ownership and modes of directories, binary and the other release files"

# Access control (ADR 0009): the fresh installation is in setup mode, its
# secrets are the service user's alone, and the API gives nothing without an account.
[ "$(own "$R/var/lib/oarfish/setup-token")" = "oarfish:oarfish 600" ] || fail "setup-token is $(own "$R/var/lib/oarfish/setup-token")"
[ "$(own "$R/var/lib/oarfish/auth")" = "oarfish:oarfish 700" ] || fail "auth/ is $(own "$R/var/lib/oarfish/auth")"
[ "$(own "$R/var/lib/oarfish/audit")" = "oarfish:oarfish 700" ] || fail "audit/ is $(own "$R/var/lib/oarfish/audit")"
[ ! -e "$R/var/lib/oarfish/auth/account.json" ] || fail "an account exists after a fresh installation"
ok "setup token 0600, auth/ and audit/ 0700, all owned by oarfish; no account yet"
code="$(curl -s -o /tmp/device.json -w '%{http_code}' "http://$listen/api/v1/device")"
if [ "$code" != 401 ] || ! grep -q '"setup_required"' /tmp/device.json; then fail "/api/v1/device answered $code without an account"; fi
ok "GET /api/v1/device -> 401 setup_required"
if grep -qF "$(cat "$R/var/lib/oarfish/setup-token")" /tmp/fake-systemd/oarfish.log; then fail "the setup token is in the service log"; fi
grep -q 'sudo cat /var/lib/oarfish/setup-token' /tmp/install.out || fail "the installer does not name the setup-token command"
ok "the setup token is not in the service log; the installer names the command to read it"

pid="$(cat /tmp/fake-systemd/pid)"
[ "$(awk '/^Uid:/ {print $2}' "/proc/$pid/status")" = "$(id -u oarfish)" ] || fail "process $pid does not run as oarfish"
ok "the process runs as the oarfish user (not root)"

# /proc/net/tcp lists addresses as little-endian hex; state 0A = LISTEN. Only loopback is acceptable.
host="${listen%:*}"
port="${listen##*:}"
[ "$host" = 127.0.0.1 ] || fail "the unit does not listen on loopback: $listen"
hexport="$(printf '%04X' "$port")"
awk -v want="0100007F:$hexport" '$2 == want && $4 == "0A" { f = 1 } END { exit !f }' /proc/net/tcp || fail "nothing listens on $listen"
if awk -v p=":$hexport" '$4 == "0A" && $2 ~ /^0+:/ && index($2, p) { f = 1 } END { exit !f }' /proc/net/tcp /proc/net/tcp6 2>/dev/null; then fail "port $port is open on all interfaces"; fi
ok "listening on $listen only"

if [ "$SHAPE" = components ]; then
  echo "== update helper layout"
  chk_link() { { [ -L "$1" ] && [ "$(readlink "$1")" = "$2" ]; } || fail "$1 is not a link to $2 (it is: $(readlink "$1" 2>/dev/null || echo 'not a link'))"; }
  chk_link "$R/opt/oarfish/components/active" "$VER"
  chk_link "$R/opt/oarfish/components/known-good" "$VER"
  chk_link "$R/usr/libexec/oarfish-update-helper" /opt/oarfish/components/active/oarfish-update-helper
  chk_link "$R/etc/systemd/system/oarfish.service" /opt/oarfish/current/oarfish.service
  chk_link "$R/etc/systemd/system/oarfish-update-helper.socket" /opt/oarfish/components/active/oarfish-update-helper.socket
  chk_link "$R/etc/systemd/system/oarfish-update-helper.service" /opt/oarfish/components/active/oarfish-update-helper.service
  chk_link "$R/etc/systemd/system/oarfish-update-recover.service" /opt/oarfish/components/known-good/oarfish-update-recover.service
  chk_link "$R/etc/systemd/system/multi-user.target.wants/oarfish.service" ../oarfish.service
  chk_link "$R/etc/systemd/system/sockets.target.wants/oarfish-update-helper.socket" ../oarfish-update-helper.socket
  chk_link "$R/etc/systemd/system/multi-user.target.wants/oarfish-update-recover.service" ../oarfish-update-recover.service
  ok "the helper link, the four unit links and the three relative wants links have the exact targets"
  cmp -s "$R/opt/oarfish/current/oarfish.service" "$R/opt/oarfish/releases/$VER/oarfish.service" || fail "oarfish.service reached through current differs from the release's"
  cmp -s "$R/opt/oarfish/components/known-good/oarfish-update-recover.service" "$R/opt/oarfish/components/$VER/oarfish-update-recover.service" || fail "known-good does not reach the set"
  [ "$(own "$R/opt/oarfish/components")" = "root:root 755" ] || fail "components is $(own "$R/opt/oarfish/components")"
  [ "$(own "$R/opt/oarfish/components/$VER")" = "root:root 755" ] || fail "components/$VER is $(own "$R/opt/oarfish/components/$VER")"
  [ "$(own "$R/opt/oarfish/components/$VER/oarfish-update-helper")" = "root:root 755" ] || fail "the helper is $(own "$R/opt/oarfish/components/$VER/oarfish-update-helper")"
  for f in oarfish-update-helper.socket oarfish-update-helper.service oarfish-update-recover.service; do
    [ "$(own "$R/opt/oarfish/components/$VER/$f")" = "root:root 644" ] || fail "$f is $(own "$R/opt/oarfish/components/$VER/$f")"
  done
  [ "$(own "$R/var/lib/oarfish-update")" = "root:root 700" ] || fail "/var/lib/oarfish-update is $(own "$R/var/lib/oarfish-update")"
  [ "$(own "$R/usr/libexec")" = "root:root 755" ] || fail "/usr/libexec is $(own "$R/usr/libexec")"
  ok "ownership and modes of the component set, the helper, the units, /usr/libexec and /var/lib/oarfish-update"
  # The helper stub is a real arm64 program here (the whole container is emulated).
  H="$R/opt/oarfish/components/active/oarfish-update-helper"
  rc=0; "$H" recover >/tmp/h1.out 2>&1 || rc=$?
  if [ "$rc" != 0 ] || [ -s /tmp/h1.out ]; then fail "helper recover exited $rc: $(cat /tmp/h1.out)"; fi
  rc=0; "$H" serve >/tmp/h2.out 2>&1 </dev/null || rc=$?
  [ "$rc" = 1 ] || fail "helper serve without socket activation exited $rc, expected 1"
  ok "the installed helper stub runs: recover exits 0, serve without socket activation exits 1"
  flock -x -n "$R/var/lib/oarfish-update" true || fail "the lock on /var/lib/oarfish-update is still held after the installer ended"
  ok "the lock is free after the run (real flock on the directory)"
  [ -f /tmp/fake-systemd/active.oarfish-update-helper.socket ] || fail "the helper socket was not started"
  if grep -qE '^(enable|is-enabled) ' /tmp/fake-systemd/calls.log && [ "$HAVE_LEGACY" = 0 ]; then fail "systemctl enable was used"; fi
  if grep -qE '^(start|restart) oarfish-update-recover' /tmp/fake-systemd/calls.log; then fail "the recover unit was started"; fi
  ok "the helper socket was started, the recover unit was not"
  if [ "$HAVE_LEGACY" = 1 ]; then
    [ -L "$R/etc/systemd/system/oarfish.service" ] || fail "the alpha-style unit file was not replaced by a link"
    [ "$(sha256sum "$R/etc/systemd/system/oarfish.service.d/override.conf")" = "$dropin_sum" ] || fail "the drop-in changed"
    [ "$(readlink "$R/opt/oarfish/current")" = "releases/$VER" ] || fail "current was not switched"
    [ -x "$R/opt/oarfish/releases/$LEGACY_VER/oarfish" ] || fail "the previous release was not kept"
    ok "migration from the alpha-style installation: the unit became a link, the drop-in is unchanged, current switched, the old release is kept"
  fi
fi

echo "== re-run"
CALLS_BEFORE="$(wc -l < /tmp/fake-systemd/calls.log)"
sh /work/install.sh > /tmp/install2.out 2>&1 || { cat /tmp/install2.out; fail "second run failed"; }
grep -q "nothing to restart" /tmp/install2.out || { cat /tmp/install2.out; fail "second run restarted the service"; }
[ "$(cat /tmp/fake-systemd/pid)" = "$pid" ] || fail "the process was replaced by the second run"
ok "second run changed nothing and did not restart the process"
if [ "$SHAPE" = components ] && tail -n +"$((CALLS_BEFORE + 1))" /tmp/fake-systemd/calls.log | grep -qE '^(restart|start|stop|enable|disable|daemon-reload)'; then
  fail "the second run issued: $(tail -n +"$((CALLS_BEFORE + 1))" /tmp/fake-systemd/calls.log | tr '\n' ';')"
fi
ok "the second run restarted, stopped and reloaded nothing"

echo "== uninstall"
sh /work/install.sh --uninstall > /tmp/un.out 2>&1 || { cat /tmp/un.out; fail "uninstall failed"; }
if [ -e "$R/opt/oarfish" ] || [ -e "$R/etc/systemd/system/oarfish.service" ] || [ -L "$R/etc/systemd/system/oarfish.service" ]; then fail "uninstall left files"; fi
if [ "$SHAPE" = components ]; then
  for l in usr/libexec/oarfish-update-helper etc/systemd/system/oarfish-update-helper.socket etc/systemd/system/oarfish-update-helper.service etc/systemd/system/oarfish-update-recover.service \
    etc/systemd/system/multi-user.target.wants/oarfish.service etc/systemd/system/sockets.target.wants/oarfish-update-helper.socket etc/systemd/system/multi-user.target.wants/oarfish-update-recover.service; do
    if [ -e "$R/$l" ] || [ -L "$R/$l" ]; then fail "uninstall left $l"; fi
  done
  [ -f "$R/etc/systemd/system/oarfish.service.d/override.conf" ] || [ "$HAVE_LEGACY" = 0 ] || fail "uninstall removed the drop-in"
  [ -d "$R/var/lib/oarfish-update" ] || fail "uninstall removed /var/lib/oarfish-update without --purge"
  ok "uninstall removed the helper links and the wants links, kept the drop-in and /var/lib/oarfish-update"
fi
if [ ! -d "$R/etc/oarfish" ] || [ ! -d "$R/var/lib/oarfish" ]; then fail "uninstall removed user data"; fi
id oarfish >/dev/null 2>&1 || fail "uninstall removed the user without --purge"
ok "uninstall kept data and user"
sh /work/install.sh --uninstall --purge > /tmp/purge.out 2>&1 || { cat /tmp/purge.out; fail "purge failed"; }
if [ -e "$R/etc/oarfish" ] || [ -e "$R/var/lib/oarfish" ] || [ -e "$R/var/lib/oarfish-update" ]; then fail "purge left data"; fi
if id oarfish >/dev/null 2>&1; then fail "purge left the user"; fi
ok "purge removed data and user"
echo "CONTAINER TEST PASSED"
