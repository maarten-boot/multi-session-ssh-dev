#!/bin/bash
# Headless GUI test of terminal extras: find bar (search addon), Unicode 11
# widths for emoji, copy-on-select.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-terminal-extras.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
CFG=$OUT/config.toml
cat >$CFG <<TOML
[ui]
copy_on_select = true
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-extra"
name = "extras"
host = "local"
local_path = "/tmp"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep 0.5; }
t() { xdotool type --delay 8 "$1"; xdotool key Return; sleep ${2:-0.8}; }
shot() { import -window root $OUT/$1.png; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+shift+1; sleep 1.5
# Typed as ASCII octal escapes (xdotool mangles non-ASCII): two emoji, two
# CJK characters and "abcd" are 4 cells each, so the bars must line up.
t 'clear; seq 1 60 | sed "s/^/line-/"; printf "\360\237\230\200\360\237\232\200|emoji\n\346\227\245\346\234\254|cjk\nabcd|ascii\n"' 1.5
shot 1-unicode
k ctrl+shift+f; xdotool type --delay 30 "line-17"; sleep 0.6
shot 2-find                                      # match highlighted, scrolled into view
k Escape; sleep 0.3
# Copy on select: double-click a word in the last output line, then paste it.
xdotool mousemove 322 140 click --repeat 2 1; sleep 0.6   # on "line-N" near the top
k End
xdotool type --delay 8 'echo "pasted=['; k ctrl+shift+v; xdotool type --delay 8 ']" | tee $SW_OUT/pasted'; xdotool key Return; sleep 0.8
shot 3-copy-on-select
alive
kill $APP; wait $APP 2>/dev/null
check "copy on select: a double-clicked word is pasted ($(cat $SW_OUT/pasted 2>&1))" grep -qxE 'pasted=\[line-[0-9]+\]' $SW_OUT/pasted
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-extra'" 2>/dev/null
finish
