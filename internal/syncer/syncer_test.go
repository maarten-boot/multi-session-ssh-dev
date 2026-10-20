package syncer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if f.Branch != "main" || f.MoreBranches {
		t.Fatalf("fetched %+v", f)
	}
	if len(f.Agent) != 1 || f.AgentBranch != "main" {
		t.Fatalf("agent branches %+v, agent on %q", f.Agent, f.AgentBranch)
	}
	if b := f.Agent[0]; b.Name != "main" || b.AgentRef != "agent-s-test/main" || b.State != "ahead" || b.Ahead != 1 || !b.Current ||
		len(b.Commits) != 1 || b.Commits[0].Subject != "agent work" || fmt.Sprint(b.Changes) != "[{changed a.txt}]" {
		t.Fatalf("main: %+v", b)
	}
	if done, err := Take(ctx, s, []string{"main"}); err != nil || len(done) != 1 || !strings.Contains(done[0], "fast-forwarded by 1") {
		t.Fatalf("take main: %q %v", done, err)
	}
	if b, _ := os.ReadFile(filepath.Join(host, "a.txt")); string(b) != "one\ntwo\n" {
		t.Errorf("host a.txt = %q", b)
	}
	if _, err := Take(ctx, s, []string{"origin/main"}); err == nil {
		t.Error("taking a non-agent branch accepted")
	}

	// New branches on the agent (the one checked out there, and another):
	// listed as new, taken over without switching; a host branch with its
	// own commits is not touched.
	remoteSh(t, target, agent+"git checkout -q -b feature/x && echo f >f.txt && git add f.txt && "+
		"git -c user.name=c -c user.email=c@example.com commit -qm 'on feature' && git branch side")
	gitIn(t, host, "branch", "mine")
	gitIn(t, host, "checkout", "-q", "mine")
	os.WriteFile(filepath.Join(host, "m.txt"), []byte("host\n"), 0o644)
	gitIn(t, host, "add", "m.txt")
	gitIn(t, host, "commit", "-q", "-m", "host only")
	gitIn(t, host, "checkout", "-q", "main")
	remoteSh(t, target, agent+"git branch mine main")
	if f, err = Get(ctx, s); err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, b := range f.Agent {
		states[b.Name] = fmt.Sprintf("%s %d/%d", b.State, b.Ahead, b.Behind)
	}
	want := map[string]string{"feature/x": "new 1/0", "side": "new 1/0", "main": "same 0/0", "mine": "behind 0/1"} // side starts at the feature commit
	if fmt.Sprint(states) != fmt.Sprint(want) || f.AgentBranch != "feature/x" {
		t.Fatalf("states %v (want %v), agent on %q", states, want, f.AgentBranch)
	}
	for _, b := range f.Agent {
		if b.Name == "feature/x" && (len(b.Commits) != 1 || fmt.Sprint(b.Changes) != "[{new f.txt}]") {
			t.Errorf("feature/x brings %+v %+v", b.Commits, b.Changes)
		}
	}
	done, err := Take(ctx, s, []string{"feature/x", "side", "mine"})
	if err == nil || !strings.Contains(err.Error(), "mine: your branch has commits") || len(done) != 2 {
		t.Fatalf("take: %q %v", done, err)
	}
	if out, _ := localGit(ctx, s, "log", "-1", "--format=%s", "feature/x"); strings.TrimSpace(out) != "on feature" {
		t.Errorf("feature/x: %q", out)
	}
	if out, _ := localGit(ctx, s, "symbolic-ref", "--short", "HEAD"); strings.TrimSpace(out) != "main" {
		t.Errorf("switched to %q", out)
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

// TestSSHSendMirror checks that rsync and the built-in transfer give the
// same result: the preview counts, changed and new files arrive, files
// deleted here are deleted there, and files git ignores on the agent host
// stay. Run with SW_SSH_TARGET (make test-ssh).
func TestSSHSendMirror(t *testing.T) {
	target := os.Getenv("SW_SSH_TARGET")
	if target == "" {
		t.Skip("SW_SSH_TARGET not set")
	}
	for _, mode := range []string{"rsync", "builtin"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "builtin" {
				t.Setenv("SW_NO_RSYNC", "1")
			} else if _, err := exec.LookPath("rsync"); err != nil {
				t.Skip("no rsync here")
			} else {
				t.Setenv("SW_NO_RSYNC", "")
			}
			ctx := context.Background()
			host := t.TempDir()
			os.WriteFile(filepath.Join(host, "a.txt"), []byte("one\n"), 0o644)
			os.WriteFile(filepath.Join(host, "b.txt"), []byte("bee\n"), 0o644)
			os.WriteFile(filepath.Join(host, ".gitignore"), []byte("build/\n"), 0o644)
			gitIn(t, host, "init", "-q")
			gitIn(t, host, "add", "-A")
			gitIn(t, host, "commit", "-q", "-m", "base")
			base := fmt.Sprintf("sw-mirror-%s-%d", mode, os.Getpid())
			t.Cleanup(func() { exec.Command("ssh", must(remote.ScriptArgs(target, "rm -rf ~/"+base))...).Run() })
			s := Session{ID: "s-mir", Target: target, LocalPath: host, RemotePath: "~/" + base + "/proj"}
			agent := "cd ~/" + base + "/proj && "
			if p, err := Send(ctx, s, false, false); err != nil || !p.Done || p.New < 3 {
				t.Fatalf("first send: %+v %v", p, err)
			}
			// On the agent: build output (ignored) and an untracked file.
			remoteSh(t, target, agent+"mkdir -p build && echo out > build/agent-only && echo x > extra.txt")
			// Here: a.txt changed, b.txt deleted, c.txt new.
			os.WriteFile(filepath.Join(host, "a.txt"), []byte("two\n"), 0o644)
			// Same size: a new mtime, or rsync's quick check may miss it within a second.
			os.Chtimes(filepath.Join(host, "a.txt"), time.Time{}, time.Now().Add(time.Hour))
			os.Remove(filepath.Join(host, "b.txt"))
			os.WriteFile(filepath.Join(host, "c.txt"), []byte("sea\n"), 0o644)
			p, err := Send(ctx, s, true, false)
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]string{}
			for _, c := range p.Changes {
				if !strings.HasPrefix(c.Path, ".git/") {
					got[c.Path] = c.Op
				}
			}
			want := map[string]string{"a.txt": "changed", "b.txt": "deleted", "c.txt": "new", "extra.txt": "deleted"}
			for k, v := range want {
				if got[k] != v {
					t.Errorf("preview: %s = %q, want %q (all: %v)", k, got[k], v, got)
				}
			}
			if _, ok := got["build/agent-only"]; ok {
				t.Error("preview lists an ignored file on the agent host")
			}
			if p, err = Send(ctx, s, false, true); err != nil || !p.Done {
				t.Fatalf("send: %+v %v", p, err)
			}
			out := remoteSh(t, target, agent+"cat a.txt c.txt; ls; cat build/agent-only")
			for _, w := range []string{"two", "sea", "out"} {
				if !strings.Contains(out, w) {
					t.Errorf("agent copy lacks %q: %q", w, out)
				}
			}
			for _, w := range []string{"b.txt", "extra.txt"} {
				if strings.Contains(out, w) {
					t.Errorf("agent copy still has %s: %q", w, out)
				}
			}
		})
	}
}

// Outside a repository top, the built-in send lists files by walking the
// directory and applies only the .gitignore files inside it, like rsync's
// filter: a session in a folder the parent repository ignores still sends
// its files, and a directory whose files are all ignored is an error
// instead of a plan that deletes the whole agent copy.
func TestLocalFilesOutsideRepositoryTop(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("build/\n"), 0o644)
	proj := filepath.Join(repo, "build", "proj")
	os.MkdirAll(filepath.Join(proj, "node_modules"), 0o755)
	os.WriteFile(filepath.Join(proj, "a.txt"), []byte("a\n"), 0o644)
	os.WriteFile(filepath.Join(proj, ".gitignore"), []byte("node_modules/\n"), 0o644)
	os.WriteFile(filepath.Join(proj, "node_modules", "m.js"), []byte("m\n"), 0o644)
	s := Session{LocalPath: proj}
	ic := ignoreChecker{s: s, top: repoTop(ctx, s)}
	if ic.top {
		t.Fatal("subdirectory taken for a repository top")
	}
	files, err := localFiles(ctx, s, ic)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["a.txt"]; !ok || len(files) != 2 {
		t.Errorf("files %v, want a.txt and .gitignore", files)
	}
	ign, err := ic.ignored(ctx, []string{"node_modules/x/y.js", "a.txt", "gone.txt"})
	if err != nil || !ign["node_modules/x/y.js"] || len(ign) != 1 {
		t.Errorf("ignored %v %v", ign, err)
	}

	all := t.TempDir() // not a repository
	os.WriteFile(filepath.Join(all, ".gitignore"), []byte("*\n"), 0o644)
	os.WriteFile(filepath.Join(all, "x"), []byte("x\n"), 0o644)
	if files, err := localFiles(ctx, Session{LocalPath: all}, ignoreChecker{s: Session{LocalPath: all}}); err == nil {
		t.Errorf("everything ignored: %v, want an error", files)
	}
}

// TestSSHSendIgnoredOutsideTop: sessions that are not a repository top
// (a subdirectory with its own .gitignore, a folder the parent repository
// ignores) get the same result from rsync and the built-in transfer:
// files ignored here are kept on the agent host, the rest is mirrored.
func TestSSHSendIgnoredOutsideTop(t *testing.T) {
	target := os.Getenv("SW_SSH_TARGET")
	if target == "" {
		t.Skip("SW_SSH_TARGET not set")
	}
	for _, mode := range []string{"rsync", "builtin"} {
		for _, layout := range []string{"subdir", "ignored"} {
			t.Run(mode+"-"+layout, func(t *testing.T) {
				if mode == "builtin" {
					t.Setenv("SW_NO_RSYNC", "1")
				} else if _, err := exec.LookPath("rsync"); err != nil {
					t.Skip("no rsync here")
				} else {
					t.Setenv("SW_NO_RSYNC", "")
				}
				ctx := context.Background()
				repo := t.TempDir()
				gitIn(t, repo, "init", "-q")
				dir := filepath.Join(repo, "sub")
				if layout == "ignored" {
					os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("build/\n"), 0o644)
					dir = filepath.Join(repo, "build", "proj")
				}
				os.MkdirAll(dir, 0o755)
				os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules/\n"), 0o644)
				os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
				os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bee\n"), 0o644)
				gitIn(t, repo, "add", "-A")
				gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "base")
				base := fmt.Sprintf("sw-ign-%s-%s-%d", mode, layout, os.Getpid())
				t.Cleanup(func() { exec.Command("ssh", must(remote.ScriptArgs(target, "rm -rf ~/"+base))...).Run() })
				s := Session{ID: "s-ign", Target: target, LocalPath: dir, RemotePath: "~/" + base + "/proj"}
				agent := "cd ~/" + base + "/proj && "
				if p, err := Send(ctx, s, false, false); err != nil || !p.Done || p.New < 3 {
					t.Fatalf("first send: %+v %v", p, err)
				}
				remoteSh(t, target, agent+"mkdir -p node_modules/m && echo mod > node_modules/m/i.js && echo x > extra.txt")
				os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644)
				// Same size: a new mtime, or rsync's quick check may miss it within a second.
				os.Chtimes(filepath.Join(dir, "a.txt"), time.Time{}, time.Now().Add(time.Hour))
				os.Remove(filepath.Join(dir, "b.txt"))
				os.WriteFile(filepath.Join(dir, "c.txt"), []byte("sea\n"), 0o644)
				p, err := Send(ctx, s, true, false)
				if err != nil {
					t.Fatal(err)
				}
				got := map[string]string{}
				for _, c := range p.Changes {
					got[c.Path] = c.Op
				}
				want := map[string]string{"a.txt": "changed", "b.txt": "deleted", "c.txt": "new", "extra.txt": "deleted"}
				for k, v := range want {
					if got[k] != v {
						t.Errorf("preview: %s = %q, want %q (all: %v)", k, got[k], v, got)
					}
				}
				for k := range got {
					if strings.HasPrefix(k, "node_modules") {
						t.Errorf("preview lists ignored %s: %v", k, got)
					}
				}
				if p, err = Send(ctx, s, false, true); err != nil || !p.Done {
					t.Fatalf("send: %+v %v", p, err)
				}
				out := remoteSh(t, target, agent+"cat a.txt c.txt node_modules/m/i.js; ls")
				for _, w := range []string{"two", "sea", "mod"} {
					if !strings.Contains(out, w) {
						t.Errorf("agent copy lacks %q: %q", w, out)
					}
				}
				for _, w := range []string{"b.txt", "extra.txt"} {
					if strings.Contains(out, w) {
						t.Errorf("agent copy still has %s: %q", w, out)
					}
				}
			})
		}
	}
}

// agentRepo makes a host repository with agent branches as Get leaves them
// (refs/remotes/agent-<id>/…), without ssh: commits made on a side branch
// and moved under the agent prefix.
func agentRepo(t *testing.T) (Session, func(args ...string) string) {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) string { return gitIn(t, dir, args...) }
	commit := func(file, msg string) {
		os.WriteFile(filepath.Join(dir, file), []byte(msg+"\n"), 0o644)
		git("add", file)
		git("commit", "-q", "-m", msg)
	}
	git("init", "-q")
	commit("a.txt", "base")
	s := Session{ID: "s-t", LocalPath: dir}
	agent := func(name string) { git("update-ref", RefPrefix(s.ID)+name, "HEAD") }
	agent("main")                     // same
	agent("x;touch${IFS}PWNED&&true") // a name a shell would act on (| is not allowed on Windows)
	git("checkout", "-q", "-b", "tmp")
	commit("f.txt", "on feature")
	agent("feature/x") // new, 1 commit
	git("checkout", "-q", "main")
	git("branch", "ahead")
	git("checkout", "-q", "tmp")
	commit("b.txt", "agent work")
	agent("ahead") // ahead by 2 (on feature, agent work)
	git("checkout", "-q", "main")
	git("branch", "split")
	git("checkout", "-q", "split")
	commit("h.txt", "host only")
	git("checkout", "-q", "tmp")
	agent("split") // diverged: 2 agent commits, 1 host commit
	git("checkout", "-q", "main")
	git("branch", "-D", "tmp")
	return s, git
}

func TestCompareAgentBranches(t *testing.T) {
	s, _ := agentRepo(t)
	var f Fetched
	compareAll(context.Background(), s, &f)
	got := map[string]string{}
	for _, b := range f.Agent {
		got[b.Name] = fmt.Sprintf("%s %d/%d %d/%d", b.State, b.Ahead, b.Behind, len(b.Commits), len(b.Changes))
	}
	want := map[string]string{
		"main":                     "same 0/0 0/0",
		"feature/x":                "new 1/0 1/1",
		"ahead":                    "ahead 2/0 2/2",
		"split":                    "diverged 2/1 2/2",
		"x;touch${IFS}PWNED&&true": "unsafe 0/0 0/0",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) || f.Branch != "main" || f.MoreBranches {
		t.Errorf("branches:\n got %v\nwant %v (on %q)", got, want, f.Branch)
	}
	for _, b := range f.Agent {
		if b.Name == "split" && fmt.Sprint(b.Changes) != "[{new b.txt} {new f.txt}]" {
			t.Errorf("split brings %v (since the branches split, not undoing the host's commit)", b.Changes)
		}
	}
}

func TestTakeAgentBranches(t *testing.T) {
	s, git := agentRepo(t)
	ctx := context.Background()
	done, err := Take(ctx, s, []string{"feature/x", "ahead", "split", "x;touch${IFS}PWNED&&true", "-x", "main", "nope"})
	if len(done) != 3 || err == nil {
		t.Fatalf("done %q, err %v", done, err)
	}
	for _, want := range []string{"split: your branch has commits", "x;touch${IFS}PWNED&&true: not taken over", "-x: not taken over", "nope: not an agent branch"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if got := strings.TrimSpace(git("log", "-1", "--format=%s", "feature/x")); got != "on feature" {
		t.Errorf("feature/x: %q", got)
	}
	if got := strings.TrimSpace(git("log", "-1", "--format=%s", "ahead")); got != "agent work" {
		t.Errorf("ahead: %q", got)
	}
	if got := strings.TrimSpace(git("log", "-1", "--format=%s", "split")); got != "host only" {
		t.Errorf("split changed: %q", got)
	}
	if got := strings.TrimSpace(git("symbolic-ref", "--short", "HEAD")); got != "main" {
		t.Errorf("switched to %q", got)
	}
	if _, err := os.Stat(filepath.Join(s.LocalPath, "PWNED")); err == nil {
		t.Error("a branch name ran a command")
	}
	// The checked-out branch is fast-forwarded with its files.
	git("update-ref", RefPrefix(s.ID)+"main", "ahead")
	if done, err := Take(ctx, s, []string{"main"}); err != nil || !strings.Contains(done[0], "fast-forwarded by 2") {
		t.Fatalf("main: %q %v", done, err)
	}
	if _, err := os.Stat(filepath.Join(s.LocalPath, "b.txt")); err != nil {
		t.Error("work tree not updated")
	}
}
