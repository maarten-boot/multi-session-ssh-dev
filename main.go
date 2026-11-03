package main

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"

	"session-workbench/internal/config"
	"session-workbench/internal/instance"
	"session-workbench/internal/profile"
	"session-workbench/internal/session"
	"session-workbench/internal/winstate"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var icon []byte

// version is set with -ldflags "-X main.version=..." by the Makefile and CI.
var version = "dev"

func main() {
	fs := flag.NewFlagSet("session-workbench", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file (default <config dir>/session-workbench/config.toml, or the profile's folder)")
	prof := fs.String("profile", "", "a separate setup: its own folders instead of session-workbench, "+
		"its own tmux sessions and containers (also "+profile.EnvVar+"); letters, digits, - and _")
	showVersion := fs.Bool("version", false, "print the version and exit")
	attach := fs.String("container-attach", "", "(used by the app) connect a Remote tab to a session's container")
	fs.Parse(os.Args[1:])
	name := *prof
	if name == "" {
		name = os.Getenv(profile.EnvVar)
	}
	if err := profile.Set(name); err != nil {
		fmt.Fprintln(os.Stderr, "session-workbench:", err)
		os.Exit(2)
	}
	if *showVersion {
		fmt.Println("session-workbench", version)
		return
	}
	if *cfgPath == "" {
		p, err := config.DefaultPath()
		if err != nil {
			p = "config.toml"
		}
		*cfgPath = p
	}
	if *attach != "" {
		os.Exit(runContainerAttach(*attach))
	}

	// One instance per config file: a second one would overwrite the
	// config. A second start brings the first window forward (D-Bus, a
	// Windows mutex, macOS notifications); where that is not available the
	// lock file catches it and the second instance only says so.
	lock, lockErr := instance.Acquire(*cfgPath + ".lock")
	defer lock.Release()

	sessions := session.Open(*cfgPath)
	if err := sessions.LoadError(); err != nil {
		fmt.Fprintln(os.Stderr, "warning:", err)
	}
	// Notes live next to the config file, outside any project directory.
	dataDir, err := config.DataDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: no data directory, Claude backups are off:", err)
	}
	app := NewApp(sessions, filepath.Join(filepath.Dir(*cfgPath), "notes"), dataDir)
	if lockErr != nil {
		if !errors.Is(lockErr, instance.ErrRunning) {
			fmt.Fprintln(os.Stderr, "warning: instance lock:", lockErr) // e.g. read-only dir: run anyway
		} else {
			app.running = lockErr
		}
	}
	bg := &options.RGBA{R: 255, G: 255, B: 255, A: 1}
	if sessions.Config().UI.Theme == "dark" {
		bg = &options.RGBA{R: 30, G: 30, B: 30, A: 1}
	}

	// The size the window had when it was last closed (winstate); the
	// position follows once the window exists (domReady).
	win := winstate.Load(dataDir)
	app.win = win
	start := options.Normal
	if win.Maximised {
		start = options.Maximised
	}

	err = wails.Run(&options.App{
		Title:            profile.Title(),
		Width:            win.Width,
		Height:           win.Height,
		WindowStartState: start,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: bg,
		Menu:             appMenu(),
		Linux: &linux.Options{
			Icon: icon,
			// WM_CLASS, matches the .desktop file (a profile's window is
			// grouped on its own).
			ProgramName: profile.Dir(),
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               instance.ID(*cfgPath),
			OnSecondInstanceLaunch: app.secondInstance,
		},
		OnStartup:     app.startup,
		OnDomReady:    app.domReady,
		OnBeforeClose: app.beforeClose,
		OnShutdown:    app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
