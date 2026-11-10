#!/bin/bash
# Headless GUI test of the sidebar's headings (SPEC F5): with folders, the
# sessions in no folder are under Hosts, the folders under Groups; Active
# and Inactive list every session again by name (collapsed at first, no
# numbers; the balloon adds host and folder). A row there opens the
# session; its dot and marks follow the session like the row above.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-lists.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
P=s-ls$$
mkdir -p $OUT/top $OUT/work $OUT/client $OUT/private
CFG=$OUT/config.toml
cat >$CFG <<TOML
groups = ["Work", "Work/Client A", "Private"]
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "${P}1"
name = "zeta top"
host = "local"
local_path = "$OUT/top"
start_mode = "shell"
[[sessions]]
id = "${P}2"
name = "alpha work"
host = "local"
local_path = "$OUT/work"
start_mode = "shell"
group = "Work"
[[sessions]]
id = "${P}3"
name = "client old"
host = "local"
local_path = "$OUT/client"
start_mode = "shell"
group = "Work/Client A"
inactive = true
[[sessions]]
id = "${P}4"
name = "beta private"
host = "local"
local_path = "$OUT/private"
start_mode = "shell"
group = "Private"
TOML
k() { xdotool key "$@"; sleep 0.6; }
t() { xdotool type --delay 8 "$1"; xdotool key Return; sleep ${2:-1}; }
shot() { import -window root $OUT/$1.png; }
cleanup() { for i in 1 2 3 4; do tmux kill-session -t "=cw-$P$i" 2>/dev/null; done; }
trap cleanup EXIT
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null; sleep 2
shot 1-start
xdotool mousemove 100 573 click 1; sleep 0.8               # open Active
shot 2-active
xdotool mousemove 110 646 click 1; sleep 2                 # "beta private" in Active
k ctrl+shift+1; sleep 1.5; t 'pwd >$SW_OUT/opened'
xdotool mousemove 110 650; sleep 2
shot 3-balloon
xdotool mousemove 700 400; sleep 0.5
xdotool mousemove 100 738 click 1; sleep 0.8               # open Inactive
xdotool mousemove 700 400; sleep 0.5                       # (its tooltip would take the click)
shot 4-inactive
xdotool mousemove 110 768 click 1; sleep 1.5               # "client old" there
shot 5-inactive-session
xdotool mousemove 403 189 click 1; sleep 2                 # Activate session
shot 6-activated
check "the row in Inactive opened it: Activate session saved" sh -c "! grep -q 'inactive = true' $CFG"
alive
kill $APP; wait $APP 2>/dev/null
$BIN -config $CFG >>$OUT/app.log 2>&1 &                    # opened headings stay open
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null; sleep 2
shot 7-restart
expect "a row in Active opens its session" $OUT/private "$(cat $SW_OUT/opened 2>&1)"
alive
kill $APP; wait $APP 2>/dev/null
finish
