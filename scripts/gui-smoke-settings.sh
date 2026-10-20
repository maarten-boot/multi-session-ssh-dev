#!/bin/bash
# Headless GUI test of configurable keys and the settings dialog (M5).
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-settings.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
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
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+alt+1; k ctrl+shift+1; sleep 1.5
t 'printf "\e[33myellow\e[0m \e[92mlight-green\e[0m \e[44;37m blue-bg \e[0m \e[1mbold\e[0m\n"'
shot 1-warning-and-light
k ctrl+Tab; sleep 1
shot 2-ctrl-tab                                # -> "two"
k ctrl+shift+1; sleep 1.5; t 'pwd >$SW_OUT/after-ctrl-tab'
k ctrl+shift+p; sleep 0.5; shot 3-switcher-default-kept; k Escape
k ctrl+alt+1; sleep 12                         # let the hidden Remote tab go idle
k ctrl+shift+comma; sleep 0.8
shot 4-settings
k d; k Tab; k m; k Tab; k Tab; k Tab           # theme=dark, colors=mono, -> font family
k Return; sleep 4
shot 5-dark-mono-live                          # no false activity badge from the repaint
# A bad value on a page that is not shown: Save says why (the form does not
# try to focus the hidden field and silently do nothing).
k ctrl+shift+comma; sleep 0.8
k Tab; k Tab; k Tab; k Tab; k ctrl+a; xdotool type '99'   # font size 99 on Appearance
xdotool mousemove $((60+551)) $((50+109)) click 1; sleep 0.5   # the Commands page
xdotool mousemove $((60+931)) $((50+520)) click 1; sleep 1   # Save: the message names the problem
shot 5b-error-on-hidden-page
k Escape; sleep 0.5
check "a bad value is not saved" test -z "$(grep 'font_size = 99' $CFG)"
alive
kill $APP; wait $APP 2>/dev/null
expect "rebound Ctrl+Tab goes to the next session" /usr "$(cat $SW_OUT/after-ctrl-tab 2>&1)"
expect "Settings saved theme and colors" 'theme = "dark"|color_mode = "mono"|' \
	"$(sed -n '/^\[ui\]/,/^\[/p' $CFG | grep -E '^ *(theme|color_mode) =' | sed 's/^ *//' | tr '\n' '|')"
expect "rebinding kept" 'next_session = "Ctrl+Tab"' "$(grep -E '^ *next_session =' $CFG | sed 's/^ *//')"
for id in s-set1 s-set2; do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; done
finish
