#!/bin/bash
# Headless GUI test of private shell history: a session's Local and Remote
# shells get their own history file (HISTFILE; on the agent host in
# ~/.local/state/session-workbench/history) and write to it; a session with
# history = "off" keeps the shared one. Needs `ssh localhost` without
# prompts, tmux and bash.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-history.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-hist"
name = "own history"
host = "local"
local_path = "$OUT"
start_mode = "shell"
[[sessions]]
id = "s-shared"
name = "shared history"
host = "local"
local_path = "/tmp"
start_mode = "shell"
history = "off"
TOML
REMOTE_HIST=$HOME/.local/state/session-workbench/history/s-hist
rm -f "$REMOTE_HIST"
k() { xdotool key "$@"; sleep 0.5; }
t() { xdotool type --delay 10 "$1"; xdotool key Return; sleep ${2:-1}; }
for id in s-hist s-shared; do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; done
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
# Session 1, Local tab.
k ctrl+alt+1; k ctrl+shift+1; sleep 1.5
t 'echo "local-marker-$((40+2))"; history -a; echo "$HISTFILE" >$SW_OUT/local-histfile'
# Session 1, Remote tab.
k ctrl+shift+3; sleep 4
t 'echo "remote-marker-$((40+2))"; history -a; echo "$HISTFILE" >'"$OUT"'/remote-histfile' 1.5
# Session 2 (history off), Local tab.
k ctrl+alt+2; k ctrl+shift+1; sleep 1.5
t 'echo "$HISTFILE" >$SW_OUT/shared-histfile'
alive
kill $APP; wait $APP 2>/dev/null
LOCAL_HIST=$XDG_DATA_HOME/session-workbench/history/s-hist.history
expect "Local tab: the session's own history file" "$LOCAL_HIST" "$(cat $SW_OUT/local-histfile 2>&1)"
check "Local tab: commands go into it" grep -q 'local-marker-$((40+2))' "$LOCAL_HIST"
expect "Remote tab: the session's own history file on the agent host" "$REMOTE_HIST" "$(cat $OUT/remote-histfile 2>&1)"
check "Remote tab: commands go into it" grep -q 'remote-marker-$((40+2))' "$REMOTE_HIST"
check "history off: the shared history" test "$(cat $SW_OUT/shared-histfile 2>&1)" != "$XDG_DATA_HOME/session-workbench/history/s-shared.history"
for id in s-hist s-shared; do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; done
rm -f "$REMOTE_HIST"
finish
