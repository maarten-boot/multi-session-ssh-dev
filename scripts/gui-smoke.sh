#!/bin/bash
# Headless GUI smoke test: drives the app with xdotool and saves screenshots.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke.sh build/bin/session-workbench <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
# Throwaway config: one session whose Local tab we test (never the user's config).
cat >$OUT/config.toml <<TOML
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-smoke"
name = "smoke"
host = "local"
local_path = "$HOME"
start_mode = "shell"
TOML
$BIN -config $OUT/config.toml >$OUT/app.log 2>&1 &
APP=$!
for i in $(seq 40); do W=$(xdotool search --name "Session Workbench" 2>/dev/null | head -1); [ -n "$W" ] && break; sleep 0.25; done
sleep 3
xdotool windowactivate --sync $W 2>/dev/null; xdotool windowfocus $W
xdotool key ctrl+shift+2; sleep 2     # Local tab: terminal fidelity without ssh
t() { xdotool type --delay 15 "$1"; xdotool key Return; sleep ${2:-0.8}; }
t 'printf "\e[1;31mred \e[32mgreen \e[38;2;255;128;0mtruecolor\e[0m  utf8: héllo ✓ 日本\n"; stty size'
t 'ls --color=always /'
import -window root $OUT/1-colors.png
t 'vi /etc/hostname' 2.5
import -window root $OUT/2-vi.png
xdotool key Escape; sleep 0.3; xdotool type ':qa!'; xdotool key Return; sleep 1
xdotool windowsize $W 800 500; sleep 1.5
t 'clear; stty size'
import -window root $OUT/3-resized.png
t 'exit' 1.5
import -window root $OUT/4-exited.png
xdotool key Return; sleep 1.5
t 'echo restarted-ok'
import -window root $OUT/5-restarted.png
kill $APP; wait $APP 2>/dev/null
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-smoke'" 2>/dev/null
exit 0
