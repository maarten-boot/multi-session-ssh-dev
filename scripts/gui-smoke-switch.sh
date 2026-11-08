#!/bin/bash
# Headless GUI test of one project on two agent hosts (SPEC F16): two hosts
# on localhost whose path mappings differ (so the project's path, and
# Claude Code's folder for it, differ); session menu → Requirements… shows
# and checks the project's list; Switch host… moves the session with its
# Claude conversation and memory (the folder renamed for the new path),
# the old host goes into hosts; switching back offers it first.
# Both "hosts" share this user's ~/.claude: the test makes and removes only
# its own project folders there; ~/.claude.json is read, never written (a
# test path has no entry in it).
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-switch.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
ID=s-sw$$
mkdir -p $OUT/l/proj/.session-workbench $OUT/w/proj $OUT/h/proj
printf 'sh\nno-such-tool-xyz >= 1\n' >$OUT/l/proj/.session-workbench/requirements
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "work"
ssh_target = "localhost"
local_prefix = "$OUT/l"
remote_prefix = "$OUT/w"
[[hosts]]
name = "home"
ssh_target = "localhost"
local_prefix = "$OUT/l"
remote_prefix = "$OUT/h"
[[sessions]]
id = "$ID"
name = "travelling"
host = "work"
local_path = "$OUT/l/proj"
start_mode = "shell"
TOML
enc() { printf '%s' "$1" | sed 's/[^A-Za-z0-9]/-/g'; }
WP=$HOME/.claude/projects/$(enc $OUT/w/proj)
HP=$HOME/.claude/projects/$(enc $OUT/h/proj)
mkdir -p $WP/memory
echo '{"type":"user","message":"hello from work"}' >$WP/conv1.jsonl
echo '- remembered at work' >$WP/memory/MEMORY.md
cleanup() { rm -rf "$WP" "$HP"; tmux kill-session -t "=cw-$ID" 2>/dev/null; }
trap cleanup EXIT
k() { xdotool key "$@"; sleep 0.6; }
shot() { import -window root $OUT/$1.png; }
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 2; }
host_of() { awk -F'"' '/^\[\[sessions\]\]/{s=1} s && /^ *host =/{print $2}' $CFG; }
hosts_of() { awk '/^\[\[sessions\]\]/{s=1} s && /^ *hosts =/{print}' $CFG | sed 's/ //g'; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null

xdotool mousemove 114 131 click 3; menu r r            # Record the Remote tab, Requirements…
k Tab; k Tab; k Return; sleep 4                        # (text →) Save → Check on work
shot 1-requirements
k Escape; sleep 1
xdotool mousemove 114 131 click 3; menu s              # Switch host…
sleep 4                                                # the requirements check on home
shot 2-switch-dialog
k Tab; k Tab; k Return; sleep 6                        # (host →) end tmux → Switch
shot 3-switched
expect "the session is on home now" "home" "$(host_of)"
expect "the old host is remembered" 'hosts=["work"]' "$(hosts_of)"
check "the conversation went along (renamed folder)" grep -q 'hello from work' $HP/conv1.jsonl
check "and the memory" grep -q 'remembered at work' $HP/memory/MEMORY.md
check "the old host keeps its copy" test -s $WP/conv1.jsonl
echo '{"type":"user","message":"written at home"}' >>$HP/conv1.jsonl
xdotool mousemove 114 176 click 3; menu s              # (now under HOME) Switch host… back: work, worked there before
sleep 4
shot 4-switch-back
k Tab; k Tab; k Return; sleep 6
expect "back on work" "work" "$(host_of)"
expect "home is remembered now" 'hosts=["home"]' "$(hosts_of)"
check "the newer conversation from home came back" grep -q 'written at home' $WP/conv1.jsonl
alive
kill $APP; wait $APP 2>/dev/null
finish
