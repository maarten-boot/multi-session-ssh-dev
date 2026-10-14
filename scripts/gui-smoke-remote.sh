#!/bin/bash
# Headless GUI test of the Remote tab: ssh -> tmux, tab switching, survival
# of the tmux session across ssh drops and app restarts.
# usage: xvfb-run -a -s "-screen 0 1280x860x24" scripts/gui-smoke-remote.sh <binary> <outdir> <app args...>
# The app must be started with -mode shell and an -ssh target reachable without prompts.
BIN=$1; OUT=$2; shift 2; mkdir -p $OUT
start() {
	$BIN "$@" >>$OUT/app.log 2>&1 &
	APP=$!
	for i in $(seq 40); do W=$(xdotool search --name "Session Workbench" 2>/dev/null | head -1); [ -n "$W" ] && break; sleep 0.25; done
	sleep 3
	xdotool windowactivate --sync $W 2>/dev/null; xdotool windowfocus $W
}
stop() { kill $APP; wait $APP 2>/dev/null; }
t() { xdotool type --delay 15 "$1"; xdotool key Return; sleep ${2:-1}; }
shot() { import -window root $OUT/$1.png; }

start "$@"
t 'echo "remote: $(tmux display -p "#S") in $(pwd)"; export KEEP=state-kept'
shot 1-remote
xdotool key ctrl+shift+2; sleep 2
t 'echo "local: $(pwd) TMUX=${TMUX:-none}"'
shot 2-local
xdotool key ctrl+shift+1; sleep 1
pkill -f 'ssh -t .*new-session -A -s cw-' ; sleep 1.5
shot 3-dropped
xdotool key Return; sleep 3
t 'echo "after reconnect: $KEEP"'
shot 4-reconnected
stop
sleep 1
start "$@"
t 'echo "after app restart: $KEEP"'
shot 5-restarted
t 'tmux kill-session' 1.5   # clean up the remote session
shot 6-killed
stop
