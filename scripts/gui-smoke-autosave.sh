#!/bin/bash
# Headless GUI test of saving agent copies automatically (working copies,
# F12): the scheduled save about a minute after start (status in the bar),
# saving at quit (the window waits, with progress, until the copy is saved),
# and Quit now, which stops a save.
# Needs `ssh localhost` without prompts, git and rsync or tar.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-autosave.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
R=$OUT/repo; WD="sw-autosave-$$"; rm -rf $R; mkdir -p $R
git -C $R init -q -b main; echo one >$R/a.txt
git -C $R add -A; git -C $R -c user.name=t -c user.email=t@example.com commit -q -m base
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "agent"
ssh_target = "localhost"
files = "sync"
remote_workdir = "~/$WD"
[[sessions]]
id = "s-auto"
name = "autosaved"
host = "agent"
local_path = "$R"
start_mode = "shell"
TOML
AG="~/$WD/repo-s-auto"
SAVED=$XDG_DATA_HOME/session-workbench/working-copies/s-auto/files
k() { xdotool key "$@"; sleep 0.5; }
shot() { import -window root $OUT/$1.png; }
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 0.8; }
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }
agent() { ssh -o BatchMode=yes localhost "$1" 2>&1; }
start() {
	$BIN -config $CFG >>$OUT/app.log 2>&1 &
	APP=$!
	wait_window
	sleep 3; xdotool windowactivate --sync $W 2>/dev/null
}
# quit: SIGTERM (closes the window the normal way); waits up to $1 s for the exit.
quit() {
	kill $APP
	for i in $(seq $((${1:-60} * 4))); do kill -0 $APP 2>/dev/null || return 0; sleep 0.25; done
	return 1
}
start
k ctrl+shift+3; sleep 2
click 120 90 3; menu; sleep 3; k Return; sleep 3   # Send to agent…
agent "cd $AG && echo uncommitted >draft.txt" >/dev/null
sleep 65                                         # the first scheduled round (1 minute after start)
shot 1-scheduled-save
check "scheduled save: the uncommitted file" test -f $SAVED/draft.txt
check "scheduled save: recorded" test -f $XDG_DATA_HOME/session-workbench/working-copies/s-auto/saved.json

# Quitting saves first: a big new file on the agent makes it take a moment.
agent "cd $AG && head -c 600000000 /dev/urandom >big.bin && echo made" >/dev/null
quit 1 || true                                   # SIGTERM; the window stays while saving
shot 2-saving-at-quit
check "the window stays while saving" kill -0 $APP
if quit 120; then ok "quit after saving"; else fail "still running 2 minutes after quitting"; kill -9 $APP; fi
expect "saved at quit: the big file" 600000000 "$(stat -c %s $SAVED/big.bin 2>&1)"

# Quit now stops a save that is still running.
start
agent "cd $AG && head -c 600000000 /dev/urandom >big2.bin && echo made" >/dev/null
quit 1 || true
sleep 0.5; shot 3-quit-now
k Return                                         # Quit now (stop saving) has the focus
if quit 10; then ok "Quit now quits at once"; else fail "Quit now: still running"; kill -9 $APP; fi
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-auto'; rm -rf ~/$WD" 2>/dev/null
finish
