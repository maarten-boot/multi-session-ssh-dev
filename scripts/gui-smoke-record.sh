#!/bin/bash
# Headless GUI test of Remote tab recordings: off by default; switched on
# for one session from its menu it records from that moment, as text with
# timestamped "> " input lines and input at a password prompt hidden, into
# <data dir>/logs/<host>/<session path>/YYYYMMDD-HHMM.log; switched off it
# stops; the global setting (Settings) starts it for every session.
# Needs `ssh localhost` without prompts and tmux.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-record.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
P=$OUT/proj; mkdir -p $P
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-rec"
name = "recorded"
host = "local"
local_path = "$P"
start_mode = "shell"
TOML
LOGS=$XDG_DATA_HOME/session-workbench/logs/local${P}
k() { xdotool key "$@"; sleep 0.5; }
t() { xdotool type --delay 15 -- "$1"; xdotool key Return; sleep ${2:-1}; }
shot() { import -window root $OUT/$1.png; }
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 1; }
logtext() { cat "$LOGS"/*.log 2>/dev/null; }
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-rec'" 2>/dev/null
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+shift+3; sleep 4
t 'echo not-recorded-1'
check "nothing recorded by default" test ! -e "$LOGS"

# On for this session (session menu): records from now.
click 150 85 3; menu r                           # "Record the Remote tab"
sleep 1; shot 1-recording
t 'echo rec-$((6*7))'
t "read -s -p 'Password: ' x; echo; echo got-\${#x}-chars"
t 's3cret' 1.5
# A program of its own: tmux renames the window and redraws its status bar
# after the prompt, so the prompt is no longer the line the cursor is on.
t "python3 -c 'import getpass; print(\"py-got-%d-chars\" % len(getpass.getpass(\"Password: \")))'" 2
t 'pyS3cret' 1.5
F=$(ls "$LOGS"/*.log 2>/dev/null | head -1)
check "log file named YYYYMMDD-HHMM.log" grep -qE '/[0-9]{8}-[0-9]{4}\.log$' <<<"$F"
check "input recorded with a timestamp" grep -qE '^[0-9-]{10} [0-9:]{8} > echo rec-\$\(\(6\*7\)\)$' <<<"$(logtext)"
check "output recorded as text" grep -qx 'rec-42' <<<"$(logtext)"
check "password input hidden" grep -qE '> \[hidden\]$' <<<"$(logtext)"
check "password not in the log" test -z "$(logtext | grep -F s3cret)"
check "output after it" grep -qx 'got-6-chars' <<<"$(logtext)"
check "password hidden after a window rename" test "$(logtext | grep -cE '> \[hidden\]$')" -ge 2
check "that password not in the log" test -z "$(logtext | grep -F pyS3cret)"
check "output after that" grep -qx 'py-got-8-chars' <<<"$(logtext)"
check "not recorded before switching on" test -z "$(logtext | grep -F not-recorded-1)"
expect "log file is private" "-rw-------" "$(stat -c %A "$F" 2>&1)"

# Off again.
click 150 85 3; menu s                           # "Stop recording the Remote tab"
t 'echo not-recorded-2'
check "recording stopped line" grep -q '^# recording stopped at' <<<"$(logtext)"
check "not recorded after switching off" test -z "$(logtext | grep -F not-recorded-2)"
check "off like the global setting: no override saved" test -z "$(grep '^record' $CFG)"

# The global setting (Settings): records every session from that moment.
k ctrl+shift+comma; sleep 1
click 328 531                                    # "Record the Remote tabs of all sessions"
shot 2-settings
click 475 607; k Return; sleep 1.5               # Enter in the folder field saves
expect "global setting saved" 'record_remote = true' "$(grep -o 'record_remote = true' $CFG)"
shot 3-global-on
t 'echo global-rec-ok' 1.5
check "recording again after the global switch" grep -qx 'global-rec-ok' <<<"$(logtext)"

alive
kill $APP; wait $APP 2>/dev/null
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-rec'" 2>/dev/null
finish
