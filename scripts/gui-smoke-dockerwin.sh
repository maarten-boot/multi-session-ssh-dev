#!/bin/bash
# Headless GUI test of the Dockerfile window (no container is made): it
# moves when its heading is dragged, grows when its corner is dragged, and
# keeps both when opened again; the right-click menu shows inside it (a
# modal dialog) — Copy and Paste through the menu put a line in twice, and
# Save writes that. Needs Podman or Docker (the window asks the engine for
# the container's state).
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-dockerwin.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
export SW_IN_DBUS=1 # rootless Podman needs the user's systemd bus
. "$(dirname "$0")/smoke-lib.sh"
ENGINE=$(command -v podman >/dev/null && echo podman || echo docker)
command -v $ENGINE >/dev/null || { echo "SKIP: neither Podman nor Docker installed"; exit 0; }
cat >$OUT/storage.conf <<CONF
[storage]
driver = "overlay"
graphroot = "$HOME/.local/share/containers/storage"
runroot = "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/containers"
CONF
export CONTAINERS_STORAGE_CONF=$OUT/storage.conf
ID=s-dw$$
PROJ=$OUT/proj; rm -rf $PROJ; mkdir -p $PROJ
CFG=$OUT/config.toml
cat >$CFG <<TOML
[ui]
container_engine = "$ENGINE"
[[hosts]]
name = "boxes"
kind = "container"
[[sessions]]
id = "$ID"
name = "boxed"
host = "boxes"
local_path = "$PROJ"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep 0.4; }
shot() { import -window root $OUT/$1.png; }
# crop <shot> <name> <WxH+X+Y>; same <a> <b>: two crops are equal.
crop() { convert $OUT/$1.png -crop $3 +repage $OUT/$2.png; }
same() { [ "$(compare -metric AE $OUT/$1.png $OUT/$2.png null: 2>&1 | cut -d' ' -f1)" = 0 ]; }
drag() { xdotool mousemove $1 $2 mousedown 1; sleep 0.2; xdotool mousemove $(( ($1+$3)/2 )) $(( ($2+$4)/2 )); sleep 0.2
	xdotool mousemove $3 $4; sleep 0.2; xdotool mouseup 1; sleep 0.6; }
open() { xdotool mousemove 114 131 click 3; sleep 0.8; xdotool mousemove 170 190 click 1; sleep 2; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null

# Centred at first: x 381–1019, y 170–730; the heading at y 198.
open; shot 1-open
crop 1-open head-1 '170x24+395+186'
crop 1-open save-1 '64x34+938+683'
drag 480 198 330 148                                  # by (-150, -50)
shot 2-moved
crop 2-moved head-2 '170x24+245+136'
check "dragging the heading moves the window" same head-1 head-2
drag 863 674 1063 794                                 # the corner, by (+200, +120)
shot 3-resized
crop 3-resized save-3 '64x34+988+753'
check "dragging the corner makes it larger (Save moved with the corner)" same save-1 save-3

# The menu: copy the first line through it, paste it on a new line.
xdotool mousemove 600 400 click 1; k ctrl+Home
xdotool type --delay 15 '# menu-mark'; k Return; k Up; k shift+End
xdotool mousemove 300 253 click 3; sleep 0.8; shot 4-menu   # on the selected text
k c; k Return                                         # Cut, Copy: "c" goes to Copy
xdotool mousemove 259 267 click 3; sleep 0.8          # the caret goes to line 2's start; nothing selected: Paste has the focus
k Return; sleep 0.6
shot 5-pasted
xdotool mousemove 1020 770 click 1; sleep 1.5         # Save, where the corner took it
DF=$XDG_DATA_HOME/session-workbench/containers/$ID/Dockerfile
expect "Copy and Paste from the right-click menu" "# menu-mark|# menu-mark# The container" "$(head -2 $DF 2>&1 | cut -c1-26 | paste -sd'|')"

# Opened again: where it was left, at the size it was given.
open; shot 6-again
crop 6-again head-6 '170x24+245+136'
crop 6-again save-6 '64x34+988+753'
check "opened again: same place" same head-1 head-6
check "opened again: same size" same save-1 save-6
k Escape
alive
kill $APP
finish
