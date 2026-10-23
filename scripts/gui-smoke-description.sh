#!/bin/bash
# Headless GUI test of a session's description: the balloon on the session
# in the list shows after description_delay (not before), goes when the
# mouse leaves; Edit… saves a new description to config.toml.
# Needs `ssh localhost` without prompts and tmux.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-description.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
CFG=$OUT/config.toml
cat >$CFG <<TOML
[ui]
description_delay = 1.5
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-desc"
name = "described"
description = "Fixing the parser.\nSee issue 42 first."
host = "local"
local_path = "/tmp"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep 0.6; }
shot() { import -window root $OUT/$1.png; }
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 0.8; }
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }
# area <shot>: the part of the screen right of the sidebar where the balloon goes.
area() { convert $OUT/$1.png -crop 500x200+305+112 +repage $OUT/$1-area.png; }
same() { [ "$(compare -metric AE $OUT/$1-area.png $OUT/$2-area.png null: 2>&1 | cut -d' ' -f1)" = 0 ]; }  # "0" or "0 (0)"
differs() { ! same "$@"; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 5; xdotool windowactivate --sync $W 2>/dev/null   # the Local tab's prompt is up

shot 0-before; area 0-before
xdotool mousemove 160 124; sleep 0.7       # on the session, before the delay
shot 1-early; area 1-early
sleep 1.5                                   # after it
shot 2-balloon; area 2-balloon
xdotool mousemove 700 600; sleep 0.5        # away
shot 3-gone; area 3-gone
check "no balloon before the delay" same 0-before 1-early
check "the balloon after the delay" differs 0-before 2-balloon
check "the balloon goes when the mouse leaves" same 0-before 3-gone

# Edit…: the description field follows the name; Return in the name saves.
click 100 74 3; menu                        # Edit… (focused first)
k Tab; k ctrl+a
xdotool type --delay 10 'New text.'; k Return
xdotool type --delay 10 'Second line.'
k shift+Tab; shot 4-edit; k Return; sleep 1
check "Edit… saves the description" grep -qF 'description = "New text.\nSecond line."' $CFG
alive
kill $APP; wait $APP 2>/dev/null
finish
