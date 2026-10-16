#!/bin/bash
# Headless GUI test from an empty config: empty state, add-host dialog,
# session context menu, move, delete with "end tmux session".
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-hosts.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
CFG=$OUT/config.toml; rm -f $CFG
typ() { xdotool type --delay 12 "$1"; }
k() { xdotool key "$@"; sleep 0.6; }
shot() { import -window root $OUT/$1.png; }
# menu c1 c2 ...: pick a context-menu item by typing first letters, then Enter.
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 0.8; }
# Window geometry: 1280x800 centred on 1400x900 -> origin (60,50).
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }

$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
shot 01-empty
click 760 420                          # "Add host…" in the empty state
typ "local"; k Tab; typ "localhost"; k Return   # no prefixes: same paths
sleep 1; shot 02-host-added
for d in /tmp /var /usr; do
	k ctrl+shift+n; typ "$d"; sleep 0.4; k Tab; k Tab; k Tab; k Tab; k s; k shift+Tab; k shift+Tab; k Return; sleep 2.5
done
shot 03-three
expect "three sessions, in order, start mode shell" "/tmp shell|/var shell|/usr shell|" \
	"$(sessions $CFG | awk -F'\t' '{printf "%s %s|", $3, $4}')"
# New sessions open on Local, so give tmp a tmux session for "end tmux session" to end.
TMPID=$(field $CFG tmp 1)
ssh -o BatchMode=yes localhost "tmux new-session -d -s 'cw-$TMPID'"
# Rows are at y=90/133/176; menu items are picked from the keyboard.
click 120 90 3                         # right-click first session (tmp)
shot 04-menu
menu m m                               # "Move down" -> var, tmp, usr
sleep 1; shot 05-moved
click 120 133 3                        # right-click the second row (now tmp)
menu d d                               # Disconnect, Delete session…
sleep 0.5; k space                     # the checkbox has focus: tick "also end tmux session"
shot 06-delete-dialog
k Tab; k Return; sleep 3               # Delete
shot 07-deleted
alive
kill $APP; wait $APP 2>/dev/null
cp $CFG $OUT/config-after.toml
expect "host added" 'name = "local"|ssh_target = "localhost"|' "$(grep -A2 '^\[\[hosts\]\]' $CFG | sed -n 's/^ *//; 2,3p' | tr '\n' '|')"
expect "moved down, then deleted" "var usr" "$(sessions $CFG | awk -F'\t' '{print $2}' | xargs)"
if has_tmux "$TMPID"; then fail "deleting ended the tmux session"; else ok "deleting ended the tmux session"; fi
# Clean up the tmux sessions this test created on localhost.
for id in $(sed -n 's/^ *id = "\(.*\)"/\1/p' $CFG); do
	ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null
done
finish
