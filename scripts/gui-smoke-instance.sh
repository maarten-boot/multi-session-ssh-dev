#!/bin/bash
# Headless GUI test of the one-instance-per-config guard: a second start with
# the same config exits (D-Bus) or, without D-Bus, only shows a message and
# leaves the config alone; another config may run alongside.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-instance.sh <binary> <outdir>
# Needs a session bus for the first part: always its own private one (the
# test library otherwise points the tests at an unreachable bus).
if [ -z "$SW_IN_DBUS" ]; then
	SW_IN_DBUS=1 exec dbus-run-session -- "$0" "$@"
fi
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-inst"
name = "instance"
host = "local"
local_path = "/tmp"
start_mode = "shell"
TOML
cp $CFG $OUT/other.toml
windows() { xdotool search --name "Session Workbench" 2>/dev/null | wc -l; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3
BEFORE=$(stat -c %Y $CFG)
$BIN -config $CFG >>$OUT/second.log 2>&1 &
SECOND=$!
for i in $(seq 40); do kill -0 $SECOND 2>/dev/null || break; sleep 0.25; done
if kill -0 $SECOND 2>/dev/null; then fail "second start with D-Bus exits"; kill $SECOND; else ok "second start with D-Bus exits"; fi
expect "still one window" 1 "$(windows)"
# An unreachable bus (an empty address would fall back to the user bus).
DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent $BIN -config $CFG >>$OUT/third.log 2>&1 &
THIRD=$!
sleep 5
import -window root $OUT/1-without-dbus.png
check "without D-Bus the second instance stays to show why" kill -0 $THIRD
expect "and has its own window" 2 "$(windows)"
kill $THIRD; wait $THIRD 2>/dev/null; sleep 1
expect "config untouched by the second instances" "$BEFORE" "$(stat -c %Y $CFG)"
$BIN -config $OUT/other.toml >>$OUT/other.log 2>&1 &
OTHER=$!
sleep 5
check "another config runs alongside" kill -0 $OTHER
expect "two windows" 2 "$(windows)"
kill $OTHER; wait $OTHER 2>/dev/null
alive
kill $APP; wait $APP 2>/dev/null
finish
