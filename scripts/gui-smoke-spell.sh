#!/bin/bash
# Headless GUI test of check lists, bulleted lists and spelling in the notes:
# Enter continues a check list, Ctrl+Enter and a click tick boxes,
# Ctrl+Shift+8 makes a bullet, a right-click on a misspelled word offers
# corrections and "Add to dictionary" (saved as spell_words), Settings → Notes.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-spell.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT; rm -rf $OUT/notes
. "$(dirname "$0")/smoke-lib.sh"
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-sp1"
name = "spelling"
host = "local"
local_path = "/tmp"
start_mode = "shell"
TOML
N=$OUT/notes/s-sp1
k() { xdotool key "$@"; sleep 0.5; }
type() { xdotool type --delay 15 -- "$1"; }
shot() { import -window root $OUT/$1.png; }
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 0.8; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+shift+4; sleep 1
type "Teh first word"; k Return
type "- [ ] buy milk"; k Return                 # Enter continues the check list
type "call Bob"; k Return; k Return             # an empty item ends it
type "kubectl rocks "; k Return
type "plain line"; k ctrl+shift+8               # a bullet
sleep 1.5; shot 1-typed
# Window coordinates of the lines (13px font, line height 1.5): line 1 at y=113.
line() { echo $((113 + 19*($1-1))); }
click 457 $(line 3)                             # the box of "call Bob"
click 520 $(line 2); k ctrl+Return              # Ctrl+Enter ticks the cursor's line
click 445 $(line 1) 3; sleep 0.5; shot 2-suggestions; menu   # the first correction: "The"
click 450 $(line 4) 3; menu a                   # Add “kubectl” to dictionary
sleep 1.5; shot 3-fixed
expect "lists, ticks and corrections saved" $'The first word\n- [x] buy milk\n- [x] call Bob\nkubectl rocks \n- plain line' \
	"$(cat $N/Home.md 2>&1)"
expect "own word saved" 'spell_words = ["kubectl"]' "$(grep -E '^ *spell_words' $CFG | sed 's/^ *//')"
k ctrl+shift+comma; sleep 0.8
click 810 109; shot 4-settings-notes            # the Notes page: the word is listed
alive
kill $APP; wait $APP 2>/dev/null
finish
