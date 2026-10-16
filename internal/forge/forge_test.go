package forge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:maarten-boot/multi-session-ssh-dev.git": "github https://github.com/maarten-boot/multi-session-ssh-dev",
		"https://github.com/maarten-boot/multi-session-ssh-dev": "github https://github.com/maarten-boot/multi-session-ssh-dev",
		"https://me:ghp_secret@github.com/o/r.git":              "github https://github.com/o/r",
		"ssh://git@github.com:22/o/r.git":                       "github https://github.com/o/r",
		"git@gitlab.example.com:group/sub/proj.git":             "gitlab https://gitlab.example.com/group/sub/proj",
		"https://git.example.org/team/tool":                     "generic https://git.example.org/team/tool",
		"  git@GitHub.com:Owner/Repo.git/ ":                     "github https://github.com/Owner/Repo",
	} {
		r, ok := Parse(in)
		if got := r.Kind + " " + r.Web; !ok || got != want {
			t.Errorf("Parse(%q) = %q %v, want %q", in, got, ok, want)
		}
		if strings.Contains(r.Web, "secret") {
			t.Errorf("credentials kept: %q", r.Web)
		}
	}
	for _, bad := range []string{"", "/srv/git/repo.git", "../other", "file:///srv/repo", "github.com", `C:\repos\x`, "https://github.com/onlyowner"} {
		if r, ok := Parse(bad); ok {
			t.Errorf("Parse(%q) accepted: %+v", bad, r)
		}
	}
}

func TestLinks(t *testing.T) {
	r, _ := Parse("git@github.com:o/r.git")
	l := r.Links()
	if len(l) != 5 || l[1].Name != "Actions" || l[1].URL != "https://github.com/o/r/actions" {
		t.Errorf("github links %v", l)
	}
	g, _ := Parse("git@gitlab.com:g/r.git")
	if g.Links()[1].URL != "https://gitlab.com/g/r/-/pipelines" {
		t.Errorf("gitlab links %v", g.Links())
	}
}

func TestValidLink(t *testing.T) {
	if err := ValidLink(Link{"Staging", "https://staging.example.com/app"}); err != nil {
		t.Error(err)
	}
	for _, l := range []Link{{"", "https://x.y"}, {"x", "javascript:alert(1)"}, {"x", "file:///etc/passwd"}, {"x", "not a url"}} {
		if ValidLink(l) == nil {
			t.Errorf("%v accepted", l)
		}
	}
}

func TestRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake gh is a shell script")
	}
	bin := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")
	os.WriteFile(filepath.Join(bin, "gh"), []byte(`#!/bin/sh
echo "$@" >`+argsFile+`
cat <<'JSON'
[{"databaseId":7,"workflowName":"build","displayTitle":"Fix release upload","headBranch":"main","event":"push","status":"completed","conclusion":"success","createdAt":"2026-10-16T12:00:00Z","url":"https://github.com/o/r/actions/runs/7"},
 {"databaseId":8,"workflowName":"build","displayTitle":"v0.8.4","headBranch":"v0.8.4","event":"push","status":"in_progress","conclusion":"","createdAt":"2026-10-16T12:05:00Z","url":"https://github.com/o/r/actions/runs/8"}]
JSON
`), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	r, _ := Parse("git@github.com:o/r.git")
	runs, err := Runs(context.Background(), r, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].Conclusion != "success" || runs[1].Status != "in_progress" || runs[1].ID != 8 {
		t.Errorf("runs %+v", runs)
	}
	if a, _ := os.ReadFile(argsFile); !strings.Contains(string(a), "-R o/r -L 10") {
		t.Errorf("gh called with %q", a)
	}
	if _, err := Runs(context.Background(), Repo{Kind: "gitlab"}, 5); !errors.Is(err, ErrNotGitHb) {
		t.Errorf("gitlab: %v", err)
	}
	os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\necho 'To get started with GitHub CLI, please run:  gh auth login' >&2\nexit 4\n"), 0o755)
	if _, err := Runs(context.Background(), r, 5); err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Errorf("not logged in: %v", err)
	}
}
