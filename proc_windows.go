package main

import (
	"os/exec"
	"syscall"
)

// hideConsole keeps a console program (ssh.exe) started from this GUI app
// from flashing a console window.
func hideConsole(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
