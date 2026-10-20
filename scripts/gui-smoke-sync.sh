#!/bin/bash
# Headless GUI test of working-copy sessions (files = "sync"): missing copy,
# Send with preview, Remote tab in the copy, commit there and make a new
# branch, Get: main fast-forwarded and the new branch created here.
# Needs `ssh localhost` without prompts and git; rsync is used when installed
# (SW_NO_RSYNC=1 tests the built-in transfer).
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-sync.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
R=$OUT/repo; WD="sw-guitest-$$"; rm -rf $R; mkdir -p $R
git -C $R init -q -b main; echo one >$R/a.txt; printf 'build/\n' >$R/.gitignore; mkdir $R/build; echo x >$R/build/out
git -C $R add -A; git -C $R -c user.name=t -c user.email=t@example.com commit -q -m base
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "agent"
ssh_target = "localhost"
files = "sync"
remote_workdir = "~/$WD"
[[sessions]]
id = "s-sync"
name = "synced"
host = "agent"
local_path = "$R"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep 0.5; }
t() { xdotool type --delay 8 "$1"; xdotool key Return; sleep ${2:-1}; }
shot() { import -window root $OUT/$1.png; }
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 0.8; }
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 4; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+shift+3; sleep 3                         # Remote tab
shot 1-no-copy-yet                              # Remote tab: no working copy, "Send to agent…"
click 120 90 3; menu s                          # session menu: Send to agent…
sleep 3; shot 2-send-preview
k Return; sleep 4                               # Send; Remote tab restarts in the copy
t 'pwd; ls -A; git log --oneline | head -3' 1.5
shot 3-remote-in-copy
t 'echo agent >>a.txt && git -c user.name=c -c user.email=c@example.com commit -qam "change made on the agent" && git branch from-agent && echo COMMITTED' 1.5
click 120 90 3; menu g                          # Get commits from agent…
sleep 3; shot 4-get
k Return; sleep 2                               # Take over ticked branches (main, from-agent)
shot 5-fast-forwarded
alive
kill $APP; wait $APP 2>/dev/null
expect "host file after Get + fast-forward" "one agent" "$(cat $R/a.txt | xargs)"
expect "host branch has the agent's commit" "change made on the agent|base|" "$(git -C $R log --format=%s | head -2 | tr '\n' '|')"
expect "the agent's new branch is a branch here" "change made on the agent" "$(git -C $R log -1 --format=%s from-agent 2>&1)"
expect "still on main" main "$(git -C $R symbolic-ref --short HEAD)"
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-sync'; rm -rf ~/$WD" 2>/dev/null
finish
