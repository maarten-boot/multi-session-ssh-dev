#!/bin/bash
# Headless GUI test of live-share sessions (files = "sshfs"): the app serves
# the directory, the agent host mounts it, the Remote tab works in the mount,
# and closing the app unmounts. Needs `ssh localhost`, sshfs and FUSE.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-share.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
L=$OUT/project; WD="sw-sharegui-$$"; rm -rf $L; mkdir -p $L; echo "written on the host" >$L/hello.txt
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "agent"
ssh_target = "localhost"
files = "sshfs"
remote_workdir = "~/$WD"
[[sessions]]
id = "s-share"
name = "shared"
host = "agent"
local_path = "$L"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep 0.5; }
t() { xdotool type --delay 8 "$1"; xdotool key Return; sleep ${2:-1}; }
shot() { import -window root $OUT/$1.png; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
for i in $(seq 40); do W=$(xdotool search --name "Session Workbench" 2>/dev/null | head -1); [ -n "$W" ] && break; sleep 0.25; done
sleep 6; xdotool windowactivate --sync $W 2>/dev/null
t 'pwd; cat hello.txt; echo "written on the agent" >from-agent.txt; ls' 1.5
shot 1-remote-in-share
echo "host sees: $(cat $L/from-agent.txt 2>&1)"
echo "mounted while running: $(ssh -o BatchMode=yes localhost "mount | grep -c $WD")"
kill $APP; wait $APP 2>/dev/null; sleep 2
echo "mounted after closing the app: $(ssh -o BatchMode=yes localhost "mount | grep -c $WD")"
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-share'; rm -rf ~/$WD" 2>/dev/null
exit 0
