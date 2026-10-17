package backup

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	missingJSON := `rsync: [sender] link_stat "/home/u/.claude.json" failed: No such file or directory (2)
rsync error: some files/attrs were not transferred (see previous errors) (code 23) at main.c(1338) [Receiver=3.2.7]`
	missingDir := `rsync: [sender] link_stat "/home/u/.claude" failed: No such file or directory (2)
rsync error: some files/attrs were not transferred (see previous errors) (code 23) at main.c(1338) [Receiver=3.2.7]`
	other := `rsync: [receiver] mkstemp "/b/home/.claude/x" failed: Permission denied (13)
rsync error: some files/attrs were not transferred (see previous errors) (code 23) at main.c(1338) [Receiver=3.2.7]`
	cases := []struct {
		code   int
		stderr string
		ok     bool
		want   string
	}{
		{0, "", true, ""},
		{24, "file has vanished", true, ""},
		{23, missingJSON, true, ""},
		{23, missingDir, false, "no ~/.claude"},
		{23, other, false, "Permission denied"},
		{255, "ssh: connect to host x: Connection refused", false, "Connection refused"},
	}
	for _, c := range cases {
		err := classify(c.code, c.stderr)
		if (err == nil) != c.ok || (err != nil && !strings.Contains(err.Error(), c.want)) {
			t.Errorf("classify(%d, %q) = %v", c.code, c.stderr, err)
		}
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{"vibe": "vibe", "a/b": "a_b", "..": "_..", "lab box": "lab_box"} {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDue(t *testing.T) {
	r := &Runner{Dir: t.TempDir(), Hosts: func() []Host { return nil }}
	now := time.Now()
	if !r.due("h", time.Hour, now) {
		t.Error("never backed up: due")
	}
	r.status["h"] = Status{Last: now.Add(-30 * time.Minute)}
	if r.due("h", time.Hour, now) {
		t.Error("30 min after a good backup, every 1h: not due")
	}
	r.status["h"] = Status{Last: now.Add(-20 * time.Minute), Error: "boom"}
	if !r.due("h", time.Hour, now) {
		t.Error("20 min after a failure: retry")
	}
	r.status["h"] = Status{Busy: true}
	if r.due("h", time.Hour, now) {
		t.Error("busy: not due")
	}
}

// TestSSHBackupRoundTrip backs up a fake home over ssh, checks what is left
// out and that nothing is deleted, and restores into an empty home.
// Run with SW_SSH_TARGET=localhost (make test-ssh).
func TestSSHBackupRoundTrip(t *testing.T) {
	target := os.Getenv("SW_SSH_TARGET")
	if target == "" || runtime.GOOS == "windows" {
		t.Skip("set SW_SSH_TARGET (needs the same paths on both sides, e.g. localhost)")
	}
	write := func(p, s string) {
		t.Helper()
		os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	home := t.TempDir()
	write(filepath.Join(home, ".claude", "settings.json"), `{"theme":"light"}`)
	write(filepath.Join(home, ".claude", "projects", "p", "memory", "MEMORY.md"), "- a memory")
	write(filepath.Join(home, ".claude", ".credentials.json"), "SECRET")
	write(filepath.Join(home, ".claude", "cache", "big"), "cache")
	write(filepath.Join(home, ".claude.json"), `{"projects":{}}`)
	h := Host{Name: "h", Target: target, Home: home}
	dir := t.TempDir()
	ctx := context.Background()
	if err := Run(ctx, h, dir); err != nil {
		t.Fatal(err)
	}
	got := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(dir, "home", rel))
		if err != nil {
			return "<missing>"
		}
		return string(b)
	}
	if got(".claude/projects/p/memory/MEMORY.md") != "- a memory" || got(".claude.json") != `{"projects":{}}` {
		t.Errorf("not backed up: memory %q, .claude.json %q", got(".claude/projects/p/memory/MEMORY.md"), got(".claude.json"))
	}
	if got(".claude/.credentials.json") != "<missing>" || got(".claude/cache/big") != "<missing>" {
		t.Error("credentials or cache were backed up")
	}
	// Deleted on the agent host: stays in the backup.
	os.Remove(filepath.Join(home, ".claude", "settings.json"))
	if err := Run(ctx, h, dir); err != nil {
		t.Fatal(err)
	}
	if got(".claude/settings.json") != `{"theme":"light"}` {
		t.Error("a file deleted on the agent host was deleted from the backup")
	}
	// Restore into a fresh home that already has its own settings.json.
	fresh := t.TempDir()
	write(filepath.Join(fresh, ".claude", "settings.json"), "mine")
	if err := Restore(ctx, Host{Name: "h", Target: target, Home: fresh}, dir); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(fresh, ".claude", "projects", "p", "memory", "MEMORY.md"))
	s, _ := os.ReadFile(filepath.Join(fresh, ".claude", "settings.json"))
	if string(b) != "- a memory" || string(s) != "mine" {
		t.Errorf("restore: memory %q (want restored), settings %q (want kept)", b, s)
	}
	// A host without ~/.claude says so.
	if err := Run(ctx, Host{Name: "e", Target: target, Home: t.TempDir()}, t.TempDir()); err == nil || !strings.Contains(err.Error(), "no ~/.claude") {
		t.Errorf("empty home: %v", err)
	}
}
