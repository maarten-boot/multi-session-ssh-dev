#!/bin/bash
# Headless GUI test of what claude cost (SPEC F18), with made-up
# transcripts in the project folder of session "costs" (in the real
# ~/.claude/projects, removed afterwards), checked every 3 s
# (SW_IDLE_TICK): the top bar shows today and the total at API prices, the
# list balloon the session's own; a new reply adds to it; a model without
# a price makes the amounts a lower bound (≥); clicking the figure resets
# the total (today stays); Settings hides it.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-cost.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
A=s-ca$$; B=s-cb$$
mkdir -p $OUT/p1 $OUT/p2
PROJ=$HOME/.claude/projects/$(cd $OUT/p1 && pwd -P | sed 's/[^a-zA-Z0-9]/-/g')
cleanup() {
	tmux kill-session -t "=cw-$A" 2>/dev/null; tmux kill-session -t "=cw-$B" 2>/dev/null
	rm -rf "$PROJ"
}
trap cleanup EXIT
mkdir -p "$PROJ/conv-a/subagents"
now=$(date -u +%Y-%m-%dT%H:%M:%S.000Z)
# reply <id> <model> <input> <output>: a transcript line of a reply.
reply() {
	printf '{"parentUuid":"p","cwd":"%s","message":{"model":"%s","id":"%s","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":%s,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":%s}},"type":"assistant","timestamp":"%s"}\n' \
		"$OUT/p1" "$2" "$1" "$3" "$4" "$now"
}
reply msg_1 claude-opus-5-5 0 100000 >"$PROJ/conv-a.jsonl"              # $2.00
reply msg_s claude-haiku-5-5 1000000 0 >"$PROJ/conv-a/subagents/agent-1.jsonl"  # $0.50 (a long prompt)
CFG=$OUT/config.toml
cat >$CFG <<TOML
[ui]
cost_per_session = true
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "$A"
name = "costs"
host = "local"
local_path = "$OUT/p1"
start_mode = "shell"
[[sessions]]
id = "$B"
name = "other"
host = "local"
local_path = "$OUT/p2"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep 0.6; }
shot() { import -window root $OUT/$1.png; }
SW_IDLE_TICK=3s $BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null; sleep 5
shot 1-start
LEDGER=$XDG_DATA_HOME/session-workbench/cost.json
check "the counts are kept" grep -q '"claude-opus-5-5"' $LEDGER
check "the subagent's transcript is counted" grep -q '"claude-haiku-5-5:long"' $LEDGER
xdotool mousemove 150 140; sleep 2.5
shot 2-balloon
xdotool mousemove 700 400; sleep 0.5
# A new reply: $10 more.
reply msg_2 claude-sonnet-5-5 0 1000000 >>"$PROJ/conv-a.jsonl"
sleep 6
shot 3-more
check "the new reply is counted" grep -q '"claude-sonnet-5-5"' $LEDGER
# A model without a price: the amounts become a lower bound.
reply msg_3 claude-new-9 1000 1000 >>"$PROJ/conv-a.jsonl"
sleep 6
shot 4-unpriced
# Reset: click the figure, confirm.
xdotool mousemove 1254 66 click 1; sleep 1
shot 5-reset-question
k Return; sleep 1
shot 6-reset
check "the reset is kept" grep -q '"reset_at"' $LEDGER
k ctrl+shift+comma; sleep 1
xdotool mousemove 510 159 click 1; sleep 1                # → Terminals
shot 7-settings
xdotool mousemove 388 391 click 1; sleep 0.3              # Show what claude cost: off
xdotool mousemove 990 626 click 1; sleep 2                # Save
shot 8-hidden
check "Settings saves it off" grep -q 'show_cost = false' $CFG
alive
kill $APP; wait $APP 2>/dev/null
finish
