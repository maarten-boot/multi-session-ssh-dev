// Package config loads and saves the app's single TOML config file.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Host is how to reach one agent host and how paths map onto it.
type Host struct {
	Name         string `toml:"name" json:"name"`
	SSHTarget    string `toml:"ssh_target" json:"ssh_target"`
	LocalPrefix  string `toml:"local_prefix,omitempty" json:"local_prefix"` // empty with RemotePrefix: same paths on both sides
	RemotePrefix string `toml:"remote_prefix,omitempty" json:"remote_prefix"`
	// Files is how the agent host gets at session directories:
	// "" or "shared" (a shared file system, mapped with the prefixes),
	// "sync" (a working copy, sent with rsync, commits fetched back) or
	// "sshfs" (the app serves the directory; the agent host mounts it).
	// See docs/CONNECTIVITY.md.
	Files string `toml:"files,omitempty" json:"files"`
	// RemoteWorkdir holds the agent-host copies or mounts for "sync" and
	// "sshfs" (default ~/sw): <remote_workdir>/<dir name>-<session id>.
	RemoteWorkdir string `toml:"remote_workdir,omitempty" json:"remote_workdir"`
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
	ColorMode string            `toml:"color_mode,omitempty" json:"color_mode"` // "" = UI.ColorMode
	Group     string            `toml:"group,omitempty" json:"group"`           // sidebar folder; "" = none
	// TrustedGit is the fingerprint of repository hooks/config the user
	// reviewed; the repository check warns again when it changes.
	TrustedGit string `toml:"trusted_git,omitempty" json:"trusted_git"`
	// Links are the session's own web links, shown in its Web tab.
	Links []Link `toml:"links,omitempty" json:"links"`
	// Record overrides UI.RecordRemote for this session: "on", "off", or
	// "" to follow it.
	Record string `toml:"record,omitempty" json:"record"`
}

// Link is a named web address.
type Link struct {
	Name string `toml:"name" json:"name"`
	URL  string `toml:"url" json:"url"`
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
	// Theme is "light" (black on white) or "dark".
	Theme string `toml:"theme,omitempty" json:"theme"`
	// ColorMode is "full" (program colors, corrected to MinContrast against
	// the background) or "mono" (no colors at all).
	ColorMode   string  `toml:"color_mode,omitempty" json:"color_mode"`
	MinContrast float64 `toml:"min_contrast,omitempty" json:"min_contrast"` // 1 (off) .. 21
	// AutoReconnect retries a dropped Remote connection with backoff.
	AutoReconnect *bool `toml:"auto_reconnect,omitempty" json:"auto_reconnect"`
	// ActivityBadges marks sessions whose hidden terminals ring the bell or
	// start producing output after being idle.
	ActivityBadges *bool `toml:"activity_badges,omitempty" json:"activity_badges"`
	// CopyOnSelect copies terminal selections to the clipboard as they are made.
	CopyOnSelect bool `toml:"copy_on_select,omitempty" json:"copy_on_select"`
	// CtrlCopyPaste (Linux, Windows): Ctrl+C copies when text is selected
	// (otherwise it interrupts as usual) and Ctrl+V pastes.
	CtrlCopyPaste *bool `toml:"ctrl_copy_paste,omitempty" json:"ctrl_copy_paste"`
	// TabOrder is the order of a session's tabs; HiddenTabs are not shown.
	TabOrder   []string `toml:"tab_order,omitempty" json:"tab_order"`
	HiddenTabs []string `toml:"hidden_tabs,omitempty" json:"hidden_tabs"`
	// ClaudeBackup copies each agent host's ~/.claude and ~/.claude.json to
	// DataDir every ClaudeBackupEvery (a Go duration) while the app runs, to
	// restore a crashed or reinstalled agent host (see internal/backup).
	ClaudeBackup *bool `toml:"claude_backup,omitempty" json:"claude_backup"`
	// Notifications shows a desktop notification when a terminal of a
	// session you are not looking at rings the bell (claude waiting).
	Notifications     *bool  `toml:"notifications,omitempty" json:"notifications"`
	ClaudeBackupEvery string `toml:"claude_backup_every,omitempty" json:"claude_backup_every"`
	// RecordRemote records every session's Remote tab (input and output as
	// readable text, see internal/reclog) unless the session says "off";
	// RecordEvery (a Go duration, 5m to 24h) starts a new file per segment;
	// RecordDir is where ("" = <data dir>/logs).
	RecordRemote bool   `toml:"record_remote,omitempty" json:"record_remote"`
	RecordEvery  string `toml:"record_every,omitempty" json:"record_every"`
	RecordDir    string `toml:"record_dir,omitempty" json:"record_dir"`
}

// Tabs lists every session tab in the default order.
var Tabs = []string{"local", "files", "remote", "notes", "web"}

// NormalizedTabs returns u's tab order and hidden tabs, normalized.
func NormalizedTabs(u UI) ([]string, []string) {
	normalizeTabs(&u)
	return u.TabOrder, u.HiddenTabs
}

// normalizeTabs keeps known tab names once each and appends tabs missing
// from the order (e.g. added in a newer version) so they show up.
func normalizeTabs(u *UI) {
	seen := map[string]bool{}
	var order []string
	for _, t := range append(append([]string{}, u.TabOrder...), Tabs...) {
		if slices.Contains(Tabs, t) && !seen[t] {
			seen[t] = true
			order = append(order, t)
		}
	}
	u.TabOrder = order
	var hidden []string
	for _, t := range u.HiddenTabs {
		if slices.Contains(Tabs, t) && !slices.Contains(hidden, t) {
			hidden = append(hidden, t)
		}
	}
	u.HiddenTabs = hidden
	if len(hidden) >= len(Tabs) { // a hand-edited file hid every tab: show them all
		u.HiddenTabs = nil
	}
}

type Config struct {
	WorkspaceRoots []string `toml:"workspace_roots" json:"workspace_roots"`
	// Groups are sidebar folders (e.g. "Work", "Private"), in display order.
	Groups   []string  `toml:"groups,omitempty" json:"groups"`
	UI       UI        `toml:"ui" json:"ui"`
	Local    Local     `toml:"local" json:"local"`
	Hosts    []Host    `toml:"hosts" json:"hosts"`
	Sessions []Session `toml:"sessions" json:"sessions"`
	// Keys overrides keyboard shortcuts: action name -> key, e.g.
	// next_session = "Ctrl+Shift+PageDown". See the README for actions.
	Keys map[string]string `toml:"keys,omitempty" json:"keys"`
}

// Default returns the configuration used when no file exists.
func Default() *Config {
	c := &Config{}
	c.applyDefaults()
	return c
}

func (c *Config) applyDefaults() {
	// Unset or out of range (a hand-edited file): the default, so the UI
	// is usable and Settings → Save passes ValidateUI.
	if c.UI.FontSize < 6 || c.UI.FontSize > 48 {
		c.UI.FontSize = 13
	}
	if c.UI.Scrollback < 1 || c.UI.Scrollback > 1000000 {
		c.UI.Scrollback = 10000
	}
	if c.UI.ConnectOnStart != "eager" {
		c.UI.ConnectOnStart = "lazy"
	}
	if c.UI.Theme != "dark" {
		c.UI.Theme = "light"
	}
	if c.UI.ColorMode != "mono" {
		c.UI.ColorMode = "full"
	}
	if c.UI.MinContrast < 1 || c.UI.MinContrast > 21 {
		c.UI.MinContrast = 4.5
	}
	if c.UI.AutoReconnect == nil {
		c.UI.AutoReconnect = ptr(true)
	}
	if c.UI.ActivityBadges == nil {
		c.UI.ActivityBadges = ptr(true)
	}
	if c.UI.ClaudeBackup == nil {
		c.UI.ClaudeBackup = ptr(true)
	}
	if c.UI.Notifications == nil {
		c.UI.Notifications = ptr(true)
	}
	if c.UI.CtrlCopyPaste == nil {
		c.UI.CtrlCopyPaste = ptr(true)
	}
	if _, err := BackupEvery(c.UI); err != nil {
		c.UI.ClaudeBackupEvery = "1h"
	}
	if _, err := RecordEvery(c.UI); err != nil {
		c.UI.RecordEvery = "1h"
	}
	if !validRecordDir(c.UI.RecordDir) {
		c.UI.RecordDir = "" // the default folder
	}
	normalizeTabs(&c.UI)
}

func ptr[T any](v T) *T { return &v }

// BackupEvery parses UI.ClaudeBackupEvery: 5 minutes to 7 days.
func BackupEvery(u UI) (time.Duration, error) {
	d, err := time.ParseDuration(u.ClaudeBackupEvery)
	if err != nil || d < 5*time.Minute || d > 7*24*time.Hour {
		return 0, fmt.Errorf("claude backup interval %q: use 5m to 168h, e.g. 1h", u.ClaudeBackupEvery)
	}
	return d, nil
}

// RecordEvery parses UI.RecordEvery: 5 minutes to 24 hours.
func RecordEvery(u UI) (time.Duration, error) {
	d, err := time.ParseDuration(u.RecordEvery)
	if err != nil || d < 5*time.Minute || d > 24*time.Hour {
		return 0, fmt.Errorf("recording segment %q: use 5m to 24h, e.g. 1h", u.RecordEvery)
	}
	return d, nil
}

// validRecordDir: empty (the default), absolute, or under ~.
func validRecordDir(d string) bool {
	return d == "" || filepath.IsAbs(d) || d == "~" || strings.HasPrefix(d, "~/") || strings.HasPrefix(d, `~\`)
}

// ValidateUI checks UI settings coming from the settings dialog.
func ValidateUI(u UI) error {
	switch {
	case u.Theme != "light" && u.Theme != "dark":
		return fmt.Errorf("unknown theme %q", u.Theme)
	case u.ColorMode != "full" && u.ColorMode != "mono":
		return fmt.Errorf("unknown color mode %q", u.ColorMode)
	case u.MinContrast < 1 || u.MinContrast > 21:
		return fmt.Errorf("minimum contrast must be between 1 and 21")
	case u.FontSize < 6 || u.FontSize > 48:
		return fmt.Errorf("font size must be between 6 and 48")
	case u.Scrollback < 1 || u.Scrollback > 1000000:
		return fmt.Errorf("scrollback must be between 1 and 1000000 lines")
	case u.ConnectOnStart != "lazy" && u.ConnectOnStart != "eager":
		return fmt.Errorf("unknown connect_on_start %q", u.ConnectOnStart)
	}
	if _, err := BackupEvery(u); err != nil {
		return err
	}
	if _, err := RecordEvery(u); err != nil {
		return err
	}
	if !validRecordDir(u.RecordDir) {
		return fmt.Errorf("recording folder %q: use a full path (or ~/…)", u.RecordDir)
	}
	for _, t := range append(append([]string{}, u.TabOrder...), u.HiddenTabs...) {
		if !slices.Contains(Tabs, t) {
			return fmt.Errorf("unknown tab %q", t)
		}
	}
	if len(u.HiddenTabs) >= len(Tabs) {
		return fmt.Errorf("at least one tab must stay visible")
	}
	return nil
}

// DefaultPath is <user config dir>/session-workbench/config.toml:
// $XDG_CONFIG_HOME or ~/.config on Linux, %AppData% on Windows,
// ~/Library/Application Support on macOS. A relative $XDG_CONFIG_HOME is
// ignored (as for DataDir), not taken as "the current directory".
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil && runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", err
		}
		dir, err = filepath.Join(home, ".config"), nil
	}
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session-workbench", "config.toml"), nil
}

// DataDir is <user data dir>/session-workbench, for data that is not
// configuration (Claude backups): $XDG_DATA_HOME or ~/.local/share on Linux
// (where WebKit keeps the app's storage too), ~/Library/Application Support
// on macOS, %LocalAppData% on Windows.
func DataDir() (string, error) {
	var dir string
	switch runtime.GOOS {
	case "windows":
		dir = os.Getenv("LocalAppData")
	case "darwin":
		d, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		dir = d
	default:
		dir = os.Getenv("XDG_DATA_HOME")
		if dir == "" || !filepath.IsAbs(dir) {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			dir = filepath.Join(home, ".local", "share")
		}
	}
	if dir == "" {
		return "", errors.New("no user data directory")
	}
	return filepath.Join(dir, "session-workbench"), nil
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
