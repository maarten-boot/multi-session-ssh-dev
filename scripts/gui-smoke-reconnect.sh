#!/bin/bash
# Headless GUI test of the disconnect overlay and auto-reconnect (M5).
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-reconnect.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "local"
ssh_target = "localhost"
[[hosts]]
name = "unreachable"
ssh_target = "nohost.invalid"   # never resolves -> ssh exits 255 at once
[[sessions]]
id = "s-rok"
name = "ok"
host = "local"
local_path = "/tmp"
start_mode = "shell"
[[sessions]]
id = "s-rbad"
name = "bad"
host = "unreachable"
local_path = "/tmp"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep 0.5; }
t() { xdotool type --delay 10 "$1"; xdotool key Return; sleep ${2:-0.8}; }
shot() { import -window root $OUT/$1.png; }
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
for i in $(seq 40); do W=$(xdotool search --name "Session Workbench" 2>/dev/null | head -1); [ -n "$W" ] && break; sleep 0.25; done
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+alt+1; sleep 2; t 'export KEEP=survived'
pkill -f '^/usr/bin/ssh -t .*cw-s-rok'; sleep 0.4   # anchored: never matches this script's caller
shot 1-lost-reconnecting
sleep 3; t 'echo "after auto-reconnect: $KEEP"'
shot 2-reconnected
k ctrl+alt+2; sleep 9
shot 3-backoff                                  # attempts with growing delay
k Escape; sleep 0.5
shot 4-stopped
k ctrl+alt+1; t 'tmux detach' 1.5
shot 5-detached                                 # exit 0: no auto-reconnect
sleep 3; k Return; sleep 2.5; t 'echo "after Enter: $KEEP"'
shot 6-reattached
click 120 90 3; shot 7-menu; click 150 $((90+158))  # session menu: Disconnect
sleep 4
shot 8-disconnected                             # intentional: no auto-reconnect
kill $APP; wait $APP 2>/dev/null
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-rok'" 2>/dev/null
