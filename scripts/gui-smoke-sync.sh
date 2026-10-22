#!/bin/bash
# Headless GUI test of working-copy sessions (files = "sync"): missing copy,
# Send with preview, Remote tab in the copy, commit there and make a new
# branch, Get: main fast-forwarded and the new branch created here; the full
# transfers: Save agent copy (everything, with .git and ignored files), Take
# all files (uncommitted work), Restore saved copy after the agent copy is
# gone; a session without git, where Get takes the files; deleting the session
# with its saved copy.
# Needs `ssh localhost` without prompts and git; rsync is used when installed
# (SW_NO_RSYNC=1 tests the built-in transfer).
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-sync.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
R=$OUT/repo; WD="sw-guitest-$$"; rm -rf $R; mkdir -p $R
git -C $R init -q -b main; echo one >$R/a.txt; printf 'build/\n' >$R/.gitignore; mkdir $R/build; echo x >$R/build/out
git -C $R add -A; git -C $R -c user.name=t -c user.email=t@example.com commit -q -m base
P=$OUT/plain; rm -rf $P; mkdir -p $P; echo first >$P/notes.txt       # an experiment without git
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "agent"
ssh_target = "localhost"
files = "sync"
remote_workdir = "~/$WD"
[[sessions]]
id = "s-sync"
name = "synced"
host = "agent"
local_path = "$R"
start_mode = "shell"
[[sessions]]
id = "s-plain"
name = "plain"
host = "agent"
local_path = "$P"
start_mode = "shell"
TOML
SAVED=$XDG_DATA_HOME/session-workbench/working-copies/s-sync/files
k() { xdotool key "$@"; sleep 0.5; }
t() { xdotool type --delay 8 "$1"; xdotool key Return; sleep ${2:-1}; }
shot() { import -window root $OUT/$1.png; }
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 0.8; }
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 4; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+shift+3; sleep 3                         # Remote tab
shot 1-no-copy-yet                              # Remote tab: no working copy, "Send to agent…"
click 120 90 3; menu                            # session menu: Send to agent… (the first item)
sleep 3; shot 2-send-preview
k Return; sleep 4                               # Send; Remote tab restarts in the copy
t 'pwd; ls -A; git log --oneline | head -3' 1.5
shot 3-remote-in-copy
t 'echo agent >>a.txt && git -c user.name=c -c user.email=c@example.com commit -qam "change made on the agent" && git branch from-agent && echo COMMITTED' 1.5
click 120 90 3; menu g                          # Get from agent… (commits: this is a repository)
sleep 3; shot 4-get
k Return; sleep 2                               # Take over ticked branches (main, from-agent)
shot 5-fast-forwarded

# Full transfers: work left uncommitted and ignored on the agent.
t 'echo draft >notes-draft.txt && echo more >>a.txt && mkdir -p build && echo bin >build/agent-out && echo WORK' 1.5
click 120 90 3; menu s                          # Save agent copy… (the next s after Send)
sleep 2; shot 6-save-dialog; k Return; sleep 4
check "saved: uncommitted file" test -f $SAVED/notes-draft.txt
check "saved: ignored build output" test -f $SAVED/build/agent-out
check "saved: .git" test -f $SAVED/.git/HEAD
click 120 90 3; menu t                          # Take all files from the agent…
sleep 3; shot 7-take-preview
k Return; sleep 3
check "taken: the agent's uncommitted file is here" test -f $R/notes-draft.txt
check "taken: .git untouched (no agent commit for it)" test -z "$(git -C $R status --porcelain --untracked-files=no -- .git 2>&1)"
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-sync'; rm -rf ~/$WD/repo-s-sync" 2>/dev/null   # the agent copy is gone
click 120 90 3; menu r                          # Restore saved copy to the agent…
sleep 3; shot 8-restore-preview; k Return; sleep 4
expect "restored: uncommitted work back on the agent" draft "$(ssh -o BatchMode=yes localhost "cat ~/$WD/repo-s-sync/notes-draft.txt" 2>&1)"
expect "restored: with its commits" "change made on the agent" "$(ssh -o BatchMode=yes localhost "cd ~/$WD/repo-s-sync && git log -1 --format=%s" 2>&1)"

# A session without git: Send, then Get copies the agent's files (and deletions).
k ctrl+alt+2; sleep 2
click 120 ${PLAINY:-134} 3; menu; sleep 3; k Return; sleep 3     # Send to agent…
ssh -o BatchMode=yes localhost "cd ~/$WD/plain-s-plain && echo result >result.txt && rm notes.txt" 2>/dev/null
click 120 ${PLAINY:-134} 3; menu g; sleep 3; shot 9-get-without-git; k Return; sleep 3
check "no git: Get brought the new file" test -f $P/result.txt
check "no git: and the deletion (ticked by default)" test ! -e $P/notes.txt

# Deleting the working-copy session can take its saved agent copy along (ticked here).
k ctrl+alt+1; sleep 1
click 120 90 3; menu d d; sleep 1               # Delete session… (after Disconnect)
shot 10-delete-dialog; k Tab; k space; k shift+Tab; k Return; sleep 2   # tick the second box
check "the saved copy went with the session" test ! -e $XDG_DATA_HOME/session-workbench/working-copies/s-sync
check "the session is gone" test -z "$(grep 's-sync' $CFG)"
alive
kill $APP; wait $APP 2>/dev/null
expect "host file after Get + fast-forward, then Take" "one agent more" "$(cat $R/a.txt | xargs)"
expect "host branch has the agent's commit" "change made on the agent|base|" "$(git -C $R log --format=%s | head -2 | tr '\n' '|')"
expect "the agent's new branch is a branch here" "change made on the agent" "$(git -C $R log -1 --format=%s from-agent 2>&1)"
expect "still on main" main "$(git -C $R symbolic-ref --short HEAD)"
ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-s-sync'; tmux kill-session -t '=cw-s-plain'; rm -rf ~/$WD" 2>/dev/null
finish
