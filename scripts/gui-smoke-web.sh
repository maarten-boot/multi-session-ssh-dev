#!/bin/bash
# Headless GUI test of the Web tab (links from the git remote, build runs via
# a fake gh, own links) and of a custom tab order with a hidden tab.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-web.sh <binary> <outdir> [measure]
BIN=$1; OUT=$2; MEASURE=$3; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
REPO=$(cd "$(dirname "$0")/.." && pwd)             # this repository: a real GitHub remote
FAKE=$OUT/fakebin; mkdir -p $FAKE; LOG=$OUT/opened.log; : >$LOG
printf '#!/bin/sh\necho "$1" >>%s\n' "$LOG" >$FAKE/xdg-open
cat >$FAKE/gh <<'GH'
#!/bin/sh
cat <<'JSON'
[{"databaseId":8,"workflowName":"build","displayTitle":"Tabs in the order Local, Files, Remote, Notes","headBranch":"main","event":"push","status":"in_progress","conclusion":"","createdAt":"2026-10-16T14:00:00Z","url":"https://github.com/maarten-boot/multi-session-ssh-dev/actions/runs/8"},
 {"databaseId":7,"workflowName":"build","displayTitle":"v0.8.4","headBranch":"v0.8.4","event":"push","status":"completed","conclusion":"success","createdAt":"2026-10-16T12:00:00Z","url":"https://github.com/maarten-boot/multi-session-ssh-dev/actions/runs/7"},
 {"databaseId":6,"workflowName":"build","displayTitle":"v0.8.3","headBranch":"v0.8.3","event":"push","status":"completed","conclusion":"failure","createdAt":"2026-10-16T10:00:00Z","url":"https://github.com/maarten-boot/multi-session-ssh-dev/actions/runs/6"}]
JSON
GH
chmod +x $FAKE/*
CFG=$OUT/config.toml
cat >$CFG <<TOML
[ui]
tab_order = ["web", "local", "files", "remote", "notes"]
hidden_tabs = ["notes"]
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-web"
name = "workbench"
host = "local"
local_path = "$REPO"
start_mode = "shell"
[[sessions]]
id = "s-web2"
name = "other"
host = "local"
local_path = "/tmp"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep 0.5; }
shot() { import -window root $OUT/$1.png; }
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }
env PATH=$FAKE:$PATH DBUS_SESSION_BUS_ADDRESS= $BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+alt+1; k ctrl+shift+1; sleep 2          # first tab = Web (custom order)
shot 1-web
if [ -n "$MEASURE" ]; then kill $APP; exit 0; fi
# Window-relative positions (window at 60,50): link buttons at y=89 (Actions
# x=398), run rows at y=170/198/226, the add-link name field at (335,326).
click 398 89                                    # "Actions ↗"
click 700 198                                   # second run row
click 335 326; xdotool type --delay 15 "Staging"; k Tab
xdotool type --delay 10 "https://staging.example.com/app"; k Return; sleep 1
click 335 326; xdotool type --delay 15 "Bad"; k Tab; xdotool type --delay 10 "javascript:alert(1)"; k Return; sleep 1
shot 2-links-added                              # Staging added; the javascript: link refused
k ctrl+shift+4; sleep 1.5
shot 3-ctrl-shift-4-is-remote                   # 4th visible tab in the custom order
k ctrl+alt+2; sleep 1
shot 4-other-session                            # highlight moves to the other session
alive
kill $APP; wait $APP 2>/dev/null
GH=https://github.com/maarten-boot/multi-session-ssh-dev
expect "opened in the browser: Actions page, second run" "$GH/actions|$GH/actions/runs/7|" "$(tr '\n' '|' <$LOG)"
expect "own link saved, javascript: refused" 'name = "Staging"|url = "https://staging.example.com/app"|' \
	"$(grep -A2 '\[\[sessions.links\]\]' $CFG | grep -E 'name|url' | sed 's/^ *//' | tr '\n' '|')"
for id in s-web s-web2; do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; done
finish
