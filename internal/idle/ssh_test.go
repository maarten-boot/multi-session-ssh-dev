package idle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"session-workbench/internal/remote"
	"session-workbench/internal/xfer"
)

// fakeClaude records its arguments, runs the SessionStart hook of its
// --settings (as claude does) and waits; while <dir>/busy exists it runs a
// "Bash tool command" (a child with shell-snapshots in its arguments).
const fakeClaude = `#!/bin/sh
echo "$*" >> DIR/args
settings=
while [ $# -gt 0 ]; do [ "$1" = --settings ] && { settings=$2; shift; }; shift; done
cmd=$(printf '%s' "$settings" | python3 -c 'import json,sys; print(json.load(sys.stdin)["hooks"]["SessionStart"][0]["hooks"][0]["command"])')
printf '{"session_id":"conv-xyz","transcript_path":"DIR/tx.jsonl"}' | sh -c "$cmd"
while :; do
  if [ -f DIR/busy ]; then sh -c ': shell-snapshots; sleep 2'; fi
  sleep 1
done
`

// TestSSHIdle stops and resumes a (fake) claude in a real tmux session on
// SW_SSH_TARGET (make test-ssh).
func TestSSHIdle(t *testing.T) {
	target := os.Getenv("SW_SSH_TARGET")
	if target == "" {
		t.Skip("SW_SSH_TARGET not set")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	fake := filepath.Join(dir, "bin", "claude")
	os.WriteFile(fake, []byte(strings.ReplaceAll(fakeClaude, "DIR", dir)), 0o755)
	os.WriteFile(filepath.Join(dir, "tx.jsonl"), []byte("{}\n"), 0o644)
	id := fmt.Sprintf("s-idle%d", os.Getpid())
	tn := remote.TmuxName(id)
	start := fake + " --continue || " + fake
	ctx := context.Background()
	run := func(script string) (string, int) {
		t.Helper()
		out, code, stderr, err := xfer.Run(ctx, target, "exec sh -c "+remote.Quote(script), nil)
		if err != nil {
			t.Fatalf("%v %s", err, stderr)
		}
		return string(out), code
	}
	t.Cleanup(func() {
		run("tmux kill-session -t " + remote.Quote("="+tn) + ` 2>/dev/null; rm -f "` + Dir() + "/" + id + `" "` + Dir() + "/" + id + `.stopped"`)
	})
	check := func() State {
		t.Helper()
		s, err := CheckScript([]string{id})
		if err != nil {
			t.Fatal(err)
		}
		out, code := run(s)
		list := Parse(out)
		if code != 0 || len(list) != 1 {
			t.Fatalf("check: exit %d: %q", code, out)
		}
		return list[0]
	}
	waitFor := func(what string, ok func(State) bool) State {
		t.Helper()
		for i := 0; i < 40; i++ {
			if s := check(); ok(s) {
				return s
			}
			time.Sleep(250 * time.Millisecond)
		}
		t.Fatalf("waiting for %s: %+v", what, check())
		return State{}
	}
	pane, err := remote.PaneCommand(WithHooks(start, id, false))
	if err != nil {
		t.Fatal(err)
	}
	if _, code := run("tmux new-session -d -x 100 -y 30 -s " + tn + " -c " + remote.Quote(dir) + " " + remote.Quote(pane)); code != 0 {
		t.Fatal("tmux new-session failed")
	}
	st := waitFor("the SessionStart hook", func(s State) bool { return s.Event == "SessionStart" && s.Alive })
	if st.Conv != "conv-xyz" || !st.Transcript || st.PaneSession != tn || !strings.HasPrefix(st.Pane, "%") {
		t.Fatalf("%+v", st)
	}

	// A Bash tool command runs: not idle.
	os.WriteFile(filepath.Join(dir, "busy"), nil, 0o644)
	st = waitFor("busy", func(s State) bool { return s.Busy > 0 })
	time.Sleep(1100 * time.Millisecond)
	if st = check(); st.Busy > 0 && st.Idle(tn, time.Second) {
		t.Error("busy but idle")
	}
	os.Remove(filepath.Join(dir, "busy"))
	st = waitFor("not busy", func(s State) bool { return s.Busy == 0 })
	time.Sleep(2 * time.Second)
	if st = check(); !st.Idle(tn, time.Second) {
		t.Fatalf("not idle: %+v", st)
	}

	stop := func(st State) {
		t.Helper()
		s, err := StopScript(st, tn, dir, time.Second, IdleNotice(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if _, code := run(s); code != 0 {
			t.Fatalf("stop: exit %d", code)
		}
	}
	// Nothing of it may survive: not claude, not the start command's shell
	// (which would run "|| claude" next).
	left := func() {
		t.Helper()
		time.Sleep(time.Second)
		if out, _ := run(`ps -A -o args= | grep -c '[` + id[:1] + `]` + id[1:] + `\.tmp'`); strings.TrimSpace(out) != "0" {
			t.Errorf("still running: %s", out)
		}
	}
	stop(st)
	st = waitFor("stopped", func(s State) bool { return s.Stopped && !s.Alive })
	left()
	if st.StoppedConv != "conv-xyz" || !st.StoppedTx || st.StoppedPane == "" {
		t.Errorf("%+v", st)
	}
	time.Sleep(500 * time.Millisecond)
	if out, _ := run("tmux capture-pane -p -t " + st.StoppedPane); !strings.Contains(out, "claude stopped after 0 min without input") {
		t.Errorf("pane: %q", out)
	}

	resume := func(st State) int {
		t.Helper()
		p, err := remote.PaneCommand(ResumeCommand(start, id, st.StoppedConv, false))
		if err != nil {
			t.Fatal(err)
		}
		s, err := ResumeScript(st, tn, dir, p)
		if err != nil {
			t.Fatal(err)
		}
		_, code := run(s)
		return code
	}
	if code := resume(st); code != 0 {
		t.Fatalf("resume: exit %d", code)
	}
	st = waitFor("resumed", func(s State) bool { return !s.Stopped && s.Alive && s.Event == "SessionStart" })
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	lines := strings.Split(strings.TrimSpace(string(args)), "\n")
	if last := lines[len(lines)-1]; len(lines) != 2 || !strings.Contains(last, "--resume conv-xyz") || strings.Contains(last, "--continue") {
		t.Errorf("args: %q", args)
	}

	// Stopped now while busy (forced): the command it ran ends too.
	os.WriteFile(filepath.Join(dir, "busy"), nil, 0o644)
	st = waitFor("busy again", func(s State) bool { return s.Busy > 0 })
	s, err := StopScript(st, tn, dir, 0, NowNotice)
	if err != nil {
		t.Fatal(err)
	}
	if _, code := run(s); code != 0 {
		t.Fatalf("stop now: exit %d", code)
	}
	os.Remove(filepath.Join(dir, "busy"))
	st = waitFor("stopped now", func(s State) bool { return s.Stopped && !s.Alive })
	left()
	if out, _ := run(`ps -A -o args= | grep -c '[:] shell-snapshots'`); strings.TrimSpace(out) != "0" {
		t.Errorf("its command still runs: %s", out)
	}
	if code := resume(st); code != 0 {
		t.Fatalf("resume after stop now: exit %d", code)
	}
	waitFor("resumed again", func(s State) bool { return !s.Stopped && s.Alive })

	// Taken over by hand: something runs in the shell of the stopped pane.
	time.Sleep(1100 * time.Millisecond)
	stop(check())
	st = waitFor("stopped again", func(s State) bool { return s.Stopped && !s.Alive })
	left()
	time.Sleep(500 * time.Millisecond)
	run("tmux send-keys -t " + st.StoppedPane + " 'sleep 30' Enter")
	time.Sleep(time.Second)
	if code := resume(st); code != 4 {
		t.Errorf("resume over a running command: exit %d", code)
	}
	if check().Stopped {
		t.Error("the mark stayed")
	}

	// The tmux session ended: the mark goes.
	run("tmux send-keys -t " + st.StoppedPane + " C-c")
	run(`echo 'conv-xyz 1 ` + st.StoppedPane + `' > "` + Dir() + "/" + id + `.stopped"; tmux kill-session -t ` + remote.Quote("="+tn))
	if st = check(); st.Stopped {
		t.Errorf("stale mark kept: %+v", st)
	}
}
