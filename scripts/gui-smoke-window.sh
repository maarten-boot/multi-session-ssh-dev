#!/bin/bash
# Headless GUI test of the remembered window size and position: first start
# 1280x800, resized, moved and closed (SIGTERM closes the window the normal
# way), the next start comes back there; a bad size or a position off every
# screen in window.json is not used.
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
pos() { xdotool getwindowgeometry --shell $W | awk -F= '/^X/{x=$2} /^Y/{y=$2} END{print x "," y}'; }
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
xdotool windowmove --sync $W 230 140; sleep 1
expect "moved" 230,140 "$(pos)"
close "resized and moved"
check "window.json written" test -s $STATE
expect "window.json" '{"width":1000,"height":650,"pos":true,"x":230,"y":140,"maximised":false}' "$(cat $STATE 2>&1)"

start
expect "restart: remembered size" 1000x650 "$(size)"
expect "restart: remembered position" 230,140 "$(pos)"
import -window root $OUT/1-remembered.png
close "remembered"

echo '{"width":1000,"height":650,"pos":true,"x":5000,"y":140}' >$STATE
start
P=$(pos)
if [ "${P%,*}" -lt 1300 ]; then ok "position off the screen not used ($P)"; else fail "position off the screen used: $P"; fi
close "off screen"

echo '{"width":50,"height":50}' >$STATE
start
expect "too small in window.json: default size" 1280x800 "$(size)"
close "bad file"
finish
