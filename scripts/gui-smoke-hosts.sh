#!/bin/bash
# Headless GUI test from an empty config: empty state, add-host dialog,
# session context menu, move, delete with "end tmux session".
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-hosts.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
CFG=$OUT/config.toml; rm -f $CFG
typ() { xdotool type --delay 12 "$1"; }
k() { xdotool key "$@"; sleep 0.6; }
shot() { import -window root $OUT/$1.png; }
# Window geometry: 1280x800 centred on 1400x900 -> origin (60,50).
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }

$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
for i in $(seq 40); do W=$(xdotool search --name "Session Workbench" 2>/dev/null | head -1); [ -n "$W" ] && break; sleep 0.25; done
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
shot 01-empty
click 760 420                          # "Add host…" in the empty state
typ "local"; k Tab; typ "localhost"; k Return   # no prefixes: same paths
sleep 1; shot 02-host-added
for d in /tmp /var /usr; do
	k ctrl+shift+n; typ "$d"; sleep 0.4; k Tab; k Tab; k Tab; k s; k shift+Tab; k Return; sleep 2.5
done
shot 03-three
# Rows are at y=90/133/176; the menu opens at the pointer, items at +20/+50/+80/+119/+150/+185.
click 120 90 3                         # right-click first session (tmp)
shot 04-menu
click 150 170                          # "Move down" -> var, tmp, usr
sleep 1; shot 05-moved
click 120 133 3                        # right-click the second row (now tmp)
click 150 318                          # "Delete session…"
sleep 0.5; k space                     # the checkbox has focus: tick "also end tmux session"
shot 06-delete-dialog
k Tab; k Return; sleep 3               # Delete
shot 07-deleted
kill $APP; wait $APP 2>/dev/null
cp $CFG $OUT/config-after.toml
# Clean up the tmux sessions this test created on localhost.
for id in $(sed -n 's/^ *id = "\(.*\)"/\1/p' $CFG); do
	ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null
done
