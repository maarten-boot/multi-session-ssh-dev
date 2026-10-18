#!/bin/bash
# Headless GUI test of the remembered window size: first start 1280x800,
# resized and closed (SIGTERM closes the window the normal way), the next
# start comes back at that size; a bad window.json gives the default.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-window.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-win"
name = "window"
host = "local"
local_path = "/tmp"
start_mode = "shell"
TOML
STATE=$XDG_DATA_HOME/session-workbench/window.json
size() { xdotool getwindowgeometry --shell $W | awk -F= '/^WIDTH/{w=$2} /^HEIGHT/{h=$2} END{print w "x" h}'; }
start() { $BIN -config $CFG >>$OUT/app.log 2>&1 & APP=$!; wait_window; sleep 3; }
# close: SIGTERM, then the app must exit by itself.
close() {
	kill -TERM $APP
	for i in $(seq 40); do kill -0 $APP 2>/dev/null || break; sleep 0.25; done
	if kill -0 $APP 2>/dev/null; then fail "$1: app exits on SIGTERM"; kill -9 $APP; else ok "$1: app exits on SIGTERM"; fi
	wait $APP 2>/dev/null
}

start
expect "first start: default size" 1280x800 "$(size)"
xdotool windowsize --sync $W 1000 650; sleep 1
expect "resized" 1000x650 "$(size)"
close "resized"
check "window.json written" test -s $STATE
expect "window.json" '{"width":1000,"height":650,"maximised":false}' "$(cat $STATE 2>&1)"

start
expect "restart: remembered size" 1000x650 "$(size)"
import -window root $OUT/1-remembered.png
close "remembered"

echo '{"width":50,"height":50}' >$STATE
start
expect "too small in window.json: default size" 1280x800 "$(size)"
close "bad file"
finish
