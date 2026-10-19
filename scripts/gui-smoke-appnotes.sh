#!/bin/bash
# Headless GUI test of the notes page for all sessions and of copy/paste in
# the terminals: the notes from before pages existed (notes/_app.md) are the
# Prompts page now, a "## name" prompt added there (Ctrl+Shift+M) is saved to
# notes/_app/Prompts.md and "Paste into Remote" puts it into the Remote tab
# without running it; Ctrl+C copies a selection, Ctrl+V pastes, Ctrl+C
# without a selection still interrupts; the right-click menu pastes.
# Needs `ssh localhost` without prompts and tmux.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-appnotes.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT; rm -rf $OUT/notes
mkdir -p $OUT/notes; printf '## Old\necho old-prompt\n\n' >$OUT/notes/_app.md
. "$(dirname "$0")/smoke-lib.sh"
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-apn"
name = "prompts"
host = "local"
local_path = "/tmp"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep 0.6; }
t() { xdotool type --delay 10 "$1"; xdotool key Return; sleep ${2:-0.8}; }
shot() { import -window root $OUT/$1.png; }
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 0.8; }
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null

# The notes page opens on the Prompts page (the old notes); a second
# prompt, saved, then pasted into the Remote tab.
k ctrl+shift+m; sleep 1
k ctrl+End
xdotool type --delay 10 '## Answer'; xdotool key Return
xdotool type --delay 10 'echo $((6*7))-pasted'; sleep 1.5
shot 1-notes-page
check "old notes moved to the Prompts page" test ! -e $OUT/notes/_app.md
expect "prompt saved to notes/_app/Prompts.md" $'## Old\necho old-prompt\n\n## Answer\necho $((6*7))-pasted' "$(cat $OUT/notes/_app/Prompts.md 2>&1)"
click 1070 222                                   # Paste into Remote (second prompt)
sleep 4
shot 2-pasted
check "pasted into the Remote tab" grep -qF 'echo $((6*7))-pasted' <<<"$(pane s-apn)"
check "not run until Enter" test -z "$(pane s-apn | grep -F '42-pasted')"
xdotool key Return; sleep 1
in_pane "Enter runs it" s-apn "42-pasted"

# Copy and paste in a terminal (the Local tab).
k ctrl+shift+1; sleep 1.5
t 'clear; echo copy-me-12345'
xdotool mousemove 350 96 click --repeat 2 1; sleep 0.5    # double-click the word in the first line (screen coordinates)
k ctrl+c                                         # with a selection: copies
xdotool type --delay 10 'echo "got=['; k ctrl+v; xdotool type --delay 10 ']" > $SW_OUT/ctrlv'; xdotool key Return; sleep 0.8
expect "Ctrl+C copied the selection, Ctrl+V pasted it" "got=[copy-me-12345]" "$(cat $SW_OUT/ctrlv 2>&1)"
t 'sleep 30' 0.5
k ctrl+c                                         # no selection: interrupts
t 'echo interrupted > $SW_OUT/intr' 1
expect "Ctrl+C without a selection still interrupts" interrupted "$(cat $SW_OUT/intr 2>&1)"
# The right-click menu: Paste.
xdotool type --delay 10 'echo "menu=['
xdotool mousemove 700 500 click 3; sleep 0.6; menu p   # Copy (disabled), Paste
xdotool type --delay 10 ']" > $SW_OUT/menu'; xdotool key Return; sleep 0.8
shot 3-terminal
expect "the right-click menu pastes" "menu=[copy-me-12345]" "$(cat $SW_OUT/menu 2>&1)"
alive
kill $APP; wait $APP 2>/dev/null
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-apn'" 2>/dev/null
finish
