//go:build !windows

package terminal

import "os"

// DefaultShell returns the user's login shell.
func DefaultShell() (string, []string) {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	return sh, []string{"-l"}
}
