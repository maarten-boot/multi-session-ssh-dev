#!/bin/bash
# Headless GUI test of the quick switcher (M5): MRU order, fuzzy filter.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-switcher.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
CFG=$OUT/config.toml
{
	printf '[[hosts]]\nname = "local"\nssh_target = "localhost"\n'
	for s in "usr:/usr" "var:/var" "workbench:/opt" "temp files:/tmp" "etc config:/etc"; do
		n=${s%%:*}; d=${s#*:}; id=s-sw$(echo $n | tr -dc a-z | head -c 4)
		printf '[[sessions]]\nid = "%s"\nname = "%s"\nhost = "local"\nlocal_path = "%s"\nstart_mode = "shell"\n' $id "$n" $d
	done
} >$CFG
k() { xdotool key "$@"; sleep 0.6; }
shot() { import -window root $OUT/$1.png; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
for i in $(seq 40); do W=$(xdotool search --name "Session Workbench" 2>/dev/null | head -1); [ -n "$W" ] && break; sleep 0.25; done
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+alt+2; k ctrl+alt+4; sleep 1             # MRU: temp files, var
k ctrl+shift+p; sleep 0.5
shot 1-mru                                     # var first, temp files (current) last
k Return; sleep 1
shot 2-back-to-var
k ctrl+shift+p; xdotool type --delay 30 "wb"; sleep 0.5
shot 3-filtered
k Return; sleep 1
shot 4-workbench
k ctrl+shift+p; xdotool type --delay 30 "zzz"; sleep 0.5
shot 5-nomatch
k Escape
kill $APP; wait $APP 2>/dev/null
for id in $(sed -n 's/^id = "\(.*\)"/\1/p' $CFG); do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; done
exit 0
