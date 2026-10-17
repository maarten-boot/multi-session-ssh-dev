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
