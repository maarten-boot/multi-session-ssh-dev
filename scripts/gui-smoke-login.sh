#!/bin/bash
# Headless GUI test of plain login sessions (no local directory): a host
# with local_dir = false; its session shows no Local and Files tabs, the
# Remote tab's shell starts in the session's remote folder; New session on
# that host is a login by default, named after the host, its shell in the
# login's home; the config gets no local_path for it. Needs `ssh localhost`
# without prompts and tmux.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-login.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "jump"
ssh_target = "localhost"
local_dir = false
[[sessions]]
id = "s-lg"
name = "logs"
host = "jump"
start_mode = "shell"
remote_dir = "/tmp"
TOML
k() { xdotool key "$@"; sleep 0.5; }
shot() { import -window root $OUT/$1.png; }
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 1.5; }
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-lg'" 2>/dev/null
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 4; xdotool windowactivate --sync $W 2>/dev/null
shot 1-login
# The first tab is Remote (no Local, no Files): it connects.
check "the Remote tab connected (its tmux session)" ssh -o BatchMode=yes localhost "tmux has-session -t '=cw-s-lg'"
xdotool type --delay 10 'pwd'; k Return; sleep 1
in_pane "its shell started in the remote folder" s-lg '/tmp'
# The Local tab's key does nothing (there is none): keys still go to Remote.
k ctrl+shift+1; sleep 1
xdotool type --delay 10 'echo still-remote'; k Return; sleep 1
in_pane "no Local tab to switch to" s-lg 'still-remote'

# New session on the login host: a login by default, named after the host.
xdotool mousemove 281 67 click 1; menu                  # + → New session…
shot 2-new-login
k Return; sleep 2                                       # Create (the name field has the focus)
shot 3-created
check "a second login, named after the host" bash -c "grep -A3 'id = \"s-' $CFG | grep -q 'name = \"jump\"'"
check "a new login starts a shell, not claude" bash -c "grep -A6 'name = \"jump\"' $CFG | grep -q 'start_mode = \"shell\"'"
check "no local_path for logins" bash -c "! grep -q local_path $CFG"
NEW=$(awk '/^\[\[sessions\]\]/{s=1} /^\[\[hosts\]\]/{s=0} s && /^ *id = "s-/{id=$3} s && /name = "jump"/{gsub(/"/,"",id); print id}' $CFG | head -1)
sleep 3
check "the new login's tmux session" ssh -o BatchMode=yes localhost "tmux has-session -t '=cw-$NEW'"
xdotool type --delay 10 'pwd > /tmp/sw-login-pwd.txt'; k Return; sleep 1
expect "the new login starts in the home folder" "$HOME" "$(cat /tmp/sw-login-pwd.txt 2>&1)"
rm -f /tmp/sw-login-pwd.txt
alive
kill $APP; wait $APP 2>/dev/null
for id in s-lg $NEW; do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; done
finish
