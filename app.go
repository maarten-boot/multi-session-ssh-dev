package main

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"session-workbench/internal/pathmap"
	"session-workbench/internal/remote"
	"session-workbench/internal/terminal"
)

// Options describe the single session the app shows until sessions and
// config arrive (M3). They come from command-line flags.
type Options struct {
	SSHTarget    string // empty: no Remote tab
	LocalPrefix  string // empty with RemotePrefix empty: same path on both sides
	RemotePrefix string
	Dir          string
	Mode         remote.Mode
}

// SessionInfo is what the frontend shows about the session.
type SessionInfo struct {
	Name       string `json:"name"`
	Target     string `json:"target"`
	LocalPath  string `json:"localPath"`
	RemotePath string `json:"remotePath"`
	TmuxName   string `json:"tmuxName"`
	HasRemote  bool   `json:"hasRemote"`
	Error      string `json:"error"`
}

// App is the object bound to the frontend; its exported methods become
// JS functions under wailsjs/go/main/App.
type App struct {
	ctx   context.Context
	terms *terminal.Manager
	opts  Options
	info  SessionInfo
	id    string
}

func NewApp(opts Options) *App {
	a := &App{opts: opts}
	a.resolve()
	return a
}

// resolve fills in info; problems are reported in info.Error so the window
// can show them instead of the app failing to start.
func (a *App) resolve() {
	dir := a.opts.Dir
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	a.info = SessionInfo{Name: filepath.Base(dir), LocalPath: dir, Target: a.opts.SSHTarget}
	if a.opts.SSHTarget == "" {
		return
	}
	remotePath := filepath.ToSlash(dir)
	if a.opts.LocalPrefix != "" || a.opts.RemotePrefix != "" {
		p, err := pathmap.ToRemote(dir, a.opts.LocalPrefix, a.opts.RemotePrefix)
		if err != nil {
			a.info.Error = err.Error()
			return
		}
		remotePath = p
	}
	a.id = sessionID(dir)
	a.info.RemotePath = remotePath
	a.info.TmuxName = remote.TmuxName(a.id)
	a.info.HasRemote = true
}

var nonID = regexp.MustCompile(`[^a-z0-9]+`)

// sessionID derives a stable tmux-safe id from the local path, so that
// restarting the app on the same directory re-attaches.
func sessionID(dir string) string {
	base := strings.Trim(nonID.ReplaceAllString(strings.ToLower(filepath.Base(dir)), "-"), "-")
	if len(base) > 40 {
		base = strings.TrimRight(base[:40], "-")
	}
	h := fnv.New32a()
	h.Write([]byte(dir))
	if base == "" {
		return fmt.Sprintf("%08x", h.Sum32())
	}
	return fmt.Sprintf("%s-%04x", base, h.Sum32()&0xffff)
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

func (a *App) Session() SessionInfo {
	return a.info
}

// StartLocal starts the user's shell in the session directory.
func (a *App) StartLocal(cols, rows int) (string, error) {
	sh, args := terminal.DefaultShell()
	return a.terms.Start(terminal.Spec{Command: sh, Args: args, Dir: a.info.LocalPath, Cols: cols, Rows: rows})
}

// StartRemote connects to (or creates) the session's tmux session.
func (a *App) StartRemote(cols, rows int) (string, error) {
	if !a.info.HasRemote {
		return "", fmt.Errorf("no remote configured: %s", a.info.Error)
	}
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		return "", fmt.Errorf("ssh client not found: %w", err)
	}
	args, err := remote.AttachArgs(a.opts.SSHTarget, a.id, a.info.RemotePath, a.opts.Mode)
	if err != nil {
		return "", err
	}
	return a.terms.Start(terminal.Spec{Command: ssh, Args: args, Cols: cols, Rows: rows})
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
