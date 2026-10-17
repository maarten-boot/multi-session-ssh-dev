//go:build windows

package instance

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

var errLocked = errors.New("locked")

// lockFile locks a byte far beyond the pid text, so other processes can still
// read the pid while the lock is held.
func lockFile(f *os.File) error {
	ol := &windows.Overlapped{Offset: 1 << 30}
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return errLocked
	}
	return err
}

func unlockFile(f *os.File) {
	windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{Offset: 1 << 30})
}
