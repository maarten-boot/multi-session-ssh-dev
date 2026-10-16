#!/bin/bash
# Headless GUI test of the tab order (Local, Files, Remote, Notes) and the
# Notes tab: autosave to <config dir>/notes/<id>.md, kept across restarts.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-notes.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT; rm -rf $OUT/notes
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-nt1"
name = "project one"
host = "local"
local_path = "/tmp"
start_mode = "shell"
[[sessions]]
id = "s-nt2"
name = "project two"
host = "local"
local_path = "/usr"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep 0.5; }
shot() { import -window root $OUT/$1.png; }
start() {
	$BIN -config $CFG >>$OUT/app.log 2>&1 &
	APP=$!
	for i in $(seq 40); do W=$(xdotool search --name "Session Workbench" 2>/dev/null | head -1); [ -n "$W" ] && break; sleep 0.25; done
	sleep 3; xdotool windowactivate --sync $W 2>/dev/null
}
start
k ctrl+alt+2; sleep 1.5
shot 1-opens-on-local                           # tabs: Local Files Remote Notes; Local active
k ctrl+alt+1; k ctrl+shift+4; sleep 1
xdotool type --delay 15 "# Project one"; k Return; k Return
xdotool type --delay 15 -- "- ask claude about the parser"; k Return
k Tab; xdotool type --delay 15 "indented with Tab"
sleep 0.3; shot 2-typing
k ctrl+alt+2; sleep 1                           # switching away saves
echo "== saved file:"; cat $OUT/notes/s-nt1.md; echo; ls -l $OUT/notes | awk 'NR>1{print $1, $NF}'
kill $APP; wait $APP 2>/dev/null; sleep 1
start
k ctrl+alt+1; sleep 1                           # reopens on Notes (the last tab used)
shot 3-after-restart
kill $APP; wait $APP 2>/dev/null
for id in s-nt1 s-nt2; do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; done
exit 0
