package main

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"

	"session-workbench/internal/config"
	"session-workbench/internal/session"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var icon []byte

// version is set with -ldflags "-X main.version=..." by the Makefile and CI.
var version = "dev"

func main() {
	defPath, err := config.DefaultPath()
	if err != nil {
		defPath = "config.toml"
	}
	fs := flag.NewFlagSet("session-workbench", flag.ExitOnError)
	cfgPath := fs.String("config", defPath, "config file")
	showVersion := fs.Bool("version", false, "print the version and exit")
	fs.Parse(os.Args[1:])
	if *showVersion {
		fmt.Println("session-workbench", version)
		return
	}

	sessions := session.Open(*cfgPath)
	if err := sessions.LoadError(); err != nil {
		fmt.Fprintln(os.Stderr, "warning:", err)
	}
	// Notes live next to the config file, outside any project directory.
	app := NewApp(sessions, filepath.Join(filepath.Dir(*cfgPath), "notes"))
	bg := &options.RGBA{R: 255, G: 255, B: 255, A: 1}
	if sessions.Config().UI.Theme == "dark" {
		bg = &options.RGBA{R: 30, G: 30, B: 30, A: 1}
	}

	err = wails.Run(&options.App{
		Title:  "Session Workbench",
		Width:  1280,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: bg,
		Menu:             appMenu(),
		Linux: &linux.Options{
			Icon:        icon,
			ProgramName: "session-workbench", // WM_CLASS, matches the .desktop file
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
