//go:build windows

package terminal

import (
	"errors"
	"os"
)

// hangup is not available on Windows: Kill ends the process at once.
func hangup(*os.Process) error { return errors.New("terminal: no hangup on Windows") }
