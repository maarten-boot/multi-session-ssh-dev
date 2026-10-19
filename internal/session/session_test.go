package session

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"session-workbench/internal/config"
	"session-workbench/internal/remote"
)

// setup returns a manager with one host "h" mapping root -> /remote.
func setup(t *testing.T) (m *Manager, root, cfgPath string) {
	t.Helper()
	root = t.TempDir()
	cfgPath = filepath.Join(t.TempDir(), "config.toml")
	m = Open(cfgPath)
	if err := m.SaveHost("", config.Host{Name: "h", SSHTarget: "u@h", LocalPrefix: root, RemotePrefix: "/remote"}); err != nil {
		t.Fatal(err)
	}
	return m, root, cfgPath
}

func mkdir(t *testing.T, root, name string) string {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func ids(m *Manager) []string {
	var out []string
	for _, s := range m.State().Sessions {
		out = append(out, s.ID)
	}
	return out
}

func TestCreateAndPersist(t *testing.T) {
	m, root, cfgPath := setup(t)
	dir := mkdir(t, root, "proj/a")
	v, err := m.Create(NewSession{Host: "h", LocalPath: dir + "/", StartMode: "claude-continue"})
	if err != nil {
		t.Fatal(err)
	}
	if !remote.ValidID(v.ID) || v.Name != "a" || v.LocalPath != dir || v.RemotePath != "/remote/proj/a" || v.TmuxName != "cw-"+v.ID {
		t.Errorf("created %+v", v)
	}

	// A fresh manager reads the same thing back.
	st := Open(cfgPath).State()
	if len(st.Sessions) != 1 || st.Sessions[0] != v {
		t.Errorf("reloaded %+v, want %+v", st.Sessions, v)
	}
	r, err := Open(cfgPath).Resolve(v.ID)
	if err != nil || r.RemotePath != "/remote/proj/a" || r.Host.SSHTarget != "u@h" {
		t.Errorf("resolve: %+v %v", r, err)
	}
}

func TestCreateValidation(t *testing.T) {
	m, root, _ := setup(t)
	dir := mkdir(t, root, "a")
	file := filepath.Join(root, "f")
	os.WriteFile(file, nil, 0o644)
	outside := t.TempDir()
	if _, err := m.Create(NewSession{Host: "h", LocalPath: dir}); err != nil {
		t.Fatal(err)
	}
	for name, in := range map[string]NewSession{
		"no dir":       {Host: "h"},
		"relative":     {Host: "h", LocalPath: "a"},
		"missing":      {Host: "h", LocalPath: filepath.Join(root, "missing")},
		"file":         {Host: "h", LocalPath: file},
		"outside":      {Host: "h", LocalPath: outside},
		"unknown host": {Host: "nope", LocalPath: dir},
		"bad mode":     {Host: "h", LocalPath: dir, StartMode: "vim"},
		"bad colors":   {Host: "h", LocalPath: dir, ColorMode: "sepia"},
		"duplicate":    {Host: "h", LocalPath: dir + "/."},
	} {
		if _, err := m.Create(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if p := m.Preview(in); p.Error == "" {
			t.Errorf("%s: preview has no error", name)
		}
	}
	if n := len(m.State().Sessions); n != 1 {
		t.Errorf("%d sessions after failed creates", n)
	}
}

func TestSameDirOnTwoHosts(t *testing.T) {
	m, root, _ := setup(t)
	if err := m.SaveHost("", config.Host{Name: "g", SSHTarget: "g", LocalPrefix: root, RemotePrefix: "/other"}); err != nil {
		t.Fatal(err)
	}
	dir := mkdir(t, root, "a")
	if _, err := m.Create(NewSession{Host: "h", LocalPath: dir}); err != nil {
		t.Fatal(err)
	}
	v, err := m.Create(NewSession{Host: "g", LocalPath: dir})
	if err != nil || v.RemotePath != "/other/a" {
		t.Errorf("%+v %v", v, err)
	}
}

func TestPreview(t *testing.T) {
	m, root, _ := setup(t)
	dir := mkdir(t, root, "x y")
	p := m.Preview(NewSession{Host: "h", LocalPath: dir})
	if p.Error != "" || p.RemotePath != "/remote/x y" || p.Name != "x y" {
		t.Errorf("%+v", p)
	}
	if len(m.State().Sessions) != 0 {
		t.Error("preview created a session")
	}
}

func TestUpdateMoveDelete(t *testing.T) {
	m, root, cfgPath := setup(t)
	var all []string
	for _, n := range []string{"a", "b", "c", "d"} {
		v, err := m.Create(NewSession{Host: "h", LocalPath: mkdir(t, root, n)})
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, v.ID)
	}
	a, b, c, d := all[0], all[1], all[2], all[3]

	if err := m.Move(d, a); err != nil {
		t.Fatal(err)
	}
	if err := m.Move(a, ""); err != nil {
		t.Fatal(err)
	}
	if err := m.Move(b, b); err != nil {
		t.Fatal(err)
	}
	if got, want := ids(m), []string{d, b, c, a}; !slices.Equal(got, want) {
		t.Errorf("order %v, want %v", got, want)
	}
	if err := m.Move("nope", ""); err == nil {
		t.Error("moved unknown session")
	}

	if err := m.Update(c, Edit{Name: "  renamed ", StartMode: "shell", ColorMode: "mono"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Update(c, Edit{Name: " ", StartMode: "shell"}); err == nil {
		t.Error("empty name accepted")
	}
	if err := m.Update(c, Edit{Name: "x", StartMode: "bogus"}); err == nil {
		t.Error("bad mode accepted")
	}
	if err := m.Update(c, Edit{Name: "x", StartMode: "shell", ColorMode: "sepia"}); err == nil {
		t.Error("bad color mode accepted")
	}
	if err := m.Delete(b); err != nil {
		t.Fatal(err)
	}

	st := Open(cfgPath).State()
	if got, want := ids(&Manager{cfg: mustLoad(t, cfgPath)}), []string{d, c, a}; !slices.Equal(got, want) {
		t.Errorf("persisted order %v, want %v", got, want)
	}
	if st.Sessions[1].Name != "renamed" || st.Sessions[1].StartMode != "shell" || st.Sessions[1].ColorMode != "mono" {
		t.Errorf("update not persisted: %+v", st.Sessions[1])
	}
}

func mustLoad(t *testing.T, p string) *config.Config {
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestHosts(t *testing.T) {
	m, root, _ := setup(t)
	v, err := m.Create(NewSession{Host: "h", LocalPath: mkdir(t, root, "a")})
	if err != nil {
		t.Fatal(err)
	}
	for name, h := range map[string]config.Host{
		"no name":       {SSHTarget: "x"},
		"no target":     {Name: "x"},
		"option target": {Name: "x", SSHTarget: "-oProxyCommand=x"},
		"one prefix":    {Name: "x", SSHTarget: "x", LocalPrefix: "/a"},
		"rel remote":    {Name: "x", SSHTarget: "x", LocalPrefix: "/a", RemotePrefix: "b"},
		"duplicate":     {Name: "h", SSHTarget: "x"},
	} {
		if err := m.SaveHost("", h); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	// Rename cascades to sessions; changing the prefix changes remote paths.
	if err := m.SaveHost("h", config.Host{Name: "h2", SSHTarget: "u@h", LocalPrefix: root, RemotePrefix: "/mnt"}); err != nil {
		t.Fatal(err)
	}
	r, err := m.Resolve(v.ID)
	if err != nil || r.Host.Name != "h2" || r.RemotePath != "/mnt/a" {
		t.Errorf("after rename: %+v %v", r, err)
	}

	if err := m.DeleteHost("h2"); err == nil || !strings.Contains(err.Error(), "1 session") {
		t.Errorf("deleted host in use: %v", err)
	}
	m.Delete(v.ID)
	if err := m.DeleteHost("h2"); err != nil {
		t.Fatal(err)
	}
	if len(m.State().Hosts) != 0 {
		t.Error("host not deleted")
	}
}

func TestNoMappingHost(t *testing.T) {
	m := Open(filepath.Join(t.TempDir(), "c.toml"))
	if err := m.SaveHost("", config.Host{Name: "same", SSHTarget: "localhost"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	v, err := m.Create(NewSession{Host: "same", LocalPath: dir})
	if runtime.GOOS == "windows" {
		// C:\... has no POSIX equivalent without a prefix mapping.
		if err == nil {
			t.Errorf("Windows path accepted without mapping: %+v", v)
		}
		return
	}
	if err != nil || v.RemotePath != filepath.ToSlash(dir) {
		t.Errorf("%+v %v", v, err)
	}
}

func TestUnreadableConfigIsNeverOverwritten(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	bad := []byte("[[hosts]\nthis is broken")
	os.WriteFile(p, bad, 0o644)
	m := Open(p)
	if m.LoadError() == nil || m.State().ConfigError == "" {
		t.Fatal("no load error reported")
	}
	if err := m.SaveHost("", config.Host{Name: "x", SSHTarget: "x"}); err == nil {
		t.Error("change accepted with unreadable config")
	}
	if got, _ := os.ReadFile(p); string(got) != string(bad) {
		t.Error("config file was modified")
	}
}

func TestBrokenSessionIsReportedNotFatal(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte(`
[[hosts]]
name = "h"
ssh_target = "h"
local_prefix = "/a"
remote_prefix = "/r"

[[sessions]]
id = "s-1"
name = "outside"
host = "h"
local_path = "/elsewhere"
start_mode = "claude"

[[sessions]]
id = "s-2"
name = "orphan"
host = "gone"
local_path = "/a/x"
start_mode = "claude"
`), 0o644)
	st := Open(p).State()
	if len(st.Sessions) != 2 || st.Sessions[0].Error == "" || st.Sessions[1].Error == "" {
		t.Errorf("%+v", st.Sessions)
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	for in, want := range map[string]string{
		"~":       home,
		"~/a/b":   filepath.Join(home, "a/b"),
		"/x/~":    "/x/~",
		"~other/": "~other/",
	} {
		if got := ExpandHome(in); got != want {
			t.Errorf("ExpandHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSaveUI(t *testing.T) {
	m, _, cfgPath := setup(t)
	ui := m.State().UI
	ui.Theme = "dark"
	ui.ColorMode = "mono"
	if err := m.SaveUI(ui); err != nil {
		t.Fatal(err)
	}
	if got := Open(cfgPath).State().UI; got.Theme != "dark" || got.ColorMode != "mono" {
		t.Errorf("not persisted: %+v", got)
	}
	ui.Theme = "neon"
	if err := m.SaveUI(ui); err == nil {
		t.Error("invalid theme accepted")
	}
	ui.Theme = "light"
	ui.AutoReconnect = nil
	if err := m.SaveUI(ui); err == nil {
		t.Error("nil bool accepted")
	}
}

func TestGroups(t *testing.T) {
	m, root, cfgPath := setup(t)
	for _, g := range []string{"Work", "Private", "Misc"} {
		if err := m.SaveGroup("", g); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.SaveGroup("", " Work "); err == nil {
		t.Error("duplicate folder accepted")
	}
	if err := m.SaveGroup("", " "); err == nil {
		t.Error("empty folder name accepted")
	}
	a, err := m.Create(NewSession{Host: "h", LocalPath: mkdir(t, root, "a"), Group: "Work"})
	if err != nil || a.Group != "Work" {
		t.Fatalf("%+v %v", a, err)
	}
	if _, err := m.Create(NewSession{Host: "h", LocalPath: mkdir(t, root, "x"), Group: "Nope"}); err == nil {
		t.Error("unknown folder accepted on create")
	}
	b, _ := m.Create(NewSession{Host: "h", LocalPath: mkdir(t, root, "b")})
	if err := m.SetGroup(b.ID, "Private"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetGroup(b.ID, "Nope"); err == nil {
		t.Error("unknown folder accepted on move")
	}
	if err := m.Update(a.ID, Edit{Name: "a", StartMode: "shell", Group: "Nope"}); err == nil {
		t.Error("unknown folder accepted on update")
	}

	// Rename cascades; reorder; delete ungroups (never deletes sessions).
	if err := m.SaveGroup("Work", "Job"); err != nil {
		t.Fatal(err)
	}
	if err := m.MoveGroup("Misc", "Job"); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteGroup("Private"); err != nil {
		t.Fatal(err)
	}
	st := Open(cfgPath).State()
	if !slices.Equal(st.Groups, []string{"Misc", "Job"}) {
		t.Errorf("groups %q", st.Groups)
	}
	if st.Sessions[0].Group != "Job" || st.Sessions[1].Group != "" || len(st.Sessions) != 2 {
		t.Errorf("sessions %+v", st.Sessions)
	}
	if err := m.MoveGroup("Nope", ""); err == nil {
		t.Error("moved unknown folder")
	}
}

func TestTrustGit(t *testing.T) {
	m, root, cfgPath := setup(t)
	v, _ := m.Create(NewSession{Host: "h", LocalPath: mkdir(t, root, "a")})
	if err := m.TrustGit(v.ID, "abc123"); err != nil {
		t.Fatal(err)
	}
	if got := Open(cfgPath).State().Sessions[0].TrustedGit; got != "abc123" {
		t.Errorf("trusted_git = %q", got)
	}
	if err := m.TrustGit("nope", "x"); err == nil {
		t.Error("unknown session accepted")
	}
}

func TestWorkdirModes(t *testing.T) {
	m := Open(filepath.Join(t.TempDir(), "c.toml"))
	for _, h := range []config.Host{
		{Name: "s", SSHTarget: "x", Files: "sync"},
		{Name: "f", SSHTarget: "x", Files: "sshfs", RemoteWorkdir: "/srv/work/"},
	} {
		if err := m.SaveHost("", h); err != nil {
			t.Fatal(err)
		}
	}
	for name, h := range map[string]config.Host{
		"bad mode":        {Name: "b", SSHTarget: "x", Files: "nfs"},
		"prefix and sync": {Name: "b", SSHTarget: "x", Files: "sync", LocalPrefix: "/a", RemotePrefix: "/b"},
		"relative wd":     {Name: "b", SSHTarget: "x", Files: "sync", RemoteWorkdir: "work"},
	} {
		if err := m.SaveHost("", h); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	dir := filepath.Join(t.TempDir(), "My Project!")
	os.Mkdir(dir, 0o755)
	// Any local directory works: nothing is mapped.
	a, err := m.Create(NewSession{Host: "s", LocalPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	if a.RemotePath != "~/sw/My-Project-"+a.ID {
		t.Errorf("sync remote path %q", a.RemotePath)
	}
	b, _ := m.Create(NewSession{Host: "f", LocalPath: dir})
	if b.RemotePath != "/srv/work/My-Project-"+b.ID {
		t.Errorf("sshfs remote path %q", b.RemotePath)
	}
	// Renaming does not move it.
	m.Update(a.ID, Edit{Name: "renamed", StartMode: "claude"})
	if r, _ := m.Resolve(a.ID); r.RemotePath != a.RemotePath {
		t.Errorf("moved after rename: %q", r.RemotePath)
	}
}

func TestLinks(t *testing.T) {
	m, root, cfgPath := setup(t)
	v, _ := m.Create(NewSession{Host: "h", LocalPath: mkdir(t, root, "a")})
	if err := m.SetLinks(v.ID, []config.Link{{Name: " Staging ", URL: " https://staging.example.com "}}); err != nil {
		t.Fatal(err)
	}
	if got := Open(cfgPath).Links(v.ID); len(got) != 1 || got[0].Name != "Staging" || got[0].URL != "https://staging.example.com" {
		t.Errorf("links %+v", got)
	}
	if err := m.SetLinks(v.ID, []config.Link{{Name: "x", URL: "javascript:alert(1)"}}); err == nil {
		t.Error("javascript: link accepted")
	}
}

// Hand-edited hosts and sessions are checked with the dialogs' rules when
// the config is read: what keeps a session from working blocks its Remote
// tab, the rest is reported; nothing in the file changes.
func TestProblemsOfHandEditedConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	// Native absolute paths under a mapped prefix, so the test means the same
	// on Windows (where /srv/a is not absolute and needs a mapping anyway).
	base := t.TempDir()
	dir := func(name string) string { return filepath.Join(base, name) }
	text := strings.NewReplacer("@BASE@", base, "@A@", dir("a"), "@B@", dir("b"), "@C@", dir("c"),
		"@T1@", dir("t1"), "@T2@", dir("t2")).Replace(`groups = ["g1", "g1", ""]

[[hosts]]
name = "ok"
ssh_target = "me@ok"
local_prefix = '@BASE@'
remote_prefix = "/r"

[[hosts]]
name = "bad"
ssh_target = "me@bad"
files = "nfs"

[[hosts]]
name = "ok"
ssh_target = "me@other"

[[sessions]]
id = "s-0001"
name = "fine"
host = "ok"
local_path = '@A@'
start_mode = "shell"

[[sessions]]
id = "s-0002"
name = "on bad host"
host = "bad"
local_path = '@B@'
start_mode = "shell"

[[sessions]]
id = "s-0003"
name = "odd values"
host = "ok"
local_path = '@C@'
start_mode = "claude"
color_mode = "purple"
links = [{ name = "x", url = "javascript:alert(1)" }]

[[sessions]]
id = "s-0004"
name = "bad mode"
host = "ok"
local_path = "relative/dir"
start_mode = "zsh"

[[sessions]]
id = "s-0005"
name = ""
host = "ok"
local_path = '@A@'
start_mode = "shell"

[[sessions]]
id = "s-0006"
name = "twin 1"
host = "ok"
local_path = '@T1@'
start_mode = "shell"

[[sessions]]
id = "s-0006"
name = "twin 2"
host = "ok"
local_path = '@T2@'
start_mode = "shell"

[[sessions]]
id = "s-0007"
name = "lost"
host = "ghost"
local_path = '@A@'
start_mode = "shell"
group = "nope"
`)
	if err := os.WriteFile(cfgPath, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	m := Open(cfgPath)
	if err := m.LoadError(); err != nil {
		t.Fatal(err)
	}
	st := m.State()
	errs := map[string]string{}
	for _, v := range st.Sessions {
		errs[v.Name] = v.Error
	}
	for name, want := range map[string]string{
		"fine": "", "odd values": "",
		"on bad host": `host "bad": unknown file access mode "nfs"`,
		"bad mode":    `unknown start mode "zsh"`,
		"twin 1":      `session id "s-0006" is used by 2 sessions`,
	} {
		if want == "" && errs[name] != "" || want != "" && !strings.Contains(errs[name], want) {
			t.Errorf("session %q: error %q, want %q", name, errs[name], want)
		}
	}
	all := strings.Join(st.Problems, "\n")
	for _, want := range []string{
		`host "bad": unknown file access mode "nfs"`,
		`host "ok" is defined twice`,
		`unknown color mode "purple"; the global setting is used`,
		`link: "javascript:alert(1)" is not an http(s) address`,
		`session "s-0005" has no name`,
		dir("a") + ` already has a session on host "ok"`,
		`session "bad mode": unknown start mode "zsh"`,
		`session "lost": host "ghost" is not configured`,
		`session "lost": folder "nope" is not in groups; it is shown as (missing)`,
		`folder "g1" is listed twice in groups`,
		`groups has an empty folder name`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("problems lack %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, `session "on bad host": host "bad"`) {
		t.Errorf("an invalid host is listed per session too:\n%s", all)
	}
	if got := m.Links("s-0003"); len(got) != 0 {
		t.Errorf("an invalid hand-edited link is passed on: %v", got)
	}
	if b, _ := os.ReadFile(cfgPath); string(b) != text {
		t.Error("checking the config changed the file")
	}
	// A clean config has no problems.
	clean := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(clean, []byte("[[hosts]]\nname = \"h\"\nssh_target = \"me@h\"\n"), 0o600)
	if p := Open(clean).State().Problems; len(p) != 0 {
		t.Errorf("clean config: %v", p)
	}
}

func TestRecords(t *testing.T) {
	for _, c := range []struct {
		record string
		global bool
		want   bool
	}{
		{"", false, false}, {"", true, true}, {"on", false, true}, {"off", true, false}, {"bogus", true, true},
	} {
		if got := Records(config.Session{Record: c.record}, config.UI{RecordRemote: c.global}); got != c.want {
			t.Errorf("Records(%q, global %v) = %v", c.record, c.global, got)
		}
	}
	c := &config.Config{Hosts: []config.Host{{Name: "h", SSHTarget: "h"}},
		Sessions: []config.Session{{ID: "s-1", Name: "x", Host: "h", LocalPath: "/x", StartMode: "shell", Record: "always"}}}
	if p := Problems(c); len(p) != 1 || !strings.Contains(p[0], `record must be "on", "off"`) {
		t.Errorf("problems: %q", p)
	}
}
