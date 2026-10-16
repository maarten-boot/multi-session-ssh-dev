#!/bin/bash
# Headless GUI test of activity badges (M5): bell and output-after-idle in
# hidden sessions, no false badge from tmux's once-a-minute status redraw,
# badge cleared on view, count in the window title. Takes ~2 minutes.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-badges.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
CFG=$OUT/config.toml
{
	printf '[[hosts]]\nname = "local"\nssh_target = "localhost"\n'
	for n in alpha beta gamma; do
		printf '[[sessions]]\nid = "s-b%s"\nname = "%s"\nhost = "local"\nlocal_path = "/tmp"\nstart_mode = "shell"\n' $n $n
	done
} >$CFG
k() { xdotool key "$@"; sleep 0.6; }
t() { xdotool type --delay 10 "$1"; xdotool key Return; sleep 0.3; }
# shot <name> <expected window title>
shot() { import -window root $OUT/$1.png; expect "$1: window title" "$2" "$(xdotool getwindowname $W)"; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+alt+1; k ctrl+shift+3; sleep 2; t 'sleep 5; printf "\a"; echo rang'     # alpha Remote: bell in 5 s
k ctrl+alt+2; sleep 2; k ctrl+shift+1; sleep 1.5
t 'sleep 12; seq 1 3000'                                        # beta Local: output after 12 s idle
k ctrl+alt+3; sleep 16
shot 1-bell-and-activity "(2) Session Workbench"               # alpha bell, beta activity
k ctrl+alt+2; sleep 1
shot 2-beta-viewed "(1) Session Workbench"                      # beta cleared
k ctrl+alt+3; sleep 70                                          # tmux clock redraws in hidden sessions
shot 3-after-status-redraw "(1) Session Workbench"              # still only alpha
k ctrl+alt+1; sleep 1
shot 4-all-cleared "Session Workbench"
alive
kill $APP; wait $APP 2>/dev/null
for n in alpha beta gamma; do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-b$n'" 2>/dev/null; done
finish
