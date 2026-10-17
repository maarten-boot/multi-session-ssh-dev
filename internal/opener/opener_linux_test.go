package opener

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeXdgOpen puts an xdg-open on PATH that logs its argument.
func fakeXdgOpen(t *testing.T, exit int) (log string) {
	t.Helper()
	bin := t.TempDir()
	log = filepath.Join(t.TempDir(), "log")
	script := "#!/bin/sh\necho \"$1\" >>" + log + "\necho 'no application for this file' >&2\nexit " + string(rune('0'+exit)) + "\n"
	os.WriteFile(filepath.Join(bin, "xdg-open"), []byte(script), 0o755)
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "") // force the Reveal fallback
	return log
}

func waitLog(t *testing.T, log string) string {
	t.Helper()
	for i := 0; i < 50; i++ {
		if b, err := os.ReadFile(log); err == nil {
			return strings.TrimSpace(string(b))
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("xdg-open not called")
	return ""
}

func TestOpenAndRevealUseXdgOpen(t *testing.T) {
	log := fakeXdgOpen(t, 0)
	dir := t.TempDir()
	f := write(t, dir, "it's a file.txt", []byte("x"), 0o644)
	if err := Open(f); err != nil {
		t.Fatal(err)
	}
	if got := waitLog(t, log); got != f {
		t.Errorf("opened %q, want %q", got, f)
	}
	os.Remove(log)
	if err := Reveal(f); err != nil {
		t.Fatal(err)
	}
	if got := waitLog(t, log); got != dir {
		t.Errorf("revealed %q, want parent %q", got, dir)
	}
}

func TestOpenReportsLauncherFailure(t *testing.T) {
	fakeXdgOpen(t, 3)
	f := write(t, t.TempDir(), "a.xyz", []byte("x"), 0o644)
	err := Open(f)
	if err == nil || !strings.Contains(err.Error(), "no application") {
		t.Errorf("err = %v", err)
	}
}

func TestOpenRefusesBeforeLaunching(t *testing.T) {
	log := fakeXdgOpen(t, 0)
	f := write(t, t.TempDir(), "tool", []byte("\x7fELF"), 0o755)
	if err := Open(f); !errors.Is(err, ErrExecutable) {
		t.Errorf("err = %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(log); err == nil {
		t.Error("xdg-open was called for a binary")
	}
}

// Open launches the checked target of a symlink, so the launcher cannot
// pick an application by the link's name.
func TestOpenLaunchesSymlinkTarget(t *testing.T) {
	log := fakeXdgOpen(t, 0)
	dir := t.TempDir()
	real := write(t, dir, "real.md", []byte("x"), 0o644)
	link := filepath.Join(dir, "link.txt")
	os.Symlink("real.md", link)
	if err := Open(link); err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.EvalSymlinks(real); waitLog(t, log) != want {
		t.Errorf("opened the link, want its target %q", want)
	}
}
