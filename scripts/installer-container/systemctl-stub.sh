#!/bin/sh
# systemctl stand-in for containers that have no systemd. "start" and "restart"
# run the installed binary as the oarfish user with exactly the command line of
# the installed unit's ExecStart (so unit and binary cannot drift apart: an
# argument the binary does not know makes it exit and the health check fail),
# minus everything systemd adds (sandboxing, restart policy, boot). The one
# thing it does emulate is StateDirectory=: systemd passes STATE_DIRECTORY, and
# the binary refuses to start without a state directory.
S=/tmp/fake-systemd
mkdir -p "$S"
echo "$*" >> "$S/calls.log"
UNIT="$FAKE_ROOT/etc/systemd/system/oarfish.service"

running() { [ -f "$S/pid" ] && kill -0 "$(cat "$S/pid")" 2>/dev/null; }
stop_proc() {
  if running; then
    kill "$(cat "$S/pid")"
    # Emulated processes can take a while to exit; userdel needs them gone.
    i=0
    while running && [ "$i" -lt 40 ]; do sleep 0.5; i=$((i + 1)); done
  fi
  rm -f "$S/pid"
}
start_proc() {
  line="$(sed -n 's/^ExecStart=//p' "$UNIT")"
  # shellcheck disable=SC2086 # splitting the command line into words is the point
  set -- $line
  exe="$1"
  shift
  if [ "$exe" != /opt/oarfish/current/oarfish ]; then echo "unexpected ExecStart binary: $exe" >&2; exit 1; fi
  echo "ExecStart arguments: $*" >> "$S/calls.log"
  grep -q '^StateDirectory=oarfish$' "$UNIT" || { echo "the unit has no StateDirectory=oarfish" >&2; exit 1; }
  STATE_DIRECTORY="$FAKE_ROOT/var/lib/oarfish" setpriv --reuid=oarfish --regid=oarfish --clear-groups "$FAKE_ROOT$exe" "$@" </dev/null >"$S/oarfish.log" 2>&1 &
  echo $! > "$S/pid"
}

case "$1" in
  is-enabled) [ -f "$S/enabled" ] ;;
  enable) : > "$S/enabled" ;;
  disable) rm -f "$S/enabled" ;;
  is-active) running ;;
  start) running || start_proc ;;
  restart) stop_proc; start_proc ;;
  stop) stop_proc ;;
  cat) [ -f "$FAKE_ROOT/etc/systemd/system/oarfish.service" ] ;;
  daemon-reload) ;;
  *) exit 1 ;;
esac
