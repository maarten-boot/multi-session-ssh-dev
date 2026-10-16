#!/bin/bash
# Headless GUI test of themes and color modes (M5): the classic unreadable
# colors on white, in a "full" and a "mono" session, plus a live switch.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-colors.sh <binary> <outdir> [theme]
BIN=$1; OUT=$2; THEME=${3:-light}; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
CFG=$OUT/config.toml
cat >$CFG <<TOML
[ui]
theme = "$THEME"
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-full"
name = "full colors"
host = "local"
local_path = "/usr"
start_mode = "shell"
[[sessions]]
id = "s-mono"
name = "mono session"
host = "local"
local_path = "/usr"
start_mode = "shell"
color_mode = "mono"
TOML
k() { xdotool key "$@"; sleep 0.5; }
t() { xdotool type --delay 8 "$1"; xdotool key Return; sleep ${2:-1}; }
shot() { import -window root $OUT/$1.png; }
# menu c1 c2 ...: pick a context-menu item by typing first letters, then Enter.
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 0.8; }
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }
TEST='clear; printf "\e[33myellow\e[0m \e[92mbright-green\e[0m \e[93mbright-yellow\e[0m \e[38;5;226m256-yellow\e[0m \e[38;2;255;255;0mtruecolor-yellow\e[0m \e[43;30m yellow-bg \e[0m \e[1mbold\e[0m \e[1;37mbold-white\e[0m \e[4munderline\e[0m \e[7minverse\e[0m\n"; ls --color=always /usr/bin | head -c 600; echo'

$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+alt+1; k ctrl+shift+1; sleep 1.5; t "$TEST" 1.5
shot 1-full
k ctrl+alt+2; k ctrl+shift+1; sleep 1.5; t "$TEST" 1.5
shot 2-mono
k ctrl+alt+1; sleep 0.5
click 120 90 3; menu u                         # session menu: "Use mono colors"
sleep 1; shot 3-switched-live                  # old output neutralized
k ctrl+shift+1; t "$TEST" 1.5
shot 4-switched-new-output
k ctrl+shift+3; sleep 2.5
shot 5-remote-tmux
alive
check "Remote tab started tmux" has_tmux s-full
kill $APP; wait $APP 2>/dev/null
expect "\"Use mono colors\" saved for the session" "mono" "$(field $CFG "full colors" 6)"
expect "the mono session kept its mode" "mono" "$(field $CFG "mono session" 6)"
for id in s-full s-mono; do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; done
finish
