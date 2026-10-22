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

// changeOps maps the changes outside .git to their op.
func changeOps(p Plan) map[string]string {
	got := map[string]string{}
	for _, c := range p.Changes {
		if !inGit(c.Path) {
			got[c.Path] = c.Op
		}
	}
	return got
}

func TestInGit(t *testing.T) {
	for rel, want := range map[string]bool{".git": true, ".git/config": true, "sub/.git/hooks/x": true,
		".github/x": false, "a.git": false, "x/.gitignore": false} {
		if inGit(rel) != want {
			t.Errorf("inGit(%q) = %v", rel, !want)
		}
	}
}

// A work cycle with full transfers: the agent works in a git copy and in a
// plain directory (an experiment without git); Save keeps everything,
// TakeFiles brings the files (not .git), SendAll restores the saved copy to
// a new agent copy, ignored files included.
func TestSSHFullTransfers(t *testing.T) {
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
			base := fmt.Sprintf("sw-full-%s-%d", mode, os.Getpid())
			t.Cleanup(func() { exec.Command("ssh", must(remote.ScriptArgs(target, "rm -rf ~/"+base))...).Run() })

			// A git session: work committed, uncommitted, untracked and ignored there.
			host := t.TempDir()
			os.WriteFile(filepath.Join(host, "a.txt"), []byte("one\n"), 0o644)
			os.WriteFile(filepath.Join(host, ".gitignore"), []byte("build/\n.env\n"), 0o644)
			os.WriteFile(filepath.Join(host, ".env"), []byte("TOKEN=here\n"), 0o600)
			gitIn(t, host, "init", "-q")
			gitIn(t, host, "add", "-A")
			gitIn(t, host, "commit", "-q", "-m", "base")
			s := Session{ID: "s-full", Target: target, LocalPath: host, RemotePath: "~/" + base + "/proj"}
			agent := "cd ~/" + base + "/proj && "
			if _, err := TakeFiles(ctx, s, true, false); err == nil {
				t.Error("TakeFiles before any send: no error")
			}
			if _, err := Save(ctx, s, filepath.Join(t.TempDir(), "files"), nil); err == nil {
				t.Error("Save before any send: no error")
			}
			// Send all: the ignored .env goes too.
			if p, err := SendAll(ctx, s, host, false); err != nil || !p.Done {
				t.Fatalf("send all: %+v %v", p, err)
			}
			if out := remoteSh(t, target, agent+"cat .env"); !strings.Contains(out, "TOKEN=here") {
				t.Errorf("send all left out the ignored .env: %q", out)
			}
			remoteSh(t, target, agent+"git -c user.name=a -c user.email=a@b commit -q --allow-empty -m agent && "+
				"echo changed > a.txt && echo new > new.txt && mkdir -p build sub && echo out > build/bin && "+
				"echo deep > sub/deep.txt && mkdir -p sub/.git && echo no > sub/.git/hook")
			// Send would refuse now (an unfetched commit); SendAll's preview says so but is not blocked.
			if p, err := Send(ctx, s, true, false); err != nil || p.Blocked == "" {
				t.Errorf("Send not blocked by the agent's commit: %+v %v", p, err)
			}
			p, err := SendAll(ctx, s, host, true)
			if err != nil || p.Blocked != "" || p.Agent.Unfetched == 0 || p.Agent.Dirty == 0 {
				t.Errorf("send all preview: %+v %v", p.Agent, err)
			}

			// Save: everything, with .git and the ignored build output.
			saved := filepath.Join(t.TempDir(), "copy", "files")
			if note, err := Save(ctx, s, saved, nil); err != nil {
				t.Fatalf("save: %v (%s)", err, note)
			}
			for _, f := range []string{"a.txt", "new.txt", "build/bin", ".env", "sub/deep.txt", ".git/HEAD"} {
				if _, err := os.Stat(filepath.Join(saved, filepath.FromSlash(f))); err != nil {
					t.Errorf("saved copy lacks %s", f)
				}
			}
			remoteSh(t, target, agent+"rm new.txt")
			var pcts []int
			if _, err := Save(ctx, s, saved, func(p int) { pcts = append(pcts, p) }); err != nil {
				t.Fatal(err)
			}
			// Only a deletion: rsync reports 0% and is done (the caller counts it as 100).
			if mode == "rsync" && rsyncProgress() && len(pcts) == 0 {
				t.Error("no progress from rsync")
			}
			if _, err := os.Stat(filepath.Join(saved, "new.txt")); err == nil {
				t.Error("a second save kept a file deleted on the agent host")
			}
			remoteSh(t, target, agent+"echo new > new.txt")

			// Take the files: a.txt (with uncommitted changes here too) and the new ones.
			os.WriteFile(filepath.Join(host, "a.txt"), []byte("mine\n"), 0o644)
			os.Chtimes(filepath.Join(host, "a.txt"), time.Time{}, time.Now().Add(-time.Hour))
			os.WriteFile(filepath.Join(host, "local-only.txt"), []byte("keep\n"), 0o644)
			p, err = TakeFiles(ctx, s, true, false)
			if err != nil {
				t.Fatal(err)
			}
			got := changeOps(p)
			for k, v := range map[string]string{"a.txt": "changed", "new.txt": "new", "build/bin": "new", "sub/deep.txt": "new"} {
				if got[k] != v {
					t.Errorf("take preview: %s = %q, want %q (all: %v)", k, got[k], v, got)
				}
			}
			if p.Overwrites != 1 || got["local-only.txt"] != "" || got["sub/.git/hook"] != "" {
				t.Errorf("take preview: overwrites %d, changes %v", p.Overwrites, got)
			}
			if p, err = TakeFiles(ctx, s, false, false); err != nil || !p.Done {
				t.Fatalf("take: %+v %v", p, err)
			}
			for f, want := range map[string]string{"a.txt": "changed\n", "new.txt": "new\n", "build/bin": "out\n", "local-only.txt": "keep\n"} {
				if b, _ := os.ReadFile(filepath.Join(host, filepath.FromSlash(f))); string(b) != want {
					t.Errorf("after take: %s = %q, want %q", f, b, want)
				}
			}
			if _, err := os.Stat(filepath.Join(host, "sub", ".git")); err == nil {
				t.Error("take brought a nested .git")
			}
			if out := gitIn(t, host, "log", "--format=%s"); strings.Contains(out, "agent") {
				t.Error("take changed the host's .git")
			}

			// Restore the saved copy to a new agent copy (the host went away).
			s2 := s
			s2.RemotePath = "~/" + base + "/restored"
			if p, err := SendAll(ctx, s2, saved, false); err != nil || !p.Done || p.New == 0 {
				t.Fatalf("restore: %+v %v", p, err)
			}
			out := remoteSh(t, target, "cd ~/"+base+"/restored && cat build/bin .env && git log --format=%s")
			for _, w := range []string{"out", "TOKEN=here", "agent", "base"} {
				if !strings.Contains(out, w) {
					t.Errorf("restored copy lacks %q: %q", w, out)
				}
			}

			// An experiment without git: take with deletes mirrors the agent's files.
			plain := t.TempDir()
			os.WriteFile(filepath.Join(plain, "x.txt"), []byte("x\n"), 0o644)
			sp := Session{ID: "s-plain", Target: target, LocalPath: plain, RemotePath: "~/" + base + "/plain"}
			if p, err := Send(ctx, sp, false, false); err != nil || !p.Done {
				t.Fatalf("send without git: %+v %v", p, err)
			}
			remoteSh(t, target, "cd ~/"+base+"/plain && echo y > y.txt && rm x.txt")
			os.WriteFile(filepath.Join(plain, "z.txt"), []byte("z\n"), 0o644)
			p, err = TakeFiles(ctx, sp, true, true)
			if got := changeOps(p); err != nil || got["y.txt"] != "new" || got["x.txt"] != "deleted" || got["z.txt"] != "deleted" {
				t.Errorf("take with deletes, preview: %v %v", got, err)
			}
			if _, err := TakeFiles(ctx, sp, false, true); err != nil {
				t.Fatal(err)
			}
			for f, there := range map[string]bool{"y.txt": true, "x.txt": false, "z.txt": false} {
				if _, err := os.Stat(filepath.Join(plain, f)); (err == nil) != there {
					t.Errorf("after take with deletes: %s there = %v", f, err == nil)
				}
			}
		})
	}
}

// The take's overwrite count: changed or deleted files with uncommitted
// changes here, staged or not, with paths relative to the session
// directory also when it is a repository's subdirectory.
func TestOverwritten(t *testing.T) {
	repo := t.TempDir()
	sub := filepath.Join(repo, "sub")
	os.MkdirAll(sub, 0o755)
	for _, f := range []string{"sub/a.txt", "sub/b.txt", "sub/c.txt", "sub/d.txt", "top.txt"} {
		os.WriteFile(filepath.Join(repo, filepath.FromSlash(f)), []byte("x\n"), 0o644)
	}
	gitIn(t, repo, "init", "-q")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "-m", "base")
	os.WriteFile(filepath.Join(sub, "a.txt"), []byte("mine\n"), 0o644) // unstaged
	os.WriteFile(filepath.Join(sub, "b.txt"), []byte("mine\n"), 0o644)
	gitIn(t, repo, "add", "sub/b.txt") // staged
	os.WriteFile(filepath.Join(sub, "d.txt"), []byte("mine\n"), 0o644)
	changes := []Change{{"changed", "a.txt"}, {"changed", "b.txt"}, {"changed", "c.txt"}, {"deleted", "d.txt"}, {"new", "e.txt"}}
	if n := overwritten(context.Background(), Session{LocalPath: sub}, changes); n != 3 {
		t.Errorf("in a subdirectory: %d, want 3 (a, b staged, d deleted)", n)
	}
	if n := overwritten(context.Background(), Session{LocalPath: t.TempDir()}, changes); n != 0 {
		t.Errorf("without git: %d", n)
	}
}

func TestReadProgress(t *testing.T) {
	out := "\r          0   0%    0.00kB/s    0:00:00 (xfr#0, to-chk=0/5)" +
		"\r     32,768  45%   31.25MB/s    0:00:00 (xfr#1, to-chk=3/5)" +
		"\r     32,768  45%   31.25MB/s    0:00:00 (xfr#1, to-chk=3/5)" +
		"\r     65,536 100%   62.50MB/s    0:00:00 (xfr#2, to-chk=0/5)\n" +
		"some other line with 50% in the text\n"
	var got []int
	readProgress(strings.NewReader(out), func(p int) { got = append(got, p) })
	if fmt.Sprint(got) != "[0 45 100 50]" {
		t.Errorf("progress %v", got)
	}
}
