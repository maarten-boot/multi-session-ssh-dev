package main

// Exporting one session as an archive and importing it as a new session
// (internal/sessionarchive). The parts: notes and the Dockerfile always;
// Q&A, recordings and shell history by default; saved agent copies and the
// project directory when asked. Never keys, the Claude login or window
// state. Import makes a new session (a new id) and never overwrites.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"session-workbench/internal/config"
	"session-workbench/internal/container"
	"session-workbench/internal/qa"
	"session-workbench/internal/remote"
	"session-workbench/internal/session"
	"session-workbench/internal/sessionarchive"
	"session-workbench/internal/xfer"
)

// ExportPart is a part the export dialog offers.
type ExportPart struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Always: in every archive; Default: ticked when the dialog opens.
	Always  bool `json:"always"`
	Default bool `json:"default"`
	// Files and Size of what is on this computer (-1: not known yet, e.g.
	// the Remote history, fetched during the export); Note explains.
	Files int    `json:"files"`
	Size  int64  `json:"size"`
	Note  string `json:"note"`
}

// exportSources are the places of a session's data on this computer.
type exportSources struct {
	r          session.Resolved
	notes      string // notes/<id>/
	qaFile     string // qa/<id>.json
	logDir     string // recordings and the Q&A log
	history    string // the Local tab's history file ("" on Windows)
	copies     string // working-copies/<id>/
	dockerfile string // "" without one
	inRepo     bool
}

func (a *App) exportSources(id string) (exportSources, error) {
	r, err := a.sessions.Resolve(id)
	if r.ID == "" {
		return exportSources{}, err
	}
	s := exportSources{r: r, notes: filepath.Join(a.notes.Dir, id)}
	if st := a.qaStore(); st.Dir != "" {
		s.qaFile = filepath.Join(st.Dir, id+".json")
	}
	s.logDir, _, _ = a.qaLogDir(id)
	if a.dataDir != "" && goruntime.GOOS != "windows" {
		s.history = filepath.Join(a.dataDir, "history", id+".history")
	}
	s.copies, _ = a.savedBase(id)
	if r.Plain {
		return s, nil // a plain login: no directory, no Dockerfile
	}
	spec := container.Spec{ID: id, LocalPath: r.LocalPath}
	if a.dataDir != "" {
		spec.Dir = filepath.Join(a.dataDir, "containers", id)
	}
	if file, _, inRepo := spec.Dockerfile(); file != "" {
		if fi, err := os.Stat(file); err == nil && fi.Mode().IsRegular() {
			s.dockerfile, s.inRepo = file, inRepo
		}
	}
	return s, nil
}

// isQALog: a file of the logs folder that is the Q&A log, not a recording.
func isQALog(rel string) bool {
	return !strings.Contains(rel, "/") && strings.HasPrefix(rel, "qa-") && strings.HasSuffix(rel, ".md")
}

// dirStats counts the regular files under dir that keep accepts.
func dirStats(dir string, keep func(string) bool) (n int, size int64) {
	if dir == "" {
		return 0, 0
	}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if keep != nil && !keep(filepath.ToSlash(rel)) {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			n++
			size += fi.Size()
		}
		return nil
	})
	return n, size
}

func fileStats(file string) (int, int64) {
	if file == "" {
		return 0, 0
	}
	fi, err := os.Stat(file)
	if err != nil || !fi.Mode().IsRegular() {
		return 0, 0
	}
	return 1, fi.Size()
}

// ExportParts lists what an export of the session can hold, with what is
// there now.
func (a *App) ExportParts(id string) ([]ExportPart, error) {
	s, err := a.exportSources(id)
	if err != nil {
		return nil, err
	}
	part := func(id, label string) ExportPart {
		return ExportPart{ID: id, Label: label, Always: slices.Contains(sessionarchive.AlwaysParts, id),
			Default: slices.Contains(sessionarchive.DefaultParts, id)}
	}
	var parts []ExportPart
	p := part(sessionarchive.PartNotes, "Notes")
	p.Files, p.Size = dirStats(s.notes, nil)
	p.Note = "with the session's settings (name, host, commands, links); never keys or logins"
	parts = append(parts, p)
	p = part(sessionarchive.PartDockerfile, "Dockerfile")
	p.Files, p.Size = fileStats(s.dockerfile)
	switch {
	case s.dockerfile == "":
	case s.inRepo:
		p.Note = "the project's .session-workbench/Dockerfile"
	default:
		p.Note = "the app's copy"
	}
	parts = append(parts, p)
	p = part(sessionarchive.PartQA, "Q&A conversation and log")
	p.Files, p.Size = fileStats(s.qaFile)
	n, size := dirStats(s.logDir, isQALog)
	p.Files, p.Size = p.Files+n, p.Size+size
	parts = append(parts, p)
	p = part(sessionarchive.PartRecordings, "Remote tab recordings")
	p.Files, p.Size = dirStats(s.logDir, func(rel string) bool { return !isQALog(rel) })
	parts = append(parts, p)
	p = part(sessionarchive.PartHistory, "Private shell history")
	p.Files, p.Size = fileStats(s.history)
	p.Note = "the Local tab's here; the Remote tab's is fetched from " + s.r.Host.Name + " when it can be reached"
	parts = append(parts, p)
	p = part(sessionarchive.PartCopies, "Saved agent copies")
	p.Files, p.Size = dirStats(s.copies, nil)
	parts = append(parts, p)
	if !s.r.Plain { // a plain login has no project directory
		p = part(sessionarchive.PartProject, "The project directory")
		p.Files, p.Size, p.Note = -1, -1, s.r.LocalPath+": tracked and untracked files and .git, not what .gitignore excludes"
		parts = append(parts, p)
	}
	return parts, nil
}

// ExportSession asks where to save, then writes the archive with the parts
// asked for (the always-parts are added); kind is "zip" or "tar.gz". It
// returns the file written ("" when the save dialog was cancelled) and what
// could not go in.
func (a *App) ExportSession(id string, parts []string, kind string) (ExportResult, error) {
	s, err := a.exportSources(id)
	if err != nil {
		return ExportResult{}, err
	}
	ext := ".zip"
	if kind == "tar.gz" {
		ext = ".tar.gz"
	} else if kind != "zip" {
		return ExportResult{}, fmt.Errorf("unknown archive kind %q", kind)
	}
	name := safeFileName(s.r.Name) + "-" + time.Now().Format("20060102") + ext
	file, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title: "Export “" + s.r.Name + "”", DefaultDirectory: a.archiveDir(), DefaultFilename: name,
		CanCreateDirectories: true,
	})
	if err != nil || file == "" {
		return ExportResult{}, err
	}
	if _, err := sessionarchive.Kind(file); err != nil {
		file += ext
	}
	a.lastArchiveDir = filepath.Dir(file)
	return a.writeExport(s, parts, file)
}

// archiveDir is where the export and import dialogs open: the folder of
// the last archive this run (exported or imported), else the home folder.
func (a *App) archiveDir() string {
	if a.lastArchiveDir != "" {
		if fi, err := os.Stat(a.lastArchiveDir); err == nil && fi.IsDir() {
			return a.lastArchiveDir
		}
	}
	home, _ := os.UserHomeDir()
	return home
}

// ExportResult is what an export wrote.
type ExportResult struct {
	File    string   `json:"file"`
	Missing []string `json:"missing"`
	Skipped int      `json:"skipped"`
}

// writeExport writes the archive to file (through a temporary file next to
// it, so a failed export leaves nothing half-written).
func (a *App) writeExport(s exportSources, parts []string, file string) (ExportResult, error) {
	kind, err := sessionarchive.Kind(file)
	if err != nil {
		return ExportResult{}, err
	}
	want := map[string]bool{}
	for _, p := range append(slices.Clone(sessionarchive.AlwaysParts), parts...) {
		want[p] = true
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".export-*")
	if err != nil {
		return ExportResult{}, err
	}
	defer os.Remove(tmp.Name())
	w, err := sessionarchive.NewWriter(tmp, kind)
	if err != nil {
		tmp.Close()
		return ExportResult{}, err
	}
	m := sessionarchive.Manifest{Format: sessionarchive.Format, App: version, Exported: time.Now().UTC(),
		Session: s.r.Session, Host: s.r.Host}
	m.Host.SSHTarget = a.hostTarget(s.r.Host.Name) // the configured one, not "sw-<id>"
	have := func(p string) { m.Parts = append(m.Parts, p) }
	addErr := func(err error) error {
		if err != nil {
			tmp.Close()
		}
		return err
	}
	// Notes.
	if _, err := w.AddDir("notes", s.notes, nil); addErr(err) != nil {
		return ExportResult{}, fmt.Errorf("notes: %w", err)
	}
	have(sessionarchive.PartNotes)
	// Dockerfile.
	if s.dockerfile != "" {
		if err := w.AddFile("Dockerfile", s.dockerfile); addErr(err) != nil {
			return ExportResult{}, fmt.Errorf("Dockerfile: %w", err)
		}
		m.DockerfileInRepo = s.inRepo
		have(sessionarchive.PartDockerfile)
	}
	if want[sessionarchive.PartQA] {
		if n, _ := fileStats(s.qaFile); n > 0 {
			if err := w.AddFile("qa/conversation.json", s.qaFile); addErr(err) != nil {
				return ExportResult{}, fmt.Errorf("Q&A: %w", err)
			}
		}
		if s.logDir != "" {
			if _, err := w.AddDir("qa/log", s.logDir, isQALog); addErr(err) != nil {
				return ExportResult{}, fmt.Errorf("Q&A log: %w", err)
			}
		}
		have(sessionarchive.PartQA)
	}
	if want[sessionarchive.PartRecordings] {
		if s.logDir != "" {
			if _, err := w.AddDir("recordings", s.logDir, func(rel string) bool { return !isQALog(rel) }); addErr(err) != nil {
				return ExportResult{}, fmt.Errorf("recordings: %w", err)
			}
		}
		have(sessionarchive.PartRecordings)
	}
	if want[sessionarchive.PartHistory] {
		if n, _ := fileStats(s.history); n > 0 {
			if err := w.AddFile("history/local", s.history); addErr(err) != nil {
				return ExportResult{}, fmt.Errorf("history: %w", err)
			}
		}
		if b, why := a.fetchRemoteHistory(s.r); why != "" {
			m.Missing = append(m.Missing, "the Remote tab's shell history: "+why)
		} else if len(b) > 0 {
			if err := w.AddBytes("history/remote", b); addErr(err) != nil {
				return ExportResult{}, fmt.Errorf("history: %w", err)
			}
		}
		have(sessionarchive.PartHistory)
	}
	if want[sessionarchive.PartCopies] {
		if s.copies != "" {
			if _, err := w.AddDir("working-copies", s.copies, nil); addErr(err) != nil {
				return ExportResult{}, fmt.Errorf("saved copies: %w", err)
			}
		}
		have(sessionarchive.PartCopies)
	}
	if want[sessionarchive.PartProject] && !s.r.Plain {
		ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
		files, err := sessionarchive.ProjectFiles(ctx, s.r.LocalPath)
		cancel()
		if err == nil {
			err = w.AddFiles("project", s.r.LocalPath, files)
		}
		if addErr(err) != nil {
			return ExportResult{}, fmt.Errorf("the project directory: %w", err)
		}
		have(sessionarchive.PartProject)
	}
	m.Skipped = w.Skipped
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := w.AddBytes(sessionarchive.ManifestName, mb); addErr(err) != nil {
		return ExportResult{}, err
	}
	if err := w.Close(); addErr(err) != nil {
		return ExportResult{}, err
	}
	if err := tmp.Close(); err != nil {
		return ExportResult{}, err
	}
	os.Chmod(tmp.Name(), 0o600)
	if err := os.Rename(tmp.Name(), file); err != nil {
		return ExportResult{}, err
	}
	return ExportResult{File: file, Missing: m.Missing, Skipped: m.Skipped}, nil
}

// hostTarget is the host's ssh_target as configured.
func (a *App) hostTarget(name string) string {
	for _, h := range a.sessions.Config().Hosts {
		if h.Name == name {
			return h.SSHTarget
		}
	}
	return ""
}

// safeFileName makes a session name usable in a file name.
func safeFileName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < ' ' || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(s))
	if s == "" || strings.Trim(s, ".") == "" {
		return "session"
	}
	return s
}

// remoteHistoryScript prints the session's Remote history (exit 3 when
// there is none).
func remoteHistoryScript(id string) string {
	f := remote.HistoryDir() + "/" + id
	return `f="` + f + `"; [ -f "$f" ] || exit 3; cat "$f"`
}

// fetchRemoteHistory reads the Remote tab's history from the agent host;
// why says why it could not ("" with no data: there is none).
func (a *App) fetchRemoteHistory(r session.Resolved) ([]byte, string) {
	if r.Host.Kind == session.HostContainer && a.containerState(r.ID) != "running" {
		return nil, "the container is not running"
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	out, code, stderr, err := xfer.Run(ctx, r.Host.SSHTarget, remoteHistoryScript(r.ID), nil)
	switch {
	case err != nil:
		return nil, err.Error()
	case code == 3:
		return nil, ""
	case code != 0:
		return nil, fmt.Sprintf("%s could not be reached (%s)", r.Host.Name, firstLine(stderr))
	}
	return out, ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "no reason given"
	}
	return s
}

// --- import ---

// ImportInfo is what the import dialog shows of an archive.
type ImportInfo struct {
	Path     string                  `json:"path"`
	Manifest sessionarchive.Manifest `json:"manifest"`
	// HostExists: a host of the archive's name is configured here.
	HostExists bool `json:"host_exists"`
	// NotImported names settings the archive has that import leaves out
	// (they run commands on this computer, or trust its git hooks).
	NotImported []string `json:"not_imported"`
	// Plain: the session is a plain login (no directory to choose).
	Plain bool `json:"plain"`
}

// ImportOpen asks for an archive and reads its manifest (nil: cancelled).
func (a *App) ImportOpen() (*ImportInfo, error) {
	file, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Import a session", DefaultDirectory: a.archiveDir(),
		Filters: []runtime.FileFilter{{DisplayName: "Session archives (*.zip, *.tar.gz)", Pattern: "*.zip;*.tar.gz;*.tgz"}},
	})
	if err != nil || file == "" {
		return nil, err
	}
	a.lastArchiveDir = filepath.Dir(file)
	return a.importInfo(file)
}

func (a *App) importInfo(file string) (*ImportInfo, error) {
	arc, err := sessionarchive.Open(file)
	if err != nil {
		return nil, err
	}
	m := arc.Manifest
	info := &ImportInfo{Path: file, Manifest: m, Plain: !config.HasLocalDir(m.Host, m.Session)}
	for _, h := range a.sessions.Config().Hosts {
		if h.Name == m.Host.Name {
			info.HostExists = true
		}
	}
	if len(m.Session.Env) > 0 {
		info.NotImported = append(info.NotImported, "env (environment variables)")
	}
	if m.Session.LocalInit != "" {
		info.NotImported = append(info.NotImported, "local_init (a command run in the Local tab)")
	}
	if m.Session.TrustedGit != "" {
		info.NotImported = append(info.NotImported, "trusted_git")
	}
	return info, nil
}

// ImportRequest is the import dialog's answer.
type ImportRequest struct {
	Path string `json:"path"`
	// Host is the host to use; AddHost adds it first from the archive's
	// profile (under that name).
	Host      string `json:"host"`
	AddHost   bool   `json:"add_host"`
	LocalPath string `json:"local_path"`
	Name      string `json:"name"`
}

// ImportResult is the new session and notes on what was done.
type ImportResult struct {
	Session session.View `json:"session"`
	Notes   []string     `json:"notes"`
}

// ImportSession makes a new session from an archive.
func (a *App) ImportSession(req ImportRequest) (ImportResult, error) {
	arc, err := sessionarchive.Open(req.Path)
	if err != nil {
		return ImportResult{}, err
	}
	m := arc.Manifest
	cfg := a.sessions.Config()
	plain := !config.HasLocalDir(m.Host, m.Session)
	local := filepath.Clean(session.ExpandHome(strings.TrimSpace(req.LocalPath)))
	if !plain && !filepath.IsAbs(local) {
		return ImportResult{}, errors.New("choose the session's directory (a full path)")
	}
	// The directory: new or existing; with project files only empty or new.
	made := false
	if plain {
		local = "" // a plain login: no directory
	} else if fi, err := os.Stat(local); err == nil {
		if !fi.IsDir() {
			return ImportResult{}, fmt.Errorf("%s is not a directory", local)
		}
		if m.Has(sessionarchive.PartProject) {
			if es, _ := os.ReadDir(local); len(es) > 0 {
				return ImportResult{}, fmt.Errorf("%s is not empty: the project files go only into an empty or new directory", local)
			}
		}
	} else if errors.Is(err, fs.ErrNotExist) {
		made = true
	} else {
		return ImportResult{}, err
	}
	addedHost := ""
	if req.AddHost {
		h := m.Host
		h.Name = strings.TrimSpace(req.Host)
		if err := a.sessions.SaveHost("", h); err != nil {
			return ImportResult{}, fmt.Errorf("adding the host: %w", err)
		}
		addedHost = h.Name
	}
	undo := func() {
		if made {
			os.RemoveAll(local)
		}
		if addedHost != "" {
			a.sessions.DeleteHost(addedHost)
		}
	}
	if made {
		if err := os.MkdirAll(local, 0o755); err != nil {
			undo()
			return ImportResult{}, err
		}
	}
	var notes []string
	src := m.Session
	in := session.NewSession{Host: strings.TrimSpace(req.Host), LocalPath: local, Name: strings.TrimSpace(req.Name),
		Description: src.Description, ColorMode: src.ColorMode, RemoteDir: src.RemoteDir}
	// What the archive's session was, whatever the chosen host's default.
	hasDir := !plain
	in.LocalDir = &hasDir
	if in.Name == "" {
		in.Name = src.Name
	}
	if _, ok := config.FindStartCommand(cfg.UI, src.StartMode); ok {
		in.StartMode = src.StartMode
	} else if src.StartMode != "" {
		notes = append(notes, fmt.Sprintf("start mode %q is not set up here: the default is used", src.StartMode))
	}
	if slices.Contains(cfg.Groups, src.Group) {
		in.Group = src.Group
	} else if src.Group != "" {
		notes = append(notes, fmt.Sprintf("folder %q does not exist here: the session is outside folders", src.Group))
	}
	v, err := a.sessions.Create(in)
	if err != nil {
		undo()
		return ImportResult{}, err
	}
	own := src.OwnClaudeVolume
	cmds := src.Commands
	if cmds == nil {
		cmds = []config.Command{}
	}
	desc := v.Description
	if err := a.sessions.Update(v.ID, session.Edit{Name: v.Name, Description: &desc, StartMode: v.StartMode,
		ColorMode: v.ColorMode, Group: v.Group, Record: src.Record, History: src.History,
		OwnClaudeVolume: &own, Commands: cmds}); err != nil {
		notes = append(notes, "settings: "+err.Error())
	}
	if len(src.Links) > 0 {
		if err := a.sessions.SetLinks(v.ID, src.Links); err != nil {
			notes = append(notes, "links: "+err.Error())
		}
	}
	if info, _ := a.importInfo(req.Path); info != nil && len(info.NotImported) > 0 {
		notes = append(notes, "not imported: "+strings.Join(info.NotImported, ", "))
	}
	// The files, under the new id; nothing that exists is replaced.
	step := func(what string, err error) {
		if err != nil {
			notes = append(notes, what+": "+err.Error())
		}
	}
	if m.Has(sessionarchive.PartProject) && !plain {
		_, err := arc.Extract("project", local, false)
		step("project files", err)
	}
	if a.notes.Dir != "" {
		_, err := arc.Extract("notes", filepath.Join(a.notes.Dir, v.ID), false)
		step("notes", err)
	}
	if m.Has(sessionarchive.PartQA) {
		step("Q&A", a.importQA(arc, v.ID))
		if dir, _, err := a.qaLogDir(v.ID); err == nil {
			_, err = arc.Extract("qa/log", dir, false)
			step("Q&A log", err)
		}
	}
	if m.Has(sessionarchive.PartRecordings) {
		if dir, _, err := a.qaLogDir(v.ID); err == nil {
			_, err = arc.Extract("recordings", dir, false)
			step("recordings", err)
		}
	}
	if m.Has(sessionarchive.PartHistory) && a.dataDir != "" {
		if goruntime.GOOS != "windows" {
			step("Local history", a.importFile(arc, "history/local", filepath.Join(a.dataDir, "history", v.ID+".history")))
		}
		step("Remote history", a.importFile(arc, "history/remote", a.pendingHistory(v.ID)))
	}
	if m.Has(sessionarchive.PartCopies) {
		if base, err := a.savedBase(v.ID); err == nil {
			_, err = arc.Extract("working-copies", base, false)
			step("saved copies", err)
		}
	}
	if m.Has(sessionarchive.PartDockerfile) && !plain {
		repo := filepath.Join(local, filepath.FromSlash(container.RepoDockerfile))
		_, repoErr := os.Stat(repo)
		r, _ := a.sessions.Resolve(v.ID)
		switch {
		case repoErr == nil:
			// The project brought its own.
		case r.Host.Kind != session.HostContainer:
			notes = append(notes, "the Dockerfile is kept only for container hosts: not imported")
		case a.dataDir != "":
			step("Dockerfile", a.importFile(arc, "Dockerfile", filepath.Join(a.dataDir, "containers", v.ID, "Dockerfile")))
		}
	}
	return ImportResult{Session: v, Notes: notes}, nil
}

// importFile writes one file of the archive to dest (0600), unless the
// archive lacks it or dest exists.
func (a *App) importFile(arc *sessionarchive.Archive, name, dest string) error {
	b, err := arc.ReadFile(name, 1<<30)
	if err != nil || b == nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// importQA stores the archive's conversation for the new session. The
// Claude Code session ids are dropped: they belong to the other computer's
// Claude Code, so the next question starts a new one.
func (a *App) importQA(arc *sessionarchive.Archive, id string) error {
	b, err := arc.ReadFile("qa/conversation.json", 256<<20)
	if err != nil || b == nil {
		return err
	}
	var c qa.Conversation
	if err := json.NewDecoder(bytes.NewReader(b)).Decode(&c); err != nil {
		return err
	}
	for i := range c.Turns {
		c.Turns[i].Session = ""
	}
	return a.qaStore().Save(id, c)
}

// --- the Remote history an import brought ---

// pendingHistory is where an imported Remote history waits for the next
// connection: <data dir>/history/<id>.remote.
func (a *App) pendingHistory(id string) string {
	return filepath.Join(a.dataDir, "history", id+".remote")
}

// pushPendingHistory appends an imported Remote history to the agent
// host's history file, then drops it here. A container may still be
// starting: it is tried again for a while.
func (a *App) pushPendingHistory(r session.Resolved) {
	if a.dataDir == "" {
		return
	}
	file := a.pendingHistory(r.ID)
	if _, err := os.Stat(file); err != nil {
		return
	}
	tries := 1
	if r.Host.Kind == session.HostContainer {
		tries = 60
	}
	for i := 0; i < tries; i++ {
		if i > 0 {
			time.Sleep(5 * time.Second)
		}
		if r.Host.Kind == session.HostContainer && a.containerState(r.ID) != "running" {
			continue
		}
		b, err := os.ReadFile(file)
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		dir := remote.HistoryDir()
		_, code, _, err := xfer.Run(ctx, r.Host.SSHTarget,
			`mkdir -p "`+dir+`" && chmod 700 "`+dir+`" && cat >> "`+dir+"/"+r.ID+`"`, bytes.NewReader(b))
		cancel()
		if err == nil && code == 0 {
			os.Remove(file)
			return
		}
	}
}
