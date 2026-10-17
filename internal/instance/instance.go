// Package instance keeps a second copy of the app from using the same config
// file: two instances would overwrite each other's config and both talk to
// the same tmux sessions. The lock is an OS file lock, so it disappears with
// the process, even after a crash.
package instance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrRunning means another process holds the lock.
var ErrRunning = errors.New("already running")

// Lock is a held instance lock.
type Lock struct{ f *os.File }

// Acquire locks path (created if needed, with its directory) without
// waiting. When another process holds it, the error wraps ErrRunning and
// names that process if it can.
func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		f.Close()
		if !errors.Is(err, errLocked) {
			return nil, err
		}
		who := ""
		if b, err := os.ReadFile(path); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				who = fmt.Sprintf(" (process %d)", pid)
			}
		}
		return nil, fmt.Errorf("%w%s", ErrRunning, who)
	}
	// The pid is informational, for the message above.
	f.Truncate(0)
	f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	return &Lock{f: f}, nil
}

// Release gives the lock up (it also goes when the process exits).
func (l *Lock) Release() {
	if l != nil && l.f != nil {
		unlockFile(l.f)
		l.f.Close()
		l.f = nil
	}
}

// ID returns a short name for the config at path, the same for every way of
// writing the path, for single-instance mechanisms that need a name.
func ID(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return "session-workbench-" + hex.EncodeToString(sum[:6])
}
