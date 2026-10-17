package syncer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"session-workbench/internal/remote"
)

func TestParseItemize(t *testing.T) {
	out := strings.Join([]string{
		"created directory sw/proj", // first send
		"cd+++++++++ src/",
		"<f+++++++++ src/new.go", // what rsync prints when sending
		"<f.st...... README.md",
		"*deleting   old.txt",
		".d..t...... ./",
		"cL+++++++++ link -> target",
		"",
	}, "\n")
	var p Plan
	parseItemize(&p, out)
	if p.New != 2 || p.Changed != 1 || p.Deleted != 1 {
		t.Errorf("counts new=%d changed=%d deleted=%d", p.New, p.Changed, p.Deleted)
	}
	want := []Change{{"new", "src/new.go"}, {"changed", "README.md"}, {"deleted", "old.txt"}, {"new", "link -> target"}}
	if fmt.Sprint(p.Changes) != fmt.Sprint(want) {
		t.Errorf("changes %v", p.Changes)
	}
}

func TestRsyncPath(t *testing.T) {
	for in, want := range map[string]string{"~/sw/p-s-1": "sw/p-s-1", "/srv/x": "/srv/x", "~": "."} {
		if got := rsyncPath(in); got != want {
			t.Errorf("rsyncPath(%q) = %q", in, got)
		}
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com",
		"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// remoteSh runs a script on the agent host.
func remoteSh(t *testing.T, target, script string) string {
	t.Helper()
	args, _ := remote.ScriptArgs(target, script)
	out, err := exec.Command("ssh", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("remote %q: %v\n%s", script, err, out)
	}
	return string(out)
}

// TestRoundTrip needs an agent host reachable without prompts, with git and
// rsync: SW_SSH_TARGET=localhost go test ./internal/syncer -run RoundTrip -v
func TestRoundTrip(t *testing.T) {
	target := os.Getenv("SW_SSH_TARGET")
	if target == "" {
		t.Skip("SW_SSH_TARGET not set")
	}
	ctx := context.Background()
	host := t.TempDir()
	os.WriteFile(filepath.Join(host, "a.txt"), []byte("one\n"), 0o644)
	os.WriteFile(filepath.Join(host, ".gitignore"), []byte("build/\n"), 0o644)
	os.MkdirAll(filepath.Join(host, "build"), 0o755)
	os.WriteFile(filepath.Join(host, "build", "big.bin"), []byte("ignored\n"), 0o644)
	gitIn(t, host, "init", "-q")
	gitIn(t, host, "add", "-A")
	gitIn(t, host, "commit", "-q", "-m", "base")

	base := fmt.Sprintf("sw-synctest-%d", os.Getpid())
	t.Cleanup(func() { exec.Command("ssh", must(remote.ScriptArgs(target, "rm -rf ~/"+base))...).Run() })
	s := Session{ID: "s-test", Target: target, LocalPath: host, RemotePath: "~/" + base + "/proj-s-test"}
	agent := "cd ~/" + base + "/proj-s-test && "

	// Preview of the first send: everything is new, nothing is written.
	p, err := Send(ctx, s, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Done || p.Agent.Exists || p.New == 0 || p.Blocked != "" {
		t.Fatalf("preview %+v", p)
	}
	if out := remoteSh(t, target, "test -e ~/"+base+"/proj-s-test && echo exists || echo absent"); !strings.Contains(out, "absent") {
		t.Error("dry run wrote files")
	}
	if p, err = Send(ctx, s, false, false); err != nil || !p.Done {
		t.Fatalf("send: %+v %v", p, err)
	}
	if out := remoteSh(t, target, agent+"cat a.txt; test -e build && echo HAS-BUILD; git log --oneline | wc -l"); !strings.Contains(out, "one") || strings.Contains(out, "HAS-BUILD") {
		t.Errorf("agent copy: %q", out)
	}

	// claude commits on the agent: sending is now refused.
	remoteSh(t, target, agent+"echo two >>a.txt && git -c user.name=c -c user.email=c@example.com commit -qam 'agent work'")
	if p, err = Send(ctx, s, false, false); err != nil || p.Blocked == "" || p.Agent.Unfetched != 1 || p.Done {
		t.Fatalf("send with unfetched agent commit: %+v %v", p, err)
	}

	// Get the commit back and fast-forward.
	f, err := Get(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if f.Branch != "main" || f.AgentRef != "agent-s-test/main" || len(f.New) != 1 || f.New[0].Subject != "agent work" || !f.CanFF {
		t.Fatalf("fetched %+v", f)
	}
	if err := FastForward(ctx, s, f.AgentRef); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(host, "a.txt")); string(b) != "one\ntwo\n" {
		t.Errorf("host a.txt = %q", b)
	}
	if err := FastForward(ctx, s, "origin/main"); err == nil {
		t.Error("fast-forward to a non-agent ref accepted")
	}

	// Uncommitted agent changes block a real send until overwriting is confirmed.
	remoteSh(t, target, agent+"echo scratch >notes.txt")
	if p, _ = Send(ctx, s, false, false); p.Blocked == "" || p.Agent.Dirty != 1 {
		t.Fatalf("dirty agent: %+v", p)
	}
	if p, err = Send(ctx, s, false, true); err != nil || !p.Done || !strings.Contains(fmt.Sprint(p.Changes), "{deleted notes.txt}") {
		t.Fatalf("overwrite: %+v %v", p, err)
	}
	if out := remoteSh(t, target, agent+"git status --porcelain | wc -l"); strings.TrimSpace(out) != "0" {
		t.Errorf("agent not clean after send: %q", out)
	}
}

func must(a []string, err error) []string {
	if err != nil {
		panic(err)
	}
	return a
}

// A session for a subdirectory of a repository: Get says why it cannot
// work instead of "use Send first", and the send preview warns.
func TestSubdirectoryOfRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	sub := filepath.Join(repo, "sub")
	os.MkdirAll(sub, 0o755)
	s := Session{ID: "s-sub", Target: "nohost.invalid", LocalPath: sub, RemotePath: "~/sw/sub-s-sub"}
	top := subdirOf(context.Background(), s)
	if top == "" {
		t.Fatal("subdirectory not detected")
	}
	_, err := Get(context.Background(), s)
	if err == nil || !strings.Contains(err.Error(), "part of the repository at") || strings.Contains(err.Error(), "use Send") {
		t.Errorf("Get on a subdirectory: %v", err)
	}
	if got := subdirOf(context.Background(), Session{LocalPath: repo}); got != "" {
		t.Errorf("repository top reported as subdirectory of %q", got)
	}
}
