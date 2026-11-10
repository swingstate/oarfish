#!/usr/bin/env bash
# The three update-helper units must keep the directives that ADR 0010 gives them
# (U2 socket lines, U10 sandbox, amendment A1) and must not gain forbidden ones.
# This checks the text of the repository files; systemd-analyze verify (CI) checks
# that systemd accepts them.
#
# Usage: scripts/check-units.sh [UNIT_DIR]    default: install/
#        scripts/check-units.sh --self-test   mutation test of this script
#
# Rules, per unit:
#   - every required "Section|key=value" line is present, exactly;
#   - no key appears twice (systemd takes the last value, so a second
#     "NoNewPrivileges=no" after the required line would undo it);
#   - no forbidden key: dependencies (Requires, BindsTo, PartOf, ...), Environment,
#     extra Exec* commands, SupplementaryGroups;
#   - oarfish.service appears in no directive, except in the Before= line of the
#     recover unit (U1); the helper has no ordering or dependency on the service;
#   - no line continuation (a trailing backslash).
# The helper service has no [Install] section and the recover unit no RemainAfterExit.
# The recover unit has TimeoutStartSec=120 (ADR 0010 A9): a oneshot unit has no
# start timeout by default, and this one is ordered Before=oarfish.service.
# The recover unit has DefaultDependencies=no and After=local-fs.target (A14): it is
# ordered Before= the helper socket, and the default After=basic.target of a service
# would make an ordering cycle through sockets.target (scripts/verify-units.sh
# verifies the boot transaction; this check keeps the text that avoids the cycle).
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
ROOT="$(repo_root)"

UNITS="oarfish-update-helper.socket oarfish-update-helper.service oarfish-update-recover.service"
FORBIDDEN_KEYS="Requires Requisite BindsTo PartOf Wants Upholds Conflicts Environment EnvironmentFile PassEnvironment ExecStartPre ExecStartPost ExecStop ExecStopPost ExecReload ExecCondition SupplementaryGroups"
RECOVER_BEFORE='Unit|Before=oarfish-update-helper.socket oarfish.service'

# The sandbox of U10, shared by the helper service and the recover unit.
sandbox() {
  cat <<'EOF'
Service|User=root
Service|Group=root
Service|UMask=0077
Service|StateDirectory=oarfish-update
Service|StateDirectoryMode=0700
Service|ReadWritePaths=/opt/oarfish
Service|NoNewPrivileges=yes
Service|CapabilityBoundingSet=
Service|AmbientCapabilities=
Service|ProtectSystem=strict
Service|ProtectHome=yes
Service|PrivateTmp=yes
Service|PrivateDevices=yes
Service|ProtectKernelTunables=yes
Service|ProtectKernelModules=yes
Service|ProtectKernelLogs=yes
Service|ProtectControlGroups=yes
Service|ProtectClock=yes
Service|ProtectHostname=yes
Service|ProtectProc=invisible
Service|ProcSubset=pid
Service|RestrictNamespaces=yes
Service|RestrictRealtime=yes
Service|RestrictSUIDSGID=yes
Service|LockPersonality=yes
Service|MemoryDenyWriteExecute=yes
Service|RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
Service|IPAddressDeny=any
Service|IPAddressAllow=localhost
Service|SystemCallArchitectures=native
Service|SystemCallFilter=@system-service
Service|SystemCallErrorNumber=EPERM
Service|MemoryMax=64M
Service|TasksMax=16
Service|Nice=10
EOF
}

required_for() {
  case "$1" in
    oarfish-update-helper.socket)
      cat <<'EOF'
Socket|ListenStream=/run/oarfish-update/helper.sock
Socket|SocketUser=root
Socket|SocketGroup=oarfish
Socket|SocketMode=0660
Socket|DirectoryMode=0755
Socket|Accept=no
Socket|Backlog=8
Socket|RemoveOnStop=yes
Install|WantedBy=sockets.target
EOF
      ;;
    oarfish-update-helper.service)
      cat <<'EOF'
Unit|OnFailure=oarfish-update-recover.service
Unit|StartLimitIntervalSec=10
Unit|StartLimitBurst=3
Service|Type=exec
Service|ExecStart=/usr/libexec/oarfish-update-helper serve
Service|Restart=on-failure
Service|RestartSec=1
Service|SuccessExitStatus=75
Service|RestartForceExitStatus=75
Service|RuntimeMaxSec=900
EOF
      sandbox
      ;;
    oarfish-update-recover.service)
      echo "$RECOVER_BEFORE"
      cat <<'EOF'
Unit|DefaultDependencies=no
Unit|After=local-fs.target
Service|Type=oneshot
Service|TimeoutStartSec=120
Service|ExecStart=/opt/oarfish/components/known-good/oarfish-update-helper recover
Install|WantedBy=multi-user.target
EOF
      sandbox
      ;;
  esac
}

# Section|line for every directive; comments and blank lines dropped.
normalize() {
  tr -d '\r' < "$1" | awk '
    /^[[:space:]]*($|[#;])/ { next }
    /^[[:space:]]*\[[A-Za-z]+\][[:space:]]*$/ { sec = $0; gsub(/[][[:space:]]/, "", sec); next }
    { line = $0; sub(/^[[:space:]]+/, "", line); sub(/[[:space:]]+$/, "", line); print sec "|" line }
  '
}

# Check one unit file; print one line per problem; return 1 if there is any.
check_unit() {
  local name="$1" file="$2" bad=0 norm keys want key
  [ -f "$file" ] || { echo "$name: missing ($file)"; return 1; }
  norm="$(normalize "$file")"

  if tr -d '\r' < "$file" | grep -q '\\$'; then echo "$name: a line ends in a backslash (continuation)"; bad=1; fi

  while IFS= read -r want; do
    [ -z "$want" ] || { echo "$name: required line missing: ${want#*|} (section ${want%%|*})"; bad=1; }
  done < <(required_for "$name" | grep -vxFf <(printf '%s\n' "$norm") || true)

  keys="$(awk -F'|' '{ rest = substr($0, index($0, "|") + 1); e = index(rest, "="); if (e == 0) { print "!no-equals:" rest; next } k = substr(rest, 1, e - 1); gsub(/[[:space:]]/, "", k); print k }' <<<"$norm")"
  while IFS= read -r key; do
    case "$key" in "!no-equals:"*) echo "$name: not a key=value line: ${key#!no-equals:}"; bad=1 ;; esac
  done <<<"$keys"
  while IFS= read -r key; do
    [ -z "$key" ] || { echo "$name: key appears more than once: $key"; bad=1; }
  done < <(sort <<<"$keys" | uniq -d)
  for key in $FORBIDDEN_KEYS; do
    if grep -qxF -- "$key" <<<"$keys"; then echo "$name: forbidden directive $key="; bad=1; fi
  done

  case "$name" in
    oarfish-update-helper.service) grep -q '^Install|' <<<"$norm" && { echo "$name: must not have an [Install] section (socket-activated)"; bad=1; } ;;
    oarfish-update-recover.service) grep -qxF -- 'RemainAfterExit' <<<"$keys" && { echo "$name: forbidden directive RemainAfterExit="; bad=1; } ;;
  esac

  # oarfish.service as a token of any directive value. The one allowed place
  # is the ordering of the recover unit.
  while IFS= read -r want; do
    [ -z "$want" ] && continue
    if [ "$name" = oarfish-update-recover.service ] && [ "$want" = "$RECOVER_BEFORE" ]; then continue; fi
    echo "$name: refers to oarfish.service: ${want#*|}"; bad=1
  done < <(grep -E '(=|[[:space:]])oarfish\.service([[:space:]]|$)' <<<"$norm" || true)
  return "$bad"
}

check_dir() {
  local dir="$1" unit status=0
  for unit in $UNITS; do
    check_unit "$unit" "$dir/$unit" || status=1
  done
  return "$status"
}

# Mutation test: the check must pass on the real files and fail on each of
# these changes of a copy.
self_test() {
  local src="$1" tmp name want section line key n=0 failed=0
  tmp="$(mktemp -d)"
  # Expanded now: tmp is local, and the trap runs after this function has returned.
  # shellcheck disable=SC2064
  trap "rm -rf '$tmp'" EXIT

  mutant() { # unit, description; the mutant is $tmp/m/<unit>
    n=$((n + 1))
    if check_unit "$1" "$tmp/m/$1" >"$tmp/out" 2>&1; then
      echo "self-test: NOT DETECTED: $2"; failed=1
    fi
  }
  fresh() { rm -rf "$tmp/m"; mkdir "$tmp/m"; cp "$src"/oarfish-update-*.s* "$tmp/m/"; }
  # Insert LINE after the header of SECTION in unit NAME of the mutant.
  insert() {
    awk -v sec="[$2]" -v add="$3" '{ print } $0 == sec { print add }' "$tmp/m/$1" > "$tmp/m/$1.new" && mv "$tmp/m/$1.new" "$tmp/m/$1"
  }

  fresh
  check_dir "$tmp/m" >"$tmp/out" 2>&1 || { cat "$tmp/out"; die "self-test: the check fails on the unmodified files"; }

  for name in $UNITS; do
    while IFS= read -r want; do
      section="${want%%|*}"; line="${want#*|}"
      fresh
      grep -vxF -- "$line" "$src/$name" > "$tmp/m/$name" || true
      mutant "$name" "$name without '$line'"
      # The line with a changed value (the weakened form).
      fresh
      awk -v l="$line" '$0 == l { print l "x"; next } { print }' "$src/$name" > "$tmp/m/$name"
      mutant "$name" "$name with '$line' changed"
    done < <(required_for "$name")
    for key in $FORBIDDEN_KEYS; do
      case "$name" in *.socket) section=Socket ;; *) section=Service ;; esac
      fresh; insert "$name" "$section" "$key=x"; mutant "$name" "$name with $key="
    done
    fresh; insert "$name" Unit 'After=oarfish.service'; mutant "$name" "$name with After=oarfish.service"
    fresh; insert "$name" Unit 'Before=oarfish.service'; mutant "$name" "$name with a second Before="
    fresh; insert "$name" Unit 'Requires=oarfish.service'; mutant "$name" "$name with Requires=oarfish.service"
    fresh; insert "$name" Unit 'Description=two'; mutant "$name" "$name with a second Description="
    fresh; printf 'Foo=bar \\\nbaz\n' >> "$tmp/m/$name"; mutant "$name" "$name with a continuation line"
  done
  fresh; insert oarfish-update-helper.service Service 'NoNewPrivileges=no'; mutant oarfish-update-helper.service "helper service: NoNewPrivileges=no after the required line"
  fresh; insert oarfish-update-helper.service Service 'User=oarfish'; mutant oarfish-update-helper.service "helper service: a second User="
  fresh; printf '[Install]\nWantedBy=multi-user.target\n' >> "$tmp/m/oarfish-update-helper.service"; mutant oarfish-update-helper.service "helper service with [Install]"
  fresh; insert oarfish-update-recover.service Service 'TimeoutStartSec=0'; mutant oarfish-update-recover.service "recover unit: a second TimeoutStartSec= after the required line"
  fresh; insert oarfish-update-recover.service Service 'RemainAfterExit=yes'; mutant oarfish-update-recover.service "recover unit with RemainAfterExit="
  fresh; sed 's/^Before=.*/Before=oarfish-update-helper.socket/' "$src/oarfish-update-recover.service" > "$tmp/m/oarfish-update-recover.service"; mutant oarfish-update-recover.service "recover unit without Before=oarfish.service"
  fresh; rm "$tmp/m/oarfish-update-helper.socket"; mutant oarfish-update-helper.socket "socket unit missing"

  [ "$failed" -eq 0 ] || die "self-test: a mutation was not detected"
  echo "check-units self-test: $n mutations, every one detected"
}

main() {
  case "${1:-}" in
    --self-test) self_test "$ROOT/install" ;;
    *)
      local dir="${1:-$ROOT/install}"
      if check_dir "$dir"; then
        echo "update-helper units OK: socket, service and recover unit keep their required directives and have no forbidden one"
      else
        die "update-helper units: see the problems above"
      fi
      ;;
  esac
}
main "$@"
