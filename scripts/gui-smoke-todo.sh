#!/bin/bash
# Headless GUI test of the TODO marks: a session whose notes have a TODO
# line gets a red ▲ in the list, its Notes tab and the page in the tree;
# typing a TODO adds them, an edit outside the app that removes the last
# one takes them away (when the Notes tab is shown again).
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-todo.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT; rm -rf $OUT/notes
mkdir -p $OUT/notes/s-td1; printf 'Plans.\nTODO: check the logs\n' >$OUT/notes/s-td1/Home.md
. "$(dirname "$0")/smoke-lib.sh"
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-td1"
name = "with todo"
host = "local"
local_path = "/tmp"
start_mode = "shell"
[[sessions]]
id = "s-td2"
name = "without"
host = "local"
local_path = "/var"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep 0.6; }
shot() { import -window root $OUT/$1.png; }
# red <shot> <WxH+X+Y>: the area has pixels of the light theme's --danger (#b3261e).
red() { [ "$(convert $OUT/$1.png -crop $2 +repage -fuzz 12% -fill '#00ff00' -opaque '#b3261e' txt:- | grep -ci '#00ff00')" -gt 0 ]; }
plain() { ! red "$@"; }
ROW1=240x40+61+117; ROW2=240x40+61+160   # the two sessions in the list
NOTES_TAB=120x34+560+50                  # the tab bar around Notes
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 4; xdotool windowactivate --sync $W 2>/dev/null
shot 1-start
check "▲ on the session with a TODO" red 1-start $ROW1
check "no ▲ on the session without" plain 1-start $ROW2
check "▲ on its Notes tab" red 1-start $NOTES_TAB

# A TODO typed in the other session's notes.
k ctrl+alt+2; k ctrl+shift+4; sleep 1.5
shot 2-before; check "no ▲ on the Notes tab without TODOs" plain 2-before $NOTES_TAB
xdotool type --delay 10 'TODO: write the tests'; sleep 2
shot 3-typed
check "a typed TODO marks the session" red 3-typed $ROW2
check "and its Notes tab" red 3-typed $NOTES_TAB
check "and its page in the tree" red 3-typed 160x24+305+140

# The TODO done outside the app: gone once the Notes tab shows again.
printf 'Plans.\nDONE: check the logs\n' >$OUT/notes/s-td1/Home.md
k ctrl+alt+1; k ctrl+shift+1; k ctrl+shift+4; sleep 2
shot 4-done
check "the ▲ goes when the TODO is gone" plain 4-done $ROW1
check "also from its Notes tab" plain 4-done $NOTES_TAB
alive
kill $APP; wait $APP 2>/dev/null
for id in s-td1 s-td2; do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; done
finish
