package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, Default()) {
		t.Errorf("got %+v", c)
	}
	if c.UI.FontSize != 13 || c.UI.Scrollback != 10000 || c.UI.ConnectOnStart != "lazy" ||
		c.UI.Theme != "light" || c.UI.ColorMode != "full" || c.UI.MinContrast != 4.5 ||
		!*c.UI.AutoReconnect || !*c.UI.ActivityBadges {
		t.Errorf("defaults not applied: %+v", c.UI)
	}
}

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml")
	want := Default()
	want.WorkspaceRoots = []string{"/home/u/share/DEV"}
	want.UI.FontFamily = "Iosevka"
	want.UI.Theme = "dark"
	want.UI.ColorMode = "mono"
	want.UI.MinContrast = 7
	want.UI.AutoReconnect = ptr(false)
	want.UI.CopyOnSelect = true
	want.Keys = map[string]string{"next_session": "Ctrl+Tab"}
	want.Local = Local{Shell: "/bin/zsh", Args: []string{"-l"}}
	want.Hosts = []Host{
		{Name: "vibe", SSHTarget: "u@vibe", LocalPrefix: "/home/u/share", RemotePrefix: "/net/h/home/u/share"},
		{Name: "lab", SSHTarget: "lab"},
	}
	want.Sessions = []Session{
		{ID: "s-0001", Name: `it's "quoted"`, Host: "vibe", LocalPath: "/home/u/share/a b", StartMode: "claude",
			Env: map[string]string{"FOO": "bar"}, LocalInit: "git status"},
		{ID: "s-0002", Name: "lab one", Host: "lab", LocalPath: `C:\Users\u\x`, StartMode: "shell", ColorMode: "full"},
	}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip:\n got %+v\nwant %+v", got, want)
	}
	// No temp files left behind.
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want 1", len(entries))
	}
}

func TestSaveReplacesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	c := Default()
	c.WorkspaceRoots = []string{"/a"}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	c.WorkspaceRoots = []string{"/b"}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkspaceRoots[0] != "/b" {
		t.Errorf("got %q", got.WorkspaceRoots)
	}
}

func TestLoadErrors(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"syntax":  "[[hosts]\nname = 1",
		"type":    "[ui]\nfont_size = \"big\"",
		"unknown": "[[hosts]]\nname = \"x\"\nssh_taget = \"typo\"",
	} {
		p := filepath.Join(dir, name+".toml")
		os.WriteFile(p, []byte(body), 0o644)
		if _, err := Load(p); err == nil {
			t.Errorf("%s: no error", name)
		} else if name == "unknown" && !strings.Contains(err.Error(), "ssh_taget") {
			t.Errorf("unknown key error does not name the key: %v", err)
		}
	}
}

func TestHandWrittenFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte(`
workspace_roots = ["~/share-local/DEV"]

[[hosts]]
name         = "vibe"
ssh_target   = "mboot@vibe-u26"
local_prefix = "/home/mboot/share-local"
remote_prefix = "/net/192.168.122.1/home/mboot/share-local"

[[sessions]]
id         = "s-7f3a"
name       = "terminal-app"
host       = "vibe"
local_path = "/home/mboot/share-local/workspace/terminal-app"
start_mode = "claude-continue"
`), 0o644)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Hosts) != 1 || c.Hosts[0].SSHTarget != "mboot@vibe-u26" || len(c.Sessions) != 1 || c.Sessions[0].StartMode != "claude-continue" {
		t.Errorf("parsed %+v", c)
	}
}

func TestValidateUI(t *testing.T) {
	ok := Default().UI
	if err := ValidateUI(ok); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*UI){
		"theme":    func(u *UI) { u.Theme = "blue" },
		"mode":     func(u *UI) { u.ColorMode = "gray" },
		"contrast": func(u *UI) { u.MinContrast = 30 },
		"font":     func(u *UI) { u.FontSize = 2 },
		"scroll":   func(u *UI) { u.Scrollback = -1 },
		"connect":  func(u *UI) { u.ConnectOnStart = "now" },
	} {
		u := ok
		mut(&u)
		if ValidateUI(u) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
