#!/bin/bash
# Headless GUI test of the right-click menu on the empty space of the
# session list: Prompts for all sessions makes the Prompts page and opens
# it, Notes for all sessions opens the Home page, New folder… works as in
# the "+" menu, also from between the folders; a right-click on a session
# still opens the session's menu.
# Needs `ssh localhost` without prompts and tmux.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-listmenu.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT; rm -rf $OUT/notes
. "$(dirname "$0")/smoke-lib.sh"
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-lm"
name = "listmenu"
host = "local"
local_path = "/tmp"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep 0.6; }
shot() { import -window root $OUT/$1.png; }
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 0.8; }
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null

# Below the one host and session: the list menu. "p": Prompts for all
# sessions, made (there is none yet) and opened.
click 100 500 3; shot 1-menu; menu p
sleep 1
check "the Prompts page made" test -f $OUT/notes/_app/Prompts.md
k ctrl+End
xdotool type --delay 10 '## Hi'; xdotool key Return
xdotool type --delay 10 'echo hi'; sleep 1.5
shot 2-prompts
expect "typed on the Prompts page" $'# Prompts\n\n## Hi\necho hi' "$(cat $OUT/notes/_app/Prompts.md 2>&1)"

# With the page open: "n n" is Notes for all sessions (after New session,
# New folder), on its Home page.
click 100 500 3; menu n n
sleep 1
xdotool type --delay 10 'home-note'; sleep 1.5
shot 3-home
expect "Notes for all sessions opens Home" "home-note" "$(cat $OUT/notes/_app/Home.md 2>&1)"
k Escape

# "n": New folder… (the first item, New session…, has the focus).
click 100 500 3; menu n
xdotool type --delay 10 'Work'; k Return; sleep 1
check "New folder… from the list menu" grep -q '"Work"' $CFG

# Between the hosts and folders too: the empty folder's line.
click 100 161 3; shot 4-folder-line; menu n
xdotool type --delay 10 'Private'; k Return; sleep 1
check "the list menu on an empty folder's line" grep -q '"Private"' $CFG

# A right-click on the session is its own menu, not the list's.
click 100 74 3; shot 5-session-menu
k Escape
alive
kill $APP; wait $APP 2>/dev/null
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-lm'" 2>/dev/null
finish
