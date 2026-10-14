package opener

import (
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

func openPlatform(path string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}

func revealPlatform(path string) error {
	// explorer wants /select,"C:\path" verbatim; Go's argument quoting
	// would quote the whole switch, so build the command line by hand.
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + filepath.Clean(path) + `"`}
	// explorer exits with status 1 even on success; don't report it.
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
