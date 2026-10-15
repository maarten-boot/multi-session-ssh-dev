#!/bin/bash
# Headless GUI test of session folders: new folder, move a session into it,
# numbering across folders, rename / reorder / delete a folder.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-folders.sh <binary> <outdir> [measure]
BIN=$1; OUT=$2; MEASURE=$3; mkdir -p $OUT
CFG=$OUT/config.toml
cat >$CFG <<TOML
groups = ["Work", "Private"]
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-fd"
name = "loose"
host = "local"
local_path = "/tmp"
start_mode = "shell"
[[sessions]]
id = "s-fa"
name = "work-a"
host = "local"
local_path = "/usr"
start_mode = "shell"
group = "Work"
[[sessions]]
id = "s-fb"
name = "work-b"
host = "local"
local_path = "/var"
start_mode = "shell"
group = "Work"
[[sessions]]
id = "s-fc"
name = "home"
host = "local"
local_path = "/etc"
start_mode = "shell"
group = "Private"
TOML
k() { xdotool key "$@"; sleep 0.5; }
shot() { import -window root $OUT/$1.png; }
# menu c1 c2 ...: pick a context-menu item by typing first letters, then Enter.
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 0.8; }
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
for i in $(seq 40); do W=$(xdotool search --name "Session Workbench" 2>/dev/null | head -1); [ -n "$W" ] && break; sleep 0.25; done
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
shot 1-start
click 221 17; menu n                           # "+" -> New folder…
xdotool type --delay 20 "Clients"; k Return; sleep 0.8
shot 2-new-folder
click 120 90 3; menu m m m                     # loose: Move to folder…
menu c                                         # -> Clients
shot 3-moved
if [ -n "$MEASURE" ]; then kill $APP; exit 0; fi
# Folder headers are now at y=56 (Work), 209 (Private), 319 (Clients).
k ctrl+alt+4; sleep 1
shot 4-ctrl-alt-4                              # "loose", numbered last
click 100 209 3; menu r                        # Private: Rename folder…
k ctrl+a; xdotool type --delay 20 "Home"; k Return; sleep 0.8
click 100 319 3; menu m                        # Clients: Move folder up
click 100 56 3; menu d; k Return; sleep 1      # Work: Delete folder… (sessions stay)
shot 5-renamed-moved-deleted
kill $APP; wait $APP 2>/dev/null
echo "== saved:"; grep -E '^groups|^ *group =|^ *name =' $CFG
for id in s-fa s-fb s-fc s-fd; do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; done
exit 0
