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
	"runtime"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"session-workbench/internal/config"
	"session-workbench/internal/examples"
	"session-workbench/internal/forge"
	"session-workbench/internal/idle"
	"session-workbench/internal/pathmap"
	"session-workbench/internal/profile"
	"session-workbench/internal/remote"
)

// View is a session as shown to the frontend, with derived fields.
type View struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Host        string `json:"host"`
	LocalPath   string `json:"local_path"`
	StartMode   string `json:"start_mode"`
	ColorMode   string `json:"color_mode"` // "" = follow the global setting
	Group       string `json:"group"`
	TrustedGit  string `json:"trusted_git"`
	Files       string `json:"files"` // the host's file access mode; "container" for a container host
	RemotePath  string `json:"remote_path"`
	TmuxName    string `json:"tmux_name"`
	Record      string `json:"record"`    // "on", "off", "" = follow the global setting
	History     string `json:"history"`   // "on", "off", "" = follow private_history
	IdleStop    string `json:"idle_stop"` // a duration, "off", "" = follow idle_stop
	// OwnClaudeVolume: a container session's own ~/.claude volume.
	OwnClaudeVolume bool `json:"own_claude_volume"`
	// Commands is the session's own command menu (before the global one).
	Commands []config.Command `json:"commands"`
	// Plain: a login without a local directory (no Local and Files tabs);
	// RemoteDir is where its shell starts ("" = home).
	Plain     bool   `json:"plain"`
	RemoteDir string `json:"remote_dir"`
	// Tmux: the Remote tab runs in tmux (else a plain ssh login that ends
	// with the connection); TmuxOwn: the session's own setting (null:
	// the host's).
	Tmux    bool  `json:"tmux"`
	TmuxOwn *bool `json:"tmux_own"`
	// Inactive: kept for later; nothing connects (config Session.Inactive).
	Inactive bool `json:"inactive"`
	// Claude: its start command runs claude in tmux (Stop claude now).
	Claude bool `json:"claude"`
	// Hosts: the other agent hosts it has worked on, the most recent first.
	Hosts []string `json:"hosts"`
	Error string   `json:"error"` // why the Remote tab cannot work, if it cannot
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
	// StartCommand is what the start mode runs ("" is a login shell).
	StartCommand string
	// Plain: a login without a local directory (LocalPath is "").
	Plain bool
	// Tmux: the Remote tab runs in tmux (config.UsesTmux).
	Tmux bool
}

// ErrNoLocalDir: a plain login session has no local directory.
var ErrNoLocalDir = errors.New("this session is a plain login: it has no local directory")

// ErrInactive: the session is deactivated; nothing connects until it is
// activated again.
var ErrInactive = errors.New("this session is inactive (session menu → Activate session)")

// Dir is the session's local directory, or ErrNoLocalDir for a plain
// login (so nothing runs in the app's own folder by mistake).
func (r Resolved) Dir() (string, error) {
	if r.Plain || r.LocalPath == "" {
		return "", ErrNoLocalDir
	}
	return r.LocalPath, nil
}

// LogPath is the path recordings and the Q&A log are kept under (with the
// host's name, see reclog.Dir): the local directory, or logins/<id> for a
// plain login.
func (r Resolved) LogPath() string {
	if r.Plain {
		return "logins/" + r.ID
	}
	return r.LocalPath
}

// NewSession is the input for Create.
type NewSession struct {
	Host        string `json:"host"`
	LocalPath   string `json:"local_path"`
	Name        string `json:"name"`
	Description string `json:"description"`
	StartMode   string `json:"start_mode"`
	ColorMode   string `json:"color_mode"` // "" = follow the global setting
	Group       string `json:"group"`
	// LocalDir: null follows the host (local_dir); false is a plain login
	// (no directory), starting in RemoteDir ("" = home).
	LocalDir  *bool  `json:"local_dir"`
	RemoteDir string `json:"remote_dir"`
	// Tmux: null follows the host's tmux.
	Tmux *bool `json:"tmux"`
}

// Edit is the input for Update.
type Edit struct {
	Name string `json:"name"`
	// Description replaces the session's description; null (left out)
	// keeps it.
	Description *string `json:"description"`
	StartMode   string  `json:"start_mode"`
	ColorMode   string  `json:"color_mode"`
	Group       string  `json:"group"`
	Record      string  `json:"record"`
	History     string  `json:"history"` // "on", "off", "" = follow private_history
	// IdleStop: a duration, "off", "" = follow idle_stop; null (left out)
	// keeps it.
	IdleStop *string `json:"idle_stop"`
	// OwnClaudeVolume: a container session's own ~/.claude volume; null
	// (left out) keeps it.
	OwnClaudeVolume *bool `json:"own_claude_volume"`
	// Commands replaces the session's own command menu; null (left out)
	// keeps it, [] empties it.
	Commands []config.Command `json:"commands"`
	// RemoteDir: a plain login's start folder; null (left out) keeps it.
	RemoteDir *string `json:"remote_dir"`
	// Tmux: "host" follows the host, "yes" / "no" override it; "" (left
	// out) keeps it.
	Tmux string `json:"tmux"`
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
	fresh   bool // there was no config file: a first start
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
	// A first start gets the example commands (saved with the first
	// change). An existing file is never added to, also not an empty list.
	if _, err := os.Stat(path); m.loadErr == nil && errors.Is(err, os.ErrNotExist) {
		m.fresh = true
		if cs, err := examples.Commands(); err == nil {
			m.cfg.UI.Commands = cs
		}
	}
	return m
}

func (m *Manager) LoadError() error { return m.loadErr }

// Fresh: the app started without a config file (a first start).
func (m *Manager) Fresh() bool { return m.fresh }

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
		v := View{ID: se.ID, Name: se.Name, Description: se.Description, Host: se.Host, LocalPath: se.LocalPath, StartMode: se.StartMode,
			ColorMode: se.ColorMode, Group: se.Group, TrustedGit: se.TrustedGit, TmuxName: remote.TmuxName(se.ID), Record: se.Record,
			History: se.History, IdleStop: se.IdleStop, Commands: se.Commands, OwnClaudeVolume: se.OwnClaudeVolume, RemoteDir: se.RemoteDir,
			Inactive: se.Inactive, Hosts: se.Hosts}
		v.TmuxOwn = se.Tmux
		if h, ok := findHost(m.cfg, se.Host); ok {
			v.Plain = !config.HasLocalDir(h, se)
			v.Tmux = config.UsesTmux(h, se)
		}
		if r, err := resolve(m.cfg, se); err != nil {
			v.Error = err.Error()
		} else {
			v.RemotePath = r.RemotePath
			v.Claude = r.Tmux && idle.Runs(r.StartCommand)
			v.Files = viewFiles(r.Host)
			if r.Plain {
				v.Files = FilesLogin
			}
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
	// FilesLogin is shown for a plain login session: no files at all.
	FilesLogin = "login"
)

// viewFiles is the file mode the frontend shows: FilesMode, or
// "container" for a container host (the directory is mounted).
func viewFiles(h config.Host) string {
	if h.Kind == HostContainer {
		return HostContainer
	}
	return FilesMode(h)
}

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
	start, ok := config.FindStartCommand(c.UI, s.StartMode)
	if !ok {
		return r, fmt.Errorf("unknown start mode %q (not in [ui] start_commands)", s.StartMode)
	}
	r.StartCommand = start.Command
	r.Tmux = config.UsesTmux(h, s)
	if !config.HasLocalDir(h, s) {
		// A plain login: no directory here, no mapping; home or remote_dir.
		if h.Kind == HostContainer {
			return r, errors.New("a container session needs its local directory (it is mounted)")
		}
		r.Host, r.Plain, r.LocalPath = h, true, ""
		r.RemotePath = s.RemoteDir
		if r.RemotePath == "" {
			r.RemotePath = "~"
		}
		if err := remote.CheckRemotePath(r.RemotePath); err != nil {
			return r, fmt.Errorf("remote_dir %q must be absolute or start with ~/", s.RemoteDir)
		}
		return r, nil
	}
	if !filepath.IsAbs(r.LocalPath) {
		return r, fmt.Errorf("local_path %q is not an absolute path", s.LocalPath)
	}
	r.Host = h
	if h.Kind == HostContainer {
		// Its own container: the ssh alias of internal/container, the
		// directory mounted at the same path.
		r.Host.SSHTarget = "sw-" + profile.Tag("-") + s.ID // container.Name
		r.RemotePath = filepath.ToSlash(r.LocalPath)
		return r, nil
	}
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
	if err := config.ValidateCommands(c.UI.Commands); err != nil {
		out = append(out, fmt.Sprintf("[ui] commands: %v", err))
	}
	if err := config.ValidateStartCommands(c.UI.StartCommands); err != nil {
		out = append(out, fmt.Sprintf("[ui] start_commands: %v", err))
	}
	seenGroup := map[string]bool{}
	for _, g := range c.Groups {
		switch {
		case strings.TrimSpace(g) == "":
			out = append(out, "groups has an empty folder name")
		case seenGroup[g]:
			out = append(out, fmt.Sprintf("folder %q is listed twice in groups", g))
		default:
			if p, err := folderPath(g); err != nil {
				out = append(out, err.Error())
			} else if p != g {
				out = append(out, fmt.Sprintf("folder %q has spaces around a /", g))
			}
		}
		seenGroup[g] = true
	}
	for _, g := range c.Groups {
		if p, err := folderPath(g); err != nil || p != g {
			continue // reported above
		}
		if p := parentFolder(g); p != "" && !seenGroup[p] {
			out = append(out, fmt.Sprintf("folder %q: %q is not in groups; it is shown as (missing)", g, p))
		}
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
		if err := validRecord(s.Record); err != nil {
			out = append(out, fmt.Sprintf("session %q: %v; the global setting is used", label, err))
		}
		if err := validOnOff("history", s.History); err != nil {
			out = append(out, fmt.Sprintf("session %q: %v; the global setting is used", label, err))
		}
		if _, err := config.ParseIdleStop(s.IdleStop); err != nil {
			out = append(out, fmt.Sprintf("session %q: %v; claude is not stopped", label, err))
		}
		if err := config.ValidateCommands(s.Commands); err != nil {
			out = append(out, fmt.Sprintf("session %q: commands: %v", label, err))
		}
		if _, err := tidyDescription(s.Description); err != nil {
			out = append(out, fmt.Sprintf("session %q: %v; Edit… cannot save it until it is shorter", label, err))
		}
		for _, l := range s.Links {
			if err := forge.ValidLink(forge.Link{Name: l.Name, URL: l.URL}); err != nil {
				out = append(out, fmt.Sprintf("session %q: link: %v; it is not shown", label, err))
			}
		}
		k := dirHost{filepath.Clean(ExpandHome(s.LocalPath)), s.Host}
		if s.LocalPath == "" {
			continue // a plain login: no directory to share
		}
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
	v := View{ID: s.ID, Name: s.Name, Description: s.Description, Host: s.Host, LocalPath: s.LocalPath, StartMode: s.StartMode,
		ColorMode: s.ColorMode, Group: s.Group, Files: viewFiles(r.Host), RemotePath: r.RemotePath,
		TmuxName: remote.TmuxName(s.ID), Plain: r.Plain, RemoteDir: s.RemoteDir, Tmux: r.Tmux, TmuxOwn: s.Tmux,
		Claude: r.Tmux && idle.Runs(r.StartCommand)}
	if r.Plain {
		v.Files = FilesLogin
	}
	return v, nil
}

func (m *Manager) newSession(in NewSession) (config.Session, error) {
	s := config.Session{Host: in.Host, Name: strings.TrimSpace(in.Name), StartMode: in.StartMode,
		ColorMode: in.ColorMode, Group: in.Group}
	if s.StartMode == "" {
		s.StartMode = m.cfg.UI.StartCommands[0].Name
		if _, ok := config.FindStartCommand(m.cfg.UI, "claude"); ok {
			s.StartMode = "claude"
		}
	}
	h, ok := findHost(m.cfg, s.Host)
	if !ok {
		return s, fmt.Errorf("host %q is not configured", s.Host)
	}
	// Stored only when it differs from the host's default.
	if in.LocalDir != nil && *in.LocalDir != config.HasLocalDir(h, config.Session{}) {
		v := *in.LocalDir
		s.LocalDir = &v
	}
	if in.Tmux != nil && *in.Tmux != config.UsesTmux(h, config.Session{}) {
		v := *in.Tmux
		s.Tmux = &v
	}
	if !config.HasLocalDir(h, s) {
		return m.newLogin(s, h, in)
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
	d, err := tidyDescription(in.Description)
	if err != nil {
		return s, err
	}
	s.Description = d
	if _, ok := config.FindStartCommand(m.cfg.UI, s.StartMode); !ok {
		return s, fmt.Errorf("unknown start mode %q", s.StartMode)
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

// newLogin finishes a plain login session: no directory; the shell starts
// in RemoteDir (absolute or ~/…, typed as is) or the login's home; the name
// defaults to the host's ("host 2", "host 3" for more).
func (m *Manager) newLogin(s config.Session, h config.Host, in NewSession) (config.Session, error) {
	if h.Kind == HostContainer {
		return s, errors.New("a container session needs a local directory (it is mounted)")
	}
	s.RemoteDir = strings.TrimSpace(in.RemoteDir)
	if s.RemoteDir != "" {
		if err := remote.CheckRemotePath(s.RemoteDir); err != nil {
			return s, fmt.Errorf("the remote folder must be absolute or start with ~/ (empty: the home folder)")
		}
	}
	if s.Name == "" {
		s.Name = h.Name
		for n := 2; slices.ContainsFunc(m.cfg.Sessions, func(o config.Session) bool { return o.Name == s.Name }); n++ {
			s.Name = fmt.Sprintf("%s %d", h.Name, n)
		}
	}
	d, err := tidyDescription(in.Description)
	if err != nil {
		return s, err
	}
	s.Description = d
	if _, ok := config.FindStartCommand(m.cfg.UI, s.StartMode); !ok {
		return s, fmt.Errorf("unknown start mode %q", s.StartMode)
	}
	if err := validColorMode(s.ColorMode); err != nil {
		return s, err
	}
	if s.Group != "" && !slices.Contains(m.cfg.Groups, s.Group) {
		return s, fmt.Errorf("folder %q does not exist", s.Group)
	}
	s.ID = m.newID()
	return s, nil
}

func validRecord(r string) error {
	if r != "" && r != "on" && r != "off" {
		return fmt.Errorf(`record must be "on", "off" or left out, not %q`, r)
	}
	return nil
}

func validOnOff(key, v string) error {
	if v != "" && v != "on" && v != "off" {
		return fmt.Errorf(`%s must be "on", "off" or left out, not %q`, key, v)
	}
	return nil
}

// PrivateHistory reports whether a session's shells keep their own history:
// its own "on"/"off", else the global setting.
func PrivateHistory(s config.Session, ui config.UI) bool {
	switch s.History {
	case "on":
		return true
	case "off":
		return false
	}
	return ui.PrivateHistory == nil || *ui.PrivateHistory
}

// Records reports whether a session's Remote tab is to be recorded: its
// own "on"/"off", else the global setting.
func Records(s config.Session, ui config.UI) bool {
	switch s.Record {
	case "on":
		return true
	case "off":
		return false
	}
	return ui.RecordRemote
}

// MaxDescription is the longest description, in characters.
const MaxDescription = 2000

// tidyDescription trims d and its line ends (CRLF becomes LF).
func tidyDescription(d string) (string, error) {
	lines := strings.Split(strings.ReplaceAll(d, "\r\n", "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	d = strings.TrimSpace(strings.Join(lines, "\n"))
	if n := utf8.RuneCountInString(d); n > MaxDescription {
		return "", fmt.Errorf("the description has %d characters: at most %d", n, MaxDescription)
	}
	return d, nil
}

func validColorMode(mode string) error {
	if mode != "" && mode != "full" && mode != "mono" {
		return fmt.Errorf("unknown color mode %q", mode)
	}
	return nil
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
	if err := validColorMode(e.ColorMode); err != nil {
		return err
	}
	if err := validRecord(e.Record); err != nil {
		return err
	}
	if err := validOnOff("history", e.History); err != nil {
		return err
	}
	if e.IdleStop != nil {
		if _, err := config.ParseIdleStop(*e.IdleStop); err != nil {
			return err
		}
	}
	if err := config.ValidateCommands(e.Commands); err != nil {
		return err
	}
	var desc string
	if e.Description != nil {
		var err error
		if desc, err = tidyDescription(*e.Description); err != nil {
			return err
		}
	}
	return m.update(func(c *config.Config) error {
		i := indexOf(c, id)
		if i < 0 {
			return fmt.Errorf("unknown session %q", id)
		}
		// Checked only when it changes: a session whose start command was
		// removed can still get a new name, colors and so on.
		if e.StartMode != c.Sessions[i].StartMode {
			if _, ok := config.FindStartCommand(c.UI, e.StartMode); !ok {
				return fmt.Errorf("unknown start mode %q", e.StartMode)
			}
		}
		if e.Group != "" && !slices.Contains(c.Groups, e.Group) {
			return fmt.Errorf("folder %q does not exist", e.Group)
		}
		c.Sessions[i].Name = name
		if e.Description != nil { // left out: unchanged
			c.Sessions[i].Description = desc
		}
		c.Sessions[i].StartMode = e.StartMode
		c.Sessions[i].ColorMode = e.ColorMode
		c.Sessions[i].Group = e.Group
		c.Sessions[i].Record = e.Record
		c.Sessions[i].History = e.History
		if e.IdleStop != nil { // left out: unchanged
			c.Sessions[i].IdleStop = *e.IdleStop
		}
		if e.OwnClaudeVolume != nil {
			c.Sessions[i].OwnClaudeVolume = *e.OwnClaudeVolume
		}
		if e.Commands != nil { // left out: unchanged
			c.Sessions[i].Commands = config.TidyCommands(e.Commands)
		}
		switch e.Tmux {
		case "": // left out: unchanged
		case "host":
			c.Sessions[i].Tmux = nil
		case "yes", "no":
			v := e.Tmux == "yes"
			c.Sessions[i].Tmux = &v
		default:
			return fmt.Errorf(`tmux must be "host", "yes" or "no", not %q`, e.Tmux)
		}
		if e.RemoteDir != nil { // left out: unchanged
			d := strings.TrimSpace(*e.RemoteDir)
			if d != "" {
				if err := remote.CheckRemotePath(d); err != nil {
					return fmt.Errorf("the remote folder must be absolute or start with ~/ (empty: the home folder)")
				}
			}
			c.Sessions[i].RemoteDir = d
		}
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

// CanSwitch says why a session cannot move to another agent host, or ""
// (SPEC F16): it needs a local directory, and neither side a container
// host or a plain-login one.
func CanSwitch(h config.Host, s config.Session) string {
	switch {
	case h.Kind == HostContainer:
		return "a container host keeps Claude Code's data in a volume on this computer"
	case !config.HasLocalDir(h, s):
		return "its sessions are plain logins (no project directory)"
	}
	return ""
}

// ResolveOn resolves the session as if host were its agent host (for a
// switch: the project's path there), without changing anything.
func (m *Manager) ResolveOn(id, host string) (Resolved, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := indexOf(m.cfg, id)
	if i < 0 {
		return Resolved{}, fmt.Errorf("unknown session %q", id)
	}
	s := m.cfg.Sessions[i]
	h, ok := findHost(m.cfg, host)
	if !ok {
		return Resolved{}, fmt.Errorf("host %q is not configured", host)
	}
	if why := CanSwitch(h, s); why != "" {
		return Resolved{}, fmt.Errorf("host %s: %s", host, why)
	}
	s.Host = host
	return resolve(m.cfg, s)
}

// SetHost moves a session to another agent host; the old one goes first
// into Hosts (the new one leaves it).
func (m *Manager) SetHost(id, host string) error {
	if _, err := m.ResolveOn(id, host); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.update(func(c *config.Config) error {
		i := indexOf(c, id)
		if i < 0 {
			return fmt.Errorf("unknown session %q", id)
		}
		s := &c.Sessions[i]
		if s.Host == host {
			return nil
		}
		hosts := []string{s.Host}
		for _, h := range s.Hosts {
			if h != host && !slices.Contains(hosts, h) {
				hosts = append(hosts, h)
			}
		}
		s.Host, s.Hosts = host, hosts
		return nil
	})
}

// SetActive activates or deactivates a session (config Session.Inactive).
func (m *Manager) SetActive(id string, active bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.update(func(c *config.Config) error {
		i := indexOf(c, id)
		if i < 0 {
			return fmt.Errorf("unknown session %q", id)
		}
		c.Sessions[i].Inactive = !active
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

// Folders nest by path: "Work/Client A" is the folder Client A inside
// Work. groups lists every folder by its path; siblings are shown in the
// order of the list.

// folderPath tidies a folder path: each part trimmed, none empty.
func folderPath(p string) (string, error) {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
		if parts[i] == "" {
			if len(parts) == 1 {
				return "", errors.New("folder name cannot be empty")
			}
			return "", fmt.Errorf("folder %q has an empty part", p)
		}
	}
	return strings.Join(parts, "/"), nil
}

// parentFolder returns the folder that holds folder p ("" = the top).
func parentFolder(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return ""
}

// inFolder reports whether folder p is root or inside it.
func inFolder(p, root string) bool {
	return p == root || strings.HasPrefix(p, root+"/")
}

// addFolder adds folder p and any missing folders above it, at the end.
func addFolder(c *config.Config, p string) {
	if p == "" || slices.Contains(c.Groups, p) {
		return
	}
	addFolder(c, parentFolder(p))
	c.Groups = append(c.Groups, p)
}

// takeFolder removes folder name and its subfolders from groups and
// returns them in their order.
func takeFolder(c *config.Config, name string) []string {
	var tree []string
	c.Groups = slices.DeleteFunc(c.Groups, func(g string) bool {
		if inFolder(g, name) {
			tree = append(tree, g)
			return true
		}
		return false
	})
	return tree
}

// SaveGroup adds a folder (oldName "") or renames one; renaming moves its
// sessions and subfolders along. A name with / is a path: missing folders
// on the way are added, and a new parent moves the folder there (last).
func (m *Manager) SaveGroup(oldName, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	name, err := folderPath(name)
	if err != nil {
		return err
	}
	return m.update(func(c *config.Config) error {
		if name != oldName && slices.Contains(c.Groups, name) {
			return fmt.Errorf("a folder named %q already exists", name)
		}
		if oldName == "" {
			addFolder(c, name)
			return nil
		}
		i := slices.Index(c.Groups, oldName)
		if i < 0 {
			return fmt.Errorf("unknown folder %q", oldName)
		}
		if name != oldName && inFolder(name, oldName) {
			return fmt.Errorf("folder %q cannot go inside itself", oldName)
		}
		rename := func(g string) string {
			if inFolder(g, oldName) {
				return name + g[len(oldName):]
			}
			return g
		}
		// Its place once it and its subfolders are taken out.
		i -= len(slices.DeleteFunc(slices.Clone(c.Groups[:i]), func(g string) bool { return !inFolder(g, oldName) }))
		tree := takeFolder(c, oldName)
		for k, g := range tree {
			tree[k] = rename(g)
			if slices.Contains(c.Groups, tree[k]) {
				return fmt.Errorf("a folder named %q already exists", tree[k])
			}
		}
		if parentFolder(name) != parentFolder(oldName) { // a rename keeps its place
			addFolder(c, parentFolder(name))
			i = len(c.Groups)
		}
		c.Groups = slices.Insert(c.Groups, i, tree...)
		for j := range c.Sessions {
			c.Sessions[j].Group = rename(c.Sessions[j].Group)
		}
		return nil
	})
}

// DeleteGroup removes a folder and its subfolders; their sessions move to
// the folder above (never deleted).
func (m *Manager) DeleteGroup(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.update(func(c *config.Config) error {
		if !slices.Contains(c.Groups, name) {
			return fmt.Errorf("unknown folder %q", name)
		}
		takeFolder(c, name)
		for j := range c.Sessions {
			if inFolder(c.Sessions[j].Group, name) {
				c.Sessions[j].Group = parentFolder(name)
			}
		}
		return nil
	})
}

// MoveGroup puts folder name, with its subfolders, directly before
// beforeName, or last if "".
func (m *Manager) MoveGroup(name, beforeName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if name == beforeName {
		return nil
	}
	return m.update(func(c *config.Config) error {
		if !slices.Contains(c.Groups, name) {
			return fmt.Errorf("unknown folder %q", name)
		}
		if beforeName != "" && inFolder(beforeName, name) {
			return fmt.Errorf("folder %q cannot go inside itself", name)
		}
		tree := takeFolder(c, name)
		j := len(c.Groups)
		if beforeName != "" {
			if j = slices.Index(c.Groups, beforeName); j < 0 {
				return fmt.Errorf("unknown folder %q", beforeName)
			}
		}
		c.Groups = slices.Insert(c.Groups, j, tree...)
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
	ui.Commands = config.TidyCommands(ui.Commands)
	ui.SpellWords = config.TidyWords(ui.SpellWords)
	if ui.AutoReconnect == nil || ui.ActivityBadges == nil || ui.ClaudeBackup == nil || ui.Notifications == nil || ui.CtrlCopyPaste == nil ||
		ui.PrivateHistory == nil || ui.SpellCheck == nil || ui.SaveAgentCopies == nil || ui.SaveAgentCopiesAtQuit == nil ||
		ui.SavedCopiesDays == nil || ui.ShowCost == nil {
		return errors.New("incomplete UI settings")
	}
	return m.update(func(c *config.Config) error {
		c.UI = ui
		return nil
	})
}

// AddSpellWord adds a word to the personal spelling dictionary.
func (m *Manager) AddSpellWord(word string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := config.TidyWords([]string{word})
	if len(w) == 0 {
		return fmt.Errorf("not a word: %q", word)
	}
	return m.update(func(c *config.Config) error {
		c.UI.SpellWords = config.TidyWords(append(c.UI.SpellWords, w[0]))
		return nil
	})
}

// RemoveSpellWord takes a word out of the personal spelling dictionary
// (exactly as added); a word not in it is no error.
func (m *Manager) RemoveSpellWord(word string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.update(func(c *config.Config) error {
		c.UI.SpellWords = slices.DeleteFunc(c.UI.SpellWords, func(w string) bool { return w == strings.TrimSpace(word) })
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
			for k, o := range c.Sessions[j].Hosts { // the hosts it worked on before
				if o == oldName {
					c.Sessions[j].Hosts[k] = h.Name
				}
			}
			if c.Sessions[j].Host == oldName {
				c.Sessions[j].Host = h.Name
			}
		}
		return nil
	})
}

// HostContainer is the kind of a host whose sessions run in containers.
const HostContainer = "container"

func validHost(h config.Host) error {
	if h.Name == "" {
		return errors.New("host name cannot be empty")
	}
	switch h.Kind {
	case "":
	case HostContainer:
		if runtime.GOOS == "windows" {
			return errors.New("containers as agent hosts need Linux or macOS for now")
		}
		if h.SSHTarget != "" || h.LocalPrefix != "" || h.RemotePrefix != "" || h.Files != "" || h.RemoteWorkdir != "" {
			return errors.New("a container host has no ssh target, path prefixes or file mode (the directory is mounted)")
		}
		if h.LocalDir != nil && !*h.LocalDir {
			return errors.New("a container host's sessions need their local directory (it is mounted)")
		}
		return nil
	default:
		return fmt.Errorf("unknown host kind %q (\"container\", or none for ssh)", h.Kind)
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
