#!/bin/bash
# Headless GUI test of the example commands and prompts (SPEC T5, T6): a first
# start (no config file) gets them; an existing setup is never changed by
# itself, and Add example commands… / Add example prompts… add only the
# names it lacks, leaving the user's own as they are.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-examples.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
k() { xdotool key "$@"; sleep 0.6; }
shot() { import -window root $OUT/$1.png; }
# click <x> <y>: at a position in the window.
click() {
	local X Y
	eval "$(xdotool getwindowgeometry --shell $W | grep -E '^[XY]=')"
	xdotool mousemove $((X+$1)) $((Y+$2)) click 1; sleep 0.8
}
start() {
	$BIN -config $1 >>$OUT/app.log 2>&1 & APP=$!
	wait_window; sleep 3; xdotool windowactivate --sync $W 2>/dev/null; sleep 1
}
stop() { kill $APP; wait $APP 2>/dev/null; }
ncmds() { grep -c '^ *\[\[ui.commands\]\]' $1; }

# 1. A first start: no config file.
rm -rf $OUT/first; mkdir -p $OUT/first
start $OUT/first/config.toml
shot 1-first
P1=$OUT/first/notes/_app/Prompts.md
check "first start: the Prompts page is written" grep -q '^## CrossCheck' $P1
k ctrl+shift+comma; sleep 1
click 935 489                                   # Save writes the config
check "first start: the example commands are saved" grep -q 'claude-compact' $OUT/first/config.toml
stop

# 2. An existing setup: its own git-push and its own review prompt.
mkdir -p $OUT/own/notes/_app
cat >$OUT/own/config.toml <<'TOML'
[[ui.commands]]
name = "git-push"
command = "git push --force-with-lease"
group = "Git"
where = "local"
TOML
printf '# Prompts\n\n## crosscheck\nMy own cross-check.\n' >$OUT/own/notes/_app/Prompts.md
start $OUT/own/config.toml
check "existing config: no commands added at start" test "$(ncmds $OUT/own/config.toml)" = 1
check "existing Prompts page untouched at start" test "$(grep -c '^## ' $OUT/own/notes/_app/Prompts.md)" = 1
k ctrl+shift+comma; sleep 1
click 519 109                                         # Settings → Commands
shot 2-commands
click 473 269                                 # Add example commands…
shot 3-confirm
k Return; sleep 1
shot 4-added
click 461 237; k Return; sleep 1.5                    # Enter in a field saves
check "the examples are added" grep -q 'claude-compact' $OUT/own/config.toml
check "the own git-push is kept" grep -q 'git push --force-with-lease' $OUT/own/config.toml
expect "git-push once" 1 "$(grep -c 'name = "git-push"' $OUT/own/config.toml)"
k ctrl+shift+m; sleep 1.5
shot 5-prompts
click 1032 180                                   # Add example prompts…
shot 6-confirm
k Return; sleep 1.5
shot 7-prompts-added
P2=$OUT/own/notes/_app/Prompts.md
check "the own prompt is kept" grep -q '^My own cross-check.' $P2
expect "CrossCheck not added twice (names ignore case)" 1 "$(grep -ci '^## crosscheck' $P2)"
check "the other example prompts are added" grep -q '^## New Scenario' $P2
alive
stop
finish
