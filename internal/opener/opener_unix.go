//go:build !windows && !darwin

package opener

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/godbus/dbus/v5"
)

func openPlatform(path string) error {
	return start(exec.Command("xdg-open", path))
}

// revealPlatform asks the desktop's file manager to select the item via
// the freedesktop FileManager1 D-Bus interface (Thunar, Nautilus, Dolphin,
// Nemo, ...) and falls back to opening the containing directory.
func revealPlatform(path string) error {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") != "" {
		if conn, err := dbus.SessionBus(); err == nil {
			uri := (&url.URL{Scheme: "file", Path: path}).String()
			obj := conn.Object("org.freedesktop.FileManager1", "/org/freedesktop/FileManager1")
			if obj.Call("org.freedesktop.FileManager1.ShowItems", 0, []string{uri}, "").Err == nil {
				return nil
			}
		}
	}
	return start(exec.Command("xdg-open", filepath.Dir(path)))
}
