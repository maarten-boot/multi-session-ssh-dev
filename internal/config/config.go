// Package config loads and saves the app's single TOML config file.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Host is how to reach one agent host and how paths map onto it.
type Host struct {
	Name         string `toml:"name" json:"name"`
	SSHTarget    string `toml:"ssh_target" json:"ssh_target"`
	LocalPrefix  string `toml:"local_prefix,omitempty" json:"local_prefix"` // empty with RemotePrefix: same paths on both sides
	RemotePrefix string `toml:"remote_prefix,omitempty" json:"remote_prefix"`
}

// Session is one work item: a directory on the host bound to an agent host.
// The order of [[sessions]] in the file is the order in the sidebar.
type Session struct {
	ID        string            `toml:"id" json:"id"`
	Name      string            `toml:"name" json:"name"`
	Host      string            `toml:"host" json:"host"`
	LocalPath string            `toml:"local_path" json:"local_path"`
	StartMode string            `toml:"start_mode" json:"start_mode"`
	Env       map[string]string `toml:"env,omitempty" json:"env"`               // extra env for the Local tab
	LocalInit string            `toml:"local_init,omitempty" json:"local_init"` // typed into the Local tab after start
}

// Local configures the Local tab's shell; empty means the OS default.
type Local struct {
	Shell string   `toml:"shell,omitempty" json:"shell"`
	Args  []string `toml:"args,omitempty" json:"args"`
}

type UI struct {
	FontFamily string `toml:"font_family,omitempty" json:"font_family"`
	FontSize   int    `toml:"font_size,omitempty" json:"font_size"`
	Scrollback int    `toml:"scrollback,omitempty" json:"scrollback"`
	// ConnectOnStart: "lazy" connects a session's Remote tab when first
	// viewed, "eager" connects every session at startup.
	ConnectOnStart string `toml:"connect_on_start,omitempty" json:"connect_on_start"`
}

type Config struct {
	WorkspaceRoots []string  `toml:"workspace_roots" json:"workspace_roots"`
	UI             UI        `toml:"ui" json:"ui"`
	Local          Local     `toml:"local" json:"local"`
	Hosts          []Host    `toml:"hosts" json:"hosts"`
	Sessions       []Session `toml:"sessions" json:"sessions"`
}

// Default returns the configuration used when no file exists.
func Default() *Config {
	c := &Config{}
	c.applyDefaults()
	return c
}

func (c *Config) applyDefaults() {
	if c.UI.FontSize <= 0 {
		c.UI.FontSize = 13
	}
	if c.UI.Scrollback <= 0 {
		c.UI.Scrollback = 10000
	}
	if c.UI.ConnectOnStart != "eager" {
		c.UI.ConnectOnStart = "lazy"
	}
}

// DefaultPath is <user config dir>/session-workbench/config.toml:
// $XDG_CONFIG_HOME or ~/.config on Linux, %AppData% on Windows,
// ~/Library/Application Support on macOS.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session-workbench", "config.toml"), nil
}

// Load reads path. A missing file yields Default() and no error.
func Load(path string) (*Config, error) {
	c := &Config{}
	md, err := toml.DecodeFile(path, c)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		return nil, fmt.Errorf("config %s: unknown key %q", path, und[0].String())
	}
	c.applyDefaults()
	return c, nil
}

const header = `# Session Workbench configuration.
# Edited by the app; hand edits are fine while the app is not running
# (comments are not preserved).

`

// Save writes c to path atomically: a crash leaves either the old or the
// new file, never a partial one.
func Save(path string, c *Config) error {
	var buf bytes.Buffer
	buf.WriteString(header)
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op after a successful rename
	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
