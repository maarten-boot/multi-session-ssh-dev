package main

import (
	"embed"
	"flag"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"session-workbench/internal/config"
	"session-workbench/internal/session"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	defPath, err := config.DefaultPath()
	if err != nil {
		defPath = "config.toml"
	}
	fs := flag.NewFlagSet("session-workbench", flag.ExitOnError)
	cfgPath := fs.String("config", defPath, "config file")
	fs.Parse(os.Args[1:])

	sessions := session.Open(*cfgPath)
	if err := sessions.LoadError(); err != nil {
		fmt.Fprintln(os.Stderr, "warning:", err)
	}
	app := NewApp(sessions)
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
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
