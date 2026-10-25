#!/bin/sh
# systemctl stand-in for containers that have no systemd. "start" and "restart"
# run the installed oarfish binary as the oarfish user, which is what the unit
# would do, minus everything systemd adds (sandboxing, restart policy, boot).
S=/tmp/fake-systemd
mkdir -p "$S"
echo "$*" >> "$S/calls.log"
BIN="$FAKE_ROOT/opt/oarfish/current/oarfish"

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
  setpriv --reuid=oarfish --regid=oarfish --clear-groups "$BIN" -listen 127.0.0.1:8484 </dev/null >"$S/oarfish.log" 2>&1 &
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
