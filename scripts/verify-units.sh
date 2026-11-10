#!/usr/bin/env bash
# systemd-analyze verify of the three update-helper units, and, informationally,
# systemd-analyze security --offline for the helper and recover units.
#
# The units name binaries that exist only on a device. The check therefore runs
# against a temporary root that holds those paths (a copy of /bin/true under each
# name), so that it does not fail on a missing binary and the units stay as they
# are. Needs systemd-analyze (CI runs it on ubuntu-24.04).
#
# Usage: scripts/verify-units.sh [UNIT_DIR]    default: install/
#        scripts/verify-units.sh --installed FAKE_ROOT
#   The exit status is that of `verify`. `security` never fails this script and
#   no threshold applies.
#   --installed FAKE_ROOT  verify the units as the installer links them (G6.3b): FAKE_ROOT
#                          is a test root after a components-shape install. It is copied
#                          (links stay links), the runner's base units are added, and
#                          oarfish.service and the three helper units are verified by their
#                          paths in /etc/systemd/system, which are links into /opt/oarfish.
#                          systemd-analyze opens ROOT/path on the host's file system, so an
#                          absolute link target such as /opt/oarfish/current/oarfish.service
#                          would be looked up on the host: in the copy every absolute link is
#                          rewritten to the equivalent relative link (lexically, links are not
#                          expanded), which resolves the same way inside the root.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
ROOT="$(repo_root)"
INSTALLED=""
if [ "${1:-}" = "--installed" ]; then
  INSTALLED="${2:?usage: $0 --installed FAKE_ROOT}"
  [ -L "$INSTALLED/etc/systemd/system/oarfish-update-helper.socket" ] || die "$INSTALLED holds no installed helper layout"
  DIR="$ROOT/install"
else
  DIR="${1:-$ROOT/install}"
fi
command -v systemd-analyze >/dev/null || die "systemd-analyze not found"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
R="$TMP/root"
UNITDIR=/etc/systemd/system
if [ -n "$INSTALLED" ]; then
  cp -R "$INSTALLED" "$R"
  chmod -R u+w "$R"
  while IFS= read -r l; do
    t="$(readlink "$l")"
    case "$t" in
      /*) ln -sfn "$(realpath -m -s --relative-to="$(dirname "$l")" "$R$t")" "$l" ;;
    esac
  done < <(find "$R" -type l)
else
  mkdir -p "$R$UNITDIR" "$R/usr/libexec" "$R/opt/oarfish/components/known-good" "$R/opt/oarfish/current"
  for p in usr/libexec/oarfish-update-helper opt/oarfish/components/known-good/oarfish-update-helper opt/oarfish/current/oarfish; do
    cp /bin/true "$R/$p"
    chmod 0755 "$R/$p"
  done
fi
# Inside a root, systemd finds its base units (sysinit.target, sockets.target, ...)
# only under that root, so the runner's own system units are copied in.
SYSTEM_UNITS=/usr/lib/systemd/system
[ -d "$SYSTEM_UNITS" ] || SYSTEM_UNITS=/lib/systemd/system
[ -d "$SYSTEM_UNITS" ] || die "no system unit directory found"
mkdir -p "$R/usr/lib/systemd"
cp -R "$SYSTEM_UNITS" "$R/usr/lib/systemd/system"
UNITS="oarfish-update-helper.socket oarfish-update-helper.service oarfish-update-recover.service"
if [ -z "$INSTALLED" ]; then
  for u in $UNITS; do cp "$DIR/$u" "$R$UNITDIR/$u"; done
  # The recover unit is ordered before oarfish.service; give systemd the unit it names.
  cp "$ROOT/install/oarfish.service" "$R$UNITDIR/oarfish.service"
fi

systemd-analyze --version | head -1
args=()
for u in $UNITS; do args+=("$UNITDIR/$u"); done
if [ -n "$INSTALLED" ]; then
  # In the installed layout every unit path is a link into /opt/oarfish.
  args+=("$UNITDIR/oarfish.service")
  for u in $UNITS oarfish.service; do [ -L "$R$UNITDIR/$u" ] || die "$UNITDIR/$u is not a link in $INSTALLED"; done
fi
systemd-analyze verify --root="$R" "${args[@]}"
if [ -n "$INSTALLED" ]; then
  echo "systemd-analyze verify: oarfish.service and the three update-helper units are accepted as installed links"
  exit 0
fi
echo "systemd-analyze verify: the three update-helper units are accepted"

# The boot transaction. `verify` of single units cannot see an ordering cycle that
# runs through basic.target, so this verifies multi-user.target with the units
# enabled the way the installer enables them (wants links of the recover unit and
# of the helper socket). systemd breaks a cycle by deleting a job and only warns,
# so the output is searched as well as the exit status (ADR 0010 A14).
mkdir -p "$R$UNITDIR/multi-user.target.wants" "$R$UNITDIR/sockets.target.wants"
ln -s ../oarfish-update-recover.service "$R$UNITDIR/multi-user.target.wants/oarfish-update-recover.service"
ln -s ../oarfish.service "$R$UNITDIR/multi-user.target.wants/oarfish.service"
ln -s ../oarfish-update-helper.socket "$R$UNITDIR/sockets.target.wants/oarfish-update-helper.socket"
boot_status=0
systemd-analyze verify --root="$R" multi-user.target >"$TMP/boot.out" 2>&1 || boot_status=$?
cat "$TMP/boot.out"
if grep -qiE 'ordering cycle|deleted to break' "$TMP/boot.out"; then
  die "the boot transaction of multi-user.target has an ordering cycle (see above)"
fi
[ "$boot_status" -eq 0 ] || die "systemd-analyze verify multi-user.target failed with status $boot_status"
echo "systemd-analyze verify: the boot transaction of multi-user.target has no ordering cycle"

# Informational. The units are not changed to improve a score.
report="$TMP/security.md"
{
  echo "### systemd-analyze security --offline=true, update helper units (informational, no threshold)"
  for u in oarfish-update-helper.service oarfish-update-recover.service; do
    echo "#### $u"
    echo '```'
    status=0
    systemd-analyze security --offline=true --no-pager --root="$R" "$UNITDIR/$u" 2>&1 || status=$?
    echo "(exit status $status)"
    echo '```'
  done
} > "$report"
cat "$report"
[ -z "${GITHUB_STEP_SUMMARY:-}" ] || cat "$report" >> "$GITHUB_STEP_SUMMARY"
