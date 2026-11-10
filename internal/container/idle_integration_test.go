package container

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"session-workbench/internal/idle"
	"session-workbench/internal/remote"
)

// fakeClaudeSed is a fake claude for a container (no python there): it
// records its arguments, runs the SessionStart hook of its --settings
// (taken out with sed) and waits.
const fakeClaudeSed = `#!/bin/sh
echo "$*" >> DIR/args
settings=
while [ $# -gt 0 ]; do [ "$1" = --settings ] && { settings=$2; shift; }; shift; done
cmd=$(printf '%s' "$settings" | sed 's/.*"SessionStart":\[{"hooks":\[{"type":"command","command":"\(\([^"\\]\|\\.\)*\)"}.*/\1/; s/\\"/"/g')
printf '{"session_id":"conv-box","transcript_path":"DIR/tx.jsonl"}' | sh -c "$cmd"
while :; do sleep 1; done
`

// TestRealIdle stops and resumes a (fake) claude in a real container's
// tmux over ssh (SPEC F17), with SW_CONTAINER_TEST=podman or docker.
func TestRealIdle(t *testing.T) {
	name := os.Getenv("SW_CONTAINER_TEST")
	if name == "" {
		t.Skip("set SW_CONTAINER_TEST=podman or docker to build and run a real container")
	}
	e, err := Find(name)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := user.Current()
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	dir := t.TempDir()
	proj := filepath.Join(dir, "proj")
	os.MkdirAll(filepath.Join(proj, "bin"), 0o755)
	fake := filepath.Join(proj, "bin", "claude")
	os.WriteFile(fake, []byte(strings.ReplaceAll(fakeClaudeSed, "DIR", proj)), 0o755)
	os.WriteFile(filepath.Join(proj, "tx.jsonl"), []byte("{}\n"), 0o644)
	s := Spec{ID: "s-i" + strconv.FormatInt(time.Now().Unix()%100000, 10), LocalPath: proj, Dir: filepath.Join(dir, "c"),
		OwnVolume: true, User: User{Name: u.Username, UID: uid, GID: gid}}
	if p, err := exec.LookPath("claude"); err == nil {
		s.ClaudeBinary = p
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	t.Cleanup(func() { e.Remove(context.Background(), s.ID, true, true) })
	var log strings.Builder
	port, err := e.Up(ctx, s, &log)
	if err != nil {
		t.Fatalf("up: %v\n%s", err, log.String())
	}
	run := func(script string) (string, int) {
		t.Helper()
		args := InsertOptions([]string{"-T", "-o", "BatchMode=yes", "--", Name(s.ID), "exec sh -c " + remote.Quote(script)}, SSHOptions(s, port))
		out, err := exec.Command("ssh", args...).Output()
		code := 0
		if x, ok := err.(*exec.ExitError); ok {
			code = x.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), code
	}
	check := func() idle.State {
		t.Helper()
		sc, _ := idle.CheckScript([]string{s.ID})
		out, code := run(sc)
		l := idle.Parse(out)
		if code != 0 || len(l) != 1 {
			t.Fatalf("check: exit %d %q", code, out)
		}
		return l[0]
	}
	wait := func(what string, ok func(idle.State) bool) idle.State {
		t.Helper()
		for i := 0; i < 60; i++ {
			if st := check(); ok(st) {
				return st
			}
			time.Sleep(500 * time.Millisecond)
		}
		t.Fatalf("%s: %+v", what, check())
		return idle.State{}
	}
	tn := remote.TmuxName(s.ID)
	start := fake + " --continue || " + fake
	pane, _ := remote.PaneCommand(idle.WithHooks(start, s.ID, false))
	if _, code := run("tmux new-session -d -x 100 -y 30 -s " + tn + " -c " + remote.Quote(proj) + " " + remote.Quote(pane)); code != 0 {
		t.Fatal("tmux in the container")
	}
	st := wait("the hook in the container", func(st idle.State) bool { return st.Event == "SessionStart" && st.Alive })
	if st.Conv != "conv-box" || !st.Transcript || st.PaneSession != tn {
		t.Fatalf("%+v", st)
	}
	sc, err := idle.StopScript(st, tn, proj, 0, idle.NowNotice)
	if err != nil {
		t.Fatal(err)
	}
	if _, code := run(sc); code != 0 {
		t.Fatalf("stop: exit %d", code)
	}
	st = wait("stopped", func(st idle.State) bool { return st.Stopped && !st.Alive })
	time.Sleep(time.Second)
	if out, _ := run("tmux capture-pane -p -t " + st.StoppedPane); !strings.Contains(out, "claude stopped from Session Workbench") {
		t.Errorf("pane: %q", out)
	}
	if out, _ := run(`ps -A -o pid=,ppid=,args= | grep '[b]in/claude' | grep -v ' tmux new-session '`); strings.TrimSpace(out) != "" {
		t.Errorf("still running: %s", out)
	}
	p, _ := remote.PaneCommand(idle.ResumeCommand(start, s.ID, st.StoppedConv, false))
	rs, _ := idle.ResumeScript(st, tn, proj, p)
	if _, code := run(rs); code != 0 {
		t.Fatalf("resume: exit %d", code)
	}
	wait("resumed", func(st idle.State) bool { return !st.Stopped && st.Alive && st.Event == "SessionStart" })
	args, _ := os.ReadFile(filepath.Join(proj, "args"))
	if lines := strings.Split(strings.TrimSpace(string(args)), "\n"); len(lines) != 2 || !strings.Contains(lines[1], "--resume conv-box") {
		t.Errorf("args: %q", args)
	}
}
