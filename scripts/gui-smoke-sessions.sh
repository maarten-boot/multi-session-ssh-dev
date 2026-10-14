#!/bin/bash
# Headless GUI test of sessions (M3): sidebar, new-session dialog, switching
# shortcuts, persistence across restart. Needs `ssh localhost` without prompts.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-sessions.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
SL=$HOME/share-local                  # local prefix (a symlink to the NFS path on the test VM)
NFS=$(readlink -f $SL)
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "vibe"
ssh_target = "localhost"
local_prefix = "$SL"
remote_prefix = "$NFS"

[[hosts]]
name = "lab"
ssh_target = "localhost"

[[sessions]]
id = "s-t001"
name = "DEV"
host = "vibe"
local_path = "$SL/DEV"
start_mode = "shell"

[[sessions]]
id = "s-t002"
name = "workspace"
host = "vibe"
local_path = "$SL/workspace"
start_mode = "shell"

[[sessions]]
id = "s-t003"
name = "tmp-on-lab"
host = "lab"
local_path = "/tmp"
start_mode = "shell"
TOML
start() {
	$BIN -config $CFG >>$OUT/app.log 2>&1 &
	APP=$!
	for i in $(seq 40); do W=$(xdotool search --name "Session Workbench" 2>/dev/null | head -1); [ -n "$W" ] && break; sleep 0.25; done
	sleep 3
	xdotool windowactivate --sync $W 2>/dev/null; xdotool windowfocus $W
}
stop() { kill $APP; wait $APP 2>/dev/null; }
typ() { xdotool type --delay 12 "$1"; }
t() { typ "$1"; xdotool key Return; sleep ${2:-1}; }
k() { xdotool key "$@"; sleep 0.6; }
shot() { import -window root $OUT/$1.png; }

start
k ctrl+alt+1; sleep 2              # localStorage may restore another session
shot 01-start                         # first session, Remote tab connecting
t 'echo "in $(tmux display -p "#S")"; export MARK=one'
# New session via dialog: dir, then mode=shell via keyboard, then name + Enter.
k ctrl+shift+n
typ "$SL/DEV/github.com/maarten-boot/multi-session-ssh-dev"; sleep 1
shot 02-dialog
k Tab; k Tab; k Tab; k s; k shift+Tab
typ "workbench"; k Return; sleep 3
t 'echo "in $(tmux display -p "#S") at $(pwd)"; export MARK=four'
shot 03-created
# Rejected directory: outside the vibe mapping.
k ctrl+shift+n
typ "/etc"; sleep 1
k Return; sleep 1
shot 04-rejected
k Escape
# Second dialog session, default name.
k ctrl+shift+n
typ "$SL/RL"; sleep 0.5; k Tab; k Tab; k Tab; k s; k shift+Tab; k Return; sleep 3
shot 05-five
# Switching.
k ctrl+alt+1; t 'echo "back: $MARK"'
shot 06-alt1
k ctrl+shift+2; t 'echo "local tab of DEV: $(pwd)"'
shot 07-local
k ctrl+shift+Next; sleep 1
shot 08-next                          # DEV -> workspace
k ctrl+shift+Prior; k ctrl+shift+Prior; sleep 1
shot 08b-prev-wraps                   # -> DEV -> last (tmp-on-lab)
k ctrl+alt+3; sleep 1
shot 09-alt3                          # workbench (3rd in sidebar order)
stop
cp $CFG $OUT/config-after.toml
sleep 1
# Restart: everything restored, last active session selected, re-attached.
start
shot 10-restarted                     # last active (alt+3) restored
k ctrl+alt+3; sleep 2; t 'echo "after restart: $MARK"'
shot 11-reattached
stop
# Clean up the tmux sessions this test created on localhost.
for id in $(sed -n 's/^ *id = "\(.*\)"/\1/p' $CFG); do
	ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null
done
