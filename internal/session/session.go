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
	"regexp"
	"slices"
	"strings"
	"sync"

	"session-workbench/internal/config"
	"session-workbench/internal/forge"
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
	TrustedGit string `json:"trusted_git"`
	Files      string `json:"files"` // the host's file access mode
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
	// Problems are hand-edited hosts and sessions that break the rules the
	// dialogs enforce (see Problems); the file is not changed.
	Problems []string `json:"problems"`
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
	s.Problems = Problems(m.cfg)
	for _, se := range m.cfg.Sessions {
		v := View{ID: se.ID, Name: se.Name, Host: se.Host, LocalPath: se.LocalPath, StartMode: se.StartMode,
			ColorMode: se.ColorMode, Group: se.Group, TrustedGit: se.TrustedGit, TmuxName: remote.TmuxName(se.ID)}
		if r, err := resolve(m.cfg, se); err != nil {
			v.Error = err.Error()
		} else {
			v.RemotePath = r.RemotePath
			v.Files = FilesMode(r.Host)
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

// File access modes of a host (config.Host.Files).
const (
	FilesShared = "shared"
	FilesSync   = "sync"
	FilesSSHFS  = "sshfs"
)

// FilesMode returns the host's file access mode, "shared" by default.
func FilesMode(h config.Host) string {
	if h.Files == "" {
		return FilesShared
	}
	return h.Files
}

// resolve checks what the Remote tab needs, with the dialogs' rules (a
// hand-edited file may break them): a valid unique id, a valid host, a known
// start mode and an absolute directory.
func resolve(c *config.Config, s config.Session) (Resolved, error) {
	r := Resolved{Session: s, LocalPath: ExpandHome(s.LocalPath)}
	if !remote.ValidID(s.ID) {
		return r, fmt.Errorf("invalid session id %q", s.ID)
	}
	if n := countID(c, s.ID); n > 1 {
		return r, fmt.Errorf("session id %q is used by %d sessions in the config; give each its own", s.ID, n)
	}
	h, ok := findHost(c, s.Host)
	if !ok {
		return r, fmt.Errorf("host %q is not configured", s.Host)
	}
	if err := validHost(h); err != nil {
		return r, invalidHostError{fmt.Errorf("host %q: %w", h.Name, err)}
	}
	if err := validMode(s.StartMode); err != nil {
		return r, err
	}
	if !filepath.IsAbs(r.LocalPath) {
		return r, fmt.Errorf("local_path %q is not an absolute path", s.LocalPath)
	}
	r.Host = h
	if FilesMode(h) != FilesShared {
		r.RemotePath = workdirPath(h, r.LocalPath, s.ID)
		return r, nil
	}
	rp, err := remotePath(h, r.LocalPath)
	if err != nil {
		return r, err
	}
	r.RemotePath = rp
	return r, nil
}

// invalidHostError is resolve's error for a configured host that breaks
// validHost; Problems lists those per host, not per session.
type invalidHostError struct{ err error }

func (e invalidHostError) Error() string { return e.err.Error() }
func (e invalidHostError) Unwrap() error { return e.err }

func countID(c *config.Config, id string) int {
	n := 0
	for _, s := range c.Sessions {
		if s.ID == id {
			n++
		}
	}
	return n
}

// Problems checks hand-edited hosts and sessions against the rules the
// dialogs enforce. Problems that keep a session from working also make
// resolve fail (its Remote tab shows why); the others only show up here:
// the app then uses a default (color mode), skips the value (a bad link) or
// the later duplicate.
func Problems(c *config.Config) []string {
	var out []string
	seenHost := map[string]bool{}
	for _, h := range c.Hosts {
		if seenHost[h.Name] {
			out = append(out, fmt.Sprintf("host %q is defined twice; the second one is ignored", h.Name))
			continue
		}
		seenHost[h.Name] = true
		if err := validHost(h); err != nil {
			out = append(out, fmt.Sprintf("host %q: %v", h.Name, err))
		}
	}
	seenGroup := map[string]bool{}
	for _, g := range c.Groups {
		switch {
		case strings.TrimSpace(g) == "":
			out = append(out, "groups has an empty folder name")
		case seenGroup[g]:
			out = append(out, fmt.Sprintf("folder %q is listed twice in groups", g))
		}
		seenGroup[g] = true
	}
	type dirHost struct{ dir, host string }
	seenDir := map[dirHost]bool{}
	for _, s := range c.Sessions {
		label := s.Name
		if label == "" {
			label = s.ID
		}
		var hostErr invalidHostError
		if _, err := resolve(c, s); err != nil && !errors.As(err, &hostErr) {
			out = append(out, fmt.Sprintf("session %q: %v", label, err)) // invalid hosts are listed above
		}
		if s.Group != "" && !seenGroup[s.Group] {
			out = append(out, fmt.Sprintf("session %q: folder %q is not in groups; it is shown as (missing)", label, s.Group))
		}
		if strings.TrimSpace(s.Name) == "" {
			out = append(out, fmt.Sprintf("session %q has no name", s.ID))
		}
		if err := validColorMode(s.ColorMode); err != nil {
			out = append(out, fmt.Sprintf("session %q: %v; the global setting is used", label, err))
		}
		for _, l := range s.Links {
			if err := forge.ValidLink(forge.Link{Name: l.Name, URL: l.URL}); err != nil {
				out = append(out, fmt.Sprintf("session %q: link: %v; it is not shown", label, err))
			}
		}
		k := dirHost{filepath.Clean(ExpandHome(s.LocalPath)), s.Host}
		if seenDir[k] {
			out = append(out, fmt.Sprintf("session %q: %s already has a session on host %q", label, s.LocalPath, s.Host))
		}
		seenDir[k] = true
	}
	return out
}

var nonSlug = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// workdirPath is where a "sync" or "sshfs" session lives on the agent host.
// It uses the directory name and the session id, so renaming the session
// does not move it.
func workdirPath(h config.Host, local, id string) string {
	wd := strings.TrimRight(h.RemoteWorkdir, "/")
	if wd == "" {
		wd = "~/sw"
	}
	base := strings.Trim(nonSlug.ReplaceAllString(filepath.Base(local), "-"), "-.")
	if base == "" {
		base = "dir"
	}
	return wd + "/" + base + "-" + id
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
		ColorMode: s.ColorMode, Group: s.Group, Files: FilesMode(r.Host), RemotePath: r.RemotePath,
		TmuxName: remote.TmuxName(s.ID)}, nil
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

// SetLinks replaces a session's own web links (http/https only).
func (m *Manager) SetLinks(id string, links []config.Link) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range links {
		links[i].Name, links[i].URL = strings.TrimSpace(links[i].Name), strings.TrimSpace(links[i].URL)
		if err := forge.ValidLink(forge.Link{Name: links[i].Name, URL: links[i].URL}); err != nil {
			return err
		}
	}
	return m.update(func(c *config.Config) error {
		i := indexOf(c, id)
		if i < 0 {
			return fmt.Errorf("unknown session %q", id)
		}
		c.Sessions[i].Links = slices.Clone(links)
		return nil
	})
}

// Links returns a session's own web links, leaving out hand-edited ones
// that are not http(s) (Problems reports them).
func (m *Manager) Links(id string) []config.Link {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []config.Link
	if i := m.indexOf(id); i >= 0 {
		for _, l := range m.cfg.Sessions[i].Links {
			if forge.ValidLink(forge.Link{Name: l.Name, URL: l.URL}) == nil {
				out = append(out, l)
			}
		}
	}
	return out
}

// TrustGit records that the user reviewed the repository's hooks and config
// as they are now (fingerprint from gitstatus.RunAudit).
func (m *Manager) TrustGit(id, fingerprint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.update(func(c *config.Config) error {
		i := indexOf(c, id)
		if i < 0 {
			return fmt.Errorf("unknown session %q", id)
		}
		c.Sessions[i].TrustedGit = fingerprint
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
	ui.TabOrder, ui.HiddenTabs = config.NormalizedTabs(ui)
	if ui.AutoReconnect == nil || ui.ActivityBadges == nil || ui.ClaudeBackup == nil || ui.Notifications == nil || ui.CtrlCopyPaste == nil {
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
	h.RemoteWorkdir = strings.TrimSpace(h.RemoteWorkdir)
	if h.Files == FilesShared {
		h.Files = "" // the default; keep the file tidy
	}
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
	switch h.Files {
	case "", FilesShared:
	case FilesSync, FilesSSHFS:
		if h.LocalPrefix != "" || h.RemotePrefix != "" {
			return fmt.Errorf("path prefixes are only used with shared files, not %q", h.Files)
		}
		if h.RemoteWorkdir != "" {
			if err := remote.CheckRemotePath(h.RemoteWorkdir); err != nil {
				return fmt.Errorf("remote working folder %q must be absolute or start with ~/", h.RemoteWorkdir)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown file access mode %q (shared, sync or sshfs)", h.Files)
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
