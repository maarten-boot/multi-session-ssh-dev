package gitstatus

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	out := strings.Join([]string{
		"# branch.oid 1234abcd",
		"# branch.head main",
		"# branch.upstream origin/main",
		"# branch.ab +2 -1",
		"1 .M N... 100644 100644 100644 aaaa bbbb sub/mod.go",
		"1 A. N... 000000 100644 100644 0000 cccc sub/new file.txt",
		"1 .D N... 100644 100644 000000 dddd dddd gone.txt",
		"2 R. N... 100644 100644 100644 eeee eeee R100 sub/deep/renamed.go",
		"sub/old.go",
		"u UU N... 100644 100644 100644 100644 ffff gggg hhhh sub/deep/conflict.go",
		"? other/untracked.txt",
		"? newdir/",
		"! node_modules/",
		"! sub/build.log",
		"",
	}, "\x00")
	st := Status{Files: map[string]string{}, Dirs: map[string]string{}}
	parse(&st, []byte(out), "")
	if st.Branch != "main" || st.Upstream != "origin/main" || st.Ahead != 2 || st.Behind != 1 || st.Detached {
		t.Errorf("branch: %+v", st)
	}
	wantFiles := map[string]string{
		"sub/mod.go": "M", "sub/new file.txt": "A", "gone.txt": "D", "sub/deep/renamed.go": "R",
		"sub/deep/conflict.go": "C", "other/untracked.txt": "?", "newdir": "?",
		"node_modules": "I", "sub/build.log": "I",
	}
	if !reflect.DeepEqual(st.Files, wantFiles) {
		t.Errorf("files:\n got %v\nwant %v", st.Files, wantFiles)
	}
	wantDirs := map[string]string{"sub": "C", "sub/deep": "C", "other": "?"}
	if !reflect.DeepEqual(st.Dirs, wantDirs) {
		t.Errorf("dirs:\n got %v\nwant %v", st.Dirs, wantDirs)
	}

	// Seen from the subdirectory "sub/": paths are relative to it and
	// entries outside it are dropped.
	st = Status{Files: map[string]string{}, Dirs: map[string]string{}}
	parse(&st, []byte(out), "sub/")
	wantFiles = map[string]string{"mod.go": "M", "new file.txt": "A", "deep/renamed.go": "R", "deep/conflict.go": "C", "build.log": "I"}
	if !reflect.DeepEqual(st.Files, wantFiles) {
		t.Errorf("prefixed files:\n got %v\nwant %v", st.Files, wantFiles)
	}
	if !reflect.DeepEqual(st.Dirs, map[string]string{"deep": "C"}) {
		t.Errorf("prefixed dirs: %v", st.Dirs)
	}
}

func TestDetachedHead(t *testing.T) {
	st := Status{Files: map[string]string{}, Dirs: map[string]string{}}
	parse(&st, []byte("# branch.oid abc\x00# branch.head (detached)\x00"), "")
	if !st.Detached || st.Upstream != "" {
		t.Errorf("%+v", st)
	}
}

// run executes git with a fixed identity and no user/system config.
func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com",
		"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, path, data string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRealRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := t.TempDir()
	run(t, repo, "init", "-q")
	write(t, filepath.Join(repo, "a.txt"), "one\n")
	write(t, filepath.Join(repo, "sub", "b.txt"), "two\n")
	write(t, filepath.Join(repo, "old.txt"), "old\n")
	write(t, filepath.Join(repo, ".gitignore"), "build/\n")
	run(t, repo, "add", "-A")
	run(t, repo, "commit", "-q", "-m", "base")
	run(t, repo, "branch", "base")
	run(t, repo, "branch", "--set-upstream-to=base")

	write(t, filepath.Join(repo, "a.txt"), "one changed\n")        // modified
	write(t, filepath.Join(repo, "sub", "new.txt"), "new\n")       // added
	run(t, repo, "add", "sub/new.txt")                             //
	run(t, repo, "mv", "old.txt", "renamed.txt")                   // renamed
	write(t, filepath.Join(repo, "untracked dir", "x.txt"), "x\n") // untracked dir
	write(t, filepath.Join(repo, "build", "out.bin"), "bin\n")     // ignored
	os.Remove(filepath.Join(repo, "sub", "b.txt"))                 // deleted

	st, err := Get(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Repo || st.Branch != "main" || st.Upstream != "base" || st.Ahead != 0 {
		t.Errorf("repo info: %+v", st)
	}
	want := map[string]string{
		"a.txt": "M", "sub/new.txt": "A", "renamed.txt": "R", "untracked dir": "?",
		"build": "I", "sub/b.txt": "D",
	}
	if !reflect.DeepEqual(st.Files, want) {
		t.Errorf("files:\n got %v\nwant %v", st.Files, want)
	}
	if st.Dirs["sub"] != "M" {
		t.Errorf("dirs: %v", st.Dirs)
	}

	// Ahead of upstream after a commit.
	run(t, repo, "commit", "-q", "-m", "more")
	st, _ = Get(context.Background(), repo)
	if st.Ahead != 1 {
		t.Errorf("ahead = %d, want 1", st.Ahead)
	}

	// The session directory may be a subdirectory of the repository.
	sub, err := Get(context.Background(), filepath.Join(repo, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"b.txt": "D"}; !reflect.DeepEqual(sub.Files, want) {
		t.Errorf("subdir view: got %v, want %v", sub.Files, want)
	}

	// Outside a repository: not an error, just no repo.
	none, err := Get(context.Background(), t.TempDir())
	if err != nil || none.Repo {
		t.Errorf("outside repo: %+v %v", none, err)
	}
}
