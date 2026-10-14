package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"session-workbench/internal/config"
	"session-workbench/internal/remote"
	"session-workbench/internal/session"
	"session-workbench/internal/terminal"
)

// App is the object bound to the frontend; its exported methods become
// JS functions under wailsjs/go/main/App.
type App struct {
	ctx      context.Context
	terms    *terminal.Manager
	sessions *session.Manager
}

func NewApp(sessions *session.Manager) *App {
	return &App{sessions: sessions}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.terms = terminal.NewManager(terminal.EmitterFunc(func(name string, data ...any) {
		runtime.EventsEmit(ctx, name, data...)
	}))
}

// shutdown drops all connections; remote tmux sessions keep running.
func (a *App) shutdown(ctx context.Context) {
	a.terms.KillAll()
}

// --- sessions and hosts ---

func (a *App) State() session.State {
	return a.sessions.State()
}

func (a *App) PreviewSession(in session.NewSession) session.Preview {
	return a.sessions.Preview(in)
}

func (a *App) CreateSession(in session.NewSession) (session.View, error) {
	return a.sessions.Create(in)
}

func (a *App) UpdateSession(id, name, startMode string) error {
	return a.sessions.Update(id, name, startMode)
}

func (a *App) DeleteSession(id string) error {
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

// StartRemote connects to (or creates) the session's tmux session.
func (a *App) StartRemote(id string, cols, rows int) (string, error) {
	r, err := a.sessions.Resolve(id)
	if err != nil {
		return "", err
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
	out, err := exec.CommandContext(ctx, "ssh", args...).CombinedOutput()
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
