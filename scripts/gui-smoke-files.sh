#!/bin/bash
# Headless GUI test of the Files tab (M4) against a fixture directory, with a
# fake xdg-open that logs instead of launching anything.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-files.sh <binary> <outdir> [measure]
BIN=$1; OUT=$2; MEASURE=$3; mkdir -p $OUT
FIX=$OUT/fixture/proj; rm -rf $OUT/fixture
mkdir -p "$FIX/src/pkg" "$FIX/docs" "$FIX/.git/objects" "$FIX/with space"
echo '# readme' >"$FIX/README.md"; echo 'package main' >"$FIX/src/main.go"
echo x >"$FIX/.gitignore"; echo y >"$FIX/with space/it's.txt"
head -c 3000 /dev/urandom >"$FIX/docs/blob.bin"
cp /bin/true "$FIX/tool"                       # ELF: must be refused
BINDIR=$OUT/fakebin; mkdir -p $BINDIR; LOG=$OUT/xdg-open.log; : >$LOG
printf '#!/bin/sh\necho "$1" >>%s\n' "$LOG" >$BINDIR/xdg-open; chmod +x $BINDIR/xdg-open
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "fx"
ssh_target = "localhost"
local_prefix = "$OUT/fixture"
remote_prefix = "/srv/agent"
[[sessions]]
id = "s-files"
name = "proj"
host = "fx"
local_path = "$FIX"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep ${KD:-0.4}; }
shot() { import -window root $OUT/$1.png; }
# menu c1 c2 ...: pick a context-menu item by typing first letters, then Enter.
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 0.8; }
# Window origin (60,50); tree rows are 22px tall from y=ROW0 (window-relative).
ROW0=${ROW0:-74}; X=320
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }
row() { echo $((ROW0 + 22*$1 + 11)); }

env PATH=$BINDIR:$PATH DBUS_SESSION_BUS_ADDRESS= $BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
for i in $(seq 40); do W=$(xdotool search --name "Session Workbench" 2>/dev/null | head -1); [ -n "$W" ] && break; sleep 0.25; done
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+shift+3; sleep 1
shot 01-tree                                   # docs/ src/ with space/ README.md tool
if [ -n "$MEASURE" ]; then click $X $(row 3) 3; shot 00-measure; kill $APP; exit; fi
# Context menu on README.md (row 3); items picked from the keyboard.
R=$(row 3)
click $X $R 3; menu o o                         # Open containing folder -> xdg-open $FIX
click $X $R 3; menu c c                         # Copy remote path
k ctrl+shift+2; sleep 2                        # Local tab: paste it
xdotool type --delay 12 'echo "remote=['; k ctrl+shift+v; xdotool type --delay 12 ']"'; k Return
R=$(row 2)
k ctrl+shift+3; click $X $R 3; menu c          # "with space/": Copy local path
k ctrl+shift+2; xdotool type --delay 12 'echo "local=['; k ctrl+shift+v; xdotool type --delay 12 ']"'; k Return
xdotool type --delay 12 'touch NEW-FILE.txt'; k Return  # Local tab cwd is the session dir
sleep 1; shot 05-pasted
k ctrl+shift+3; sleep 1                        # refresh on show: NEW-FILE.txt appears
shot 06-refreshed-on-show
k Home; k Down; k Right; sleep 0.5             # select src, expand
shot 02-expanded
k End; k Up; k Return; sleep 1                 # README.md -> xdg-open
k End; k Return; sleep 1                       # tool -> refused
shot 03-refused
k shift+Tab; k shift+Tab; k space; sleep 1     # Hidden files on
shot 04-hidden
k space; sleep 0.5                             # and off again
kill $APP; wait $APP 2>/dev/null
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-files'" 2>/dev/null
echo "xdg-open calls:"; cat $LOG
exit 0
