# Shared by the GUI smoke tests (sourced after OUT is set): checks that make a
# test fail, so a wrong result is not just a line in the log.
#
# The app is started with SW_OUT in its environment and Local tabs inherit it:
# commands typed there leave their results in $SW_OUT. Remote tabs are tmux
# sessions on localhost, read with pane / in_pane.
export SW_OUT=$OUT/got
rm -rf "$SW_OUT"; mkdir -p "$SW_OUT"
# A fresh data directory per test: the app keeps UI state (narrow sidebar,
# last tab, collapsed folders...) in WebKit storage under it, so one test
# cannot leave state behind for the next. Restarts within a test keep it.
export XDG_DATA_HOME=$OUT/xdg-data
rm -rf "$XDG_DATA_HOME"; mkdir -p "$XDG_DATA_HOME"
FAILED=0

ok() { echo "ok: $*"; }
fail() { echo "FAIL: $*"; FAILED=1; }

# check <what> <command...>: passes when the command succeeds.
check() {
	local what=$1; shift
	if "$@" >/dev/null 2>&1; then ok "$what"; else fail "$what"; fi
}

# expect <what> <expected> <actual>
expect() {
	if [ "$2" = "$3" ]; then ok "$1"; else fail "$1: expected '$2', got '$3'"; fi
}

# wait_window: wait for the app window ($W); without one the test cannot go on.
wait_window() {
	W=
	for i in $(seq 40); do W=$(xdotool search --name "Session Workbench" 2>/dev/null | head -1); [ -n "$W" ] && break; sleep 0.25; done
	if [ -z "$W" ]; then
		fail "no app window, see $OUT/app.log"
		kill $APP 2>/dev/null
		finish
	fi
}

# alive: the app has not exited by itself (call before stopping it).
alive() {
	if kill -0 $APP 2>/dev/null; then ok "app still running"; else fail "app exited early, see $OUT/app.log"; fi
}

# pane <session id>: the text of the session's tmux session (its Remote tab).
pane() { ssh -o BatchMode=yes localhost "tmux capture-pane -p -J -S -1000 -t '=cw-$1:'" 2>/dev/null; }

# in_pane <what> <session id> <text>: the Remote tab of the session shows text.
in_pane() {
	if pane "$2" | grep -qF -- "$3"; then ok "$1"; else fail "$1: '$3' not in the Remote tab of $2"; fi
}

# has_tmux <session id>: its tmux session exists on localhost.
has_tmux() { ssh -o BatchMode=yes localhost "tmux has-session -t '=cw-$1'" 2>/dev/null; }

# sessions <config>: one line per session, in order, tab-separated:
# id, name, local_path, start_mode, group, color_mode.
sessions() {
	awk '
	function out() { if (id != "") print id "\t" nm "\t" lp "\t" sm "\t" gr "\t" cm; id = nm = lp = sm = gr = cm = "" }
	/^ *\[\[sessions\]\]/ { out(); ins = 1; sub_ = 0; next }
	/^ *\[\[sessions\./ { sub_ = 1; next }
	/^ *\[/ { out(); ins = 0; next }
	ins && !sub_ && / = / {
		k = $0; sub(/ = .*/, "", k); gsub(/ /, "", k)
		v = $0; sub(/^[^=]*= /, "", v); gsub(/^"|"$/, "", v)
		if (k == "id") id = v; else if (k == "name") nm = v; else if (k == "local_path") lp = v
		else if (k == "start_mode") sm = v; else if (k == "group") gr = v; else if (k == "color_mode") cm = v
	}
	END { out() }' "$1"
}

# field <config> <session name> <column>: one column (1 = id … 6 = color_mode)
# of the first session with that name.
field() { sessions "$1" | awk -F'\t' -v n="$2" -v c="$3" '$2 == n { print $c; exit }'; }

# finish: exit 1 when any check failed.
finish() {
	if [ $FAILED = 0 ]; then echo "PASS: $(basename "$0" .sh)"; exit 0; fi
	echo "FAILED: $(basename "$0" .sh)"; exit 1
}
