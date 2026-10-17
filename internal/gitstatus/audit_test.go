package gitstatus

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func names(a Audit) []string {
	var out []string
	for _, f := range a.Findings {
		out = append(out, f.Kind+":"+f.Name)
	}
	return out
}

func TestAudit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := t.TempDir()
	run(t, repo, "init", "-q")
	marker := filepath.Join(t.TempDir(), "ran")

	clean, err := RunAudit(context.Background(), repo)
	if err != nil || !clean.Repo || len(clean.Findings) != 0 || clean.Fingerprint != "" {
		t.Fatalf("fresh repo: %+v %v", clean, err)
	}

	// Plant what a compromised agent host could write.
	hook := filepath.Join(repo, ".git", "hooks", "pre-push")
	write(t, hook, "#!/bin/sh\ntouch "+marker+"\n")
	os.Chmod(hook, 0o755)
	write(t, filepath.Join(repo, ".git", "hooks", "pre-commit.sample"), "#!/bin/sh\n") // samples are inert
	run(t, repo, "config", "credential.helper", "!evil")
	run(t, repo, "config", "remote.origin.pushurl", "https://me:secret-token@evil.example/repo.git")
	run(t, repo, "config", "alias.st", "status")      // plain alias: fine
	run(t, repo, "config", "alias.boom", "!rm -rf ~") // shell alias: flagged
	run(t, repo, "config", "filter.x.clean", "touch "+marker)
	run(t, repo, "config", "include.path", "/somewhere/else")

	a, err := RunAudit(context.Background(), filepath.Join(repo)) // also works from a subdirectory
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(names(a), " ")
	want := "config:alias.boom config:credential.helper config:filter.x.clean config:include.path config:remote.origin.pushurl hook:pre-push"
	if got != want {
		t.Errorf("findings:\n got %s\nwant %s", got, want)
	}
	for _, f := range a.Findings {
		if strings.Contains(f.Detail, "secret-token") {
			t.Errorf("credentials shown: %q", f.Detail)
		}
		if f.Why == "" || f.Fix == "" {
			t.Errorf("finding without explanation: %+v", f)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("audit ran a repository program")
	}

	// The fingerprint is stable, and changes when a hook's content changes.
	again, _ := RunAudit(context.Background(), repo)
	if a.Fingerprint == "" || again.Fingerprint != a.Fingerprint {
		t.Errorf("fingerprint unstable: %q %q", a.Fingerprint, again.Fingerprint)
	}
	write(t, hook, "#!/bin/sh\necho different\n")
	changed, _ := RunAudit(context.Background(), repo)
	if changed.Fingerprint == a.Fingerprint {
		t.Error("fingerprint did not change with the hook's content")
	}
}

func TestAuditHooksPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if runtime.GOOS == "windows" {
		t.Skip("exec bits")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := t.TempDir()
	run(t, repo, "init", "-q")
	os.MkdirAll(filepath.Join(repo, "sub"), 0o755)
	run(t, repo, "config", "core.hooksPath", ".husky")
	h := filepath.Join(repo, ".husky", "pre-commit")
	write(t, h, "#!/bin/sh\n")
	os.Chmod(h, 0o755)
	write(t, filepath.Join(repo, ".husky", "notes.txt"), "not executable\n")
	a, err := RunAudit(context.Background(), filepath.Join(repo, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names(a), " "); got != "config:core.hookspath hook:pre-commit" { // git lower-cases key names
		t.Errorf("got %s", got)
	}
}

func TestAuditOutsideRepository(t *testing.T) {
	a, err := RunAudit(context.Background(), t.TempDir())
	if err != nil || a.Repo || len(a.Findings) != 0 {
		t.Errorf("%+v %v", a, err)
	}
}

func TestRemoteURL(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := t.TempDir()
	run(t, repo, "init", "-q")
	if got := RemoteURL(context.Background(), repo); got != "" {
		t.Errorf("no remotes: %q", got)
	}
	run(t, repo, "remote", "add", "backup", "https://example.com/b.git")
	if got := RemoteURL(context.Background(), repo); got != "https://example.com/b.git" {
		t.Errorf("first remote: %q", got)
	}
	run(t, repo, "remote", "add", "origin", "git@github.com:o/r.git")
	if got := RemoteURL(context.Background(), repo); got != "git@github.com:o/r.git" {
		t.Errorf("origin preferred: %q", got)
	}
	if got := RemoteURL(context.Background(), t.TempDir()); got != "" {
		t.Errorf("outside a repo: %q", got)
	}
}

// A hostile subsection name must not run anything when the suggested fix
// command is pasted into a shell.
func TestAuditFixCommandIsQuoted(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil || runtime.GOOS == "windows" {
		t.Skip("needs git and a POSIX shell")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := t.TempDir()
	run(t, repo, "init", "-q")
	marker := filepath.Join(t.TempDir(), "pwned")
	cfg := filepath.Join(repo, ".git", "config")
	b, _ := os.ReadFile(cfg)
	write(t, cfg, string(b)+"[url \"x$(touch "+marker+"); y\"]\n\tinsteadOf = z\n")
	a, err := RunAudit(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	var fix string
	for _, f := range a.Findings {
		if strings.HasPrefix(f.Name, "url.") {
			fix = f.Fix
		}
	}
	if fix == "" {
		t.Fatalf("url.*.insteadOf not flagged: %v", names(a))
	}
	// Run the fix as the user would; only git may run, not the planted command.
	cmd := exec.Command("/bin/sh", "-c", fix)
	cmd.Dir = repo
	cmd.CombinedOutput()
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("pasting the fix command ran the planted command: %s", fix)
	}
	again, _ := RunAudit(context.Background(), repo)
	for _, f := range again.Findings {
		if strings.HasPrefix(f.Name, "url.") {
			t.Errorf("fix command did not remove the key: %s", fix)
		}
	}
}

// Keys that make git run programs or send traffic elsewhere are flagged;
// the same keys with harmless values are not.
func TestAuditMoreKeys(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := t.TempDir()
	run(t, repo, "init", "-q")
	set := func(k, v string) { run(t, repo, "config", "--local", k, v) }
	risky := map[string]string{
		"pager.log":                    "evil",
		"difftool.x.cmd":               "evil",
		"mergetool.x.path":             "/tmp/evil",
		"submodule.lib.update":         "!evil",
		"remote.local.uploadpack":      "evil",
		"remote.local.receivepack":     "evil",
		"remote.ext.url":               "ext::sh -c evil",
		"protocol.ext.allow":           "always",
		"http.https://x.example.proxy": "http://proxy.example",
		"remote.origin.proxy":          "http://proxy.example",
		"core.alternateRefsCommand":    "evil",
		"interactive.diffFilter":       "evil",
		"gpg.ssh.defaultKeyCommand":    "evil",
		"browser.x.cmd":                "evil",
		"man.x.path":                   "/tmp/evil",
		"tar.tgz.command":              "evil",
		"trailer.sign.command":         "evil",
		"instaweb.httpd":               "evil",
		"sendemail.toCmd":              "evil",
		"sendemail.smtpServer":         "/usr/local/bin/evil",
	}
	harmless := map[string]string{
		"submodule.lib.url":      "https://example.com/lib.git",
		"submodule.other.update": "rebase",
		"remote.origin.url":      "https://example.com/repo.git",
		"sendemail.smtpServer2":  "x",
		"user.name":              "someone",
	}
	for k, v := range risky {
		set(k, v)
	}
	for k, v := range harmless {
		set(k, v)
	}
	// Last: from here on git works on that directory instead of the repository.
	set("core.worktree", t.TempDir())
	risky["core.worktree"] = ""
	a, err := RunAudit(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range a.Findings {
		got[strings.ToLower(f.Name)] = true
		if f.Why == "" || f.Fix == "" {
			t.Errorf("finding without explanation or fix: %+v", f)
		}
	}
	for k := range risky {
		if !got[strings.ToLower(k)] {
			t.Errorf("%s not flagged", k)
		}
	}
	for k := range harmless {
		if got[strings.ToLower(k)] {
			t.Errorf("%s flagged, but its value is harmless", k)
		}
	}
}

// A planted core.worktree pointing nowhere makes git refuse to run in the
// repository; it must not hide the other findings, and git status skips.
func TestPlantedWorktreeDoesNotHideFindings(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil || runtime.GOOS == "windows" {
		t.Skip("needs git and executable hooks")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := t.TempDir()
	run(t, repo, "init", "-q")
	os.MkdirAll(filepath.Join(repo, "sub"), 0o755)
	hook := filepath.Join(repo, ".git", "hooks", "pre-push")
	write(t, hook, "#!/bin/sh\nexit 0\n")
	os.Chmod(hook, 0o755)
	cfg := filepath.Join(repo, ".git", "config")
	b, _ := os.ReadFile(cfg)
	write(t, cfg, string(b)+"[core]\n\tworktree = /nonexistent/elsewhere\n")

	a, err := RunAudit(context.Background(), filepath.Join(repo, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(names(a), " ")
	for _, want := range []string{"hook:pre-push", "config:core.worktree"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s not found with core.worktree planted: %s", want, got)
		}
	}
	st, err := Get(context.Background(), repo)
	if err != nil || !st.Repo || !strings.Contains(st.Skipped, "core.worktree") {
		t.Errorf("git status with core.worktree planted: %+v %v", st, err)
	}
	if u := RemoteURL(context.Background(), repo); u != "" {
		t.Errorf("RemoteURL: %q", u)
	}
}
