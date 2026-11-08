#!/bin/bash
# Headless GUI test of inactive sessions (SPEC F15): with connect_on_start
# = eager the active session connects and the inactive one does not; opening
# the inactive one (Local and Remote tabs) starts nothing; its notice's
# Activate button connects it; session menu → Deactivate session… with the
# tmux tick ends its tmux session and marks it inactive in config.toml; after
# a restart it still connects nothing; Deactivate without the tick leaves
# tmux running; Activate session from the menu.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-inactive.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
A=s-ina$$; B=s-inb$$
mkdir -p $OUT/p1 $OUT/p2
CFG=$OUT/config.toml
cat >$CFG <<TOML
[ui]
connect_on_start = "eager"
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "$A"
name = "working"
host = "local"
local_path = "$OUT/p1"
start_mode = "shell"
[[sessions]]
id = "$B"
name = "parked"
host = "local"
local_path = "$OUT/p2"
start_mode = "shell"
inactive = true
TOML
k() { xdotool key "$@"; sleep 0.6; }
shot() { import -window root $OUT/$1.png; }
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 1.5; }
has_tmux() { tmux has-session -t "=cw-$1" 2>/dev/null; }
no_tmux() { ! has_tmux "$1"; }
# shells_in <dir>: the app's child processes (Local shells, ssh) working in dir.
shells_in() { for p in $(pgrep -P $APP); do readlink /proc/$p/cwd; done 2>/dev/null | grep -cx "$1"; }
active_in_file() { ! inactive_in_file "$1"; }
inactive_in_file() { awk -v id="$1" '/^\[\[sessions\]\]/{s=0} $0 ~ "id = \""id"\"" {s=1} s && /inactive = true/ {f=1} END {exit !f}' $CFG; }
cleanup() { tmux kill-session -t "=cw-$A" 2>/dev/null; tmux kill-session -t "=cw-$B" 2>/dev/null; }
trap cleanup EXIT
start() { $BIN -config $CFG >>$OUT/app.log 2>&1 & APP=$!; wait_window; sleep 3; xdotool windowactivate --sync $W 2>/dev/null; sleep 5; }

start
shot 1-start
check "eager: the active session connected" has_tmux $A
check "eager: the inactive session did not" no_tmux $B
xdotool mousemove 114 175 click 1; sleep 3            # open "parked": Local tab
shot 2-parked-local
k ctrl+shift+3; sleep 3                                # its Remote tab
shot 3-parked-remote
check "opening it started no tmux session" no_tmux $B
expect "and no Local shell in its directory" 0 "$(shells_in $OUT/p2)"
k Return; sleep 6                                      # the notice's Activate session (focused)
shot 4-activated
check "Activate session: it connects" has_tmux $B
check "and config.toml no longer says inactive" active_in_file $B
xdotool mousemove 114 175 click 3; menu d d            # Disconnect, Deactivate session…
shot 5-deactivate-dialog
k space; k Tab; k Return; sleep 3                      # tick: end its tmux session; Deactivate
shot 6-deactivated
check "Deactivate with the tick ended its tmux session" no_tmux $B
check "config.toml marks it inactive" inactive_in_file $B
check "the active one is untouched" has_tmux $A
alive
kill $APP; wait $APP 2>/dev/null; sleep 1
start                                                  # restart: "parked" was the last one open
shot 7-restarted
check "after a restart the inactive session connects nothing" no_tmux $B
expect "not even its Local shell" 0 "$(shells_in $OUT/p2)"
xdotool mousemove 114 131 click 3; menu d d; sleep 0.5 # "working": Deactivate session…
k Tab; k Return; sleep 3                               # no tick; Deactivate
check "Deactivate without the tick leaves tmux running" has_tmux $A
check "config.toml marks it inactive too" inactive_in_file $A
expect "its Local shell has ended" 0 "$(shells_in $OUT/p1)"
xdotool mousemove 114 131 click 3; menu a              # Activate session
sleep 5
check "Activate session from the menu" active_in_file $A
check "and it connects again" has_tmux $A
shot 8-end
alive
kill $APP; wait $APP 2>/dev/null
finish
