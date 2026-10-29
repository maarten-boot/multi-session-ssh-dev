#!/bin/bash
# Headless GUI test of nested folders: the tree, a new subfolder, a session
# moved into it, a folder moved into another one, deleting a folder with a
# subfolder (its sessions go one folder up).
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-subfolders.sh <binary> <outdir> [measure]
BIN=$1; OUT=$2; MEASURE=$3; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
CFG=$OUT/config.toml
cat >$CFG <<TOML
groups = ["Work", "Work/Acme", "Private"]
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-na"
name = "acme"
host = "local"
local_path = "/usr"
start_mode = "shell"
group = "Work/Acme"
[[sessions]]
id = "s-nb"
name = "work"
host = "local"
local_path = "/var"
start_mode = "shell"
group = "Work"
[[sessions]]
id = "s-nc"
name = "home"
host = "local"
local_path = "/etc"
start_mode = "shell"
group = "Private"
TOML
k() { xdotool key "$@"; sleep 0.5; }
shot() { import -window root $OUT/$1.png; }
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 0.8; }
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
shot 1-tree
click 100 55 3; menu n                         # Work: New subfolder…
xdotool type --delay 20 "Beta"; k Return; sleep 0.8
click 100 115 3; menu m m m                    # session "work": Move to folder…
menu b                                         # -> Beta (in Work)
shot 2-beta
click 100 89 3; menu m m                       # Acme: Move folder to… (Move folder up is off)
menu p                                         # -> Private
shot 3-acme-moved
if [ -n "$MEASURE" ]; then kill $APP; exit 0; fi
# Headers now: Work y=55, Beta 89, Private 199, Acme (in Private) 309.
click 100 89 3; menu r                         # Beta: Rename folder… (just its own name)
k ctrl+a; xdotool type --delay 20 "Gamma"; k Return; sleep 0.8
click 100 199 3; menu d; k Return; sleep 1     # Private: Delete folder… (Acme goes too)
shot 4-renamed-deleted
alive
kill $APP; wait $APP 2>/dev/null
expect "folders: subfolder added, renamed; Private and Private/Acme deleted" 'groups = ["Work", "Work/Gamma"]' \
	"$(grep -E '^ *groups =' $CFG | sed 's/^ *//')"
expect "sessions' folders" "acme= work=Work/Gamma home=" \
	"$(sessions $CFG | awk -F'\t' '{printf "%s%s=%s", sep, $2, $5; sep = " "}')"
for id in s-na s-nb s-nc; do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; done
finish
