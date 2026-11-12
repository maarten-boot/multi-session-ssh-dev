#!/bin/bash
# Screenshots for docs/TUTORIAL.md, taken headless with made-up sessions on
# localhost (as the GUI tests do): a separate profile, data and config in
# <outdir>, nothing of the real setup. The pictures go to docs/tutorial/.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/tutorial-shots.sh <binary> <outdir> [step]
BIN=$1; OUT=$2; STEP=${3:-all}; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
DOCS=$(cd "$(dirname "$0")/.." && pwd)/docs/tutorial; mkdir -p $DOCS
EX=$(cd "$(dirname "$0")/.." && pwd)/internal/examples   # the examples the app ships
PROF=tutorial
cleanup() {
	for s in $(tmux ls -F '#S' 2>/dev/null | grep "^cw-$PROF-"); do tmux kill-session -t "=$s"; done
	rm -rf "$HOME/.local/state/$PROF"
	[ -n "$CPROJ" ] && rm -rf "$CPROJ"
}
trap cleanup EXIT
k() { xdotool key "$@"; sleep 0.6; }
# shot <name>: the app window only.
shot() { sleep 0.5; import -window $W $DOCS/$1.png; }
# click <x> <y> [button]: at a position in the window.
click() {
	local X Y
	eval "$(xdotool getwindowgeometry --shell $W | grep -E '^[XY]=')"
	xdotool mousemove $((X+$1)) $((Y+$2))
	[ "${3:-1}" = 0 ] || xdotool click ${3:-1}
	sleep 0.8
}
start() {
	env PATH=$OUT/fakebin:$PATH SW_IDLE_TICK=3s $BIN -config $1 -profile $PROF >>$OUT/app.log 2>&1 & APP=$!
	wait_window; sleep 3; xdotool windowactivate --sync $W 2>/dev/null; sleep 1
}
stop() { kill $APP; wait $APP 2>/dev/null; }

# 1. The first start: no hosts yet.
if [ $STEP = all ] || [ $STEP = first ]; then
	: >$OUT/empty.toml
	start $OUT/empty.toml
	shot 01-first-start
	# The button is a blue primary one (it was white text on light grey).
	expect "Add host… button is blue" "srgb(14,99,156)" \
		"$(convert $DOCS/01-first-start.png -format '%[pixel:p{740,428}]' info:)"
	click 760 428                                   # Add host…
	xdotool type --delay 20 agent-vm; k Tab; k Tab
	xdotool type --delay 20 me@agent-vm
	shot 02-add-host
	k Escape
	stop
fi

# The demo: a host "agent-vm" (localhost here) sharing ~/share, four
# sessions in two groups, one inactive; a git project with changes, notes
# with a TODO, prompts, commands, links, a fake gh (Web tab runs) and
# made-up cost (transcripts, removed afterwards) and usage.
SHARE=$OUT/home/me/share
P=$SHARE/projects
demo_setup() {
	rm -rf $OUT/home $OUT/notes $OUT/fakebin; mkdir -p $P/webshop $P/api-server $P/blog $P/prototype $OUT/fakebin
	(
		cd $P/webshop && git init -q -b main && git config user.name Me && git config user.email me@example.com
		mkdir -p src tests docs
		printf '# Webshop\\n\\nA small web shop.\\n' >README.md
		printf 'package main\\n\\nfunc main() {}\\n' >src/main.go
		printf 'package main\\n' >src/cart.go
		printf 'package main\\n' >tests/cart_test.go
		printf 'build:\\n\\tgo build ./...\\n' >Makefile
		git add -A && git commit -qm "First version"
		git remote add origin git@github.com:example/webshop.git
		printf 'package main\\n\\n// Checkout\\n' >src/checkout.go
		printf '\\nfunc total() int { return 0 }\\n' >>src/cart.go
	)
	cat >$OUT/fakebin/gh <<'GH'
#!/bin/sh
cat <<'JSON'
[{"databaseId":3,"workflowName":"build","displayTitle":"Checkout: totals with tax","headBranch":"main","event":"push","status":"in_progress","conclusion":"","createdAt":"NOW1","url":"https://github.com/example/webshop/actions/runs/3"},
 {"databaseId":2,"workflowName":"build","displayTitle":"v0.2.0","headBranch":"v0.2.0","event":"push","status":"completed","conclusion":"success","createdAt":"NOW2","url":"https://github.com/example/webshop/actions/runs/2"},
 {"databaseId":1,"workflowName":"build","displayTitle":"Cart tests","headBranch":"main","event":"push","status":"completed","conclusion":"failure","createdAt":"NOW3","url":"https://github.com/example/webshop/actions/runs/1"}]
JSON
GH
	sed -i "s/NOW1/$(date -u -d '-3 min' +%FT%TZ)/; s/NOW2/$(date -u -d '-2 hour' +%FT%TZ)/; s/NOW3/$(date -u -d '-1 day' +%FT%TZ)/" $OUT/fakebin/gh
	printf '#!/bin/sh\n' >$OUT/fakebin/xdg-open
	chmod +x $OUT/fakebin/*
	mkdir -p $OUT/notes/s-webshop $OUT/notes/_app
	cat >$OUT/notes/s-webshop/Home.md <<'MD'
# Webshop

Working on the checkout.

## Before the release
- [x] cart tests
- [ ] TODO: totals with tax
- [ ] update the README
MD
	# The prompts and commands are the examples the app ships.
	cp $EX/prompts.md $OUT/notes/_app/Prompts.md
	cat >$OUT/config.toml <<TOML
groups = ["Work", "Private"]

[ui]
cost_per_session = true
cost_daily_limit = 20

$(grep -v '^#' $EX/commands.toml)

[[ui.start_commands]]
name = "claude"
command = '''tmux set status off >/dev/null; PS1='me@agent-vm:\\W\\$ ' exec bash --norc --noprofile'''

[local]
shell = "/bin/bash"
args = ["--norc", "--noprofile"]

[[hosts]]
name = "agent-vm"
ssh_target = "localhost"
local_prefix = "$SHARE"
remote_prefix = "$SHARE"

[[sessions]]
id = "s-webshop"
name = "webshop"
description = "The shop's web site.\nNow: checkout with tax."
host = "agent-vm"
local_path = "$P/webshop"
start_mode = "claude"
env = { PS1 = 'me@laptop:\\W\\$ ' }
group = "Work"
links = [{ name = "Staging", url = "https://staging.example.com" }]

[[sessions]]
id = "s-api"
name = "api-server"
host = "agent-vm"
local_path = "$P/api-server"
start_mode = "claude"
env = { PS1 = 'me@laptop:\\W\\$ ' }
group = "Work"

[[sessions]]
id = "s-blog"
name = "blog"
host = "agent-vm"
local_path = "$P/blog"
start_mode = "claude"
env = { PS1 = 'me@laptop:\\W\\$ ' }
group = "Private"

[[sessions]]
id = "s-proto"
name = "prototype"
host = "agent-vm"
local_path = "$P/prototype"
start_mode = "claude"
env = { PS1 = 'me@laptop:\\W\\$ ' }
inactive = true
TOML
}

# Made-up claude transcripts for the webshop (cost) and the subscription
# usage a status line saved.
cost_setup() {
	CPROJ=$HOME/.claude/projects/$(cd $P/webshop && pwd -P | sed 's/[^a-zA-Z0-9]/-/g')
	mkdir -p "$CPROJ" $HOME/.local/state/$PROF/claude
	local now; now=$(date -u +%Y-%m-%dT%H:%M:%S.000Z)
	for i in 1 2 3; do
		printf '{"cwd":"%s","message":{"model":"claude-opus-5-5","id":"msg_%s","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":20000,"cache_creation_input_tokens":0,"cache_read_input_tokens":400000,"output_tokens":60000}},"type":"assistant","timestamp":"%s"}\n' \
			"$P/webshop" $i "$now"
	done >"$CPROJ/tutorial.jsonl"
	printf '{"rate_limits":{"five_hour":{"used_percentage":23.5,"resets_at":%s},"seven_day":{"used_percentage":41.2,"resets_at":%s}}}' \
		$(( $(date +%s) + 7200 )) $(( $(date +%s) + 3*86400 )) >$HOME/.local/state/$PROF/claude/usage.json
}

if [ $STEP = all ] || [ $STEP = main ]; then
	demo_setup
	cost_setup
	start $OUT/config.toml
	click 100 260; sleep 1                          # webshop
	k ctrl+shift+3; sleep 4                         # Remote
	xdotool type --delay 30 "git log --oneline"; k Return; sleep 1
	shot 11-remote
	k ctrl+shift+1; sleep 2                         # Local
	xdotool type --delay 30 "git status --short"; k Return; sleep 1
	shot 12-local
	k ctrl+shift+2; sleep 2
	shot 13-files
	k ctrl+shift+4; sleep 2
	shot 14-notes
	k ctrl+shift+5; sleep 3
	shot 15-web
	k ctrl+shift+6; sleep 2
	shot 16-qa
	# The dialogs show the default start commands, not the demo's shell.
	stop
	sed -i '/^\[\[ui.start_commands\]\]/,/^command/d' $OUT/config.toml
	start $OUT/config.toml
	k ctrl+shift+3; sleep 2
	click 752 16; click 800 110 0; sleep 0.8        # Commands ▾, the mouse on "git" (no tooltip)
	shot 17-commands
	k Escape
	click 840 16; click 900 85 0; sleep 0.8         # Prompts ▾, the mouse on the first
	shot 18-prompts
	k Escape
	k ctrl+shift+m; sleep 1.5                       # notes for all sessions
	click 293 124; sleep 1                          # its Prompts page
	shot 19-app-prompts
	k Escape
	xdotool mousemove 1 1; click 100 260 3; sleep 0.5   # the session's menu
	shot 20-session-menu
	k Escape
	xdotool mousemove 1 1; click 75 84 3; click 200 175 0   # the host's menu, the mouse in it
	shot 22-host-menu
	k Escape
	click 44 457; click 44 620; sleep 0.5           # Active and Inactive open
	xdotool mousemove 600 600; sleep 0.3
	shot 21-sidebar
	click 100 260; sleep 1                          # webshop again
	xdotool mousemove 600 600; click 100 255 0; sleep 2.5   # its balloon
	shot 23-balloon
	click 70 656; sleep 1.5                         # the inactive prototype (under Inactive)
	shot 24-inactive
	click 100 260; sleep 1
	k ctrl+shift+n; sleep 1
	shot 03-new-session
	k Escape
	k ctrl+shift+comma; sleep 1
	shot 30-settings
	click 519 109; sleep 0.7
	shot 31-settings-commands
	click 888 109; sleep 0.7
	shot 32-settings-cost
	k Escape
	stop
fi
finish
