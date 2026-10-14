package main

import (
	"context"
	"os"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"session-workbench/internal/terminal"
)

// App is the object bound to the frontend; its exported methods become
// JS functions under wailsjs/go/main/App.
type App struct {
	ctx   context.Context
	terms *terminal.Manager
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.terms = terminal.NewManager(terminal.EmitterFunc(func(name string, data ...any) {
		runtime.EventsEmit(ctx, name, data...)
	}))
}

func (a *App) shutdown(ctx context.Context) {
	a.terms.KillAll()
}

// StartLocalShell starts the user's shell in their home directory.
func (a *App) StartLocalShell(cols, rows int) (string, error) {
	sh, args := terminal.DefaultShell()
	home, _ := os.UserHomeDir()
	return a.terms.Start(terminal.Spec{Command: sh, Args: args, Dir: home, Cols: cols, Rows: rows})
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
