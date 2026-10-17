package instance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquireExcludesSecond(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml.lock")
	l, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Acquire(path)
	if !errors.Is(err, ErrRunning) {
		t.Fatalf("second Acquire: got %v, want ErrRunning", err)
	}
	if want := fmt.Sprintf("process %d", os.Getpid()); !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name the holder (%s)", err, want)
	}
	l.Release()
	l2, err := Acquire(path)
	if err != nil {
		t.Fatalf("after Release: %v", err)
	}
	l2.Release()
}

func TestIDStable(t *testing.T) {
	dir := t.TempDir()
	a := ID(filepath.Join(dir, "config.toml"))
	b := ID(filepath.Join(dir, ".", "x", "..", "config.toml"))
	if a != b {
		t.Errorf("same file, different ids: %s %s", a, b)
	}
	if a == ID(filepath.Join(dir, "other.toml")) {
		t.Error("different files, same id")
	}
	if !strings.HasPrefix(a, "session-workbench-") {
		t.Errorf("id %q", a)
	}
}
