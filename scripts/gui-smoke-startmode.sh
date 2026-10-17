#!/bin/bash
# Headless GUI test of changing a session's start mode while its remote tmux
# session runs: the app offers to restart it, and the Remote tab comes back in
# a new tmux session started with the new mode ("claude --continue || claude").
# Needs `ssh localhost` without prompts and tmux; claude need not be installed.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-startmode.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
D=$OUT/project; mkdir -p $D
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-mode"
name = "mode"
host = "local"
local_path = "$D"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep 0.6; }
t() { xdotool type --delay 10 "$1"; xdotool key Return; sleep ${2:-1}; }
shot() { import -window root $OUT/$1.png; }
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 0.8; }
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }
started() { ssh -o BatchMode=yes localhost "tmux display -p -t '=cw-s-mode:' '#{pane_start_command}'" 2>/dev/null; }
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-mode'" 2>/dev/null
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+shift+3; sleep 2.5                       # Remote tab: tmux session in shell mode
t 'echo "first: $(tmux display -p "#S")"; export MARK=old'
check "first tmux session runs a shell" grep -qF 'SHELL\" -l' <<<"$(started)"
shot 1-shell
click 120 90 3; menu                            # session menu: Edit… (first item, focused)
k Tab; k Tab; k Up                              # Name -> Folder -> Start: claude --continue
k shift+Tab; k shift+Tab; k Return; sleep 1     # back to Name, save
shot 2-restart-offered
expect "new start mode saved" "claude-continue" "$(field $CFG mode 4)"
k Return; sleep 5                               # Restart now
shot 3-restarted
check "the new tmux session starts claude --continue, else claude" grep -qF 'claude --continue || claude' <<<"$(started)"
alive
kill $APP; wait $APP 2>/dev/null
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-mode'" 2>/dev/null
finish
