#!/bin/bash
# Headless GUI test of the Q&A tab against a fake Claude API
# (fake-claude-api.py): a question and its streamed answer, an SVG picture,
# the conversation sent back with the next question and kept across a
# restart, Stop, New conversation; every question and answer in the log.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-qa.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
REQ=$OUT/requests.jsonl; : >$REQ
python3 "$(dirname "$0")/fake-claude-api.py" $OUT/port $REQ &
FAKE=$!
for i in $(seq 20); do [ -s $OUT/port ] && break; sleep 0.2; done
CFG=$OUT/config.toml
cat >$CFG <<TOML
[ui]
qa_model = "claude-test-model"
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-qa"
name = "asker"
host = "local"
local_path = "/tmp"
start_mode = "shell"
TOML
CONV=$XDG_DATA_HOME/session-workbench/qa/s-qa.json
LOGF=$XDG_DATA_HOME/session-workbench/logs/local/tmp/qa-$(date +%F).md
k() { xdotool key "$@"; sleep 0.5; }
shot() { import -window root $OUT/$1.png; }
ask() { xdotool type --delay 10 "$1"; k Return; sleep ${2:-2}; }
start() {
	env -u ANTHROPIC_AUTH_TOKEN -u ANTHROPIC_PROFILE ANTHROPIC_API_KEY=test \
		ANTHROPIC_BASE_URL=http://127.0.0.1:$(cat $OUT/port) $BIN -config $CFG >>$OUT/app.log 2>&1 &
	APP=$!
	wait_window
	sleep 3; xdotool windowactivate --sync $W 2>/dev/null
	k ctrl+alt+1; k ctrl+shift+6; sleep 1.5            # Q&A, the 6th tab
}
start
shot 1-empty
ask "What is the answer?"
shot 2-answer                                          # text and a code block
ask "Draw a picture" 3
shot 3-picture                                         # the SVG drawn as an image
expect "first question: model, fallbacks, one message" "claude-test-model default 1" \
	"$(sed -n 1p $REQ | python3 -c 'import json,sys; b=json.load(sys.stdin)["body"]; print(b["model"], b["fallbacks"], len(b["messages"]))')"
expect "second question sends the first back" "3 assistant" \
	"$(sed -n 2p $REQ | python3 -c 'import json,sys; m=json.load(sys.stdin)["body"]["messages"]; print(len(m), m[1]["role"])')"
check "the beta header asks for fallbacks" grep -q 'server-side-fallback-2026-07-01' $REQ
alive
kill $APP; wait $APP 2>/dev/null

start                                                  # the conversation is back after a restart
shot 4-after-restart
ask "And once more?"
expect "after the restart the whole conversation is sent" "5" \
	"$(sed -n 3p $REQ | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["body"]["messages"]))')"
ask "Be slow please" 1.5
k Tab; k Return; sleep 1.5                             # Stop (the Ask button)
shot 5-stopped
expect "turns saved (the stopped one too)" "4 stopped" \
	"$(python3 -c 'import json,sys; t=json.load(open(sys.argv[1]))["turns"]; print(len(t), t[-1].get("error"))' $CONV)"
xdotool mousemove 1145 104 click 1; sleep 1                # New conversation (window at 60,50)
shot 6-new-conversation
check "New conversation removes the saved conversation" test ! -e $CONV
for want in "What is the answer?" "The answer is 42." "Draw a picture" "fake svg" "And once more?" \
	"Be slow please" "**Error:** stopped" "Tokens: 20 in (8 from cache), 20 out"; do
	check "logged: $want" grep -qF -- "$want" $LOGF
done
alive
kill $APP; wait $APP 2>/dev/null
kill $FAKE 2>/dev/null
finish
