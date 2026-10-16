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
