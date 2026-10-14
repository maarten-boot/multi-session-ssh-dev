package session

import (
	"os"
	"path/filepath"
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

	if err := m.Update(c, "  renamed ", "shell", "mono"); err != nil {
		t.Fatal(err)
	}
	if err := m.Update(c, " ", "shell", ""); err == nil {
		t.Error("empty name accepted")
	}
	if err := m.Update(c, "x", "bogus", ""); err == nil {
		t.Error("bad mode accepted")
	}
	if err := m.Update(c, "x", "shell", "sepia"); err == nil {
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
