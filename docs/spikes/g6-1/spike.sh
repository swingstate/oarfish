#!/bin/sh
# G6.1 device spike driver (ADR 0010). THROWAWAY: not product code, not installed,
# not used by the installer, CI or any release. POSIX sh (dash on Raspberry Pi OS).
#
# Usage, always as root, one block per call (see README.md for the order):
#   sudo sh spike.sh BLOCK [ARGUMENT]
#
# Every block prints a report in a fixed format and writes the same text to
# /var/tmp/oarfish-spike-report-BLOCK.txt. The report is filtered by this script
# (host names, user names, IP addresses, MAC addresses, serial numbers, machine
# and boot ids, URL paths and query strings) as defence in depth; the owner still
# looks at it before pasting.
#
# Everything this script creates on the device carries the prefix oarfish-spike.
# It never touches oarfish.service, /opt/oarfish, /etc/oarfish or /var/lib/oarfish.
# There is no block that runs a command given by the caller.

set -u
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
LC_ALL=C
export LC_ALL
umask 022

KIT=$(cd "$(dirname "$0")" && pwd)
OPT=/opt/oarfish-spike
STATE=/var/lib/oarfish-spike-update
RUNDIR=/run/oarfish-spike-update
UNITDIR=/etc/systemd/system
OUTDIR=$STATE/out
REPORT_DIR=/var/tmp
PROBE=$OPT/probe/oarfish-spike-probe
SNAP=$REPORT_DIR/oarfish-spike-prod-before.snap
SNAPLIST=$REPORT_DIR/oarfish-spike-prod-before.list
SERVICE_USER=oarfish
RELEASE_REPO=swingstate/oarfish
REQS=0
SCRUB_EXTRA=${SCRUB_EXTRA:-}

# ---- report format ---------------------------------------------------------

hdr()  { printf '\n## %s\n' "$*"; }
note() { printf '# %s\n' "$*"; }
say()  { printf '%s\n' "$*"; }
die()  { printf 'spike: error: %s\n' "$*" >&2; exit 1; }

# cmd COMMAND [ARG...]: print the command, its output (prefixed "| ") and its exit code.
cmd() {
  printf '$ %s\n' "$*"
  cmd_out=$("$@" 2>&1)
  cmd_rc=$?
  if [ -n "$cmd_out" ]; then printf '%s\n' "$cmd_out" | sed 's/^/| /'; fi
  printf 'exit=%s\n' "$cmd_rc"
  return 0
}

# cmdsh 'TEXT': the same for a fixed shell text written in this script (pipes).
cmdsh() {
  printf '$ %s\n' "$1"
  cmd_out=$(sh -c "$1" 2>&1)
  cmd_rc=$?
  if [ -n "$cmd_out" ]; then printf '%s\n' "$cmd_out" | sed 's/^/| /'; fi
  printf 'exit=%s\n' "$cmd_rc"
  return 0
}

sprop() { systemctl show -p "$2" --value "$1" 2>/dev/null; } # sprop UNIT PROPERTY

unit_state() { # unit_state UNIT: one line
  printf '%s: ActiveState=%s SubState=%s Result=%s NRestarts=%s MainPID=%s\n' "$1" \
    "$(sprop "$1" ActiveState)" "$(sprop "$1" SubState)" "$(sprop "$1" Result)" \
    "$(sprop "$1" NRestarts)" "$(sprop "$1" MainPID)"
}

wait_inactive() { # wait_inactive UNIT SECONDS: poll until the unit is inactive or failed
  wi=0
  while [ "$wi" -lt "$2" ]; do
    case $(sprop "$1" ActiveState) in inactive | failed) return 0 ;; esac
    sleep 1
    wi=$((wi + 1))
  done
  return 1
}

link_switch() { # link_switch LINK TARGET: temporary link plus rename, never ln -sfn
  rm -f "$1.new.$$"
  ln -s "$2" "$1.new.$$" && mv -T "$1.new.$$" "$1"
}

as_user() { # as_user UID GID NAME COMMAND...
  if command -v setpriv >/dev/null 2>&1; then
    au_u=$1
    au_g=$2
    shift 3
    setpriv --reuid="$au_u" --regid="$au_g" --clear-groups "$@"
  else
    au_n=$3
    shift 3
    runuser -u "$au_n" -- "$@"
  fi
}

# ---- scrubbing -------------------------------------------------------------

esc() { printf '%s' "$1" | sed -e 's/[][\\.*^$/+?(){}|]/\\&/g'; }

add_literal() { # add_literal TEXT PLACEHOLDER: replace a literal that is not part of a longer word
  [ -n "$1" ] || return 0
  al_e=$(esc "$1")
  al_r="s/(^|[^A-Za-z0-9_-])$al_e([^A-Za-z0-9_-]|\$)/\\1$2\\2/g"
  printf '%s\n%s\n' "$al_r" "$al_r" >>"$SCRUB"
}

prepare_scrub() {
  SCRUB=$REPORT_DIR/oarfish-spike-scrub.$$.sed
  (umask 077 && : >"$SCRUB")
  ps_h=$(hostname 2>/dev/null || true)
  add_literal "$ps_h" DEVICE
  add_literal "${ps_h%%.*}" DEVICE
  add_literal "$(uname -n 2>/dev/null || true)" DEVICE
  add_literal "$(head -n 1 /etc/hostname 2>/dev/null || true)" DEVICE
  if [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != root ]; then add_literal "$SUDO_USER" USER; fi
  getent passwd 2>/dev/null | awk -F: '$3 >= 1000 && $3 < 60000 { print $1 }' | while read -r ps_n; do
    add_literal "$ps_n" USER
  done
  add_literal "$(sed -n 's/^Serial[[:space:]]*:[[:space:]]*//p' /proc/cpuinfo 2>/dev/null | head -n 1)" SERIAL
  if [ -r /proc/device-tree/serial-number ]; then add_literal "$(tr -d '\0' </proc/device-tree/serial-number)" SERIAL; fi
  add_literal "$(head -n 1 /etc/machine-id 2>/dev/null || true)" MACHINEID
  add_literal "$(head -n 1 /proc/sys/kernel/random/boot_id 2>/dev/null || true)" BOOTID
  for ps_f in /sys/class/net/*/address; do
    [ -r "$ps_f" ] || continue
    ps_m=$(head -n 1 "$ps_f")
    add_literal "$ps_m" MAC
    add_literal "$(printf '%s' "$ps_m" | tr 'a-f' 'A-F')" MAC
  done
  for ps_x in $SCRUB_EXTRA; do add_literal "$ps_x" ADDRESS; done
  cat >>"$SCRUB" <<'SED'
s#(https?://[^/ ?"'<>]+)[^ "'<>]*#\1#g
s/(^|[^0-9A-Za-z])([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}([^0-9A-Za-z]|$)/\1MAC\3/g
s/(^|[^0-9A-Za-z])([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}([^0-9A-Za-z]|$)/\1MAC\3/g
s/127\.0\.0\.1/@P1@/g
s/127\.0\.0\.0\/8/@P2@/g
s/0\.0\.0\.0\/0/@P3@/g
s/::1\/128/@P4@/g
s#::/0#@P5@#g
s/\[::1\]/@P6@/g
s/(^|[^0-9A-Fa-f:])::1([^0-9A-Fa-f:]|$)/\1@P7@\2/g
s/(^|[^0-9.])([0-9]{1,3}\.){3}[0-9]{1,3}([^0-9]|$)/\1ADDRESS\3/g
s/(^|[^0-9.])([0-9]{1,3}\.){3}[0-9]{1,3}([^0-9]|$)/\1ADDRESS\3/g
s/([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}/ADDRESS/g
s/([0-9A-Fa-f]{1,4}:){1,6}:([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,5})?/ADDRESS/g
s/::[0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,6}/ADDRESS/g
s/@P1@/127.0.0.1/g
s/@P2@/127.0.0.0\/8/g
s/@P3@/0.0.0.0\/0/g
s/@P4@/::1\/128/g
s#@P5@#::/0#g
s/@P6@/[::1]/g
s/@P7@/::1/g
s/[Bb]earer [^ ]+/Bearer REDACTED/g
s/([Tt]oken|[Pp]assword|[Pp]asswd|[Ss]ecret|[Aa]uthorization|[Cc]ookie)(=|: )[^ ]+/\1\2REDACTED/g
SED
}

# run_report NAME FUNCTION [ARG...]: run the block, print and store the filtered report.
run_report() {
  rr_name=$1
  shift
  REPORT=$REPORT_DIR/oarfish-spike-report-$rr_name.txt
  prepare_scrub
  if printf x | sed -u p >/dev/null 2>&1; then
    rr_sed() { sed -u -E -f "$SCRUB"; }
  else
    rr_sed() { sed -E -f "$SCRUB"; }
  fi
  (umask 077 && : >"$REPORT")
  {
    printf '=== OARFISH G6.1 SPIKE REPORT: %s ===\n' "$rr_name"
    printf 'kit_script_sha256=%s\n' "$(sha256sum "$KIT/spike.sh" | awk '{print $1}')"
    printf 'time_utc=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    printf 'evidence=device observation with throwaway units, not the production path\n'
    "$@"
    printf '=== END REPORT: %s ===\n' "$rr_name"
  } 2>&1 | rr_sed | tee "$REPORT"
  rm -f "$SCRUB"
  case $rr_name in
    cleanup) rm -f "$REPORT" ;;
    *) printf 'report file: %s\n' "$REPORT" ;;
  esac
}

# ---- production service snapshot ---------------------------------------------

prod_snapshot() { # prod_snapshot FILE LISTFILE
  {
    printf 'is_active=%s\n' "$(systemctl is-active oarfish.service 2>&1)"
    for ps_p in ActiveState SubState Result UnitFileState FragmentPath MainPID NRestarts ActiveEnterTimestampMonotonic; do
      printf '%s=%s\n' "$ps_p" "$(systemctl show -p "$ps_p" --value oarfish.service 2>&1)"
    done
    printf 'unit_sha256=%s\n' "$(sha256sum /etc/systemd/system/oarfish.service 2>&1 | awk '{print $1}')"
    printf 'current_target=%s\n' "$(readlink /opt/oarfish/current 2>&1)"
    printf 'current_binary_sha256=%s\n' "$(sha256sum /opt/oarfish/current/oarfish 2>&1 | awk '{print $1}')"
    printf 'boot_marker=%s\n' "$(sha256sum /proc/sys/kernel/random/boot_id | cut -c 1-12)"
  } >"$1"
  ls -laR --time-style=+%s /opt/oarfish >"$2" 2>&1
}

prod_compare() { # prod_compare: compare the production service with the preflight snapshot
  [ -f "$SNAP" ] && [ -f "$SNAPLIST" ] || { say "no preflight snapshot found: run the preflight block first"; return 0; }
  pc_a=$REPORT_DIR/oarfish-spike-prod-after.snap
  pc_l=$REPORT_DIR/oarfish-spike-prod-after.list
  prod_snapshot "$pc_a" "$pc_l"
  pc_bad=0
  for pc_k in is_active ActiveState SubState UnitFileState FragmentPath unit_sha256 current_target current_binary_sha256; do
    pc_b=$(grep "^$pc_k=" "$SNAP")
    pc_n=$(grep "^$pc_k=" "$pc_a")
    if [ "$pc_b" = "$pc_n" ]; then say "STABLE   $pc_b (unchanged)"; else say "CHANGED  before: $pc_b  after: $pc_n"; pc_bad=1; fi
  done
  # The entry ".." of the recursive listing is /opt itself: creating the spike directory there changes it.
  grep -v ' \.\.$' "$SNAPLIST" >"$pc_l.b"
  grep -v ' \.\.$' "$pc_l" >"$pc_l.a"
  if diff "$pc_l.b" "$pc_l.a" >/dev/null 2>&1; then
    say "STABLE   listing of /opt/oarfish (unchanged, $(wc -l <"$pc_l.a") lines, the entry for /opt itself left out)"
  else
    say "CHANGED  listing of /opt/oarfish differs:"
    diff "$pc_l.b" "$pc_l.a" | sed 's/^/| /'
    pc_bad=1
  fi
  rm -f "$pc_l.a" "$pc_l.b"
  for pc_k in MainPID NRestarts ActiveEnterTimestampMonotonic Result boot_marker; do
    pc_b=$(grep "^$pc_k=" "$SNAP")
    pc_n=$(grep "^$pc_k=" "$pc_a")
    if [ "$pc_b" = "$pc_n" ]; then say "same     $pc_b"; else say "differs  before: $pc_b  after: $pc_n  (expected after a reboot, not otherwise)"; fi
  done
  if [ "$pc_bad" = 0 ]; then say "PRODUCTION: stable fields UNCHANGED"; else say "PRODUCTION: stable fields CHANGED, see above"; fi
  rm -f "$pc_a" "$pc_l"
}

# ---- kit and units -----------------------------------------------------------

kit_probe_sha() {
  sha256sum "$KIT/build/oarfish-spike-probe" 2>/dev/null | awk '{print $1}'
}

kit_probe_expected() {
  awk '{print $1; exit}' "$KIT/probe.sha256" 2>/dev/null
}

need_installed() {
  [ -x "$PROBE" ] && [ -f "$UNITDIR/oarfish-spike-v1-run.service" ] || die "the kit is not installed: run the install block first"
}

assemble_unit() { # assemble_unit TEMPLATE DEST: replace the two sandbox markers by the fragments
  awk -v hs="$KIT/fragments/helper-sandbox.inc" -v ss="$KIT/fragments/service-sandbox.inc" '
    $0 == "@@HELPER_SANDBOX@@" { while ((getline l < hs) > 0) print l; close(hs); next }
    $0 == "@@SERVICE_SANDBOX@@" { while ((getline l < ss) > 0) print l; close(ss); next }
    { print }' "$1" >"$2"
  chmod 0644 "$2"
}

# ---- blocks ------------------------------------------------------------------

do_kitcheck() {
  hdr "kit files"
  cmd uname -m
  cmdsh "cd '$KIT' && sha256sum spike.sh probe.go probe.sha256 build-probe.sh fragments/helper-sandbox.inc fragments/service-sandbox.inc"
  hdr "probe binary"
  say "expected_sha256=$(kit_probe_expected)"
  say "actual_sha256=$(kit_probe_sha)"
  if [ -n "$(kit_probe_sha)" ] && [ "$(kit_probe_sha)" = "$(kit_probe_expected)" ]; then say "probe_checksum=MATCH"; else say "probe_checksum=MISMATCH or missing"; fi
  if [ -f "$KIT/build/oarfish-spike-probe" ]; then
    [ -x "$KIT/build/oarfish-spike-probe" ] || chmod 0755 "$KIT/build/oarfish-spike-probe"
    cmd "$KIT/build/oarfish-spike-probe" info
  fi
  hdr "tools the scripts use"
  for kc_t in sh sed awk tr head tail grep wc cut diff sha256sum stat ls ln mv cp rm mkdir chmod chown readlink mktemp sleep date id hostname uname df findmnt curl getent setpriv runuser systemctl systemd-analyze journalctl timedatectl hwclock dpkg zcat timeout install; do
    if command -v "$kc_t" >/dev/null 2>&1; then say "have $kc_t"; else say "MISSING $kc_t"; fi
  done
  hdr "scrub self-check (the report filter must replace the made-up values in the next line)"
  kc_v4=$(printf '%s.%s.%s.%s' 192 0 2 7)
  kc_v6=$(printf '%s:%s::%s' 2001 db8 1)
  kc_mac=$(printf '%s:%s:%s:%s:%s:%s' aa bb cc dd ee ff)
  printf 'made-up: %s and %s at %s, kept: 127.0.0.1 ::1 10:15:03, https://example.org/path?sig=abc\n' "$kc_v4" "$kc_v6" "$kc_mac"
}

do_preflight() {
  hdr "device facts"
  cmdsh "tr -d '\\0' </proc/device-tree/model; echo"
  cmdsh "grep -E '^(Model|Revision|Hardware)' /proc/cpuinfo"
  cmdsh "grep -E '^(PRETTY_NAME|ID|VERSION_ID|VERSION_CODENAME|ID_LIKE)=' /etc/os-release"
  cmd uname -srmo
  cmdsh "systemctl --version | head -n 2"
  cmd systemctl is-system-running
  hdr "service user and test users (read-only)"
  cmd id "$SERVICE_USER"
  cmdsh "getent group $SERVICE_USER"
  cmdsh "getent passwd nobody | cut -d: -f1,3,4"
  hdr "file systems"
  cmdsh "findmnt -T /opt -no TARGET,FSTYPE,OPTIONS"
  cmdsh "findmnt -T /usr/libexec -no TARGET,FSTYPE,OPTIONS"
  cmdsh "findmnt -T /etc/systemd/system -no TARGET,FSTYPE,OPTIONS"
  cmdsh "findmnt -T /var/tmp -no TARGET,FSTYPE,OPTIONS"
  cmdsh "df -Pk /opt /var/tmp | awk '{print \$1, \$2, \$3, \$4, \$5, \$6}'"
  hdr "production service (read-only), snapshot written for the postflight comparison"
  prod_snapshot "$SNAP" "$SNAPLIST"
  cat "$SNAP"
  say "listing_of_/opt/oarfish_lines=$(wc -l <"$SNAPLIST")"
  cmdsh "stat -c '%a %U %G %n' /var/lib/oarfish /var/cache/oarfish /run/oarfish /opt/oarfish /etc/oarfish"
  hdr "spike leftovers (none expected before the install block)"
  cmdsh "ls -d /opt/oarfish-spike /var/lib/oarfish-spike-update /run/oarfish-spike-update /usr/libexec/oarfish-spike-* /etc/systemd/system/oarfish-spike-* 2>&1 | head -n 20"
}

do_facts() {
  hdr "V2: systemd and kernel facts the address filter needs"
  cmdsh "systemctl --version"
  cmdsh "systemctl show -p Features -p Virtualization -p Architecture -p ConfidentialVirtualization"
  cmd uname -r
  cmdsh "stat -fc 'cgroup file system type of /sys/fs/cgroup: %T' /sys/fs/cgroup"
  cmdsh "cat /sys/fs/cgroup/cgroup.controllers"
  cmdsh "for f in /proc/config.gz; do [ -r \$f ] && zcat \$f | grep -E '^CONFIG_(CGROUP_BPF|BPF|BPF_SYSCALL|BPF_JIT|CGROUPS|SECCOMP|SECCOMP_FILTER|NET_CLS_ACT|CGROUP_BPF)='; done; for f in /boot/config-\$(uname -r) /boot/firmware/config-\$(uname -r); do [ -r \$f ] && grep -E '^CONFIG_(CGROUP_BPF|BPF|BPF_SYSCALL|BPF_JIT|CGROUPS|SECCOMP|SECCOMP_FILTER)=' \$f; done; true"
  cmdsh "systemd-analyze --version | head -n 1"
  hdr "V4 (read-only part): certificates and name resolution files"
  cmdsh "dpkg -s ca-certificates 2>&1 | grep -E '^(Package|Status|Version):'"
  cmdsh "ls -l /etc/ssl/certs/ca-certificates.crt"
  cmdsh "ls /etc/ssl/certs | wc -l"
  cmdsh "ls -l /etc/resolv.conf /etc/nsswitch.conf"
  cmdsh "grep -c '^nameserver' /etc/resolv.conf"
  hdr "V6: journal and start limits (read-only)"
  cmdsh "ls -ld /var/log/journal /run/log/journal"
  cmdsh "journalctl --disk-usage"
  cmdsh "journalctl --list-boots --no-pager | wc -l"
  cmdsh "systemd-analyze cat-config systemd/journald.conf 2>&1 | grep -E '^(# /|\\[Journal\\]|Storage|SystemMaxUse|RuntimeMaxUse|Compress)'"
  cmdsh "grep -rhE '^[[:space:]]*Storage=' /etc/systemd/journald.conf /etc/systemd/journald.conf.d /usr/lib/systemd/journald.conf.d 2>/dev/null; true"
  cmdsh "systemctl show -p DefaultStartLimitIntervalUSec -p DefaultStartLimitBurst -p DefaultTimeoutStartUSec -p DefaultRestartUSec"
  cmdsh "systemctl show oarfish.service -p Restart -p RestartUSec -p StartLimitIntervalUSec -p StartLimitBurst -p StartLimitAction -p TimeoutStopUSec"
  hdr "V8: clock (read-only)"
  cmdsh "ls -l /dev/rtc* 2>&1; ls /sys/class/rtc 2>&1"
  cmdsh "timedatectl show -p NTP -p NTPSynchronized -p CanNTP -p LocalRTC -p RTCTimeUSec"
  cmdsh "systemctl is-active fake-hwclock.service; systemctl is-enabled fake-hwclock.service"
  cmdsh "systemctl is-active systemd-timesyncd.service; systemctl is-enabled systemd-timesyncd.service"
  cmdsh "f=/etc/fake-hwclock.data; if [ -e \$f ]; then echo \"fake-hwclock data file: age_s=\$(( \$(date +%s) - \$(stat -c %Y \$f) )) size=\$(stat -c %s \$f)\"; else echo 'fake-hwclock data file: absent'; fi"
  cmdsh "hwclock --show 2>&1 | head -n 1"
  cmdsh "date -u '+utc now: %Y-%m-%dT%H:%M:%SZ'"
  cmdsh "cut -d' ' -f1 /proc/uptime | sed 's/^/uptime_s=/'"
}

do_install() {
  [ -x "$KIT/build/oarfish-spike-probe" ] || die "build/oarfish-spike-probe is missing in the kit"
  [ "$(kit_probe_sha)" = "$(kit_probe_expected)" ] || die "the probe checksum does not match probe.sha256: copy the kit again"
  id "$SERVICE_USER" >/dev/null 2>&1 || die "the service user $SERVICE_USER does not exist: this device has no Oarfish installation"
  hdr "directories"
  for i_d in "$OPT" "$OPT/probe" "$OPT/fixtures" "$OPT/fixtures/cache" "$OPT/releases" "$OPT/components" "$OPT/h11" "$OPT/h11/components"; do
    cmd mkdir -p "$i_d"
  done
  cmd mkdir -p "$STATE" "$OUTDIR"
  cmd chmod 0700 "$STATE" "$OUTDIR"
  hdr "probe"
  cmd cp "$KIT/build/oarfish-spike-probe" "$PROBE.new"
  cmd chmod 0755 "$PROBE.new"
  cmd mv -f "$PROBE.new" "$PROBE"
  cmd "$PROBE" info
  hdr "fixtures for the file access check of V1"
  printf 'public\n' >"$OPT/fixtures/cache/pub.txt"
  printf 'private\n' >"$OPT/fixtures/cache/priv.txt"
  chown "$SERVICE_USER:$SERVICE_USER" "$OPT/fixtures/cache/pub.txt" "$OPT/fixtures/cache/priv.txt"
  chmod 0644 "$OPT/fixtures/cache/pub.txt"
  chmod 0600 "$OPT/fixtures/cache/priv.txt"
  cmdsh "ls -ln $OPT/fixtures/cache"
  hdr "unit files (assembled from the templates, sandbox fragments inserted)"
  for i_t in "$KIT"/units/*.service "$KIT"/units/*.socket; do
    [ -f "$i_t" ] || continue
    i_n=$(basename "$i_t")
    assemble_unit "$i_t" "$UNITDIR/$i_n"
    say "installed $UNITDIR/$i_n"
  done
  cmd systemctl daemon-reload
  cmdsh "systemctl list-unit-files 'oarfish-spike-*' --no-pager --no-legend"
  say "nothing is enabled and nothing is started by this block"
}

snap_v1() { # snap_v1 LABEL
  printf '%s target.service:   MainPID=%s ActiveState=%s NeedDaemonReload=%s\n' "$1" "$(sprop oarfish-spike-target.service MainPID)" \
    "$(sprop oarfish-spike-target.service ActiveState)" "$(sprop oarfish-spike-target.service NeedDaemonReload)"
  printf '%s target.service:   Description=%s\n' "$1" "$(sprop oarfish-spike-target.service Description)"
  printf '%s sock.socket:      ActiveState=%s ActiveEnterTimestampMonotonic=%s\n' "$1" "$(sprop oarfish-spike-sock.socket ActiveState)" \
    "$(sprop oarfish-spike-sock.socket ActiveEnterTimestampMonotonic)"
  printf '%s failing.service:  ActiveState=%s Result=%s\n' "$1" "$(sprop oarfish-spike-failing.service ActiveState)" "$(sprop oarfish-spike-failing.service Result)"
}

v1_prepare() {
  systemctl stop oarfish-spike-v1-run.service oarfish-spike-target.service oarfish-spike-sock.socket oarfish-spike-sock.service oarfish-spike-failing.service >/dev/null 2>&1
  systemctl reset-failed 'oarfish-spike-*' >/dev/null 2>&1
  rm -f "$OUTDIR/v1.txt"
  systemctl start oarfish-spike-target.service oarfish-spike-sock.socket >/dev/null 2>&1
  systemctl start oarfish-spike-failing.service >/dev/null 2>&1
  return 0
}

do_v1() {
  need_installed
  hdr "preparation: target service running, socket listening, one unit in the failed state"
  rm -rf "$UNITDIR/oarfish-spike-target.service.d"
  systemctl daemon-reload
  v1_prepare
  rm -rf "$UNITDIR/oarfish-spike-target.service.d"
  mkdir -p "$UNITDIR/oarfish-spike-target.service.d"
  printf '[Unit]\nDescription=Oarfish G6.1 spike: restart target (V1) reload-marker-%s\n' "$$" >"$UNITDIR/oarfish-spike-target.service.d/marker.conf"
  say "a drop-in was written after the unit was loaded, without a daemon-reload: the manager must report NeedDaemonReload=yes until the sandboxed run reloads"
  hdr "state before"
  snap_v1 before
  cmdsh "grep -E '^(User|CapabilityBoundingSet|AmbientCapabilities|ProtectSystem|SystemCallFilter|NoNewPrivileges)=' $UNITDIR/oarfish-spike-v1-run.service"
  hdr "the sandboxed run (uid 0, empty CapabilityBoundingSet, sandbox of ADR 0010 U10)"
  cmd systemctl start oarfish-spike-v1-run.service
  cmdsh "systemctl show oarfish-spike-v1-run.service -p Result -p ExecMainStatus -p ExecMainCode -p CapabilityBoundingSet"
  hdr "what the probe printed (exit code and message of each call)"
  if [ -f "$OUTDIR/v1.txt" ]; then cat "$OUTDIR/v1.txt"; else say "no output file: see the journal lines below"; fi
  cmdsh "journalctl -u oarfish-spike-v1-run.service -b -o cat --no-pager --no-hostname -n 30"
  hdr "state after"
  snap_v1 after
  hdr "evaluation by this script"
  if [ "$(sprop oarfish-spike-target.service MainPID)" != "0" ]; then say "target.service is running after the run"; fi
  say "(compare MainPID, ActiveEnterTimestampMonotonic and the states above with the exit lines of the probe)"
}

do_v1x() {
  need_installed
  note "EXPLORATION, not a recommendation and not adopted: run only if the V1 block failed."
  note "One sandbox line at a time is relaxed in a drop-in; CapabilityBoundingSet is never touched; no capability is added."
  note "Result: the exit codes of the four calls (daemon-reload, restart service, restart socket, reset-failed)."
  for x_item in BASELINE ProtectProc=default ProcSubset=all RestrictAddressFamilies= SystemCallFilter= PrivateDevices=no ProtectKernelTunables=no \
    ProtectControlGroups=no RestrictNamespaces=no NoNewPrivileges=no MemoryDenyWriteExecute=no ProtectSystem=no PrivateTmp=no RestrictSUIDSGID=no \
    LockPersonality=no ProtectClock=no ProtectHostname=no ProtectKernelModules=no ProtectKernelLogs=no TasksMax=infinity MemoryMax=infinity IPAddressDeny=; do
    v1_prepare
    rm -rf "$UNITDIR/oarfish-spike-v1-run.service.d"
    mkdir -p "$UNITDIR/oarfish-spike-v1-run.service.d"
    if [ "$x_item" != BASELINE ]; then printf '[Service]\n%s\n' "$x_item" >"$UNITDIR/oarfish-spike-v1-run.service.d/explore.conf"; fi
    systemctl daemon-reload
    systemctl start oarfish-spike-v1-run.service >/dev/null 2>&1
    x_exits=$(grep '^exit=' "$OUTDIR/v1.txt" 2>/dev/null | sed 's/ elapsed_ms=.*//' | tr '\n' ' ')
    say "relaxed: $x_item -> $x_exits"
  done
  rm -rf "$UNITDIR/oarfish-spike-v1-run.service.d"
  systemctl daemon-reload
  note "drop-ins removed again"
}

do_v2() {
  v2_target=${1:-}
  case $v2_target in ?*:?*) ;; *) die "usage: sh spike.sh v2 ADDRESS:PORT  (an address of another machine that you can reach, not a loopback address)" ;; esac
  need_installed
  hdr "who is who (read-only)"
  cmd id "$SERVICE_USER"
  cmdsh "getent passwd nobody | cut -d: -f1,3,4"
  v2_su=$(id -u "$SERVICE_USER")
  v2_sg=$(id -g "$SERVICE_USER")
  v2_nu=$(id -u nobody)
  v2_ng=$(id -g nobody)
  hdr "socket unit: SocketUser, SocketGroup, SocketMode, DirectoryMode"
  systemctl stop oarfish-spike-v2.socket oarfish-spike-v2.service >/dev/null 2>&1
  rm -f "$OUTDIR/v2.txt"
  cmd systemctl start oarfish-spike-v2.socket
  cmdsh "systemctl show oarfish-spike-v2.socket -p Listen -p SocketUser -p SocketGroup -p SocketMode -p DirectoryMode -p ActiveState"
  cmdsh "stat -c '%A %U %G %n' $RUNDIR $RUNDIR/v2.sock"
  hdr "three clients (the first connection starts the service by socket activation)"
  cmd "$PROBE" v2-connect "$RUNDIR/v2.sock" root
  cmd as_user "$v2_su" "$v2_sg" "$SERVICE_USER" "$PROBE" v2-connect "$RUNDIR/v2.sock" service-user
  cmd as_user "$v2_nu" "$v2_ng" nobody "$PROBE" v2-connect "$RUNDIR/v2.sock" unrelated-user
  sleep 1
  hdr "what the service saw (SO_PEERCRED of every connection that reached it)"
  if [ -f "$OUTDIR/v2.txt" ]; then cat "$OUTDIR/v2.txt"; else say "no output file"; fi
  unit_state oarfish-spike-v2.service
  hdr "IPAddressDeny=any, IPAddressAllow=localhost"
  printf '%s\n' "$v2_target" >"$STATE/ip-target"
  chmod 0600 "$STATE/ip-target"
  rm -f "$OUTDIR/ip.txt"
  cmdsh "systemctl show oarfish-spike-ip.service -p IPAddressDeny -p IPAddressAllow"
  cmd systemctl start oarfish-spike-ip.service
  cmdsh "systemctl show oarfish-spike-ip.service -p Result -p ExecMainStatus -p ExecMainCode"
  hdr "inside the sandbox: loopback must work, ADDRESS must not"
  if [ -f "$OUTDIR/ip.txt" ]; then cat "$OUTDIR/ip.txt"; else say "no output file"; fi
  hdr "control run outside any sandbox: shows that ADDRESS is reachable at all"
  cmd "$PROBE" v2-ip "$v2_target"
  hdr "manager messages about BPF and IP filtering since boot"
  cmdsh "journalctl -b -o cat --no-pager --no-hostname -g 'BPF|bpf|IP firewall|IP address' | tail -n 15"
  rm -f "$STATE/ip-target"
  systemctl stop oarfish-spike-v2.socket oarfish-spike-v2.service >/dev/null 2>&1
  note "target file removed, socket and service stopped"
}

# ---- V3 --------------------------------------------------------------------

GH_CODE=''
gh_get() { # gh_get LABEL URL [CURL ARG...]: anonymous GET, headers to $T3/LABEL.h, body to $T3/LABEL.b, status in GH_CODE
  gg_l=$1
  gg_u=$2
  shift 2
  REQS=$((REQS + 1))
  if [ "$REQS" -gt 20 ]; then GH_CODE='cap'; say "request cap of 20 reached, no further request"; return 1; fi
  GH_CODE=$(curl -q -sS --noproxy '*' --proto '=https' --max-time 30 --connect-timeout 15 -A 'oarfish-spike/g6-1' \
    -H 'Accept: application/vnd.github+json' -H 'X-GitHub-Api-Version: 2022-11-28' \
    -D "$T3/$gg_l.h" -o "$T3/$gg_l.b" -w '%{http_code}' "$@" "$gg_u" 2>"$T3/$gg_l.err") || GH_CODE="curl-failed($?)"
  return 0
}

gh_headers() { # gh_headers LABEL
  tr -d '\r' <"$T3/$1.h" 2>/dev/null | grep -iE '^(HTTP/|etag:|x-ratelimit-|retry-after:|content-type:|cache-control:|last-modified:|x-github-media-type:|link:|date:)' | sed 's/^/| /'
  [ ! -s "$T3/$1.err" ] || sed 's/^/| curl: /' "$T3/$1.err"
}

rl_remaining() { # rl_remaining LABEL: request /rate_limit and print the anonymous core counters
  gh_get "$1" 'https://api.github.com/rate_limit'
  say "request $1: GET /rate_limit -> $GH_CODE"
  rl_core=$(tr -d '\n\r ' <"$T3/$1.b" 2>/dev/null | sed -n 's/.*"core":{\([^}]*\)}.*/\1/p')
  say "core counters: $rl_core"
  RL_LAST=$(printf '%s' "$rl_core" | sed -n 's/.*"remaining":\([0-9]*\).*/\1/p')
}

do_v3() {
  T3=$REPORT_DIR/oarfish-spike-v3-tmp
  rm -rf "$T3"
  mkdir -p "$T3"
  chmod 0700 "$T3"
  REQS=0
  note "anonymous requests only: no credential of any kind is sent. Hard cap of 20 requests in this run."
  note "Redirect targets carry signed query strings: only scheme, host and status of each hop are printed."
  hdr "rate limit before"
  rl_remaining rl0
  rl_before=$RL_LAST
  hdr "release list (per_page=5)"
  gh_get list "https://api.github.com/repos/$RELEASE_REPO/releases?per_page=5"
  say "request list: GET releases?per_page=5 -> $GH_CODE"
  gh_headers list
  say "release entries (tag_name, prerelease, draft), newest first:"
  grep -oE '"(tag_name|prerelease|draft)": *("[^"]*"|true|false)' "$T3/list.b" | sed 's/^/| /'
  v3_etag=$(tr -d '\r' <"$T3/list.h" | sed -n 's/^[Ee][Tt][Aa][Gg]: *//p' | head -n 1)
  v3_tag=$(grep -oE '"tag_name": *"[^"]*"' "$T3/list.b" | head -n 1 | sed 's/.*: *"\(.*\)"/\1/')
  printf '%s' "$v3_tag" | grep -Eq '^v[0-9A-Za-z.+-]+$' || v3_tag=''
  say "etag present: $([ -n "$v3_etag" ] && echo yes || echo no)"
  hdr "rate limit after the list request"
  rl_remaining rl1
  rl_after_list=$RL_LAST
  hdr "conditional request with If-None-Match (the ETag of the list request)"
  if [ -n "$v3_etag" ]; then
    gh_get cond "https://api.github.com/repos/$RELEASE_REPO/releases?per_page=5" -H "If-None-Match: $v3_etag"
    say "request cond: GET releases?per_page=5 with If-None-Match -> $GH_CODE"
    gh_headers cond
  else
    say "no ETag, conditional request skipped"
  fi
  hdr "rate limit after the conditional request"
  rl_remaining rl2
  rl_after_cond=$RL_LAST
  hdr "/releases/latest of the API while only prereleases exist"
  gh_get latest "https://api.github.com/repos/$RELEASE_REPO/releases/latest"
  say "request latest: GET releases/latest -> $GH_CODE"
  gh_headers latest
  grep -oE '"(message|tag_name|prerelease)": *("[^"]*"|true|false)' "$T3/latest.b" | sed 's/^/| body: /'
  rl_remaining rl3
  rl_after_latest=$RL_LAST
  hdr "web address releases/latest (no API), redirect not followed"
  REQS=$((REQS + 1))
  v3_lw=$(curl -q -sS --noproxy '*' --proto '=https' --max-time 30 --connect-timeout 15 -A 'oarfish-spike/g6-1' --max-redirs 0 -o /dev/null \
    -w '%{http_code} %{redirect_url}' "https://github.com/$RELEASE_REPO/releases/latest" 2>"$T3/lw.err") || v3_lw="curl-failed($?) "
  v3_lwc=${v3_lw%% *}
  v3_lwu=${v3_lw#* }
  say "status $v3_lwc, redirect target host: $(printf '%s' "$v3_lwu" | sed -E 's#^(https?://[^/?]+).*#\1#'), points to a release tag page: $(printf '%s' "$v3_lwu" | grep -q '/releases/tag/' && echo yes || echo no)"
  hdr "redirect chain of one release asset (checksums.txt of the newest release in the list)"
  if [ -n "$v3_tag" ]; then
    v3_url="https://github.com/$RELEASE_REPO/releases/download/$v3_tag/checksums.txt"
    v3_hop=0
    while [ "$v3_hop" -lt 6 ]; do
      v3_hop=$((v3_hop + 1))
      REQS=$((REQS + 1))
      if [ "$REQS" -gt 20 ]; then say "request cap of 20 reached"; break; fi
      v3_res=$(curl -q -sS --noproxy '*' --proto '=https' --max-time 30 --connect-timeout 15 -A 'oarfish-spike/g6-1' --max-redirs 0 --max-filesize 200000 \
        -o /dev/null -w '%{http_code} %{size_download} %{redirect_url}' "$v3_url" 2>"$T3/hop.err") || v3_res="curl-failed($?) 0 "
      v3_code=${v3_res%% *}
      v3_rest=${v3_res#* }
      v3_next=${v3_rest#* }
      say "hop $v3_hop: $(printf '%s' "$v3_url" | sed -E 's#^(https?://[^/?]+).*#\1#') -> status $v3_code, body ${v3_rest%% *} bytes"
      case $v3_code in
        301 | 302 | 303 | 307 | 308) if [ -n "$v3_next" ]; then v3_url=$v3_next; else break; fi ;;
        *) break ;;
      esac
    done
    v3_url=''
    v3_next=''
  else
    say "no tag found in the list, chain skipped"
  fi
  hdr "rate limit after everything"
  rl_remaining rl4
  rl_final=$RL_LAST
  hdr "summary by this script"
  say "remaining before: $rl_before, after list: $rl_after_list, after conditional: $rl_after_cond, after /releases/latest: $rl_after_latest, final: $rl_final"
  if [ -n "$rl_before" ] && [ -n "$rl_after_list" ]; then say "cost of the 200 list request: $((rl_before - rl_after_list))"; fi
  if [ -n "$rl_after_list" ] && [ -n "$rl_after_cond" ]; then say "cost of the conditional request: $((rl_after_list - rl_after_cond))"; fi
  if [ -n "$rl_after_cond" ] && [ -n "$rl_after_latest" ]; then say "cost of /releases/latest: $((rl_after_cond - rl_after_latest))"; fi
  say "requests issued in this run: $REQS (cap 20)"
  rm -rf "$T3"
}

# ---- V4, V12 -----------------------------------------------------------------

do_v4() {
  need_installed
  hdr "inside the unit with the sandbox of install/oarfish.service (user oarfish)"
  systemctl reset-failed oarfish-spike-v4.service >/dev/null 2>&1
  cmd systemctl start oarfish-spike-v4.service
  journalctl --sync >/dev/null 2>&1
  sleep 1
  cmdsh "systemctl show oarfish-spike-v4.service -p Result -p ExecMainStatus -p ExecMainCode"
  v4_inv=$(sprop oarfish-spike-v4.service InvocationID)
  if [ -n "$v4_inv" ]; then
    cmdsh "journalctl _SYSTEMD_INVOCATION_ID=$v4_inv -o cat --no-pager --no-hostname"
  else
    cmdsh "journalctl -u oarfish-spike-v4.service -b -o cat --no-pager --no-hostname -n 60"
  fi
  hdr "control: the same probe command as root without a sandbox"
  cmd "$PROBE" v4
}

do_v12() {
  need_installed
  hdr "inside the full helper sandbox with SystemCallFilter=@system-service"
  rm -f "$OUTDIR/v12.txt"
  systemctl reset-failed oarfish-spike-v12.service >/dev/null 2>&1
  cmd systemctl start oarfish-spike-v12.service
  cmdsh "systemctl show oarfish-spike-v12.service -p Result -p ExecMainStatus -p ExecMainCode"
  if [ -f "$OUTDIR/v12.txt" ]; then cat "$OUTDIR/v12.txt"; else say "no output file (the probe may not have started)"; fi
  cmdsh "journalctl -u oarfish-spike-v12.service -b -o cat --no-pager --no-hostname -n 15"
  hdr "control: the same probe command outside any sandbox"
  cmd "$PROBE" v12
}

# ---- V6 ----------------------------------------------------------------------

do_v6() {
  need_installed
  systemctl stop oarfish-spike-crashloop.service >/dev/null 2>&1
  systemctl reset-failed oarfish-spike-crashloop.service >/dev/null 2>&1
  hdr "manager defaults and effective limits of the spike unit (it sets no limit line)"
  cmdsh "systemctl show -p DefaultStartLimitIntervalUSec -p DefaultStartLimitBurst"
  cmdsh "systemctl show oarfish-spike-crashloop.service -p Restart -p RestartUSec -p StartLimitIntervalUSec -p StartLimitBurst -p StartLimitAction"
  hdr "crash loop, sampled for 45 seconds (command exits at once, Restart=on-failure, RestartSec=2)"
  v6_since=$(date '+%Y-%m-%d %H:%M:%S')
  cmd systemctl start oarfish-spike-crashloop.service
  v6_t=0
  for v6_mark in 5 15 30 45; do
    sleep $((v6_mark - v6_t))
    v6_t=$v6_mark
    say "t=${v6_mark}s $(unit_state oarfish-spike-crashloop.service)"
  done
  hdr "journal of the unit during the test"
  journalctl --sync >/dev/null 2>&1
  v6_j=$(journalctl -u oarfish-spike-crashloop.service --since "$v6_since" -o short-monotonic --no-pager --no-hostname 2>&1)
  say "started_lines=$(printf '%s\n' "$v6_j" | grep -c 'Started ')"
  say "scheduled_restart_lines=$(printf '%s\n' "$v6_j" | grep -c 'Scheduled restart job')"
  say "start_limit_lines=$(printf '%s\n' "$v6_j" | grep -ciE 'start-limit|repeated too quickly')"
  printf '%s\n' "$v6_j" | tail -n 25 | sed 's/^/| /'
  hdr "final state, then stop and reset"
  say "$(unit_state oarfish-spike-crashloop.service)"
  cmd systemctl stop oarfish-spike-crashloop.service
  cmd systemctl reset-failed oarfish-spike-crashloop.service
}

# ---- V8, V10 -----------------------------------------------------------------

do_v8() {
  need_installed
  hdr "install and enable the boot recorder (oneshot, parallel to the boot)"
  cmd systemctl enable oarfish-spike-v8-boot.service
  cmd systemctl is-enabled oarfish-spike-v8-boot.service
  hdr "one baseline record now (before the reboot)"
  cmd systemctl start oarfish-spike-v8-boot.service
  cmdsh "cat $STATE/v8-boot.log"
}

v10_reset() {
  systemctl stop oarfish-spike-v10a.service oarfish-spike-v10b.service oarfish-spike-v10-helper.service >/dev/null 2>&1
  systemctl disable oarfish-spike-v10a.service oarfish-spike-v10b.service >/dev/null 2>&1
  rm -rf "$UNITDIR/oarfish-spike-v10a.service" "$UNITDIR/oarfish-spike-v10b.service" "$UNITDIR/oarfish-spike-v10a.service.d" \
    "$UNITDIR/multi-user.target.wants/oarfish-spike-v10a.service" "$UNITDIR/multi-user.target.wants/oarfish-spike-v10b.service"
  rm -f "$OPT/current" "$OPT/components/active" /usr/libexec/oarfish-spike-helper "$STATE/v10-v10a.log" "$STATE/v10-v10b.log" "$OUTDIR/v10-helper.txt"
  systemctl daemon-reload
}

v10_view() { # v10_view LABEL: how the manager sees the two linked units
  for vv_n in v10a v10b; do
    printf '%s %s: LoadState=%s ActiveState=%s UnitFileState=%s NeedDaemonReload=%s\n' "$1" "$vv_n" \
      "$(sprop oarfish-spike-$vv_n.service LoadState)" "$(sprop oarfish-spike-$vv_n.service ActiveState)" \
      "$(sprop oarfish-spike-$vv_n.service UnitFileState)" "$(sprop oarfish-spike-$vv_n.service NeedDaemonReload)"
    printf '%s %s: FragmentPath=%s\n' "$1" "$vv_n" "$(sprop oarfish-spike-$vv_n.service FragmentPath)"
    printf '%s %s: Description=%s\n' "$1" "$vv_n" "$(sprop oarfish-spike-$vv_n.service Description)"
  done
}

do_v10() {
  need_installed
  hdr "start from a clean V10 state"
  v10_reset
  hdr "layout of ADR 0010 U5 with spike names: unit link -> current (a link) -> release directory"
  mkdir -p "$OPT/releases/R1" "$OPT/releases/R2" "$OPT/components/c1"
  cp "$KIT"/units/v10/R1/*.service "$OPT/releases/R1/"
  cp "$KIT"/units/v10/R2/*.service "$OPT/releases/R2/"
  chmod 0644 "$OPT"/releases/R1/*.service "$OPT"/releases/R2/*.service
  cp "$PROBE" "$OPT/components/c1/oarfish-spike-helper"
  chmod 0755 "$OPT/components/c1/oarfish-spike-helper"
  link_switch "$OPT/current" releases/R1
  link_switch "$OPT/components/active" c1
  cmd ln -s "$OPT/components/active/oarfish-spike-helper" /usr/libexec/oarfish-spike-helper
  cmdsh "ls -l $OPT $OPT/components /usr/libexec/oarfish-spike-helper"
  hdr "way 1: manual link in /etc/systemd/system, then systemctl enable (unit v10a)"
  cmd ln -s "$OPT/current/oarfish-spike-v10a.service" "$UNITDIR/oarfish-spike-v10a.service"
  cmd systemctl daemon-reload
  cmd systemctl is-enabled oarfish-spike-v10a.service
  cmd systemctl enable oarfish-spike-v10a.service
  cmd systemctl is-enabled oarfish-spike-v10a.service
  hdr "way 2: systemctl link, then systemctl enable (unit v10b)"
  cmd systemctl link "$OPT/current/oarfish-spike-v10b.service"
  cmd systemctl is-enabled oarfish-spike-v10b.service
  cmd systemctl enable oarfish-spike-v10b.service
  cmd systemctl is-enabled oarfish-spike-v10b.service
  hdr "links on disk"
  cmdsh "ls -l $UNITDIR/oarfish-spike-v10a.service $UNITDIR/oarfish-spike-v10b.service"
  cmdsh "ls -l $UNITDIR/multi-user.target.wants/ | grep oarfish-spike-v10"
  cmdsh "readlink -f $UNITDIR/oarfish-spike-v10a.service $UNITDIR/oarfish-spike-v10b.service"
  hdr "the linked units load and start"
  cmd systemctl start oarfish-spike-v10a.service oarfish-spike-v10b.service
  v10_view loaded
  cmdsh "systemctl cat oarfish-spike-v10a.service | head -n 8"
  cmdsh "cat $STATE/v10-v10a.log $STATE/v10-v10b.log"
  hdr "systemd-analyze verify on the linked units"
  cmd systemd-analyze verify "$UNITDIR/oarfish-spike-v10a.service"
  cmd systemd-analyze verify "$UNITDIR/oarfish-spike-v10b.service"
  hdr "a drop-in in <unit>.d on the linked unit"
  mkdir -p "$UNITDIR/oarfish-spike-v10a.service.d"
  printf '[Service]\nEnvironment=OARFISH_SPIKE_DROPIN=yes\n' >"$UNITDIR/oarfish-spike-v10a.service.d/dropin.conf"
  say "NeedDaemonReload before the reload: $(sprop oarfish-spike-v10a.service NeedDaemonReload)"
  cmd systemctl daemon-reload
  cmd systemctl restart oarfish-spike-v10a.service
  cmdsh "systemctl show oarfish-spike-v10a.service -p DropInPaths -p Environment -p FragmentPath"
  cmdsh "tail -n 1 $STATE/v10-v10a.log"
  hdr "atomic switch of current from R1 to R2 (temporary link plus rename), then daemon-reload"
  v10_view before-switch
  link_switch "$OPT/current" releases/R2
  cmdsh "ls -l $OPT/current; readlink -f $UNITDIR/oarfish-spike-v10a.service"
  v10_view after-switch-before-reload
  cmd systemctl daemon-reload
  v10_view after-reload
  cmd systemctl restart oarfish-spike-v10a.service oarfish-spike-v10b.service
  cmdsh "tail -n 2 $STATE/v10-v10a.log; tail -n 1 $STATE/v10-v10b.log"
  hdr "helper binary through /usr/libexec -> components/active -> c1, ProtectSystem=strict, ReadWritePaths=/opt/oarfish-spike"
  cmd systemctl start oarfish-spike-v10-helper.service
  cmdsh "systemctl show oarfish-spike-v10-helper.service -p Result -p ExecMainStatus -p ExecMainCode"
  if [ -f "$OUTDIR/v10-helper.txt" ]; then cat "$OUTDIR/v10-helper.txt"; else say "no output file"; fi
  cmdsh "journalctl -u oarfish-spike-v10-helper.service -b -o cat --no-pager --no-hostname -n 15"
  hdr "systemd-analyze verify and security of the spike helper unit"
  cmd systemd-analyze verify "$UNITDIR/oarfish-spike-v10-helper.service"
  cmd systemd-analyze security --no-pager oarfish-spike-v10-helper.service
  hdr "systemd-analyze security of the helper-like unit used for V11"
  cmd systemd-analyze security --no-pager oarfish-spike-h11.service
  hdr "state for the reboot block"
  for v10_n in v10a v10b v8-boot; do
    say "oarfish-spike-$v10_n.service: is-enabled=$(systemctl is-enabled oarfish-spike-$v10_n.service 2>&1)"
  done
  say "next: the reboot block"
}

v10x_reset() {
  systemctl stop oarfish-spike-v10c.service oarfish-spike-v10d.service >/dev/null 2>&1
  systemctl disable oarfish-spike-v10c.service oarfish-spike-v10d.service >/dev/null 2>&1
  rm -rf "$UNITDIR/oarfish-spike-v10c.service" "$UNITDIR/oarfish-spike-v10d.service" \
    "$UNITDIR/multi-user.target.wants/oarfish-spike-v10c.service" "$UNITDIR/multi-user.target.wants/oarfish-spike-v10d.service" \
    "$OPT/currentx" "$OPT/releasesx" "$STATE/v10-v10c.log" "$STATE/v10-v10d.log"
  systemctl daemon-reload
}

v10x_view() { # v10x_view LABEL
  for vx_n in v10c v10d; do
    printf '%s %s: is-enabled=%s LoadState=%s ActiveState=%s UnitFileState=%s Description=%s\n' "$1" "$vx_n" \
      "$(systemctl is-enabled oarfish-spike-$vx_n.service 2>&1)" "$(sprop oarfish-spike-$vx_n.service LoadState)" \
      "$(sprop oarfish-spike-$vx_n.service ActiveState)" "$(sprop oarfish-spike-$vx_n.service UnitFileState)" \
      "$(sprop oarfish-spike-$vx_n.service Description)"
  done
}

do_v10x() {
  need_installed
  note "EXPLORATION, not adopted: what happens to an enabled linked unit when the release directory its wants link points to is removed (the retention of ADR 0010 U5)."
  note "v10c is enabled with systemctl enable (as in V10 way 1); v10d gets a relative link in the wants directory that points at the unit in /etc/systemd/system."
  note "Both units start again at the next reboot: run the reboot block afterwards."
  hdr "start from a clean state"
  v10x_reset
  hdr "layout: units link -> currentx -> releasesx/RX"
  mkdir -p "$OPT/releasesx/RX" "$OPT/releasesx/RY"
  cp "$KIT"/units/v10/RX/*.service "$OPT/releasesx/RX/"
  cp "$KIT"/units/v10/RY/*.service "$OPT/releasesx/RY/"
  chmod 0644 "$OPT"/releasesx/RX/*.service "$OPT"/releasesx/RY/*.service
  link_switch "$OPT/currentx" releasesx/RX
  cmd ln -s "$OPT/currentx/oarfish-spike-v10c.service" "$UNITDIR/oarfish-spike-v10c.service"
  cmd ln -s "$OPT/currentx/oarfish-spike-v10d.service" "$UNITDIR/oarfish-spike-v10d.service"
  cmd systemctl daemon-reload
  hdr "enable: v10c with systemctl enable, v10d with a relative link in the wants directory"
  cmd systemctl enable oarfish-spike-v10c.service
  cmd ln -s ../oarfish-spike-v10d.service "$UNITDIR/multi-user.target.wants/oarfish-spike-v10d.service"
  cmd systemctl daemon-reload
  cmdsh "ls -l $UNITDIR/multi-user.target.wants/ | grep oarfish-spike-v10[cd]"
  v10x_view enabled
  cmd systemctl start oarfish-spike-v10c.service oarfish-spike-v10d.service
  cmdsh "cat $STATE/v10-v10c.log $STATE/v10-v10d.log"
  hdr "switch currentx to RY, daemon-reload, restart"
  link_switch "$OPT/currentx" releasesx/RY
  cmd systemctl daemon-reload
  cmd systemctl restart oarfish-spike-v10c.service oarfish-spike-v10d.service
  v10x_view switched
  cmdsh "tail -n 1 $STATE/v10-v10c.log; tail -n 1 $STATE/v10-v10d.log"
  hdr "remove releasesx/RX as retention would, then daemon-reload"
  cmd rm -rf "$OPT/releasesx/RX"
  cmdsh "ls -l $UNITDIR/multi-user.target.wants/ | grep oarfish-spike-v10[cd]"
  cmdsh "find -L $UNITDIR/multi-user.target.wants -maxdepth 1 -name 'oarfish-spike-v10*' -type l"
  cmd systemctl daemon-reload
  v10x_view after-removal
  cmdsh "systemctl list-dependencies --plain --no-pager multi-user.target | grep oarfish-spike-v10"
  cmd systemd-analyze verify "$UNITDIR/oarfish-spike-v10c.service"
  cmd systemd-analyze verify "$UNITDIR/oarfish-spike-v10d.service"
  hdr "additional observation: memory controller and the MemoryMax= line of the helper sandbox"
  cmdsh "cat /sys/fs/cgroup/cgroup.controllers; cat /sys/fs/cgroup/cgroup.subtree_control"
  cmdsh "tr ' ' '\\n' </proc/cmdline | grep -i cgroup; true"
  cmdsh "systemctl show oarfish-spike-v10-helper.service -p MemoryMax -p MemoryAccounting -p TasksMax -p TasksAccounting"
  cmdsh "journalctl -b -o cat --no-pager --no-hostname -g 'memory' | grep -iE 'controller|cgroup|MemoryMax' | tail -n 5"
  say "next: the postflight block, then the reboot block; v10c and v10d must start at boot"
}

# ---- V11 ---------------------------------------------------------------------

h11_setup() {
  mkdir -p "$OPT/h11/components"
  for hs_c in c-good c-a1 c-a2 c-a3 c-a4; do
    mkdir -p "$OPT/h11/components/$hs_c"
    cp "$PROBE" "$OPT/h11/components/$hs_c/oarfish-spike-h11"
    chmod 0755 "$OPT/h11/components/$hs_c/oarfish-spike-h11"
  done
  printf 'ok\n' >"$OPT/h11/components/c-good/mode"
  link_switch "$OPT/h11/known-good" components/c-good
  link_switch "$OPT/h11/active" components/c-good
  if [ ! -L /usr/libexec/oarfish-spike-h11 ]; then ln -s "$OPT/h11/active/oarfish-spike-h11" /usr/libexec/oarfish-spike-h11; fi
  rm -f "$STATE/h11.log" "$STATE/recover.log" "$OUTDIR/h11.txt" "$OUTDIR/recover.txt"
  systemctl stop oarfish-spike-h11.service oarfish-spike-recover.service >/dev/null 2>&1
  systemctl reset-failed 'oarfish-spike-h11.service' 'oarfish-spike-recover.service' >/dev/null 2>&1
}

log_lines() { if [ -f "$1" ]; then wc -l <"$1" | tr -d ' '; else echo 0; fi; } # log_lines FILE

h11_mode() { printf '%s\n' "$2" >"$OPT/h11/components/$1/mode"; } # h11_mode COMPONENT TEXT

h11_events() { # h11_events FROMLINE: events since a log position, with counts
  he_l=$(tail -n +"$1" "$STATE/h11.log" 2>/dev/null)
  printf '%s\n' "$he_l" | sed 's/^/| /'
  say "starts=$(printf '%s\n' "$he_l" | grep -c ' event=start ')  exit75=$(printf '%s\n' "$he_l" | grep -c ' event=exit75 ')  recover_runs=$(printf '%s\n' "$he_l" | grep -c ' event=recover-start ')"
}

h11_journal() { # h11_journal SINCE
  journalctl --sync >/dev/null 2>&1
  journalctl -u oarfish-spike-h11.service -u oarfish-spike-recover.service --since "$1" -o short-monotonic --no-pager --no-hostname 2>&1 |
    grep -E 'Scheduled restart|Failed with|Main process exited|start-limit|repeated too quickly|OnFailure|Started|Stopped|runtime time limit|Deactivated|Finished' | tail -n 40 | sed 's/^/| /'
}

h11_scenario() { # h11_scenario TITLE MAXWAIT: modes are set by the caller, the run starts at c-a1
  hs_title=$1
  hs_max=$2
  hdr "$hs_title"
  systemctl stop oarfish-spike-h11.service >/dev/null 2>&1
  systemctl reset-failed oarfish-spike-h11.service oarfish-spike-recover.service >/dev/null 2>&1
  sleep 1
  link_switch "$OPT/h11/active" components/c-a1
  hs_n=$(($(log_lines "$STATE/h11.log") + 1))
  hs_since=$(date '+%Y-%m-%d %H:%M:%S')
  cmd systemctl start oarfish-spike-h11.service
  wait_inactive oarfish-spike-h11.service "$hs_max" || say "still not inactive or failed after ${hs_max}s"
  wait_inactive oarfish-spike-recover.service 20
  sleep 2
  say "events (t = seconds since boot):"
  h11_events "$hs_n"
  say "$(unit_state oarfish-spike-h11.service)"
  say "$(unit_state oarfish-spike-recover.service)"
  say "active now points to: $(readlink "$OPT/h11/active")"
  say "journal lines:"
  h11_journal "$hs_since"
}

do_v11a() {
  need_installed
  h11_setup
  note "Restart=on-failure, RestartSec=1, StartLimitIntervalSec=10, StartLimitBurst=3, OnFailure=recover: as the helper service of ADR 0010 U10."
  note "The restart after a deliberate exit 75 is measured: does it count against the start limit, does OnFailure= fire."
  h11_mode c-a1 'update75 1 c-a2'
  h11_mode c-a2 ok
  h11_scenario "S1: one update (c-a1 exits 75 after 1 s, c-a2 runs and exits 0 after 3 s)" 30
  h11_mode c-a1 'update75 0 c-a2'
  h11_mode c-a2 'update75 0 c-a3'
  h11_mode c-a3 ok
  h11_scenario "S2: two updates in a row, no delay (3 starts within about 3 s)" 30
  h11_mode c-a1 'update75 0 c-a2'
  h11_mode c-a2 'update75 0 c-a3'
  h11_mode c-a3 'update75 0 c-a4'
  h11_mode c-a4 ok
  h11_scenario "S3: three updates in a row, no delay (4 starts within about 4 s)" 30
  h11_mode c-a1 'update75 4 c-a2'
  h11_mode c-a2 'update75 4 c-a3'
  h11_mode c-a3 'update75 4 c-a4'
  h11_mode c-a4 ok
  h11_scenario "S4: three updates in a row, 4 s of work in each (starts about 5 s apart)" 45
  link_switch "$OPT/h11/active" components/c-good
  systemctl stop oarfish-spike-h11.service >/dev/null 2>&1
  systemctl reset-failed oarfish-spike-h11.service oarfish-spike-recover.service >/dev/null 2>&1
  note "active points to c-good again, units stopped"
}

do_v11b() {
  need_installed
  h11_setup
  hdr "state of the spike units before the crash and hang scenarios"
  say "$(unit_state oarfish-spike-h11.service)"
  h11_mode c-a1 crash
  h11_scenario "S5: a new binary that exits at once (crash loop), recover from known-good" 30
  hdr "S5 continued: the next connection would start the helper again; here a manual start of the unit"
  hs_n=$(($(log_lines "$STATE/h11.log") + 1))
  cmd systemctl start oarfish-spike-h11.service
  wait_inactive oarfish-spike-h11.service 20
  h11_events "$hs_n"
  say "$(unit_state oarfish-spike-h11.service)"
  h11_mode c-a1 crash
  h11_scenario "S6: the same crash loop a second time (can the same recover unit be started by OnFailure= again)" 30
  say "recover runs in the whole log so far: $(grep -c ' event=recover-start ' "$STATE/h11.log")"
  say "recover unit invocations in the journal since boot: $(journalctl -u oarfish-spike-recover.service -b -o cat --no-pager --no-hostname 2>/dev/null | grep -c 'Finished\|Deactivated successfully')"
  hdr "S7: a new binary that hangs; RuntimeMaxSec=10 in this spike instead of 900; observed for 45 s"
  systemctl stop oarfish-spike-h11.service >/dev/null 2>&1
  systemctl reset-failed oarfish-spike-h11.service oarfish-spike-recover.service >/dev/null 2>&1
  h11_mode c-a1 hang
  link_switch "$OPT/h11/active" components/c-a1
  hs_n=$(($(log_lines "$STATE/h11.log") + 1))
  hs_since=$(date '+%Y-%m-%d %H:%M:%S')
  cmd systemctl start oarfish-spike-h11.service
  hs_t=0
  for hs_mark in 8 15 25 35 45; do
    sleep $((hs_mark - hs_t))
    hs_t=$hs_mark
    say "t=${hs_mark}s $(unit_state oarfish-spike-h11.service)  recover_runs=$(grep -c ' event=recover-start ' "$STATE/h11.log")"
  done
  say "events (t = seconds since boot):"
  h11_events "$hs_n"
  say "journal lines:"
  h11_journal "$hs_since"
  cmd systemctl stop oarfish-spike-h11.service
  systemctl reset-failed oarfish-spike-h11.service oarfish-spike-recover.service >/dev/null 2>&1
  link_switch "$OPT/h11/active" components/c-good
  note "stopped by this script after 45 s, active points to c-good again"
}

do_v11x() {
  need_installed
  h11_setup
  note "EXPLORATION, not adopted: the hang scenario S7 with a longer start limit window (StartLimitIntervalSec=120, burst still 3)."
  note "Run only after the owner has seen the S7 result of the v11b block."
  mkdir -p "$UNITDIR/oarfish-spike-h11.service.d"
  printf '[Unit]\nStartLimitIntervalSec=120\nStartLimitBurst=3\n' >"$UNITDIR/oarfish-spike-h11.service.d/explore.conf"
  systemctl daemon-reload
  cmdsh "systemctl show oarfish-spike-h11.service -p StartLimitIntervalUSec -p StartLimitBurst"
  h11_mode c-a1 hang
  link_switch "$OPT/h11/active" components/c-a1
  hs_n=$(($(log_lines "$STATE/h11.log") + 1))
  hs_since=$(date '+%Y-%m-%d %H:%M:%S')
  cmd systemctl start oarfish-spike-h11.service
  hs_t=0
  for hs_mark in 15 30 40 50; do
    sleep $((hs_mark - hs_t))
    hs_t=$hs_mark
    say "t=${hs_mark}s $(unit_state oarfish-spike-h11.service)  recover_runs=$(grep -c ' event=recover-start ' "$STATE/h11.log")"
  done
  h11_events "$hs_n"
  h11_journal "$hs_since"
  systemctl stop oarfish-spike-h11.service >/dev/null 2>&1
  rm -rf "$UNITDIR/oarfish-spike-h11.service.d"
  systemctl daemon-reload
  systemctl reset-failed oarfish-spike-h11.service oarfish-spike-recover.service >/dev/null 2>&1
  link_switch "$OPT/h11/active" components/c-good
  note "drop-in removed, active points to c-good again"
}

# ---- V13 ---------------------------------------------------------------------

do_v13() {
  need_installed
  v13_u=oarfish-spike-v13
  v13_sock=v13.sock
  case ${1:-} in
    nb)
      v13_u=oarfish-spike-v13nb
      v13_sock=v13nb.sock
      note "EXPLORATION, not adopted: the service restarts its socket with 'systemctl restart --no-block'."
      ;;
    os)
      v13_u=oarfish-spike-v13os
      v13_sock=v13os.sock
      note "EXPLORATION, not adopted: the service only exits; a unit started by OnSuccess= restarts the socket afterwards."
      ;;
  esac
  [ -f "$UNITDIR/$v13_u.socket" ] || die "the unit $v13_u.socket is not installed: copy the new kit and run the install block again"
  T13=$REPORT_DIR/oarfish-spike-v13-tmp
  rm -rf "$T13"
  mkdir -p "$T13"
  systemctl stop "$v13_u.socket" "$v13_u.service" >/dev/null 2>&1
  systemctl reset-failed "$v13_u.socket" "$v13_u.service" >/dev/null 2>&1
  rm -f "$STATE/v13.log" "$OUTDIR/v13.txt" "$OUTDIR/v13nb.txt" "$OUTDIR/v13os.txt" "$OUTDIR/v13os-restart.txt"
  systemctl reset-failed oarfish-spike-v13os-restart.service >/dev/null 2>&1
  v13_since=$(date '+%Y-%m-%d %H:%M:%S')
  cmd systemctl start "$v13_u.socket"
  s13=$RUNDIR/$v13_sock
  hdr "before"
  say "$(unit_state "$v13_u.service")"
  say "$v13_u.socket: ActiveState=$(sprop "$v13_u.socket" ActiveState) SubState=$(sprop "$v13_u.socket" SubState)"
  cmdsh "systemctl show $v13_u.service -p Requires -p Wants -p After -p TriggeredBy -p Triggers | cut -c 1-300"
  cmdsh "systemctl show $v13_u.socket -p Triggers -p Before -p Conflicts -p RequiredBy -p WantedBy | cut -c 1-300"
  hdr "clients: A is served (3 s of work), C connects while A is served (waits in the backlog), E connects right after A's reply"
  "$PROBE" v13-client "$s13" A >"$T13/A" 2>&1 &
  v13_a=$!
  sleep 1
  "$PROBE" v13-client "$s13" C >"$T13/C" 2>&1 &
  v13_c=$!
  v13_i=0
  while ! grep -q 'reply \[' "$T13/A" 2>/dev/null && [ "$v13_i" -lt 100 ]; do
    sleep 0.2
    v13_i=$((v13_i + 1))
  done
  "$PROBE" v13-client "$s13" E >"$T13/E" 2>&1 &
  v13_e=$!
  wait "$v13_a" "$v13_c" "$v13_e"
  for v13_l in A C E; do
    say "--- client $v13_l"
    sed 's/^/| /' "$T13/$v13_l"
  done
  hdr "after the first service run has ended"
  sleep 6
  say "$(unit_state "$v13_u.service")"
  say "$v13_u.socket: ActiveState=$(sprop "$v13_u.socket" ActiveState) SubState=$(sprop "$v13_u.socket" SubState)"
  cmdsh "stat -c '%A %U %G %n' $s13"
  hdr "client D connects now: the socket must start the service again"
  "$PROBE" v13-client "$s13" D >"$T13/D" 2>&1
  sed 's/^/| /' "$T13/D"
  sleep 6
  hdr "final state of both units"
  say "$(unit_state "$v13_u.service")"
  say "$v13_u.socket: ActiveState=$(sprop "$v13_u.socket" ActiveState) SubState=$(sprop "$v13_u.socket" SubState) Result=$(sprop "$v13_u.socket" Result)"
  hdr "event log of the service (t = seconds since boot)"
  sed 's/^/| /' "$STATE/v13.log"
  hdr "probe output of the service"
  sed 's/^/| /' "$OUTDIR/v13.txt" "$OUTDIR/v13nb.txt" "$OUTDIR/v13os.txt" "$OUTDIR/v13os-restart.txt" 2>/dev/null
  hdr "journal of both units during the test"
  journalctl --sync >/dev/null 2>&1
  journalctl -u "$v13_u.socket" -u "$v13_u.service" -u oarfish-spike-v13os-restart.service --since "$v13_since" -o short-monotonic --no-pager --no-hostname 2>&1 | tail -n 40 | sed 's/^/| /'
  systemctl stop "$v13_u.socket" "$v13_u.service" oarfish-spike-v13os-restart.service >/dev/null 2>&1
  rm -rf "$T13"
  note "units stopped"
}

do_v11y() {
  need_installed
  h11_setup
  note "EXPLORATION, not adopted. Run only after the owner has seen the V11 result of the v11a and v11b blocks."
  note "Part 1: the recover unit has no ordering against the helper service, as ADR 0010 U1 has it (it orders the recover unit against the socket and the main service only)."
  note "Part 2 declares exit code 75 a clean exit that still forces the restart: SuccessExitStatus=75 and RestartForceExitStatus=75."
  cmdsh "systemctl show oarfish-spike-recover.service -p Before -p After -p OnFailureOf"
  h11_mode c-a1 'update75 1 c-a2'
  h11_mode c-a2 ok
  h11_scenario "X1 (part 1): one update, as in the ADR (OnFailure= starts the recover unit)" 30
  h11_mode c-a1 'update75 0 c-a2'
  h11_mode c-a2 'update75 0 c-a3'
  h11_mode c-a3 'update75 0 c-a4'
  h11_mode c-a4 ok
  h11_scenario "X2 (part 1): three updates in a row, as in the ADR" 30
  mkdir -p "$UNITDIR/oarfish-spike-h11.service.d"
  printf '[Service]\nSuccessExitStatus=75\nRestartForceExitStatus=75\n' >"$UNITDIR/oarfish-spike-h11.service.d/explore.conf"
  systemctl daemon-reload
  cmdsh "systemctl show oarfish-spike-h11.service -p SuccessExitStatus -p RestartForceExitStatus -p Restart"
  h11_mode c-a1 'update75 1 c-a2'
  h11_mode c-a2 ok
  h11_scenario "X3 (part 2): one update" 30
  h11_mode c-a1 'update75 0 c-a2'
  h11_mode c-a2 'update75 0 c-a3'
  h11_mode c-a3 ok
  h11_scenario "X4 (part 2): two updates in a row, no delay (3 starts)" 30
  h11_mode c-a1 'update75 0 c-a2'
  h11_mode c-a2 'update75 0 c-a3'
  h11_mode c-a3 'update75 0 c-a4'
  h11_mode c-a4 ok
  h11_scenario "X5 (part 2): three updates in a row, no delay (4 starts within about 4 s)" 30
  h11_mode c-a1 'update75 4 c-a2'
  h11_mode c-a2 'update75 4 c-a3'
  h11_mode c-a3 'update75 4 c-a4'
  h11_mode c-a4 ok
  h11_scenario "X6 (part 2): three updates in a row, 4 s of work in each" 45
  h11_mode c-a1 crash
  h11_scenario "X7 (part 2): a new binary that exits with 1 must still reach OnFailure" 30
  h11_mode c-a1 hang
  h11_scenario "X8 (part 2): a new binary that hangs must still reach OnFailure (RuntimeMaxSec=10)" 40
  systemctl stop oarfish-spike-h11.service >/dev/null 2>&1
  rm -rf "$UNITDIR/oarfish-spike-recover.service.d" "$UNITDIR/oarfish-spike-h11.service.d"
  systemctl daemon-reload
  systemctl reset-failed oarfish-spike-h11.service oarfish-spike-recover.service >/dev/null 2>&1
  link_switch "$OPT/h11/active" components/c-good
  note "drop-ins removed, active points to c-good again"
}

# ---- production check, reboot, post-reboot -------------------------------------

do_postflight() {
  hdr "production service against the preflight snapshot"
  prod_compare
  hdr "units in the failed state"
  cmdsh "systemctl --failed --no-pager --no-legend"
  hdr "spike units that exist now"
  cmdsh "systemctl list-unit-files 'oarfish-spike-*' --no-pager --no-legend"
}

do_reboot() {
  hdr "marker for the clock comparison after the reboot"
  printf 'pre_reboot_realtime_unix=%s\n' "$(date +%s)" >"$STATE/pre-reboot"
  cat "$STATE/pre-reboot"
  hdr "enabled spike units that must start at boot"
  cmdsh "systemctl list-unit-files 'oarfish-spike-*' --no-pager --no-legend | grep enabled"
  say "rebooting now; after the Pi is back, run the post-reboot block"
  sync
  systemctl reboot
}

do_post_reboot() {
  hdr "boot state"
  cmdsh "cut -d' ' -f1 /proc/uptime | sed 's/^/uptime_s=/'"
  cmd systemctl is-system-running
  cmdsh "systemctl --failed --no-pager --no-legend"
  hdr "V10 after the reboot: the linked units started at boot"
  for pr_n in v10a v10b v10c v10d; do
    [ -f "$STATE/v10-$pr_n.log" ] || continue
    say "oarfish-spike-$pr_n.service: is-active=$(systemctl is-active oarfish-spike-$pr_n.service 2>&1) is-enabled=$(systemctl is-enabled oarfish-spike-$pr_n.service 2>&1) ActiveEnterTimestampMonotonic=$(sprop oarfish-spike-$pr_n.service ActiveEnterTimestampMonotonic)"
    say "oarfish-spike-$pr_n.service: FragmentPath=$(sprop oarfish-spike-$pr_n.service FragmentPath) Environment=$(sprop oarfish-spike-$pr_n.service Environment)"
    cmdsh "cat $STATE/v10-$pr_n.log"
  done
  hdr "V8 after the reboot: clock at boot"
  cmdsh "cat $STATE/v8-boot.log $STATE/pre-reboot"
  cmdsh "timedatectl show -p NTP -p NTPSynchronized -p RTCTimeUSec"
  cmdsh "systemctl is-active fake-hwclock.service systemd-timesyncd.service"
  cmdsh "date -u '+utc now: %Y-%m-%dT%H:%M:%SZ'"
  pr_rt=$(tail -n 1 "$STATE/v8-boot.log" 2>/dev/null | sed -n 's/.*realtime_unix=\([0-9]*\).*/\1/p')
  pr_pre=$(sed -n 's/^pre_reboot_realtime_unix=//p' "$STATE/pre-reboot" 2>/dev/null)
  pr_fh=''
  if [ -e /etc/fake-hwclock.data ]; then pr_fh=$(stat -c %Y /etc/fake-hwclock.data); fi
  if [ -n "$pr_rt" ] && [ -n "$pr_pre" ]; then say "boot-time wall clock minus wall clock at the reboot command (s): $((pr_rt - pr_pre))"; fi
  if [ -n "$pr_rt" ] && [ -n "$pr_fh" ]; then say "boot-time wall clock minus modification time of the fake-hwclock file (s): $((pr_rt - pr_fh))"; fi
  say "(a small positive first number means the clock kept going or was restored; a large negative or tiny one means it started from an old value)"
  hdr "production service against the preflight snapshot (MainPID and friends differ after a reboot by design)"
  prod_compare
}

# ---- cleanup ---------------------------------------------------------------------

cleanup_paths() {
  for cp_p in /etc/systemd/system/oarfish-spike-* /etc/systemd/system/*.wants/oarfish-spike-* /etc/systemd/system/*.requires/oarfish-spike-* \
    /usr/libexec/oarfish-spike-* /opt/oarfish-spike /var/lib/oarfish-spike-update /var/cache/oarfish-spike-cache /run/oarfish-spike-update \
    /var/tmp/oarfish-spike-*; do
    if [ -e "$cp_p" ] || [ -L "$cp_p" ]; then
      [ "$cp_p" = "${REPORT:-}" ] && continue
      printf '%s\n' "$cp_p"
    fi
  done
}

safe_rm() { # safe_rm PATH: only paths with the spike prefix in a fixed set of places
  case $1 in
    *..*) say "refusing $1"; return 1 ;;
    /etc/systemd/system/oarfish-spike-* | /etc/systemd/system/*.wants/oarfish-spike-* | /etc/systemd/system/*.requires/oarfish-spike-*) ;;
    /usr/libexec/oarfish-spike-* | /opt/oarfish-spike | /var/lib/oarfish-spike-update | /var/cache/oarfish-spike-cache | /run/oarfish-spike-update) ;;
    /var/tmp/oarfish-spike-*) ;;
    *) say "refusing $1"; return 1 ;;
  esac
  rm -rf -- "$1"
}

spike_units() {
  {
    systemctl list-unit-files 'oarfish-spike-*' --no-pager --no-legend 2>/dev/null | awk '{print $1}'
    systemctl list-units --all 'oarfish-spike-*' --no-pager --no-legend 2>/dev/null | sed 's/^[^a-z]*//' | awk '{print $1}'
  } | grep '^oarfish-spike-' | sort -u
}

do_cleanup_dry() {
  hdr "dry run: units that would be stopped (sockets first)"
  spike_units | sed 's/^/| /'
  hdr "dry run: paths that would be removed (only names with the prefix oarfish-spike)"
  cleanup_paths | sed 's/^/| /'
  say "nothing was changed"
}

do_cleanup() {
  hdr "stop the spike units (sockets first)"
  spike_units | grep '\.socket$' | while read -r cl_u; do cmd systemctl stop "$cl_u"; done
  spike_units | grep -v '\.socket$' | while read -r cl_u; do cmd systemctl stop "$cl_u"; done
  hdr "production service against the preflight snapshot, before anything is removed"
  prod_compare
  hdr "remove what carries the prefix"
  cleanup_paths | while read -r cl_p; do
    if safe_rm "$cl_p"; then say "removed $cl_p"; fi
  done
  cmd systemctl daemon-reload
  systemctl reset-failed 'oarfish-spike-*' >/dev/null 2>&1
  hdr "leftovers (none expected)"
  cmdsh "find /etc /opt /usr /var /run -xdev -name 'oarfish-spike*' 2>/dev/null | head -n 20"
  cmdsh "systemctl list-unit-files 'oarfish-spike-*' --no-pager --no-legend"
  hdr "production service after the cleanup"
  say "$(unit_state oarfish.service)"
  say "unit_sha256=$(sha256sum /etc/systemd/system/oarfish.service | awk '{print $1}')"
}

# ---- main --------------------------------------------------------------------------

usage() {
  cat >&2 <<'USAGE'
usage: sudo sh spike.sh BLOCK [ARGUMENT]
blocks: kitcheck preflight facts install v1 v2 ADDRESS:PORT v3 v4 v12 v6 v8 v10 v11a v11b v13 postflight reboot post-reboot cleanup-dry cleanup
only if needed: v1x v10x v11x v11y "v13 nb" "v13 os"
USAGE
  exit 2
}

main() {
  [ $# -ge 1 ] || usage
  m_block=$1
  shift
  case $m_block in
    kitcheck | preflight | facts | install | v1 | v1x | v2 | v3 | v4 | v6 | v8 | v10 | v10x | v11a | v11b | v11x | v11y | v12 | v13 | postflight | reboot | post-reboot | cleanup-dry | cleanup) ;;
    *) usage ;;
  esac
  [ "$(id -u)" = 0 ] || die "run as root: sudo sh $0 $m_block"
  if [ "$m_block" = v2 ] && [ $# -ge 1 ]; then
    m_h=${1%:*}
    m_h=${m_h#\[}
    m_h=${m_h%\]}
    SCRUB_EXTRA="$m_h"
  fi
  m_fn=do_$(printf '%s' "$m_block" | tr '-' '_')
  run_report "$m_block" "$m_fn" "$@"
}

if [ "${OARFISH_SPIKE_SOURCE_ONLY:-}" != 1 ]; then main "$@"; fi
