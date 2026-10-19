package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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
	want.UI.TabOrder = []string{"remote", "local", "files", "notes", "web"}
	want.UI.HiddenTabs = []string{"notes"}
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

func TestTabsNormalized(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("[ui]\ntab_order = [\"remote\", \"bogus\", \"local\", \"remote\"]\nhidden_tabs = [\"files\", \"nope\"]\n"), 0o644)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(c.UI.TabOrder, ","); got != "remote,local,files,notes,web" {
		t.Errorf("order %s", got) // unknown dropped, duplicates removed, missing appended
	}
	if got := strings.Join(c.UI.HiddenTabs, ","); got != "files" {
		t.Errorf("hidden %s", got)
	}
	u := c.UI
	u.HiddenTabs = Tabs
	if ValidateUI(u) == nil {
		t.Error("hiding every tab accepted")
	}
	u.HiddenTabs = []string{"x"}
	if ValidateUI(u) == nil {
		t.Error("unknown tab accepted")
	}
}

func TestClaudeBackupSettings(t *testing.T) {
	c := Default()
	if c.UI.ClaudeBackup == nil || !*c.UI.ClaudeBackup || c.UI.ClaudeBackupEvery != "1h" {
		t.Errorf("defaults: backup %v every %q", c.UI.ClaudeBackup, c.UI.ClaudeBackupEvery)
	}
	for every, ok := range map[string]bool{"1h": true, "15m": true, "168h": true, "1m": false, "200h": false, "often": false, "": false} {
		u := c.UI
		u.ClaudeBackupEvery = every
		if err := ValidateUI(u); (err == nil) != ok {
			t.Errorf("ValidateUI with claude_backup_every %q: %v", every, err)
		}
	}
}

// A hand-edited file that hides every tab gets them all back on load.
func TestAllTabsHiddenOnLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("[ui]\nhidden_tabs = [\"local\", \"files\", \"remote\", \"notes\", \"web\"]\n"), 0o644)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.UI.HiddenTabs) != 0 {
		t.Errorf("all tabs stay hidden: %v", c.UI.HiddenTabs)
	}
}

func TestDefaultPathIgnoresRelativeXDG(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("XDG only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "relative/dir")
	p, err := DefaultPath()
	if want := filepath.Join(home, ".config", "session-workbench", "config.toml"); err != nil || p != want {
		t.Errorf("DefaultPath() = %q, %v; want %q", p, err, want)
	}
}

func TestNotificationsDefaultOn(t *testing.T) {
	if c := Default(); c.UI.Notifications == nil || !*c.UI.Notifications {
		t.Errorf("notifications default: %v", c.UI.Notifications)
	}
}

func TestCtrlCopyPasteDefaultOn(t *testing.T) {
	if c := Default(); c.UI.CtrlCopyPaste == nil || !*c.UI.CtrlCopyPaste {
		t.Errorf("ctrl_copy_paste default: %v", c.UI.CtrlCopyPaste)
	}
}

// TestOutOfRangeUIReset: out-of-range values in a hand-edited file fall back
// to the defaults, so the UI is usable and Settings → Save passes ValidateUI.
func TestOutOfRangeUIReset(t *testing.T) {
	for _, tc := range []struct {
		file             string
		font, scrollback int
	}{
		{"[ui]\nfont_size = 100\nscrollback = 2000000\n", 13, 10000},
		{"[ui]\nfont_size = 3\nscrollback = -5\n", 13, 10000},
		{"[ui]\nfont_size = 48\nscrollback = 1\n", 48, 1},
	} {
		p := filepath.Join(t.TempDir(), "c.toml")
		os.WriteFile(p, []byte(tc.file), 0o644)
		c, err := Load(p)
		if err != nil {
			t.Fatal(err)
		}
		if c.UI.FontSize != tc.font || c.UI.Scrollback != tc.scrollback {
			t.Errorf("%q: font %d scrollback %d, want %d %d", tc.file, c.UI.FontSize, c.UI.Scrollback, tc.font, tc.scrollback)
		}
		if err := ValidateUI(c.UI); err != nil {
			t.Errorf("%q: loaded UI does not validate: %v", tc.file, err)
		}
	}
}

func TestRecordSettings(t *testing.T) {
	c := Default()
	if c.UI.RecordRemote || c.UI.RecordEvery != "1h" || c.UI.RecordDir != "" {
		t.Errorf("defaults: record %v every %q dir %q", c.UI.RecordRemote, c.UI.RecordEvery, c.UI.RecordDir)
	}
	for every, ok := range map[string]bool{"5m": true, "1h": true, "24h": true, "1m": false, "25h": false, "soon": false} {
		u := c.UI
		u.RecordEvery = every
		if err := ValidateUI(u); (err == nil) != ok {
			t.Errorf("ValidateUI with record_every %q: %v", every, err)
		}
	}
	abs := t.TempDir() // an absolute path on every OS
	for dir, ok := range map[string]bool{"": true, abs: true, "~": true, "~/logs": true, "logs": false, "./logs": false} {
		u := c.UI
		u.RecordDir = dir
		if err := ValidateUI(u); (err == nil) != ok {
			t.Errorf("ValidateUI with record_dir %q: %v", dir, err)
		}
	}
}

// Every [ui] value out of range or unknown in a hand-edited file gets its
// default, so Settings → Save passes ValidateUI right after loading.
func TestBadUIValuesReset(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte(`[ui]
theme = "purple"
color_mode = "rainbow"
min_contrast = 99
connect_on_start = "never"
claude_backup_every = "1s"
record_every = "7d"
record_dir = "relative/logs"
`), 0o644)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateUI(c.UI); err != nil {
		t.Errorf("loaded UI does not validate: %v (%+v)", err, c.UI)
	}
	if c.UI.RecordDir != "" || c.UI.RecordEvery != "1h" {
		t.Errorf("record_dir %q, record_every %q", c.UI.RecordDir, c.UI.RecordEvery)
	}
}

// The config example in README.md loads (no unknown keys) and validates.
func TestReadmeExample(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Skip(err)
	}
	text := string(b)
	i := strings.Index(text, "```toml\nworkspace_roots")
	if i < 0 {
		t.Fatal("the ```toml config example (starting with workspace_roots) is gone from README.md")
	}
	block := text[i+len("```toml\n"):]
	block = block[:strings.Index(block, "```")]
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte(block), 0o644)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("README example: %v", err)
	}
	if err := ValidateUI(c.UI); err != nil {
		t.Errorf("README example [ui]: %v", err)
	}
	if len(c.Hosts) == 0 || len(c.Sessions) == 0 {
		t.Errorf("README example: %d hosts, %d sessions", len(c.Hosts), len(c.Sessions))
	}
}
