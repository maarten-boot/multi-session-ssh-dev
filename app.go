package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"session-workbench/internal/backup"
	"session-workbench/internal/config"
	"session-workbench/internal/forge"
	"session-workbench/internal/fsbrowse"
	"session-workbench/internal/gitstatus"
	"session-workbench/internal/instance"
	"session-workbench/internal/notes"
	"session-workbench/internal/opener"
	"session-workbench/internal/remote"
	"session-workbench/internal/session"
	"session-workbench/internal/share"
	"session-workbench/internal/syncer"
	"session-workbench/internal/terminal"
	"session-workbench/internal/winstate"
	"session-workbench/internal/xfer"
)

// App is the object bound to the frontend; its exported methods become
// JS functions under wailsjs/go/main/App.
type App struct {
	ctx      context.Context
	terms    *terminal.Manager
	sessions *session.Manager
	shares   *share.Manager
	notes    notes.Store
	backups  *backup.Runner
	dataDir  string // config.DataDir: Claude backups
	notify   notifier
	// running is set when another instance holds the config's lock: this
	// one then only shows that (InstanceProblem) and starts nothing.
	running error
}

func NewApp(sessions *session.Manager, notesDir, dataDir string) *App {
	return &App{sessions: sessions, notes: notes.Store{Dir: notesDir}, dataDir: dataDir}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// SIGTERM (logout, kill) and Ctrl+C close the window the normal way, so
	// its size is saved and connections are dropped (beforeClose, shutdown).
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		runtime.Quit(ctx)
	}()
	if a.running != nil {
		return
	}
	gitstatus.Configure = hideConsole
	syncer.Configure = hideConsole
	share.Configure = hideConsole
	forge.Configure = hideConsole
	a.shares = share.NewManager(func(id string, st share.Status) {
		runtime.EventsEmit(ctx, "share:"+id, st)
	})
	// claude may still be running in tmux on the agent host from an earlier
	// run: give it its files back without waiting for the Remote tab.
	for _, v := range a.sessions.State().Sessions {
		if v.Files == session.FilesSSHFS {
			go a.ensureShare(v.ID, 0)
		}
	}
	a.terms = terminal.NewManager(terminal.EmitterFunc(func(name string, data ...any) {
		runtime.EventsEmit(ctx, name, data...)
	}))
	backup.Configure = hideConsole
	xfer.Configure = hideConsole
	a.backups = &backup.Runner{
		Dir: filepath.Join(a.dataDir, "claude-backups"),
		Hosts: func() []backup.Host {
			var hs []backup.Host
			for _, h := range a.sessions.Config().Hosts {
				hs = append(hs, backup.Host{Name: h.Name, Target: h.SSHTarget})
			}
			return hs
		},
		Every: func() (time.Duration, bool) {
			ui := a.sessions.Config().UI
			d, err := config.BackupEvery(ui)
			return d, err == nil && ui.ClaudeBackup != nil && *ui.ClaudeBackup
		},
		Changed: func(host string, s backup.Status) { runtime.EventsEmit(ctx, "backup", host, s) },
	}
	if a.dataDir != "" {
		a.backups.Start(ctx)
	}
	a.notify.start(ctx)
}

// shutdown drops all connections; remote tmux sessions keep running.
func (a *App) shutdown(ctx context.Context) {
	if a.running != nil {
		return
	}
	a.terms.KillAll()
	a.shares.StopAll() // the agent host unmounts
	a.notify.stop(ctx)
}

// beforeClose saves the window's size for the next start; it never stops
// the window from closing.
func (a *App) beforeClose(ctx context.Context) bool {
	if a.running != nil || a.dataDir == "" {
		return false
	}
	w, h := runtime.WindowGetSize(ctx)
	normal := !runtime.WindowIsFullscreen(ctx) && !runtime.WindowIsMinimised(ctx)
	if err := winstate.Closing(a.dataDir, w, h, runtime.WindowIsMaximised(ctx), normal); err != nil {
		fmt.Fprintln(os.Stderr, "warning: window size not saved:", err)
	}
	return false
}

// InstanceProblem says why this instance does nothing (another one uses the
// same config); empty normally. The frontend asks before anything else.
func (a *App) InstanceProblem() string {
	if a.running == nil {
		return ""
	}
	return fmt.Sprintf("Session Workbench is already running with this configuration%s. "+
		"Use that window; this one does nothing and can be closed.",
		strings.TrimPrefix(a.running.Error(), instance.ErrRunning.Error()))
}

// secondInstance brings this window forward when the app is started again.
func (a *App) secondInstance(options.SecondInstanceData) {
	if a.ctx == nil {
		return
	}
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
}

// --- Claude backups (internal/backup) ---

// BackupStatus is the last backup per host name.
func (a *App) BackupStatus() map[string]backup.Status { return a.backups.Status() }

// BackupNow backs one host up and waits for it.
func (a *App) BackupNow(host string) error {
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Minute)
	defer cancel()
	return a.backups.Now(ctx, host)
}

// RestoreBackup copies a host's backup to it, keeping files that exist there.
func (a *App) RestoreBackup(host string) error {
	for _, h := range a.backups.Hosts() {
		if h.Name == host {
			ctx, cancel := context.WithTimeout(a.ctx, 30*time.Minute)
			defer cancel()
			return backup.Restore(ctx, h, a.backups.HostDir(host))
		}
	}
	return fmt.Errorf("unknown host %q", host)
}

// OpenBackupFolder shows a host's backup in the file manager.
func (a *App) OpenBackupFolder(host string) error {
	dir := a.backups.HostDir(host)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("there is no backup for %s yet", host)
	}
	return opener.Open(dir)
}

// --- sessions and hosts ---

// Version is the app version (set at build time, see the Makefile).
func (a *App) Version() string {
	return version
}

func (a *App) State() session.State {
	return a.sessions.State()
}

func (a *App) PreviewSession(in session.NewSession) session.Preview {
	return a.sessions.Preview(in)
}

func (a *App) CreateSession(in session.NewSession) (session.View, error) {
	return a.sessions.Create(in)
}

func (a *App) UpdateSession(id string, e session.Edit) error {
	return a.sessions.Update(id, e)
}

func (a *App) SetSessionGroup(id, group string) error {
	return a.sessions.SetGroup(id, group)
}

func (a *App) SaveGroup(oldName, name string) error {
	return a.sessions.SaveGroup(oldName, name)
}

func (a *App) DeleteGroup(name string) error {
	return a.sessions.DeleteGroup(name)
}

func (a *App) MoveGroup(name, beforeName string) error {
	return a.sessions.MoveGroup(name, beforeName)
}

func (a *App) SaveUI(ui config.UI) error {
	return a.sessions.SaveUI(ui)
}

func (a *App) DeleteSession(id string) error {
	a.shares.Stop(id)
	return a.sessions.Delete(id)
}

func (a *App) MoveSession(id, beforeID string) error {
	return a.sessions.Move(id, beforeID)
}

func (a *App) SaveHost(oldName string, h config.Host) error {
	return a.sessions.SaveHost(oldName, h)
}

func (a *App) DeleteHost(name string) error {
	return a.sessions.DeleteHost(name)
}

// BrowseDirectory shows the native directory picker; "" means cancelled.
func (a *App) BrowseDirectory(start string) (string, error) {
	start = session.ExpandHome(start)
	if fi, err := os.Stat(start); err != nil || !fi.IsDir() {
		start, _ = os.UserHomeDir()
	}
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                "Choose session directory",
		DefaultDirectory:     start,
		CanCreateDirectories: true,
	})
}

// --- files ---

// localRoot returns the session's local directory; Files works even when
// the session's remote side is misconfigured.
func (a *App) localRoot(id string) (session.Resolved, error) {
	r, err := a.sessions.Resolve(id)
	if r.ID == "" {
		return r, err
	}
	return r, nil
}

func (a *App) ListDir(id, rel string, showHidden bool) (fsbrowse.Listing, error) {
	r, err := a.localRoot(id)
	if err != nil {
		return fsbrowse.Listing{}, err
	}
	return fsbrowse.List(r.LocalPath, rel, showHidden)
}

// GitStatus returns git markers for the Files tab. Outside a repository,
// or without git, Repo is false.
func (a *App) GitStatus(id string) (gitstatus.Status, error) {
	r, err := a.localRoot(id)
	if err != nil {
		return gitstatus.Status{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	return gitstatus.Get(ctx, r.LocalPath)
}

// WebInfo is what the Web tab shows besides the runs.
type WebInfo struct {
	Repo   *forge.Repo   `json:"repo"`   // nil without a recognisable git remote
	Links  []forge.Link  `json:"links"`  // derived from the remote
	Custom []config.Link `json:"custom"` // the session's own links
	GH     bool          `json:"gh"`     // the GitHub CLI is installed
}

func (a *App) WebInfo(id string) (WebInfo, error) {
	r, err := a.localRoot(id)
	if err != nil {
		return WebInfo{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	w := WebInfo{Links: []forge.Link{}, Custom: a.sessions.Links(id), GH: forge.HaveGH()}
	if w.Custom == nil {
		w.Custom = []config.Link{}
	}
	if repo, ok := forge.Parse(gitstatus.RemoteURL(ctx, r.LocalPath)); ok {
		w.Repo = &repo
		w.Links = repo.Links()
	}
	return w, nil
}

// WebRuns lists the latest GitHub Actions runs of the session's repository.
func (a *App) WebRuns(id string) ([]forge.Run, error) {
	w, err := a.WebInfo(id)
	if err != nil {
		return nil, err
	}
	if w.Repo == nil {
		return nil, forge.ErrNotGitHb
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	return forge.Runs(ctx, *w.Repo, 12)
}

// SetLinks stores the session's own web links.
func (a *App) SetLinks(id string, links []config.Link) error {
	return a.sessions.SetLinks(id, links)
}

// LoadNotes returns a session's notes (empty if none yet).
func (a *App) LoadNotes(id string) (notes.Note, error) {
	return a.notes.Load(id)
}

// SaveNotes stores a session's notes.
func (a *App) SaveNotes(id, text string) (notes.Note, error) {
	return a.notes.Save(id, text)
}

// RepoCheck lists hooks and risky repository config that git on this
// machine would run (see docs/CONNECTIVITY.md).
func (a *App) RepoCheck(id string) (gitstatus.Audit, error) {
	r, err := a.localRoot(id)
	if err != nil {
		return gitstatus.Audit{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	return gitstatus.RunAudit(ctx, r.LocalPath)
}

// TrustRepo marks the repository's current hooks and config as reviewed.
func (a *App) TrustRepo(id, fingerprint string) error {
	return a.sessions.TrustGit(id, fingerprint)
}

// syncSession returns a "sync" (working copy) session's sync parameters.
func (a *App) syncSession(id string) (syncer.Session, error) {
	r, err := a.sessions.Resolve(id)
	if err != nil {
		return syncer.Session{}, err
	}
	if session.FilesMode(r.Host) != session.FilesSync {
		return syncer.Session{}, fmt.Errorf("host %q does not use working copies", r.Host.Name)
	}
	return syncer.Session{ID: r.ID, Target: r.Host.SSHTarget, LocalPath: r.LocalPath, RemotePath: r.RemotePath}, nil
}

// SyncPreview shows what "Send to agent" would change (writes nothing).
func (a *App) SyncPreview(id string) (syncer.Plan, error) {
	s, err := a.syncSession(id)
	if err != nil {
		return syncer.Plan{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Minute)
	defer cancel()
	return syncer.Send(ctx, s, true, false)
}

// SyncSend mirrors the session directory to the agent copy.
func (a *App) SyncSend(id string, overwriteDirty bool) (syncer.Plan, error) {
	s, err := a.syncSession(id)
	if err != nil {
		return syncer.Plan{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Minute)
	defer cancel()
	return syncer.Send(ctx, s, false, overwriteDirty)
}

// SyncGet fetches the agent copy's commits.
func (a *App) SyncGet(id string) (syncer.Fetched, error) {
	s, err := a.syncSession(id)
	if err != nil {
		return syncer.Fetched{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Minute)
	defer cancel()
	return syncer.Get(ctx, s)
}

// SyncFastForward moves the current branch to the agent's version.
func (a *App) SyncFastForward(id, agentRef string) error {
	s, err := a.syncSession(id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	defer cancel()
	return syncer.FastForward(ctx, s, agentRef)
}

// OpenPath opens a file with its default application (a directory in the
// file manager); executables are refused.
func (a *App) OpenPath(id, rel string) error {
	p, err := a.nativePath(id, rel)
	if err != nil {
		return err
	}
	return opener.Open(p)
}

// RevealPath shows the item in the OS file manager.
func (a *App) RevealPath(id, rel string) error {
	p, err := a.nativePath(id, rel)
	if err != nil {
		return err
	}
	return opener.Reveal(p)
}

func (a *App) nativePath(id, rel string) (string, error) {
	r, err := a.localRoot(id)
	if err != nil {
		return "", err
	}
	return fsbrowse.ResolveInside(r.LocalPath, rel) // open/reveal: no symlinks out of the directory
}

// Paths is an item's path on both machines, for "copy path".
type Paths struct {
	Local  string `json:"local"`
	Remote string `json:"remote"` // empty if the session has no valid mapping
}

func (a *App) ItemPaths(id, rel string) (Paths, error) {
	r, err := a.localRoot(id)
	if err != nil {
		return Paths{}, err
	}
	local, err := fsbrowse.Resolve(r.LocalPath, rel)
	if err != nil {
		return Paths{}, err
	}
	p := Paths{Local: local}
	if r.RemotePath != "" {
		p.Remote = path.Join(r.RemotePath, path.Clean("/"+rel))
	}
	return p, nil
}

// --- terminals ---

// StartLocal starts the local shell in the session directory.
func (a *App) StartLocal(id string, cols, rows int) (string, error) {
	r, err := a.sessions.Resolve(id)
	if r.ID == "" {
		return "", err // unknown session; other resolve errors only affect Remote
	}
	cfg := a.sessions.Config()
	sh, args := terminal.DefaultShell()
	if cfg.Local.Shell != "" {
		sh, args = cfg.Local.Shell, cfg.Local.Args
	}
	var env []string
	for k, v := range r.Env {
		env = append(env, k+"="+v)
	}
	tid, err := a.terms.Start(terminal.Spec{Command: sh, Args: args, Dir: r.LocalPath, Env: env, Cols: cols, Rows: rows})
	if err == nil && r.LocalInit != "" {
		// The pty buffers this until the shell reads its first line.
		a.terms.Write(tid, []byte(r.LocalInit+"\r"))
	}
	return tid, err
}

// ensureShare starts a live share and waits up to timeout for the mount.
func (a *App) ensureShare(id string, timeout time.Duration) error {
	r, err := a.sessions.Resolve(id)
	if err != nil {
		return err
	}
	return a.shares.Ensure(id, share.Spec{Target: r.Host.SSHTarget, LocalPath: r.LocalPath, RemotePath: r.RemotePath}, timeout)
}

// ShareStatus reports a live share's state ("stopped" if none).
func (a *App) ShareStatus(id string) share.Status {
	return a.shares.Status(id)
}

// StopShare unmounts a live share until the Remote tab connects again.
func (a *App) StopShare(id string) {
	a.shares.Stop(id)
}

// StartRemote connects to (or creates) the session's tmux session.
func (a *App) StartRemote(id string, cols, rows int) (string, error) {
	r, err := a.sessions.Resolve(id)
	if err != nil {
		return "", err
	}
	if session.FilesMode(r.Host) == session.FilesSSHFS {
		if err := a.ensureShare(id, 25*time.Second); err != nil {
			return "", fmt.Errorf("sharing the directory: %w", err)
		}
	}
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		return "", fmt.Errorf("ssh client not found: %w", err)
	}
	args, err := remote.AttachArgs(r.Host.SSHTarget, r.ID, r.RemotePath, remote.Mode(r.StartMode))
	if err != nil {
		return "", err
	}
	return a.terms.Start(terminal.Spec{Command: ssh, Args: args, Cols: cols, Rows: rows})
}

// KillRemote ends the session's tmux session (and whatever runs in it).
// A session that does not exist counts as success.
func (a *App) KillRemote(id string) error {
	r, err := a.sessions.Resolve(id)
	if r.ID == "" {
		return err
	}
	args, err := remote.KillArgs(r.Host.SSHTarget, r.ID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", args...)
	hideConsole(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "can't find session") || strings.Contains(msg, "no server running") {
			return nil
		}
		return fmt.Errorf("ending remote session: %v: %s", err, msg)
	}
	return nil
}

func (a *App) TermWrite(id, data string) error {
	return a.terms.Write(id, []byte(data))
}

func (a *App) TermResize(id string, cols, rows int) error {
	return a.terms.Resize(id, cols, rows)
}

func (a *App) TermKill(id string) error {
	return a.terms.Kill(id)
}
