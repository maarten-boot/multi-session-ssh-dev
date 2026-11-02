package main

import (
	"os"
	"os/exec"
	"syscall"
)

// hideConsole keeps a console program (ssh.exe) started from this GUI app
// from flashing a console window.
func hideConsole(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// execInPlace runs program in this console and exits with its status
// (Windows cannot replace a process).
func execInPlace(program string, args []string) error {
	cmd := exec.Command(program, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	if x, ok := err.(*exec.ExitError); ok {
		os.Exit(x.ExitCode())
	}
	if err == nil {
		os.Exit(0)
	}
	return err
}
