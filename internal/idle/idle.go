// Package idle stops a claude that waits for input nobody gives and
// resumes it later (SPEC F17). Claude Code is started with hooks (passed
// with --settings, nothing in the user's own settings changes) that write
// the last event to a state file on the agent host:
// $HOME/.local/state/session-workbench/claude/<session id>, first line
// "<epoch> <event> <claude's pid> <tmux pane>", then the hook's JSON
// (session_id, transcript_path). A claude whose last event is Stop (its
// turn ended) or SessionStart, that runs no Bash tool command, and whose
// tmux window showed nothing for the idle time, is stopped by respawning
// its pane with a notice and a login shell; <state file>.stopped keeps
// "<conversation id> <transcript exists 0|1> <pane>" for the resume,
// which respawns the pane with `claude --resume <id>` (or the start
// command, for a conversation without a prompt yet).
package idle

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"session-workbench/internal/profile"
	"session-workbench/internal/remote"
)

// Dir is where the agent host keeps the state files.
func Dir() string { return "$HOME/.local/state/" + profile.Dir() + "/claude" }

// Events the hooks record. Notification only for questions to the user
// (a permission or a form): claude waits for an answer, not idle.
var events = []struct{ name, matcher string }{
	{"SessionStart", ""},
	{"UserPromptSubmit", ""},
	{"Stop", ""},
	{"Notification", "permission_prompt|elicitation_dialog"},
	{"SessionEnd", ""},
}

// hookCommand writes the state file for event. It has no single quotes
// and no backslashes, so it survives any shell's quoting of the start
// command (fish reads \\ inside single quotes).
func hookCommand(id, event string) string {
	return `d="` + Dir() + `"; mkdir -p "$d" && { echo "$(date +%s) ` + event + ` $PPID ${TMUX_PANE:-}"; cat; } > "$d/` +
		id + `.tmp" && mv -f "$d/` + id + `.tmp" "$d/` + id + `"`
}

// UsageFile keeps the status line data of the claude that answered last
// on this agent host (in Dir): its subscription usage (SPEC F18).
const UsageFile = "usage.json"

// StatusHelper is the program the status line runs after saving the data
// (installed in Dir as "statusline" by the cost check, see internal/cost):
// the user's own status line command from ~/.claude/settings.json with the
// same data, else the usage ("5h 23% · 7d 41%").
const StatusHelper = `#!/bin/sh
# Session Workbench: the status line of the claudes it starts. The data is
# saved for the app (usage.json); then your own status line runs, as set in
# ~/.claude/settings.json, else this shows the subscription usage.
in=$1
u="$HOME/.claude/settings.json"
cmd=
if [ -f "$u" ]; then
  if command -v python3 >/dev/null 2>&1; then
    cmd=$(python3 -c 'import json, sys
try: print(json.load(open(sys.argv[1])).get("statusLine", {}).get("command", ""))
except Exception: pass' "$u" 2>/dev/null)
  elif command -v jq >/dev/null 2>&1; then
    cmd=$(jq -r '.statusLine.command // empty' "$u" 2>/dev/null)
  fi
fi
if [ -n "$cmd" ]; then exec sh -c "$cmd" < "$in"; fi
pct() { tr -d '\n' < "$in" | sed -n 's/.*"'"$1"'":{[^}]*"used_percentage":\([0-9.]*\).*/\1/p' | cut -d. -f1; }
f=$(pct five_hour); w=$(pct seven_day)
line=
[ -n "$f" ] && line="5h $f%"
[ -n "$w" ] && line="${line:+$line · }7d $w%"
[ -n "$line" ] && echo "$line"
exit 0
`

// statusCommand is the status line command: it saves the data claude
// passes (when it has the usage; a claude without a reply yet has none)
// and runs StatusHelper. Like the hooks: no single quotes, no backslashes.
func statusCommand() string {
	return `d="` + Dir() + `"; mkdir -p "$d"; t="$d/sl.$$"; cat > "$t"; ` +
		`if grep -q rate_limits "$t"; then cp "$t" "$t.u" && mv -f "$t.u" "$d/` + UsageFile + `"; fi; ` +
		`[ -x "$d/statusline" ] && "$d/statusline" "$t"; rm -f "$t"`
}

// Settings is the --settings JSON with the session's hooks; with usage
// also the status line that saves the subscription usage (SPEC F18).
func Settings(id string, usage bool) string {
	type hook struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	type entry struct {
		Matcher string `json:"matcher,omitempty"`
		Hooks   []hook `json:"hooks"`
	}
	hooks := map[string][]entry{}
	for _, e := range events {
		hooks[e.name] = []entry{{Matcher: e.matcher, Hooks: []hook{{"command", hookCommand(id, e.name)}}}}
	}
	all := map[string]any{"hooks": hooks}
	if usage {
		all["statusLine"] = map[string]any{"type": "command", "command": statusCommand()}
	}
	var b strings.Builder
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false) // && and > as they are
	e.Encode(all)
	return strings.TrimSpace(b.String())
}

// marker is in claude's arguments (its settings): it tells the session's
// claude from others.
func marker(id string) string { return id + ".tmp" }

// token is a word of a start command with its place.
type token struct {
	text       string
	start, end int
	op         bool // ||, &&, ;, |, &, (, )
}

// tokens splits a one-line shell command into words and operators;
// quotes stay part of their word.
func tokens(cmd string) []token {
	var out []token
	i := 0
	for i < len(cmd) {
		c := cmd[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case strings.ContainsRune(";|&()", rune(c)):
			j := i + 1
			if (c == '|' || c == '&') && j < len(cmd) && cmd[j] == c {
				j++
			}
			out = append(out, token{cmd[i:j], i, j, true})
			i = j
		default:
			j := i
			for j < len(cmd) && !strings.ContainsRune(" \t;|&()", rune(cmd[j])) {
				switch cmd[j] {
				case '\'':
					if k := strings.IndexByte(cmd[j+1:], '\''); k >= 0 {
						j += k + 2
						continue
					}
				case '"':
					k := j + 1
					for k < len(cmd) && cmd[k] != '"' {
						if cmd[k] == '\\' {
							k++
						}
						k++
					}
					if k < len(cmd) {
						j = k + 1
						continue
					}
				case '\\':
					j++
				}
				j++
			}
			if j > len(cmd) {
				j = len(cmd)
			}
			out = append(out, token{cmd[i:j], i, j, false})
			i = j
		}
	}
	return out
}

func isClaude(w string) bool { return w == "claude" || strings.HasSuffix(w, "/claude") }

// claudes are the indexes of the tokens that run claude: the first word
// of a command (after variable assignments and exec).
func claudes(ts []token) []int {
	var out []int
	first := true
	for i, t := range ts {
		switch {
		case t.op:
			first = true
		case !first:
		case strings.Contains(t.text, "=") && !strings.HasPrefix(t.text, "="), t.text == "exec":
			// VAR=value or exec: the command comes next
		default:
			if isClaude(t.text) {
				out = append(out, i)
			}
			first = false
		}
	}
	return out
}

// Runs reports whether a start command runs claude.
func Runs(cmd string) bool { return len(claudes(tokens(cmd))) > 0 }

// WithHooks gives every claude in cmd the session's hooks (usage: and the
// status line, see Settings).
func WithHooks(cmd, id string, usage bool) string {
	arg := " --settings " + remote.Quote(Settings(id, usage))
	ts := tokens(cmd)
	var b strings.Builder
	last := 0
	for _, i := range claudes(ts) {
		b.WriteString(cmd[last:ts[i].end])
		b.WriteString(arg)
		last = ts[i].end
	}
	b.WriteString(cmd[last:])
	return b.String()
}

// ResumeCommand resumes conversation conv (with the hooks): the first
// claude of the start command with its own options, those that pick a
// conversation (--continue, --resume, --session-id, --fork-session)
// replaced by --resume conv. Without conv (no prompt was sent: nothing to
// resume) it is the start command with hooks.
func ResumeCommand(cmd, id, conv string, usage bool) string {
	ts := tokens(cmd)
	cs := claudes(ts)
	if conv == "" || len(cs) == 0 {
		return WithHooks(cmd, id, usage)
	}
	words := []string{ts[cs[0]].text, "--settings", remote.Quote(Settings(id, usage)), "--resume", remote.Quote(conv)}
	for i := cs[0] + 1; i < len(ts) && !ts[i].op; i++ {
		w := ts[i].text
		switch {
		case w == "-c" || w == "--continue" || w == "--fork-session":
		case w == "--session-id" || w == "-r" || w == "--resume":
			if w == "--session-id" || i+1 < len(ts) && !ts[i+1].op && !strings.HasPrefix(ts[i+1].text, "-") {
				i++ // its value
			}
		case strings.HasPrefix(w, "--resume=") || strings.HasPrefix(w, "--session-id="):
		default:
			words = append(words, w)
		}
	}
	return strings.Join(words, " ")
}

// State is a session's claude as the check script saw it.
type State struct {
	ID          string
	Now         int64  // the host's clock
	At          int64  // the last event's time
	Event       string // the last event ("" when there is no state file)
	PID         int
	Pane        string
	Conv        string // the conversation's id
	Transcript  bool   // its transcript exists (there is something to resume)
	PaneSession string // the tmux session the pane is in now
	Activity    int64  // the pane's window: last output
	Alive       bool   // the claude of the last event still runs
	Busy        int    // Bash tool commands running under it
	Stopped     bool   // stopped for being idle, not resumed yet
	StoppedConv string
	StoppedTx   bool
	StoppedPane string
}

// Idle reports whether the session's claude can be stopped: alive, its
// turn ended (or it was just started), nothing running under it, in its
// own tmux session, and its window quiet for d.
func (s State) Idle(tmuxName string, d time.Duration) bool {
	return d > 0 && !s.Stopped && s.Alive && s.Busy == 0 && s.PaneSession == tmuxName &&
		(s.Event == "Stop" || s.Event == "SessionStart") && s.Now-max(s.At, s.Activity) >= int64(d/time.Second)
}

// procs defines $procs, every process as "pid ppid args" (ps, else /proc
// for systems without ps such as a plain ubuntu container), and helpers
// on it: args_of, ppid_of, desc_of (all descendants).
const procs = `plist() {
  if command -v ps >/dev/null 2>&1; then ps -A -o pid= -o ppid= -o args= 2>/dev/null; return; fi
  for d in /proc/[0-9]*; do
    pp=$(sed 's/.*) //' "$d/stat" 2>/dev/null | cut -d' ' -f2)
    [ -n "$pp" ] && printf '%s %s %s\n' "${d#/proc/}" "$pp" "$(tr '\000' ' ' < "$d/cmdline" 2>/dev/null)"
  done
}
procs=$(plist)
args_of() { printf '%s\n' "$procs" | awk -v p="$1" '$1==p {sub(/^ *[0-9]+ +[0-9]+ */, ""); print; exit}'; }
ppid_of() { printf '%s\n' "$procs" | awk -v p="$1" '$1==p {print $2; exit}'; }
desc_of() { printf '%s\n' "$procs" | awk -v root="$1" '{p[$1]=$2; a[$1]=$0} END {for (q in p) {x=q; k=0; while (x in p && x!=root && k<64) {x=p[x]; k++}; if (x==root && q!=root) print q}}'; }
`

// ids checks the session ids before they go into a script.
func ids(list []string) error {
	for _, id := range list {
		if !remote.ValidID(id) {
			return fmt.Errorf("idle: invalid session id %q", id)
		}
	}
	return nil
}

// CheckScript prints a line per session (tab-separated, see Parse).
func CheckScript(list []string) (string, error) {
	if err := ids(list); err != nil {
		return "", err
	}
	return procs + `d="` + Dir() + `"; now=$(date +%s)
for id in ` + strings.Join(list, " ") + `; do
  f="$d/$id"; line=; conv=; tx=0
  if [ -f "$f" ]; then
    line=$(head -n 1 "$f")
    conv=$(sed -n 's/.*"session_id" *: *"\([^"]*\)".*/\1/p' "$f" | head -n 1)
    tp=$(sed -n 's/.*"transcript_path" *: *"\([^"]*\)".*/\1/p' "$f" | head -n 1)
    [ -n "$tp" ] && [ -f "$tp" ] && tx=1
  fi
  set -- $line
  at=${1:-0}; ev=${2:-}; pid=${3:-0}; pane=${4:-}
  sess=; act=0; alive=0; busy=0
  if [ -n "$pane" ]; then
    sess=$(tmux display -p -t "$pane" '#{session_name}' 2>/dev/null)
    act=$(tmux display -p -t "$pane" '#{window_activity}' 2>/dev/null)
  fi
  if [ "$pid" -gt 1 ] 2>/dev/null && args_of "$pid" | grep -qF "$id.tmp"; then
    alive=1
    busy=0
    for q in $(desc_of "$pid"); do args_of "$q" | grep -q shell-snapshots && busy=$((busy+1)); done
  fi
  st=0; sc=; stx=0; sp=
  if [ -f "$f.stopped" ]; then
    st=1; read sc stx sp < "$f.stopped"
    # Its tmux session ended meanwhile (or another took the pane): stale.
    if [ "$(tmux display -p -t "$sp" '#{session_name}' 2>/dev/null)" != "` + remote.TmuxName("") + `$id" ]; then
      rm -f "$f.stopped"; st=0; sc=; stx=0; sp=
    fi
  fi
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$id" "$now" "$at" "$ev" "$pid" "$pane" "$conv" "$tx" "$sess" "${act:-0}" "$alive" "${busy:-0}" "$st" "$sc" "$stx" "$sp"
done
`, nil
}

// Parse reads CheckScript's output.
func Parse(out string) []State {
	var list []State
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 16 || !remote.ValidID(f[0]) {
			continue
		}
		n := func(i int) int64 { v, _ := strconv.ParseInt(strings.TrimSpace(f[i]), 10, 64); return v }
		list = append(list, State{ID: f[0], Now: n(1), At: n(2), Event: f[3], PID: int(n(4)), Pane: f[5],
			Conv: f[6], Transcript: f[7] == "1", PaneSession: f[8], Activity: n(9), Alive: f[10] == "1",
			Busy: int(n(11)), Stopped: f[12] == "1", StoppedConv: f[13], StoppedTx: f[14] == "1", StoppedPane: f[15]})
	}
	return list
}

var paneRE = func(p string) bool {
	if len(p) < 2 || p[0] != '%' {
		return false
	}
	_, err := strconv.Atoi(p[1:])
	return err == nil
}

var convOK = func(c string) bool {
	return c != "" && strings.IndexFunc(c, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-')
	}) < 0
}

// IdleNotice is what the pane shows after an idle stop.
func IdleNotice(d time.Duration) string {
	return fmt.Sprintf("[claude stopped after %s without input; it resumes when you open the session in Session Workbench]", Human(d))
}

// NowNotice is what the pane shows after Stop claude now.
const NowNotice = "[claude stopped from Session Workbench; it resumes when you open the session again, or with Resume claude in its menu]"

// StopScript stops the session's claude (s from a check): it checks
// again that it is the same claude and that its window stayed quiet for
// quiet (0: not checked), marks it stopped, respawns the pane with notice
// and a login shell in dir, ends the shells between the pane and claude,
// claude (SIGHUP as for a closed terminal, SIGTERM after 5 s) and what
// still runs under it (a command it ran). Exit 3: not that claude any
// more; 4: the window was used.
func StopScript(s State, tmuxName, dir string, quiet time.Duration, notice string) (string, error) {
	if err := ids([]string{s.ID}); err != nil {
		return "", err
	}
	if !paneRE(s.Pane) || s.PID < 2 || (s.Conv != "" && !convOK(s.Conv)) {
		return "", fmt.Errorf("idle: unexpected state for %s", s.ID)
	}
	conv := s.Conv
	if conv == "" {
		conv = "-"
	}
	tx := "0"
	if s.Transcript {
		tx = "1"
	}
	show := "echo; echo " + remote.Quote(notice) + `; exec "$SHELL" -l`
	return procs + `f="` + Dir() + `/` + s.ID + `"
args_of ` + strconv.Itoa(s.PID) + ` | grep -qF ` + remote.Quote(marker(s.ID)) + ` || exit 3
[ "$(tmux display -p -t ` + s.Pane + ` '#{session_name}' 2>/dev/null)" = ` + remote.Quote(tmuxName) + ` ] || exit 3
act=$(tmux display -p -t ` + s.Pane + ` '#{window_activity}')
[ $(( $(date +%s) - act )) -ge ` + strconv.FormatInt(int64(quiet/time.Second), 10) + ` ] || exit 4
# The shells from the pane down to claude: the start command's own would
# outlive the respawn and run what follows claude in it ("|| claude").
pid=` + strconv.Itoa(s.PID) + `; old=$(tmux display -p -t ` + s.Pane + ` '#{pane_pid}')
chain=; p=$pid; n=0
while [ "$p" != "$old" ] && [ $n -lt 16 ]; do
  p=$(ppid_of "$p"); n=$((n+1))
  [ -n "$p" ] && [ "$p" -gt 1 ] || { chain=; break; }
  chain="$chain $p"
done
[ "$p" = "$old" ] || chain=
desc=$(desc_of "$pid")
echo ` + remote.Quote(conv+" "+tx+" "+s.Pane) + ` > "$f.stopped"
# Frozen first: once claude ends they could start the fallback before
# they are killed.
[ -n "$chain" ] && kill -STOP $chain 2>/dev/null
tmux respawn-pane -k -t ` + s.Pane + ` -c ` + remote.ShellPath(dir) + ` ` + remote.Quote(show) + ` || { [ -n "$chain" ] && kill -CONT $chain; exit 1; }
[ -n "$chain" ] && kill -KILL $chain 2>/dev/null
kill -HUP "$pid" 2>/dev/null
for i in 1 2 3 4 5 6 7 8 9 10; do kill -0 "$pid" 2>/dev/null || break; sleep 0.5; done
kill -TERM "$pid" 2>/dev/null
# What it ran (a command claude was running when stopped now).
[ -n "$desc" ] && kill -TERM $desc 2>/dev/null
exit 0
`, nil
}

// ResumeScript resumes a stopped claude in its pane with paneCmd (see
// remote.PaneCommand of ResumeCommand). Exit 3: not stopped (or the pane
// is gone); 4: something runs in the pane's shell (taken over by hand:
// the mark is dropped and nothing is touched).
func ResumeScript(s State, tmuxName, dir, paneCmd string) (string, error) {
	if err := ids([]string{s.ID}); err != nil {
		return "", err
	}
	if !paneRE(s.StoppedPane) {
		return "", fmt.Errorf("idle: unexpected state for %s", s.ID)
	}
	p := s.StoppedPane
	return procs + `f="` + Dir() + `/` + s.ID + `"
[ -f "$f.stopped" ] || exit 3
if [ "$(tmux display -p -t ` + p + ` '#{session_name}' 2>/dev/null)" != ` + remote.Quote(tmuxName) + ` ]; then rm -f "$f.stopped"; exit 3; fi
pp=$(tmux display -p -t ` + p + ` '#{pane_pid}')
if [ -n "$(desc_of "$pp")" ]; then rm -f "$f.stopped"; exit 4; fi
rm -f "$f.stopped"
exec tmux respawn-pane -k -t ` + p + ` -c ` + remote.ShellPath(dir) + ` ` + remote.Quote(paneCmd) + `
`, nil
}

// Human is d for people: "5 min", "1 h 30 min".
func Human(d time.Duration) string {
	m := int(d / time.Minute)
	switch {
	case m < 60:
		return fmt.Sprintf("%d min", m)
	case m%60 == 0:
		return fmt.Sprintf("%d h", m/60)
	}
	return fmt.Sprintf("%d h %d min", m/60, m%60)
}
