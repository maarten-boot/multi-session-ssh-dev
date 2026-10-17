#!/bin/bash
# Headless GUI test of Claude backups: "Back up Claude data now" from the
# host menu copies ~/.claude of the agent host (here: localhost, read only)
# into the data directory without claude's login; a failing host's failure
# is recorded (and marked with ⚠, see the screenshot). Restoring is covered by
# internal/backup's ssh test (it would write into the real ~/.claude here).
# Needs `ssh localhost` without prompts and a ~/.claude on this host; uses rsync
# when installed, else the built-in transfer (force it with SW_NO_RSYNC=1).
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-backup.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
CFG=$OUT/config.toml
# Automatic backups off: only the menu action may make one here.
cat >$CFG <<TOML
[ui]
claude_backup = false
[[hosts]]
name = "local"
ssh_target = "localhost"
[[hosts]]
name = "gone"
ssh_target = "nohost.invalid"
[[sessions]]
id = "s-bk1"
name = "backup"
host = "local"
local_path = "/tmp"
start_mode = "shell"
[[sessions]]
id = "s-bk2"
name = "unreachable"
host = "gone"
local_path = "/tmp"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep 0.6; }
shot() { import -window root $OUT/$1.png; }
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 0.8; }
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }
B=$XDG_DATA_HOME/session-workbench/claude-backups
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
# Host headers: "local" at y=53, "gone" below its session at y=130.
click 100 53 3; menu b                          # local: Back up Claude data now
for i in $(seq 60); do [ -f $B/local/status.json ] && break; sleep 1; done
shot 1-backed-up
expect "backup of localhost recorded as good" '""' "$(jq .error $B/local/status.json 2>&1)"
check "~/.claude copied" test -d $B/local/home/.claude
check "claude's login left out" test ! -e $B/local/home/.claude/.credentials.json
click 100 130 3; menu b                         # gone: Back up Claude data now
for i in $(seq 30); do [ -f $B/gone/status.json ] && break; sleep 1; done
sleep 1; shot 2-failed
check "failure recorded for the unreachable host" grep -q '"error": ".\+' $B/gone/status.json
alive
kill $APP; wait $APP 2>/dev/null
for id in s-bk1 s-bk2; do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; done
finish
