#!/bin/bash
# Headless GUI test of desktop notifications: a bell in the Remote tab of a
# session you are not looking at (claude waiting) shows one notification
# with the session's name, even for two bells; clicking it opens that
# session's Remote tab; going to the session withdraws it. A fake
# notification service (scripts/fake-notifyd.py) on a private bus records
# the calls. Needs `ssh localhost` without prompts, tmux, python3-dbus.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-notify.sh <binary> <outdir>
if [ -z "$SW_IN_DBUS" ]; then
	SW_IN_DBUS=1 exec dbus-run-session -- "$0" "$@"
fi
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
LOG=$OUT/notifications.log; : >$LOG
python3 "$(dirname "$0")/fake-notifyd.py" $LOG &
NOTIFYD=$!
# Wait until it owns the name. NameHasOwner starts nothing (calling the name
# itself would make D-Bus start the desktop's real notification service).
owned() { dbus-send --session --print-reply --dest=org.freedesktop.DBus / org.freedesktop.DBus.NameHasOwner \
	string:org.freedesktop.Notifications 2>/dev/null | grep -q 'boolean true'; }
for i in $(seq 40); do owned && break; sleep 0.25; done
check "fake notification service running" owned
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-nalpha"
name = "alpha"
host = "local"
local_path = "/tmp"
start_mode = "shell"
[[sessions]]
id = "s-nbeta"
name = "beta"
host = "local"
local_path = "/var"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep 0.6; }
t() { xdotool type --delay 10 "$1"; xdotool key Return; sleep ${2:-0.5}; }
shot() { import -window root $OUT/$1.png; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+alt+1; k ctrl+shift+3; sleep 2.5          # alpha, Remote tab
t 'sleep 4; printf "\a"; sleep 2; printf "\a"'   # claude "waits" twice
k ctrl+alt+2; sleep 9                            # look at beta meanwhile
shot 1-notified
expect "one notification for two bells" 1 "$(grep -c '^NOTIFY' $LOG)"
check "it says claude waits, in which session" grep -q $'^NOTIFY 1\tClaude is waiting\talpha · local$' $LOG
# A click on the notification (its default action) opens alpha's Remote tab.
gdbus emit --session --object-path /org/freedesktop/Notifications \
	--signal org.freedesktop.Notifications.ActionInvoked "uint32 1" "'default'" >/dev/null   # typed: the spec says uint32
sleep 2
t 'echo "clicked into $(tmux display -p "#S")"' 1
shot 2-clicked
in_pane "clicking the notification opened alpha's Remote tab" s-nalpha "clicked into cw-s-nalpha"
# Again, but now go to alpha without clicking: its notification is withdrawn.
t 'sleep 3; printf "\a"'
k ctrl+alt+2; sleep 6
check "a new bell after looking notifies again" grep -q $'^NOTIFY 2\tClaude is waiting' $LOG
k ctrl+alt+1; sleep 1.5
shot 3-looked
check "looking at the session withdrew its notification" grep -q '^CLOSE 2$' $LOG
alive
kill $APP; wait $APP 2>/dev/null
kill $NOTIFYD 2>/dev/null
for id in s-nalpha s-nbeta; do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; done
finish
