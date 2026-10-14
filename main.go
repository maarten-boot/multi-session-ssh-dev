package main

import (
	"embed"
	"flag"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"session-workbench/internal/remote"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	var opts Options
	var mode string
	fs := flag.NewFlagSet("session-workbench", flag.ExitOnError)
	fs.StringVar(&opts.Dir, "dir", "", "session directory on this machine (default: home directory)")
	fs.StringVar(&opts.SSHTarget, "ssh", "", "agent host as ssh target, e.g. user@host; enables the Remote tab")
	fs.StringVar(&opts.LocalPrefix, "local-prefix", "", "local directory that the agent host sees under -remote-prefix")
	fs.StringVar(&opts.RemotePrefix, "remote-prefix", "", "agent-host path of -local-prefix (default: same paths on both sides)")
	fs.StringVar(&mode, "mode", string(remote.ModeClaude), "what a new remote tmux session runs: claude, claude-continue or shell")
	fs.Parse(os.Args[1:])
	opts.Mode = remote.Mode(mode)
	if (opts.LocalPrefix == "") != (opts.RemotePrefix == "") {
		fmt.Fprintln(os.Stderr, "-local-prefix and -remote-prefix must be given together")
		os.Exit(2)
	}

	app := NewApp(opts)

	err := wails.Run(&options.App{
		Title:  "Session Workbench",
		Width:  1200,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 30, G: 30, B: 30, A: 1},
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
