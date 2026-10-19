#!/bin/bash
# Headless GUI test of the tab order (Local, Files, Remote, Notes) and the
# Notes tab: pages in a tree (notes/<id>/Home.md, Design.md, Design/API.md…),
# autosaved, formatting from keys and the toolbar, copy and paste from the
# right-click menu and Ctrl+C / Ctrl+V, new page / subpage,
# rename (subpages move along) and delete from the tree, notes from before
# pages existed becoming the Home page, the open page kept across restarts.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-notes.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT; rm -rf $OUT/notes
. "$(dirname "$0")/smoke-lib.sh"
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-nt1"
name = "project one"
host = "local"
local_path = "/tmp"
start_mode = "shell"
[[sessions]]
id = "s-nt2"
name = "project two"
host = "local"
local_path = "/usr"
start_mode = "shell"
TOML
# Notes of project two from before pages existed.
mkdir -p $OUT/notes; printf 'old notes of two\n' >$OUT/notes/s-nt2.md
N=$OUT/notes/s-nt1
k() { xdotool key "$@"; sleep 0.5; }
type() { xdotool type --delay 15 -- "$1"; }
shot() { import -window root $OUT/$1.png; }
# Window origin (60,50); click/right-click at window coordinates.
click() { xdotool mousemove $((60+$1)) $((50+$2)) click ${3:-1}; sleep 0.8; }
menu() { sleep 0.3; for c in "$@"; do xdotool key "$c"; sleep 0.15; done; xdotool key Return; sleep 0.8; }
# Tree rows (window coordinates): first row at y=ROW0, 22px apart.
ROW0=102; TX=330
row() { echo $((ROW0 + 22*$1)); }
start() {
	$BIN -config $CFG >>$OUT/app.log 2>&1 &
	APP=$!
	wait_window
	sleep 3; xdotool windowactivate --sync $W 2>/dev/null
}
start
k ctrl+alt+2; sleep 1.5
shot 1-opens-on-local                           # tabs: Local Files Remote Notes; Local active
k ctrl+alt+1; k ctrl+shift+4; sleep 1
type "# Project one"; k Return; k Return
type "- ask claude about the parser"; k Return
k Tab; type "indented with Tab"
sleep 0.3; shot 2-typing
k ctrl+alt+2; sleep 1                           # switching away saves
NOTE=$'# Project one\n\n- ask claude about the parser\n  indented with Tab'   # Tab indents by two spaces
expect "Home page saved when switching away" "$NOTE" "$(cat $N/Home.md 2>&1)"
expect "page file is private" "-rw-------" "$(stat -c %A $N/Home.md 2>&1)"
expect "notes folder is private" "drwx------" "$(stat -c %A $N 2>&1)"

# Formatting: Ctrl+B with no selection, then a selected word and the U button.
k ctrl+alt+1; sleep 1; k ctrl+End; k Return
k ctrl+b; type "bold"; k End; type " and plain"
k ctrl+shift+Left; click 492 76                # select "plain", toolbar U
k End; type " "; k ctrl+i; type "it"; k End
sleep 1; shot 3-formatting
k ctrl+alt+2; sleep 1
expect "bold, underline and italic markers" "**bold** and <u>plain</u> _it_" "$(tail -1 $N/Home.md 2>&1)"

# Copy and paste: the right-click menu, then Ctrl+C / Ctrl+V.
k ctrl+alt+1; sleep 1; k ctrl+End; k Return
type "menu-word"; k shift+Home
click 460 216 3; shot 3a-menu; menu c            # on the selected line: Copy
k End; k Return
click 700 600 3; menu p                         # below the text: Paste (at the end)
k shift+Home; k ctrl+c; k End; k Return; k ctrl+v
sleep 1; shot 3b-copy-paste
k ctrl+alt+2; sleep 1
expect "copied and pasted with the menu and the keys" $'menu-word\nmenu-word\nmenu-word' "$(tail -3 $N/Home.md 2>&1)"

# A new page (the + button), a subpage (right-click), both saved as files.
k ctrl+alt+1; sleep 1
click 393 75; type "Design"; k Return; sleep 1
k ctrl+End; type "design notes"; sleep 1
click $TX $(row 1) 3; menu n                    # "New subpage of “Design”…" (the first n after "New page…")
type "API"; k Return; sleep 1
k ctrl+End; type "api notes"; sleep 1.5
shot 4-subpage
expect "new page saved" $'# Design\n\ndesign notes' "$(cat $N/Design.md 2>&1)"
expect "subpage saved" $'# API\n\napi notes' "$(cat $N/Design/API.md 2>&1)"

# Rename Design: its subpage moves along.
click $TX $(row 1) 3; menu r
k ctrl+a; type "Architecture"; k Return; sleep 1.5
shot 5-renamed
check "renamed page" test -f $N/Architecture.md
check "subpage moved along" test -f $N/Architecture/API.md
check "old name gone" test ! -e $N/Design.md -a ! -e $N/Design

# Delete the subpage (row 2 under the expanded Architecture).
click $TX $(row 2) 3; menu d; k Return; sleep 1.5
shot 6-deleted
check "subpage deleted" test ! -e $N/Architecture/API.md
check "its empty folder too" test ! -e $N/Architecture

# Project two: its old notes are its Home page now.
click $TX $(row 1); sleep 0.5                  # open Architecture (kept for the restart)
k ctrl+alt+2; k ctrl+shift+4; sleep 1.5
shot 7-migrated
expect "old notes moved to the Home page" "old notes of two" "$(cat $OUT/notes/s-nt2/Home.md 2>&1)"
check "old file gone" test ! -e $OUT/notes/s-nt2.md
alive
kill $APP; wait $APP 2>/dev/null; sleep 1
start
k ctrl+alt+1; sleep 1.5                         # reopens on Notes, on the last page
shot 8-after-restart
alive
kill $APP; wait $APP 2>/dev/null
expect "Home unchanged after restart" "$NOTE"$'\n'"**bold** and <u>plain</u> _it_"$'\nmenu-word\nmenu-word\nmenu-word' "$(cat $N/Home.md 2>&1)"
for id in s-nt1 s-nt2; do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; done
finish
