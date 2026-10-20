//go:build !windows

package terminal

import (
	"os"
	"syscall"
)

// hangup sends SIGHUP, as closing a terminal window does.
func hangup(p *os.Process) error { return p.Signal(syscall.SIGHUP) }
