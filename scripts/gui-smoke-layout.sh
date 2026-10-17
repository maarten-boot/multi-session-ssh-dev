#!/bin/bash
# Headless GUI test of the layout: the font size from the config sizes the
# whole app (sidebar width follows it), the current tab is highlighted like
# the current session, the narrow sidebar (Ctrl+Shift+B) and a width set by
# dragging the sidebar's edge are remembered across a restart, and a
# double-click on the edge resets the width. Checked by sampling screenshot
# pixels (light theme).
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-layout.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
CFG=$OUT/config.toml
cat >$CFG <<TOML
groups = ["Work"]
[ui]
theme = "light"
font_size = 18
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-lay1"
name = "multi-session-ssh-dev"
host = "local"
local_path = "/tmp"
start_mode = "shell"
[[sessions]]
id = "s-lay2"
name = "parser rewrite"
host = "local"
local_path = "/usr"
start_mode = "shell"
group = "Work"
TOML
k() { xdotool key "$@"; sleep 0.6; }
shot() { import -window root $OUT/$1.png; }
# px <shot> <x> <y>: the colour at window position x,y (window at 60,50).
px() { convert $OUT/$1.png -format "%[hex:p{$((60+$2)),$((50+$3))}]" info: | cut -c1-6; }
PANEL=F3F3F3; BG=FFFFFF; SELECTED=CCE4FF   # light theme: --panel, --bg, --focus-selected
start() {
	$BIN -config $CFG >>$OUT/app.log 2>&1 &
	APP=$!
	wait_window
	sleep 3; xdotool windowactivate --sync $W 2>/dev/null
}
start
k ctrl+alt+2; k ctrl+shift+2; sleep 1.5
shot 1-wide-18px
# 18px: the sidebar is 18.5rem = 333px wide (240px at 13px).
expect "sidebar width follows the font size" "$PANEL" "$(px 1-wide-18px 300 600)"
# The current tab (Files, the 2nd) is tinted: sample its row in the bar.
TABS=$(convert $OUT/1-wide-18px.png -crop 1280x1+60+$((50+20)) +repage -format %c histogram:info:)
check "current tab tinted like the current session" grep -q "#$SELECTED" <<<"$TABS"
k ctrl+shift+b; sleep 1
shot 2-narrow
expect "narrow sidebar frees the space" "$BG" "$(px 2-narrow 300 600)"
expect "narrow sidebar still shown" "$PANEL" "$(px 2-narrow 40 600)"
alive
kill $APP; wait $APP 2>/dev/null; sleep 1
start
k ctrl+alt+2; k ctrl+shift+2; sleep 1.5
shot 3-narrow-after-restart
expect "narrow sidebar remembered" "$BG" "$(px 3-narrow-after-restart 300 600)"
k ctrl+shift+b; sleep 1                         # back to wide: other tests click in the sidebar
shot 4-wide-again
expect "Ctrl+Shift+B widens again" "$PANEL" "$(px 4-wide-again 300 600)"
# Drag the sidebar's edge (at 333px) to 520px.
xdotool mousemove $((60+333)) $((50+400)) mousedown 1; sleep 0.3
for x in 380 440 500 520; do xdotool mousemove $((60+x)) $((50+400)); sleep 0.15; done
xdotool mouseup 1; sleep 0.8
shot 5-dragged
expect "dragging the edge widens the sidebar" "$PANEL" "$(px 5-dragged 450 600)"
alive
kill $APP; wait $APP 2>/dev/null; sleep 1
start
sleep 1.5
shot 6-width-after-restart
expect "dragged width remembered" "$PANEL" "$(px 6-width-after-restart 450 600)"
xdotool mousemove $((60+520)) $((50+400)) click --repeat 2 1; sleep 0.8   # double-click the edge
shot 7-default-width
expect "double-click resets the width" "$BG" "$(px 7-default-width 450 600)"
expect "back to the default width" "$PANEL" "$(px 7-default-width 300 600)"
alive
kill $APP; wait $APP 2>/dev/null
for id in s-lay1 s-lay2; do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; done
finish
