package config

import (
	"path/filepath"
	"testing"

	"session-workbench/internal/profile"
)

// TestProfileDirs: a profile's config and data folders carry its name.
func TestProfileDirs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	defer profile.Set("")
	def, _ := DefaultPath()
	data, _ := DataDir()
	profile.Set("ssh-switcher")
	p, err := DefaultPath()
	if err != nil || filepath.Base(filepath.Dir(p)) != "ssh-switcher" || filepath.Dir(filepath.Dir(p)) != filepath.Dir(filepath.Dir(def)) {
		t.Errorf("config %s (default %s): %v", p, def, err)
	}
	d, err := DataDir()
	if err != nil || filepath.Base(d) != "ssh-switcher" || filepath.Dir(d) != filepath.Dir(data) {
		t.Errorf("data %s (default %s): %v", d, data, err)
	}
}

// TestBacksUp: hosts are backed up unless switched off; a host whose
// sessions are plain logins is not, unless switched on.
func TestBacksUp(t *testing.T) {
	no, yes := false, true
	for _, c := range []struct {
		h    Host
		want bool
	}{
		{Host{}, true},
		{Host{LocalDir: &yes}, true},
		{Host{LocalDir: &no}, false},
		{Host{LocalDir: &no, Backup: &yes}, true},
		{Host{Backup: &no}, false},
	} {
		if BacksUp(c.h) != c.want {
			t.Errorf("%+v: want %v", c.h, c.want)
		}
	}
}
