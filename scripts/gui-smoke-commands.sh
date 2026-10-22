#!/bin/bash
# Headless GUI test of the command menu and start commands: a session that
# starts with a start command of its own ([[ui.start_commands]]); the
# command menu from the terminal's right-click menu and from Commands ▾ in
# the bar, with the session's own commands first; a command with run = false
# is only typed; Settings → Commands adds one; Prompts ▾ pastes the
# session's own prompts (its notes' Prompts page) and those for all sessions,
# as does a section of the terminal's right-click menu.
# Needs `ssh localhost` without prompts and tmux.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-commands.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
RAN=$OUT/ran.txt; : >$RAN
CFG=$OUT/config.toml
cat >$CFG <<TOML
[ui]
[[ui.commands]]
name = "answer"
command = "echo answer-\$((6*7)) >>$RAN"
[[ui.commands]]
name = "typed only"
command = "echo typed-\$((1+1)) >>$RAN"
run = false
[[ui.start_commands]]
name = "custom"
command = "echo started-\$((2+3))"
[[ui.start_commands]]
name = "shell"
command = ""
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-cmd"
name = "commands"
host = "local"
local_path = "$OUT"
start_mode = "custom"
commands = [{ name = "own", command = "echo own-\$((3*3)) >>$RAN" }]
TOML
# Prompts: the session's own Prompts page, and the one for all sessions.
rm -rf $OUT/notes; mkdir -p -m 700 $OUT/notes $OUT/notes/s-cmd $OUT/notes/_app
printf '## own prompt\necho own-prompt-$((2*4)) >>%s\n' "$RAN" >$OUT/notes/s-cmd/Prompts.md
printf '## global prompt\necho global-prompt-$((3*4)) >>%s\n' "$RAN" >$OUT/notes/_app/Prompts.md
k() { xdotool key "$@"; sleep 0.5; }
shot() { import -window root $OUT/$1.png; }
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 1.5; }
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-cmd'" 2>/dev/null
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+shift+3; sleep 4
in_pane "the session started with its start command" s-cmd "started-5"
shot 1-remote

# The right-click menu of the terminal: its Commands section.
click 700 500 3; sleep 0.5; shot 2-context-menu; menu a       # "answer"
expect "a global command from the right-click menu ran" "answer-42" "$(cat $RAN)"
# Commands ▾ in the bar: the session's own command first.
click ${CMDX:-700} 17; sleep 0.5; shot 3-commands-button; menu o   # "own"
expect "the session's command from the bar ran" "answer-42|own-9|" "$(tr '\n' '|' <$RAN)"
click ${CMDX:-700} 17; menu t                    # "typed only": typed, no Enter
sleep 1
check "typed only: not run" test -z "$(grep typed $RAN)"
in_pane "typed only: on the command line" s-cmd 'echo typed-$((1+1))'
k Return; sleep 1
check "and run with Enter" grep -qx typed-2 $RAN

# Prompts ▾: pasted without Enter, the session's own first.
: >$RAN
click ${PROMPTX:-800} 17; sleep 0.5; shot 3b-prompts-button; menu   # the first: "own prompt"
check "a prompt is pasted, not run" test ! -s $RAN
in_pane "the session's prompt is on the command line" s-cmd 'echo own-prompt-$((2*4))'
k Return; sleep 1
click ${PROMPTX:-800} 17; menu g; k Return; sleep 1                # "global prompt"
expect "both prompts ran after Enter" "own-prompt-8|global-prompt-12|" "$(tr '\n' '|' <$RAN)"
# The terminal's right-click menu has them too, after the commands.
click 700 500 3; sleep 0.5; shot 3c-context-prompts; menu g; k Return; sleep 1   # "global prompt"
expect "a prompt from the right-click menu ran" "own-prompt-8|global-prompt-12|global-prompt-12|" "$(tr '\n' '|' <$RAN)"

# Settings → Commands: the lists, and a command added there.
k ctrl+shift+comma; sleep 1
shot 4-settings-pages
click 551 109; sleep 0.5                          # the Commands page
shot 5-settings-commands
click 347 289; sleep 0.5                          # + Add (command menu): the new row's name has focus
xdotool type --delay 10 'where'; k Tab; xdotool type --delay 10 'pwd'
k Return; sleep 1.5                               # Enter in a field saves
expect "a command added in Settings is saved" 'name = "where"|command = "pwd"' "$(grep -A1 'name = "where"' $CFG | tr -d ' ' | sed 's/=/ = /' | paste -sd'|')"
click ${CMDX:-700} 17; sleep 0.5; shot 6-menu-with-new-command; k Escape
xdotool mousemove 284 137 click 1; sleep 0.5; menu     # session ⋯ → Edit… (focused first): start command and own commands
shot 7-session-dialog; k Escape
alive
kill $APP; wait $APP 2>/dev/null
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-cmd'" 2>/dev/null
finish
