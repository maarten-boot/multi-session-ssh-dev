#!/bin/bash
# Headless GUI test of exporting a session and importing it as a new one:
# session menu → Export… with the project directory ticked, saved as a
# .zip through the save dialog — the archive has the manifest, notes, Q&A,
# recordings, both shell histories and the project (tracked, untracked,
# .git; not git-ignored files), never keys; + → Import session… makes a
# new session (new id) in a new directory with the files, the Q&A without
# Claude Code's session ids, and the Remote history that goes to the agent
# host when the Remote tab connects. Needs `ssh localhost` without prompts,
# tmux and git.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-export.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT; rm -rf $OUT/notes $OUT/proj $OUT/proj2 $OUT/proj3 $OUT/*.zip $OUT/*.tar.gz
. "$(dirname "$0")/smoke-lib.sh"
PROJ=$OUT/proj; mkdir -p $PROJ
git -C $PROJ init -q; printf 'build/\n' >$PROJ/.gitignore; echo 'package main' >$PROJ/main.go
git -C $PROJ add .; echo untracked >$PROJ/new.txt; mkdir -p $PROJ/build; echo ignored >$PROJ/build/out
mkdir -p $PROJ/.session-workbench; printf 'services:\n  db:\n    image: x\n' >$PROJ/.session-workbench/compose.yaml
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-ex"
name = "exported"
host = "local"
local_path = "$PROJ"
start_mode = "shell"
commands = [{ name = "own", command = "make" }]
local_init = "echo should-not-come-along"
TOML
DATA=$XDG_DATA_HOME/session-workbench
mkdir -p -m 700 $OUT/notes/s-ex $DATA/qa $DATA/history "$DATA/logs/local$PROJ"
echo '# exported home' >$OUT/notes/s-ex/Home.md
cat >$DATA/qa/s-ex.json <<'JSON'
{"turns":[{"time":"2026-11-02T10:00:00Z","question":"q?","answer":"a.","session":"cc-123","usage":{}}]}
JSON
echo 'echo local-history' >$DATA/history/s-ex.history
echo 'recorded text' >"$DATA/logs/local$PROJ/20261102-1000.log"
echo '# Q&A log' >"$DATA/logs/local$PROJ/qa-2026-11-02.md"
RH=$HOME/.local/state/session-workbench/history
mkdir -p $RH; echo 'echo remote-history-xyz' >$RH/s-ex
k() { xdotool key "$@"; sleep 0.5; }
shot() { import -window root $OUT/$1.png; }
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 1.5; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null

# Export: the session's menu; Edit… has the focus, "e" goes to the next: Export….
xdotool mousemove 114 131 click 3; menu e
shot 1-export-dialog
k shift+Tab; k shift+Tab; k space                     # from Export…: Format, then the project's box
shot 2-project-ticked
k Tab; k Tab; k Return; sleep 2                        # Export…: the save dialog
shot 3-save-dialog
k ctrl+a; xdotool type --delay 10 "$OUT/s.zip"; k Return; sleep 3
shot 4-exported
check "the archive was written" test -s $OUT/s.zip
LIST=$(python3 -c "import zipfile,sys; print('\n'.join(zipfile.ZipFile(sys.argv[1]).namelist()))" $OUT/s.zip 2>&1)
for f in manifest.json notes/Home.md qa/conversation.json qa/log/qa-2026-11-02.md recordings/20261102-1000.log \
	history/local history/remote project/main.go project/new.txt project/.gitignore project/.git/HEAD \
	compose.yaml project/.session-workbench/compose.yaml; do
	check "in the archive: $f" grep -qx "$f" <<<"$LIST"
done
check "not in it: the git-ignored build/out" bash -c "! grep -q 'build/out' <<<'$LIST'"
check "not in it: keys" bash -c "! grep -qE 'id_ed25519|ssh_host' <<<'$LIST'"
MAN=$(python3 -c "import zipfile,sys; print(zipfile.ZipFile(sys.argv[1]).read('manifest.json').decode())" $OUT/s.zip)
check "the manifest names the session" grep -q '"name": "exported"' <<<"$MAN"
check "the manifest: the services' compose file was the project's" grep -q '"compose_in_repo": true' <<<"$MAN"

# Import: + → Import session…, the open dialog, then the import dialog.
xdotool mousemove 281 67 click 1; menu i
sleep 1; shot 5-open-dialog
k ctrl+l; xdotool type --delay 30 's.zip'; sleep 0.5; k Return; sleep 2   # it opens in the export's folder
shot 6-import-dialog
k ctrl+a; xdotool type --delay 10 "$OUT/proj2"        # the directory field has the focus
k Tab; k Tab; k ctrl+a; xdotool type --delay 10 'imported'
k Return; sleep 3
shot 7-imported
NEW=$(awk '/^ *id = "s-/{id=$3} /name = "imported"/{gsub(/"/,"",id); print id}' $CFG | head -1)
check "a new session with a new id" bash -c "[ -n '$NEW' ] && [ '$NEW' != s-ex ]"
check "in the new directory" grep -q "local_path = \"$OUT/proj2\"" $CFG
check "its project files" test -s $OUT/proj2/main.go -a -s $OUT/proj2/new.txt -a -d $OUT/proj2/.git
check "git works there" git -C $OUT/proj2 rev-parse --git-dir
check "its notes" grep -q 'exported home' $OUT/notes/$NEW/Home.md
check "its Q&A, without Claude Code's session id" bash -c "grep -q '\"q?\"' $DATA/qa/$NEW.json && ! grep -q cc-123 $DATA/qa/$NEW.json"
check "its recordings and Q&A log" test -s "$DATA/logs/local$OUT/proj2/20261102-1000.log" -a -s "$DATA/logs/local$OUT/proj2/qa-2026-11-02.md"
check "its Local history" grep -q local-history $DATA/history/$NEW.history
check "its own commands" bash -c "grep -A12 'name = \"imported\"' $CFG | grep -q 'command = \"make\"'"
check "local_init not imported" bash -c "[ \$(grep -c should-not-come-along $CFG) = 1 ]"
check "the Remote history waits for the connection" test -s $DATA/history/$NEW.remote
# The same as .tar.gz: Format → the second choice; imported into proj3.
xdotool mousemove 114 131 click 3; menu e              # "exported" (the first row): Export…
k shift+Tab; k Down; k shift+Tab; k space; sleep 0.3   # Format: .tar.gz; the project ticked
shot 9-tar-gz
k Tab; k Tab; k Return; sleep 2                        # Export…: the save dialog
k ctrl+a; xdotool type --delay 10 "$OUT/t.tar.gz"; k Return; sleep 3
check "the .tar.gz archive was written" bash -c "tar -tzf $OUT/t.tar.gz | grep -qx manifest.json"
xdotool mousemove 281 67 click 1; menu i; sleep 1
k ctrl+l; xdotool type --delay 30 't.tar.gz'; sleep 0.5; k Return; sleep 2
k ctrl+a; xdotool type --delay 10 "$OUT/proj3"
k Tab; k Tab; k ctrl+a; xdotool type --delay 10 'from-tgz'
k Return; sleep 3
shot 10-imported-tgz
NEW3=$(awk '/^ *id = "s-/{id=$3} /name = "from-tgz"/{gsub(/"/,"",id); print id}' $CFG | head -1)
check "imported from .tar.gz: a new session" bash -c "[ -n '$NEW3' ] && [ '$NEW3' != s-ex ] && [ '$NEW3' != '$NEW' ]"
check "imported from .tar.gz: a working repository" bash -c "test -s $OUT/proj3/main.go && git -C $OUT/proj3 rev-parse --git-dir"
check "imported from .tar.gz: its notes" grep -q 'exported home' $OUT/notes/$NEW3/Home.md
xdotool mousemove 114 174 click 1; sleep 1             # back to "imported" (the second row)

# The Remote tab of the new session: the history goes to the agent host.
k ctrl+shift+3; sleep 6
shot 8-remote
check "the Remote history is on the agent host" grep -q remote-history-xyz $RH/$NEW
check "and no longer waits here" test ! -e $DATA/history/$NEW.remote
alive
k Escape; k Escape                                     # no dialog left open: quitting waits for one
kill $APP; wait $APP 2>/dev/null
for id in s-ex $NEW $NEW3; do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; rm -f $RH/$id; done
finish
