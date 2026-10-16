#!/bin/bash
# Headless GUI test of configurable keys and the settings dialog (M5).
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-settings.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
CFG=$OUT/config.toml
cat >$CFG <<TOML
[keys]
next_session = "Ctrl+Tab"
quick_switcher = "Ctrl+Shift+Banana"   # invalid: warned, default kept
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-set1"
name = "one"
host = "local"
local_path = "/tmp"
start_mode = "shell"
[[sessions]]
id = "s-set2"
name = "two"
host = "local"
local_path = "/usr"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep 0.6; }
t() { xdotool type --delay 8 "$1"; xdotool key Return; sleep ${2:-1}; }
shot() { import -window root $OUT/$1.png; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
for i in $(seq 40); do W=$(xdotool search --name "Session Workbench" 2>/dev/null | head -1); [ -n "$W" ] && break; sleep 0.25; done
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+alt+1; k ctrl+shift+1; sleep 1.5
t 'printf "\e[33myellow\e[0m \e[92mlight-green\e[0m \e[44;37m blue-bg \e[0m \e[1mbold\e[0m\n"'
shot 1-warning-and-light
k ctrl+Tab; sleep 1
shot 2-ctrl-tab                                # -> "two"
k ctrl+shift+p; sleep 0.5; shot 3-switcher-default-kept; k Escape
k ctrl+alt+1; sleep 12                         # let the hidden Remote tab go idle
k ctrl+shift+comma; sleep 0.8
shot 4-settings
k d; k Tab; k m; k Tab; k Tab; k Tab           # theme=dark, colors=mono, -> font family
k Return; sleep 4
shot 5-dark-mono-live                          # no false activity badge from the repaint
kill $APP; wait $APP 2>/dev/null
grep -E '^ *(theme|color_mode) =' $CFG
for id in s-set1 s-set2; do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; done
exit 0
