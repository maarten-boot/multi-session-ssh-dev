package handover

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestEncode(t *testing.T) {
	for in, want := range map[string]string{
		"/home/ann/src/app":                "-home-ann-src-app",
		"/net/192.168.122.1/home/x/my_dev": "-net-192-168-122-1-home-x-my-dev",
	} {
		if got := Encode(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}

func write(t *testing.T, p, s string, mtime time.Time) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(p, mtime, mtime)
}

// TestChanges: missing or older on the other side goes; as new or newer
// there stays.
func TestChanges(t *testing.T) {
	from, to := t.TempDir(), t.TempDir()
	old, now := time.Now().Add(-time.Hour), time.Now()
	write(t, filepath.Join(from, "a.jsonl"), "new", now)
	write(t, filepath.Join(to, "a.jsonl"), "old", old)
	write(t, filepath.Join(from, "b.jsonl"), "same", old)
	write(t, filepath.Join(to, "b.jsonl"), "same", old)
	write(t, filepath.Join(from, "c.jsonl"), "older here", old)
	write(t, filepath.Join(to, "c.jsonl"), "newer there", now)
	write(t, filepath.Join(from, "memory", "MEMORY.md"), "m", now)
	got, err := Changes(from, to)
	if err != nil || strings.Join(got, ",") != "a.jsonl,memory/MEMORY.md" {
		t.Errorf("%v %v", got, err)
	}
	if got, err := Changes(filepath.Join(from, "none"), to); err != nil || len(got) != 0 {
		t.Errorf("no folder: %v %v", got, err)
	}
}

// TestMergeConfig: the entry goes under the new path; the new host's own
// keys win, allowedTools are joined, trust is kept; other projects and
// top-level keys stay.
func TestMergeConfig(t *testing.T) {
	from := `{"projects":{"/w/app":{"allowedTools":["Bash(make)","Read"],"hasTrustDialogAccepted":true,"mcpServers":{"x":{}},"lastCost":1.5}}}`
	to := `{"numStartups":7,"projects":{"/h/app":{"allowedTools":["Read","Edit"],"hasTrustDialogAccepted":false,"lastCost":0.25},"/h/other":{"a":1}}}`
	b, changed, err := MergeConfig([]byte(from), []byte(to), "/w/app", "/h/app")
	if err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	var m map[string]any
	json.Unmarshal(b, &m)
	p := m["projects"].(map[string]any)
	e := p["/h/app"].(map[string]any)
	if got, _ := json.Marshal(e["allowedTools"]); string(got) != `["Read","Edit","Bash(make)"]` {
		t.Errorf("allowedTools %s", got)
	}
	if e["hasTrustDialogAccepted"] != true || e["lastCost"] != 0.25 || e["mcpServers"] == nil {
		t.Errorf("entry %v", e)
	}
	if p["/h/other"] == nil || m["numStartups"] != 7.0 || p["/w/app"] != nil {
		t.Errorf("rest %v", m)
	}
	if !strings.Contains(string(b), `"numStartups": 7`) {
		t.Errorf("numbers as written: %s", b)
	}
	// Again: nothing new.
	if _, changed, _ := MergeConfig([]byte(from), b, "/w/app", "/h/app"); changed {
		t.Error("changed twice")
	}
	// No file on the new host; no entry on the old one.
	if b, changed, err := MergeConfig([]byte(from), nil, "/w/app", "/w/app"); err != nil || !changed || !strings.Contains(string(b), `"/w/app"`) {
		t.Errorf("no file: %s %v %v", b, changed, err)
	}
	if _, changed, err := MergeConfig([]byte(`{"projects":{}}`), []byte(to), "/w/app", "/h/app"); changed || err != nil {
		t.Errorf("no entry: %v %v", changed, err)
	}
	if _, _, err := MergeConfig([]byte(`{bad`), nil, "/a", "/a"); err == nil {
		t.Error("bad JSON accepted")
	}
}

// TestSSHSwitch moves a project's state between two homes on one ssh
// target (two "hosts"), with different project paths. Run with
// SW_SSH_TARGET=localhost (make test-ssh).
func TestSSHSwitch(t *testing.T) {
	target := os.Getenv("SW_SSH_TARGET")
	if target == "" || runtime.GOOS == "windows" {
		t.Skip("set SW_SSH_TARGET (e.g. localhost)")
	}
	work, home := t.TempDir(), t.TempDir()
	old, now := time.Now().Add(-time.Hour), time.Now()
	wp := filepath.Join(work, ".claude", "projects", Encode("/work/app"))
	hp := filepath.Join(home, ".claude", "projects", Encode("/home/app"))
	write(t, filepath.Join(wp, "s1.jsonl"), "conversation one", now)
	write(t, filepath.Join(wp, "s2.jsonl"), "older at work", old)
	write(t, filepath.Join(wp, "s1", "tool-results", "r.txt"), "result", now)
	write(t, filepath.Join(wp, "memory", "MEMORY.md"), "- remember", now)
	write(t, filepath.Join(hp, "s2.jsonl"), "newer at home", now)
	write(t, filepath.Join(hp, "s3.jsonl"), "only at home", now)
	write(t, filepath.Join(work, ".claude.json"), `{"projects":{"/work/app":{"allowedTools":["Bash(make)"],"hasTrustDialogAccepted":true}}}`, now)
	write(t, filepath.Join(home, ".claude.json"), `{"userID":"h","projects":{}}`, now)
	var log strings.Builder
	rep, err := Switch(context.Background(), Side{Target: target, Home: work, Path: "/work/app"},
		Side{Target: target, Home: home, Path: "/home/app"}, filepath.Join(t.TempDir(), "stage"), &log)
	if err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if !rep.Found || rep.Files != 3 || rep.Conversations != 1 || rep.Memory != 1 || !rep.Config {
		t.Errorf("report %+v", rep)
	}
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }
	if read(filepath.Join(hp, "s1.jsonl")) != "conversation one" || read(filepath.Join(hp, "memory", "MEMORY.md")) != "- remember" ||
		read(filepath.Join(hp, "s1", "tool-results", "r.txt")) != "result" {
		t.Error("not copied")
	}
	if read(filepath.Join(hp, "s2.jsonl")) != "newer at home" || read(filepath.Join(hp, "s3.jsonl")) != "only at home" {
		t.Error("the newer or only copy at home was replaced")
	}
	if fi, err := os.Stat(filepath.Join(hp, "s1.jsonl")); err != nil || !fi.ModTime().Truncate(time.Second).Equal(now.Truncate(time.Second)) {
		t.Errorf("mtime not kept: %v", fi)
	}
	cfg := read(filepath.Join(home, ".claude.json"))
	if !strings.Contains(cfg, `"/home/app"`) || !strings.Contains(cfg, "Bash(make)") || !strings.Contains(cfg, `"userID": "h"`) {
		t.Errorf("config:\n%s", cfg)
	}
	if fi, _ := os.Stat(filepath.Join(home, ".claude.json")); fi.Mode().Perm() != 0o600 {
		t.Errorf("config mode %v", fi.Mode())
	}
	// Again: nothing new to send.
	if rep, err := Switch(context.Background(), Side{Target: target, Home: work, Path: "/work/app"},
		Side{Target: target, Home: home, Path: "/home/app"}, filepath.Join(t.TempDir(), "stage"), &log); err != nil || rep.Files != 0 || rep.Config {
		t.Errorf("second switch %+v %v", rep, err)
	}
}
