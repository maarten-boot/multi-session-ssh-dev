#!/bin/bash
# Headless GUI test of the quick switcher (M5): MRU order, fuzzy filter,
# search in the descriptions, resizing (kept for the next time).
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-switcher.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
CFG=$OUT/config.toml
{
	printf '[[hosts]]\nname = "local"\nssh_target = "localhost"\n'
	for s in "usr:/usr" "var:/var" "workbench:/opt" "temp files:/tmp" "etc config:/etc"; do
		n=${s%%:*}; d=${s#*:}; id=s-sw$(echo $n | tr -dc a-z | head -c 4)
		printf '[[sessions]]\nid = "%s"\nname = "%s"\nhost = "local"\nlocal_path = "%s"\nstart_mode = "shell"\n' $id "$n" $d
	done
	printf 'description = "Mail server settings.\\nSecond line."\n'   # of "etc config"
} >$CFG
k() { xdotool key "$@"; sleep 0.6; }
shot() { import -window root $OUT/$1.png; }
# here <file>: the current session's directory, typed into its Local tab.
here() { k ctrl+shift+1; sleep 1.5; xdotool type --delay 10 "pwd >\$SW_OUT/$1"; xdotool key Return; sleep 0.8; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+alt+2; k ctrl+alt+4; sleep 1             # MRU: temp files, var
k ctrl+shift+p; sleep 0.5
shot 1-mru                                     # var first, temp files (current) last
k Return; sleep 1
shot 2-back-to-var
here 1-mru
k ctrl+shift+p; xdotool type --delay 30 "wb"; sleep 0.5
shot 3-filtered
k Return; sleep 1
shot 4-workbench
here 2-fuzzy
k ctrl+shift+p; xdotool type --delay 30 "zzz"; sleep 0.5
shot 5-nomatch
k Escape
here 3-escape
# The description is searched as typed: "server sett" finds "etc config".
k ctrl+shift+p; xdotool type --delay 30 "server sett"; sleep 0.5
shot 6-description
k Return; sleep 1
here 4-description
# Resizable: drag its corner; the size is kept for the next time.
k ctrl+shift+p; sleep 0.5; shot 7-before-resize
xdotool mousemove 975 356 mousedown 1; sleep 0.3
xdotool mousemove 1100 450; sleep 0.3; xdotool mousemove 1275 556; sleep 0.3; xdotool mouseup 1; sleep 0.5
shot 8-resized
k Escape
k ctrl+shift+p; sleep 0.5; shot 9-reopened            # as big as it was left
k Escape
# big: the switcher's panel colour below and right of its first size
# (the dimmed backdrop there before resizing).
big() { [ "$(convert $OUT/$1.png -format '%[pixel:p{1100,500}]' info:)" = "$(convert $OUT/$1.png -format '%[pixel:p{900,345}]' info:)" ]; }
small() { ! big "$@"; }
check "the switcher before resizing is small" small 7-before-resize
check "the switcher is resizable" big 8-resized
check "the switcher keeps its size" big 9-reopened
alive
kill $APP; wait $APP 2>/dev/null
expect "Enter picks the most recent other session" /var "$(cat $SW_OUT/1-mru 2>&1)"
expect "\"wb\" finds workbench" /opt "$(cat $SW_OUT/2-fuzzy 2>&1)"
expect "Escape keeps the session" /opt "$(cat $SW_OUT/3-escape 2>&1)"
expect "the description is searched" /etc "$(cat $SW_OUT/4-description 2>&1)"
for id in $(sed -n 's/^id = "\(.*\)"/\1/p' $CFG); do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; done
finish
