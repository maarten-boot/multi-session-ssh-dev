// Package session manages hosts and sessions on top of the config file:
// validation, path mapping, ordering and persistence. Every change is saved
// immediately; a change that fails validation or saving leaves both the
// in-memory state and the file untouched.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"session-workbench/internal/config"
	"session-workbench/internal/pathmap"
	"session-workbench/internal/remote"
)

// View is a session as shown to the frontend, with derived fields.
type View struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Host       string `json:"host"`
	LocalPath  string `json:"local_path"`
	StartMode  string `json:"start_mode"`
	ColorMode  string `json:"color_mode"` // "" = follow the global setting
	Group      string `json:"group"`
	RemotePath string `json:"remote_path"`
	TmuxName   string `json:"tmux_name"`
	Error      string `json:"error"` // why the Remote tab cannot work, if it cannot
}

// State is everything the frontend needs to render.
type State struct {
	Hosts          []config.Host     `json:"hosts"`
	Groups         []string          `json:"groups"`
	Sessions       []View            `json:"sessions"`
	WorkspaceRoots []string          `json:"workspace_roots"` // ~ expanded
	UI             config.UI         `json:"ui"`
	Keys           map[string]string `json:"keys"`
	ConfigPath     string            `json:"config_path"`
	ConfigError    string            `json:"config_error"` // set: file unreadable, changes are not saved
}

// Resolved is a session with its host and computed remote path.
type Resolved struct {
	config.Session
	Host       config.Host
	LocalPath  string // ~ expanded
	RemotePath string
}

// NewSession is the input for Create.
type NewSession struct {
	Host      string `json:"host"`
	LocalPath string `json:"local_path"`
	Name      string `json:"name"`
	StartMode string `json:"start_mode"`
	ColorMode string `json:"color_mode"` // "" = follow the global setting
	Group     string `json:"group"`
}

// Edit is the input for Update.
type Edit struct {
	Name      string `json:"name"`
	StartMode string `json:"start_mode"`
	ColorMode string `json:"color_mode"`
	Group     string `json:"group"`
}

// Preview is what Create would produce, for live feedback in the dialog.
type Preview struct {
	LocalPath  string `json:"local_path"`
	RemotePath string `json:"remote_path"`
	Name       string `json:"name"`
	Error      string `json:"error"`
}

type Manager struct {
	mu      sync.Mutex
	path    string
	cfg     *config.Config
	loadErr error
}

// Open loads the config at path. If the file exists but cannot be read the
// manager starts empty and read-only so the user's file is never clobbered;
// LoadError reports why.
func Open(path string) *Manager {
	m := &Manager{path: path}
	m.cfg, m.loadErr = config.Load(path)
	if m.loadErr != nil {
		m.cfg = config.Default()
	}
	return m
}

func (m *Manager) LoadError() error { return m.loadErr }

// Config returns a copy of the current configuration.
func (m *Manager) Config() config.Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return *clone(m.cfg)
}

func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := State{
		Hosts:      slices.Clone(m.cfg.Hosts),
		Sessions:   []View{},
		UI:         m.cfg.UI,
		Keys:       maps.Clone(m.cfg.Keys),
		Groups:     slices.Clone(m.cfg.Groups),
		ConfigPath: m.path,
	}
	if s.Keys == nil {
		s.Keys = map[string]string{}
	}
	if s.Groups == nil {
		s.Groups = []string{}
	}
	if s.Hosts == nil {
		s.Hosts = []config.Host{}
	}
	for _, r := range m.cfg.WorkspaceRoots {
		s.WorkspaceRoots = append(s.WorkspaceRoots, ExpandHome(r))
	}
	if m.loadErr != nil {
		s.ConfigError = m.loadErr.Error()
	}
	for _, se := range m.cfg.Sessions {
		v := View{ID: se.ID, Name: se.Name, Host: se.Host, LocalPath: se.LocalPath, StartMode: se.StartMode,
			ColorMode: se.ColorMode, Group: se.Group, TmuxName: remote.TmuxName(se.ID)}
		if r, err := resolve(m.cfg, se); err != nil {
			v.Error = err.Error()
		} else {
			v.RemotePath = r.RemotePath
		}
		s.Sessions = append(s.Sessions, v)
	}
	return s
}

// Resolve returns the session with its host and remote path.
func (m *Manager) Resolve(id string) (Resolved, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.indexOf(id)
	if i < 0 {
		return Resolved{}, fmt.Errorf("unknown session %q", id)
	}
	return resolve(m.cfg, m.cfg.Sessions[i])
}

func resolve(c *config.Config, s config.Session) (Resolved, error) {
	r := Resolved{Session: s, LocalPath: ExpandHome(s.LocalPath)}
	if !remote.ValidID(s.ID) {
		return r, fmt.Errorf("invalid session id %q", s.ID)
	}
	h, ok := findHost(c, s.Host)
	if !ok {
		return r, fmt.Errorf("host %q is not configured", s.Host)
	}
	r.Host = h
	rp, err := remotePath(h, r.LocalPath)
	if err != nil {
		return r, err
	}
	r.RemotePath = rp
	return r, nil
}

func remotePath(h config.Host, local string) (string, error) {
	if h.LocalPrefix == "" && h.RemotePrefix == "" {
		p := filepath.ToSlash(local)
		if !strings.HasPrefix(p, "/") {
			return "", fmt.Errorf("host %q has no path mapping and %q is not a POSIX path", h.Name, local)
		}
		return p, nil
	}
	prefix := ExpandHome(h.LocalPrefix)
	p, err := pathmap.ToRemote(local, prefix, h.RemotePrefix)
	if errors.Is(err, pathmap.ErrNotUnderPrefix) {
		return "", fmt.Errorf("%s is outside %s, the directory host %q shares with the agent host", local, prefix, h.Name)
	}
	return p, err
}

// Preview validates a would-be session without creating it.
func (m *Manager) Preview(in NewSession) Preview {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.newSession(in)
	p := Preview{LocalPath: s.LocalPath, Name: s.Name}
	if err == nil {
		var r Resolved
		r, err = resolve(m.cfg, s)
		p.RemotePath = r.RemotePath
	}
	if err != nil {
		p.Error = err.Error()
	}
	return p
}

// Create adds a session at the end of the list.
func (m *Manager) Create(in NewSession) (View, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.newSession(in)
	if err != nil {
		return View{}, err
	}
	r, err := resolve(m.cfg, s)
	if err != nil {
		return View{}, err
	}
	if err := m.update(func(c *config.Config) error {
		c.Sessions = append(c.Sessions, s)
		return nil
	}); err != nil {
		return View{}, err
	}
	return View{ID: s.ID, Name: s.Name, Host: s.Host, LocalPath: s.LocalPath, StartMode: s.StartMode,
		ColorMode: s.ColorMode, Group: s.Group, RemotePath: r.RemotePath, TmuxName: remote.TmuxName(s.ID)}, nil
}

func (m *Manager) newSession(in NewSession) (config.Session, error) {
	s := config.Session{Host: in.Host, Name: strings.TrimSpace(in.Name), StartMode: in.StartMode,
		ColorMode: in.ColorMode, Group: in.Group}
	if s.StartMode == "" {
		s.StartMode = string(remote.ModeClaude)
	}
	p := strings.TrimSpace(in.LocalPath)
	if p == "" {
		return s, errors.New("choose a directory")
	}
	p = ExpandHome(p)
	if !filepath.IsAbs(p) {
		return s, fmt.Errorf("%q is not an absolute path", p)
	}
	s.LocalPath = filepath.Clean(p)
	if s.Name == "" {
		s.Name = filepath.Base(s.LocalPath)
	}
	if err := validMode(s.StartMode); err != nil {
		return s, err
	}
	if err := validColorMode(s.ColorMode); err != nil {
		return s, err
	}
	if s.Group != "" && !slices.Contains(m.cfg.Groups, s.Group) {
		return s, fmt.Errorf("folder %q does not exist", s.Group)
	}
	if _, ok := findHost(m.cfg, s.Host); !ok {
		return s, fmt.Errorf("host %q is not configured", s.Host)
	}
	if fi, err := os.Stat(s.LocalPath); err != nil {
		return s, fmt.Errorf("%s: %w", s.LocalPath, errors.Unwrap(err))
	} else if !fi.IsDir() {
		return s, fmt.Errorf("%s is not a directory", s.LocalPath)
	}
	for _, o := range m.cfg.Sessions {
		if o.Host == s.Host && filepath.Clean(ExpandHome(o.LocalPath)) == s.LocalPath {
			return s, fmt.Errorf("session %q already uses this directory on %s", o.Name, s.Host)
		}
	}
	s.ID = m.newID()
	return s, nil
}

func validColorMode(mode string) error {
	if mode != "" && mode != "full" && mode != "mono" {
		return fmt.Errorf("unknown color mode %q", mode)
	}
	return nil
}

func validMode(mode string) error {
	switch remote.Mode(mode) {
	case remote.ModeClaude, remote.ModeClaudeContinue, remote.ModeShell:
		return nil
	}
	return fmt.Errorf("unknown start mode %q", mode)
}

func (m *Manager) newID() string {
	for {
		b := make([]byte, 2)
		rand.Read(b)
		id := "s-" + hex.EncodeToString(b)
		if m.indexOf(id) < 0 {
			return id
		}
	}
}

// Update changes a session's name, start mode, color mode ("" follows the
// global setting) and folder ("" = none). The start mode only matters when
// its tmux session is next created.
func (m *Manager) Update(id string, e Edit) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	name := strings.TrimSpace(e.Name)
	if name == "" {
		return errors.New("name cannot be empty")
	}
	if err := validMode(e.StartMode); err != nil {
		return err
	}
	if err := validColorMode(e.ColorMode); err != nil {
		return err
	}
	return m.update(func(c *config.Config) error {
		i := indexOf(c, id)
		if i < 0 {
			return fmt.Errorf("unknown session %q", id)
		}
		if e.Group != "" && !slices.Contains(c.Groups, e.Group) {
			return fmt.Errorf("folder %q does not exist", e.Group)
		}
		c.Sessions[i].Name = name
		c.Sessions[i].StartMode = e.StartMode
		c.Sessions[i].ColorMode = e.ColorMode
		c.Sessions[i].Group = e.Group
		return nil
	})
}

// SetGroup moves a session into a folder ("" = none).
func (m *Manager) SetGroup(id, group string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.update(func(c *config.Config) error {
		i := indexOf(c, id)
		if i < 0 {
			return fmt.Errorf("unknown session %q", id)
		}
		if group != "" && !slices.Contains(c.Groups, group) {
			return fmt.Errorf("folder %q does not exist", group)
		}
		c.Sessions[i].Group = group
		return nil
	})
}

// SaveGroup adds a folder (oldName "") or renames one; renaming moves its
// sessions along.
func (m *Manager) SaveGroup(oldName, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("folder name cannot be empty")
	}
	return m.update(func(c *config.Config) error {
		if name != oldName && slices.Contains(c.Groups, name) {
			return fmt.Errorf("a folder named %q already exists", name)
		}
		if oldName == "" {
			c.Groups = append(c.Groups, name)
			return nil
		}
		i := slices.Index(c.Groups, oldName)
		if i < 0 {
			return fmt.Errorf("unknown folder %q", oldName)
		}
		c.Groups[i] = name
		for j := range c.Sessions {
			if c.Sessions[j].Group == oldName {
				c.Sessions[j].Group = name
			}
		}
		return nil
	})
}

// DeleteGroup removes a folder; its sessions become ungrouped (never deleted).
func (m *Manager) DeleteGroup(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.update(func(c *config.Config) error {
		i := slices.Index(c.Groups, name)
		if i < 0 {
			return fmt.Errorf("unknown folder %q", name)
		}
		c.Groups = slices.Delete(c.Groups, i, i+1)
		for j := range c.Sessions {
			if c.Sessions[j].Group == name {
				c.Sessions[j].Group = ""
			}
		}
		return nil
	})
}

// MoveGroup puts folder name directly before beforeName, or last if "".
func (m *Manager) MoveGroup(name, beforeName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if name == beforeName {
		return nil
	}
	return m.update(func(c *config.Config) error {
		i := slices.Index(c.Groups, name)
		if i < 0 {
			return fmt.Errorf("unknown folder %q", name)
		}
		c.Groups = slices.Delete(c.Groups, i, i+1)
		j := len(c.Groups)
		if beforeName != "" {
			if j = slices.Index(c.Groups, beforeName); j < 0 {
				return fmt.Errorf("unknown folder %q", beforeName)
			}
		}
		c.Groups = slices.Insert(c.Groups, j, name)
		return nil
	})
}

// SaveUI replaces the UI settings.
func (m *Manager) SaveUI(ui config.UI) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := config.ValidateUI(ui); err != nil {
		return err
	}
	if ui.AutoReconnect == nil || ui.ActivityBadges == nil {
		return errors.New("incomplete UI settings")
	}
	return m.update(func(c *config.Config) error {
		c.UI = ui
		return nil
	})
}

func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.update(func(c *config.Config) error {
		i := indexOf(c, id)
		if i < 0 {
			return fmt.Errorf("unknown session %q", id)
		}
		c.Sessions = slices.Delete(c.Sessions, i, i+1)
		return nil
	})
}

// Move puts session id directly before beforeID, or last if beforeID is "".
func (m *Manager) Move(id, beforeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == beforeID {
		return nil
	}
	return m.update(func(c *config.Config) error {
		i := indexOf(c, id)
		if i < 0 {
			return fmt.Errorf("unknown session %q", id)
		}
		s := c.Sessions[i]
		c.Sessions = slices.Delete(c.Sessions, i, i+1)
		j := len(c.Sessions)
		if beforeID != "" {
			if j = indexOf(c, beforeID); j < 0 {
				return fmt.Errorf("unknown session %q", beforeID)
			}
		}
		c.Sessions = slices.Insert(c.Sessions, j, s)
		return nil
	})
}

// SaveHost adds a host (oldName "") or replaces the host named oldName.
// Renaming a host updates its sessions.
func (m *Manager) SaveHost(oldName string, h config.Host) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	h.Name = strings.TrimSpace(h.Name)
	h.SSHTarget = strings.TrimSpace(h.SSHTarget)
	h.LocalPrefix = strings.TrimSpace(h.LocalPrefix)
	h.RemotePrefix = strings.TrimSpace(h.RemotePrefix)
	if err := validHost(h); err != nil {
		return err
	}
	return m.update(func(c *config.Config) error {
		for _, o := range c.Hosts {
			if o.Name == h.Name && o.Name != oldName {
				return fmt.Errorf("a host named %q already exists", h.Name)
			}
		}
		if oldName == "" {
			c.Hosts = append(c.Hosts, h)
			return nil
		}
		i := slices.IndexFunc(c.Hosts, func(o config.Host) bool { return o.Name == oldName })
		if i < 0 {
			return fmt.Errorf("unknown host %q", oldName)
		}
		c.Hosts[i] = h
		for j := range c.Sessions {
			if c.Sessions[j].Host == oldName {
				c.Sessions[j].Host = h.Name
			}
		}
		return nil
	})
}

func validHost(h config.Host) error {
	if h.Name == "" {
		return errors.New("host name cannot be empty")
	}
	if h.SSHTarget == "" || strings.HasPrefix(h.SSHTarget, "-") || strings.ContainsAny(h.SSHTarget, " \t\r\n") {
		return fmt.Errorf("invalid ssh target %q", h.SSHTarget)
	}
	if (h.LocalPrefix == "") != (h.RemotePrefix == "") {
		return errors.New("set both path prefixes, or neither (same paths on both sides)")
	}
	if h.LocalPrefix != "" {
		if !filepath.IsAbs(ExpandHome(h.LocalPrefix)) {
			return fmt.Errorf("local prefix %q is not absolute", h.LocalPrefix)
		}
		if !strings.HasPrefix(h.RemotePrefix, "/") {
			return fmt.Errorf("remote prefix %q is not an absolute POSIX path", h.RemotePrefix)
		}
	}
	return nil
}

// DeleteHost removes a host that no session uses.
func (m *Manager) DeleteHost(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.update(func(c *config.Config) error {
		n := 0
		for _, s := range c.Sessions {
			if s.Host == name {
				n++
			}
		}
		if n > 0 {
			return fmt.Errorf("host %q still has %d session(s)", name, n)
		}
		i := slices.IndexFunc(c.Hosts, func(o config.Host) bool { return o.Name == name })
		if i < 0 {
			return fmt.Errorf("unknown host %q", name)
		}
		c.Hosts = slices.Delete(c.Hosts, i, i+1)
		return nil
	})
}

// update applies fn to a copy of the config and saves it; only on success
// does the copy become current. Callers hold m.mu.
func (m *Manager) update(fn func(*config.Config) error) error {
	if m.loadErr != nil {
		return fmt.Errorf("not saving: the config file could not be read (%v); fix or move it and restart", m.loadErr)
	}
	c := clone(m.cfg)
	if err := fn(c); err != nil {
		return err
	}
	if err := config.Save(m.path, c); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	m.cfg = c
	return nil
}

func clone(c *config.Config) *config.Config {
	n := *c
	n.WorkspaceRoots = slices.Clone(c.WorkspaceRoots)
	n.Local.Args = slices.Clone(c.Local.Args)
	n.Hosts = slices.Clone(c.Hosts)
	n.Sessions = slices.Clone(c.Sessions)
	n.Keys = maps.Clone(c.Keys)
	n.Groups = slices.Clone(c.Groups)
	return &n
}

func findHost(c *config.Config, name string) (config.Host, bool) {
	for _, h := range c.Hosts {
		if h.Name == name {
			return h, true
		}
	}
	return config.Host{}, false
}

func (m *Manager) indexOf(id string) int { return indexOf(m.cfg, id) }

func indexOf(c *config.Config, id string) int {
	return slices.IndexFunc(c.Sessions, func(s config.Session) bool { return s.ID == id })
}

// ExpandHome replaces a leading ~ with the user's home directory.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}
