#!/bin/bash
# Headless GUI test of a separate setup (-profile / SESSION_WORKBENCH_PROFILE):
# with the environment variable the app reads <config>/ssh-switcher/config.toml,
# keeps its data in <data>/ssh-switcher, says so in the window title, and its
# Remote tab's tmux session is cw-ssh-switcher-<id>; nothing goes to the
# session-workbench folders. -profile wins over the variable; a bad name is
# refused. Needs `ssh localhost` without prompts and tmux.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-profile.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
export XDG_CONFIG_HOME=$OUT/xdg-config
rm -rf $XDG_CONFIG_HOME; mkdir -p $XDG_CONFIG_HOME/ssh-switcher
cat >$XDG_CONFIG_HOME/ssh-switcher/config.toml <<TOML
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-pf"
name = "a login"
host = "local"
local_path = "$OUT"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep 0.5; }
shot() { import -window root $OUT/$1.png; }
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-ssh-switcher-s-pf'" 2>/dev/null

# With the environment variable (no -config).
SESSION_WORKBENCH_PROFILE=ssh-switcher $BIN >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
shot 1-started
check "the title names the profile" bash -c "xdotool getwindowname $W | grep -q 'Session Workbench — ssh-switcher'"
check "its session from its own config" bash -c "xdotool getwindowname $W | grep -q 'a login'"
k ctrl+shift+3; sleep 5
shot 2-remote
check "its tmux session carries the name" ssh -o BatchMode=yes localhost "tmux has-session -t '=cw-ssh-switcher-s-pf'"
check "not the default name" bash -c "! ssh -o BatchMode=yes localhost \"tmux has-session -t '=cw-s-pf'\" 2>/dev/null"
alive
kill $APP; wait $APP 2>/dev/null; sleep 1
check "its data folder" test -s $XDG_DATA_HOME/ssh-switcher/window.json
# (WebKit's own storage stays in one place; the app keeps a profile's keys
# apart there.)
check "nothing of the app in the session-workbench folders" bash -c "! test -e $XDG_DATA_HOME/session-workbench/window.json && ! test -e $XDG_CONFIG_HOME/session-workbench"

# -profile wins over the variable.
SESSION_WORKBENCH_PROFILE=other $BIN -profile ssh-switcher >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3
check "-profile wins over the variable" bash -c "xdotool getwindowname $W | grep -q 'ssh-switcher'"
check "no folder for the variable's name" test ! -e $XDG_CONFIG_HOME/other
kill $APP; wait $APP 2>/dev/null

# A bad name is refused before anything starts.
$BIN -profile 'Bad/Name' >$OUT/bad.txt 2>&1
check "a bad name is refused (exit 2)" test $? = 2
check "and says why" grep -q 'lower-case letters' $OUT/bad.txt
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-ssh-switcher-s-pf'" 2>/dev/null
finish
