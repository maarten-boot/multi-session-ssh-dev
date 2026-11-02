//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func hideConsole(*exec.Cmd) {}

// execInPlace replaces this process with program (the Remote tab's
// container helper becomes ssh, in the same terminal).
func execInPlace(program string, args []string) error {
	return syscall.Exec(program, append([]string{program}, args...), os.Environ())
}
