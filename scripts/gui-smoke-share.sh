#!/bin/bash
# Headless GUI test of live-share sessions (files = "sshfs"): the app serves
# the directory, the agent host mounts it, the Remote tab works in the mount,
# git on the agent host commits and creates branches through it (appending
# writes, lock files: they once crashed the app), and closing the app
# unmounts. Needs `ssh localhost`, sshfs and FUSE.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-share.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
L=$OUT/project; WD="sw-sharegui-$$"; rm -rf $L; mkdir -p $L; echo "written on the host" >$L/hello.txt
git -C $L init -q -b main; git -C $L add -A; git -C $L -c user.name=t -c user.email=t@example.com commit -q -m base
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
wait_window
sleep 6; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+shift+3; sleep 3                         # Remote tab (waits for the mount)
t 'pwd; cat hello.txt; echo "written on the agent" >from-agent.txt; ls' 1.5
shot 1-remote-in-share
alive
in_pane "the agent reads the host's file through the share" s-share "written on the host"
expect "the host sees the agent's file" "written on the agent" "$(cat $L/from-agent.txt 2>&1)"
# git on the agent host: a new branch with a commit, and a second branch.
t 'git checkout -q -b feature-x; echo f >f.txt; git add f.txt; git -c user.name=a -c user.email=a@example.com commit -q -m "on feature"; git branch side/y; echo git-done' 4
shot 2-git-through-share
alive
expect "the agent's branches are in the host repository" "feature-x|main|side/y|" "$(git -C $L for-each-ref --format='%(refname:short)' refs/heads | tr '\n' '|')"
expect "and its commit" "on feature" "$(git -C $L log -1 --format=%s feature-x 2>&1)"
check "no lock files left" test -z "$(find $L/.git -name '*.lock')"
expect "the reflog got the agent's entries in order (appending writes)" "3|commit: on feature" "$(wc -l <$L/.git/logs/HEAD | tr -d ' ')|$(tail -1 $L/.git/logs/HEAD | cut -f2)"
check "the repository is sound" git -C $L fsck --no-progress
t '(umask 077; echo s >secret.txt); echo made-secret' 2
expect "a file created private on the agent is private here" "-rw-------" "$(stat -c %A $L/secret.txt 2>&1)"
xdotool mousemove 284 137 click 1; sleep 0.3; xdotool mousemove 1000 700; sleep 2  # session ⋯ menu: "Files: live share (sshfs), mounted" on top
shot 3-session-menu
k Escape
expect "mounted while running" 1 "$(ssh -o BatchMode=yes localhost "mount | grep -c $WD")"
kill $APP; wait $APP 2>/dev/null; sleep 2
expect "unmounted after closing the app" 0 "$(ssh -o BatchMode=yes localhost "mount | grep -c $WD")"
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-share'; rm -rf ~/$WD" 2>/dev/null
finish
