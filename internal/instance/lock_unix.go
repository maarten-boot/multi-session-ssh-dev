//go:build !windows

package instance

import (
	"errors"
	"os"
	"syscall"
)

var errLocked = errors.New("locked")

// lockFile takes an exclusive flock. flock locks belong to the open file, so
// a second open of the same file conflicts even within one process.
func lockFile(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return errLocked
	}
	return err
}

func unlockFile(f *os.File) { syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
