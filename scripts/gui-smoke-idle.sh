#!/bin/bash
# Headless GUI test of stopping an idle claude (SPEC F17) with a fake claude
# (it runs the SessionStart hook of its --settings, as claude does, and
# waits): idle_stop = "1m", checked every 3 s (SW_IDLE_TICK). "fake" is
# connected at start (eager) but not on screen: after a minute without
# output its claude is stopped, the pane says so and the list marks it 💤;
# the session on screen ("shown", same start command) is not stopped.
# Opening "fake" resumes it with --resume <conversation>; the mark goes;
# "shown", which left the screen then, counts as idle from then on.
# Session menu → Stop claude now on the session on screen: stopped at
# once and it stays so (not resumed while on screen); Resume claude brings
# it back. A busy claude (a command runs) is stopped after a question.
# Edit… offers the session's own idle time; Settings the global one
# (set to never and saved).
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-idle.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
A=s-ida$$; B=s-idb$$
mkdir -p $OUT/p1 $OUT/p2 $OUT/bin
cat >$OUT/bin/claude <<'SH'
#!/bin/sh
here=$(pwd)
echo "$*" >> "$here/args"
settings=
while [ $# -gt 0 ]; do [ "$1" = --settings ] && { settings=$2; shift; }; shift; done
cmd=$(printf '%s' "$settings" | python3 -c 'import json,sys; print(json.load(sys.stdin)["hooks"]["SessionStart"][0]["hooks"][0]["command"])')
printf '{"session_id":"conv-%s","transcript_path":"%s/tx.jsonl"}' "$(basename "$here")" "$here" | sh -c "$cmd"
echo "fake claude ready"
while :; do
	# busy: "a Bash tool command" runs (shell-snapshots in its arguments)
	if [ -f "$here/busy" ]; then sh -c ': shell-snapshots; sleep 2'; fi
	sleep 1
done
SH
chmod +x $OUT/bin/claude
echo '{}' >$OUT/p1/tx.jsonl; echo '{}' >$OUT/p2/tx.jsonl
CFG=$OUT/config.toml
cat >$CFG <<TOML
[ui]
connect_on_start = "eager"
idle_stop = "1m"
[[ui.start_commands]]
name = "fake"
command = "$OUT/bin/claude --continue || $OUT/bin/claude"
[[ui.start_commands]]
name = "shell"
command = ""
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "$B"
name = "shown"
host = "local"
local_path = "$OUT/p2"
start_mode = "fake"
[[sessions]]
id = "$A"
name = "fake"
host = "local"
local_path = "$OUT/p1"
start_mode = "fake"
TOML
ST=$HOME/.local/state/session-workbench/claude
k() { xdotool key "$@"; sleep 0.6; }
shot() { import -window root $OUT/$1.png; }
lines() { [ -f "$1" ] && wc -l <"$1" | tr -d ' ' || echo 0; }
stopped() { test -f $ST/$A.stopped; }
not_stopped() { ! stopped; }
fake_runs() { pgrep -f "^/bin/sh $OUT/bin/claude --settings .*$1\.tmp" >/dev/null; }
fake_gone() { ! fake_runs "$1"; }
cleanup() {
	tmux kill-session -t "=cw-$A" 2>/dev/null; tmux kill-session -t "=cw-$B" 2>/dev/null
	rm -f $ST/$A $ST/$A.stopped $ST/$B $ST/$B.stopped
}
trap cleanup EXIT
SW_IDLE_TICK=3s $BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null; sleep 6
shot 1-start
check "fake claude started in both (with the hooks)" fake_runs $A
check "its hook wrote the state" test -s $ST/$A
in_pane "the Remote tab of fake runs it" $A "fake claude ready"

# A minute without output, checked every 3 s.
for i in $(seq 40); do stopped && break; sleep 2; done
sleep 4
shot 2-stopped
check "after a minute idle: fake is stopped" stopped
check "its claude ended (and nothing restarted it)" fake_gone $A
in_pane "the pane says so" $A "claude stopped after 1 min without input"
check "the session on screen keeps its claude" fake_runs $B
expect "fake claude ran once" 1 "$(lines $OUT/p1/args)"
check "the app logged it" grep -q "idle: stopped claude of $A" $OUT/app.log

xdotool mousemove 114 175 click 1; sleep 8              # open "fake"
shot 3-resumed
check "opening it resumes it" not_stopped
check "claude runs again" fake_runs $A
expect "started twice now" 2 "$(lines $OUT/p1/args)"
check "with --resume <its conversation>" grep -q -- "--resume conv-p1" <(tail -1 $OUT/p1/args)
check "not --continue" sh -c "! tail -1 $OUT/p1/args | grep -q -- --continue"

# "shown" left the screen when "fake" was opened: idle from then on, so
# not stopped at once although its claude waited longer.
sleep 8
check "the session that left the screen is not stopped at once" test ! -f $ST/$B.stopped
check "its claude still runs" fake_runs $B

menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 1.5; }
# Stop claude now ("fake" is on screen): S → Switch host…, Stop claude now.
xdotool mousemove 114 175 click 3; menu s s
sleep 3
shot 4-stopped-now
check "Stop claude now: stopped at once" stopped
check "its claude ended" fake_gone $A
in_pane "the pane says how it comes back" $A "claude stopped from Session Workbench"
sleep 8
check "on screen it stays stopped" stopped
# Resume claude: R → Record the Remote tab, Requirements…, Resume claude.
xdotool mousemove 114 175 click 3; menu r r r
sleep 4
check "Resume claude: running again" fake_runs $A
check "not stopped any more" not_stopped
check "resumed with --resume" grep -q -- "--resume conv-p1" <(tail -1 $OUT/p1/args)
# Busy: a command runs under claude.
touch $OUT/p1/busy; sleep 3
xdotool mousemove 114 175 click 3; menu s s
sleep 2
shot 5-busy-question
check "a busy claude is not stopped without asking" fake_runs $A
k Return; sleep 4                                         # Stop claude
check "after the question it is stopped" stopped
check "and the command it ran ended too" sh -c "! pgrep -f '[:] shell-snapshots' >/dev/null"
rm -f $OUT/p1/busy
xdotool mousemove 114 175 click 3; menu r r r; sleep 4
check "and resumed again" fake_runs $A

# Edit…: the session's own idle time.
xdotool mousemove 114 175 click 3; menu                   # Edit… (focused first)
shot 6-edit
k Escape
k ctrl+shift+comma; sleep 1                                    # Settings
xdotool mousemove 510 159 click 1; sleep 1                # → Terminals
shot 7-settings
xdotool mousemove 535 289 click 1; sleep 0.8; k Home; k Return   # the idle time: never
xdotool mousemove 990 574 click 1; sleep 2                # Save
check "Settings saves it (never: no idle_stop left)" sh -c "! grep -q idle_stop $CFG"
alive
kill $APP; wait $APP 2>/dev/null
finish
