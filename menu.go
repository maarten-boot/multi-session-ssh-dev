package main

import (
	"runtime"

	"github.com/wailsapp/wails/v2/pkg/menu"
)

// appMenu is the macOS menu bar: the application menu (About, Hide, Quit
// with Cmd+Q) and the Edit menu, without which Cmd+C/V/X/A do nothing in
// text fields. Terminals copy and paste through the same Edit menu (xterm.js
// handles the copy/paste events). Other platforms get no menu bar.
func appMenu() *menu.Menu {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return menu.NewMenuFromItems(menu.AppMenu(), menu.EditMenu(), menu.WindowMenu())
}
