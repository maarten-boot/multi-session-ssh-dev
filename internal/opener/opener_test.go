package opener

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func write(t *testing.T, dir, name string, data []byte, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	text := write(t, dir, "notes.md", []byte("# hi"), 0o644)
	script := write(t, dir, "build.sh", []byte("#!/bin/sh\necho hi\n"), 0o755)
	elf := write(t, dir, "tool", []byte("\x7fELF\x02\x01\x01"), 0o755)
	pe := write(t, dir, "data.bin", []byte("MZ\x90\x00"), 0o644) // a PE without .exe
	macho := write(t, dir, "prog", []byte{0xcf, 0xfa, 0xed, 0xfe, 7}, 0o644)
	bat := write(t, dir, "Run.BAT", []byte("echo"), 0o644)
	js := write(t, dir, "app.js", []byte("console.log(1)"), 0o644)
	desktop := write(t, dir, "x.desktop", []byte("[Desktop Entry]"), 0o644)
	command := write(t, dir, "go.command", []byte("echo"), 0o644)
	empty := write(t, dir, "empty", nil, 0o644)
	app := filepath.Join(dir, "Thing.app")
	os.Mkdir(app, 0o755)
	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0o755)

	cases := []struct {
		goos, path string
		refused    bool
	}{
		{"linux", text, false}, {"darwin", text, false}, {"windows", text, false},
		{"linux", sub, false}, {"darwin", sub, false}, {"windows", sub, false},
		{"linux", empty, false},
		// Executable scripts: xdg-open shows them; macOS open runs them.
		{"linux", script, false}, {"darwin", script, true},
		// Native binaries are refused everywhere, whatever their name.
		{"linux", elf, true}, {"windows", elf, true},
		{"linux", pe, true}, {"windows", pe, true},
		{"darwin", macho, true}, {"linux", macho, true},
		// Extensions, case-insensitively, per OS.
		{"windows", bat, true}, {"linux", bat, false},
		{"windows", js, true}, {"linux", js, false}, {"darwin", js, false},
		{"linux", desktop, true}, {"freebsd", desktop, true}, {"windows", desktop, false},
		{"darwin", command, true}, {"linux", command, false},
		{"darwin", app, true}, {"linux", app, false},
	}
	for _, c := range cases {
		if runtime.GOOS == "windows" && c.path == script && c.goos == "darwin" {
			continue // the exec bit cannot be set on Windows
		}
		err := Check(c.goos, c.path)
		if refused := errors.Is(err, ErrExecutable); refused != c.refused || (err != nil && !refused) {
			t.Errorf("Check(%s, %s) = %v, want refused=%v", c.goos, filepath.Base(c.path), err, c.refused)
		}
	}
	if err := Check("linux", filepath.Join(dir, "missing")); err == nil || errors.Is(err, ErrExecutable) {
		t.Errorf("missing file: %v", err)
	}
}

// A symlink is judged by its target too: macOS open and xdg-open launch an
// .app or .desktop behind an innocent name.
func TestCheckSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	app := filepath.Join(dir, "Evil.app")
	os.Mkdir(app, 0o755)
	desktop := write(t, dir, "x.desktop", []byte("[Desktop Entry]"), 0o644)
	text := write(t, dir, "real.md", []byte("# hi"), 0o644)
	docs, notes, good := filepath.Join(dir, "docs"), filepath.Join(dir, "notes.txt"), filepath.Join(dir, "good.md")
	os.Symlink(app+"/", docs)
	os.Symlink("x.desktop", notes)
	os.Symlink(text, good)
	if err := Check("linux", desktop); !errors.Is(err, ErrExecutable) {
		t.Fatalf("the target itself: %v", err)
	}
	for _, c := range []struct {
		goos, path string
		refused    bool
	}{
		{"darwin", docs, true}, {"linux", notes, true}, {"linux", good, false}, {"darwin", good, false},
	} {
		err := Check(c.goos, c.path)
		if refused := errors.Is(err, ErrExecutable); refused != c.refused || (err != nil && !refused) {
			t.Errorf("Check(%s, %s) = %v, want refused=%v", c.goos, filepath.Base(c.path), err, c.refused)
		}
	}
}

func TestCheckWindowsScriptTypes(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.py", "a.PYW", "a.pyz", "a.pyzw", "a.chm", "a.ws", "a.wsc", "a.sct", "a.vb", "a.xll"} {
		p := write(t, dir, n, []byte("x"), 0o644)
		if err := Check("windows", p); !errors.Is(err, ErrExecutable) {
			t.Errorf("Check(windows, %s) = %v", n, err)
		}
	}
}

// Opening a FIFO must neither hang (reading its first bytes blocks) nor
// hand it to a launcher.
func TestCheckFIFO(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no mkfifo")
	}
	fifo := filepath.Join(t.TempDir(), "pipe.txt")
	if out, err := exec.Command("mkfifo", fifo).CombinedOutput(); err != nil {
		t.Skipf("mkfifo: %v %s", err, out)
	}
	done := make(chan error, 1)
	go func() { done <- Check("linux", fifo) }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrExecutable) {
			t.Errorf("FIFO: %v", err)
		}
	case <-time.After(5 * time.Second):
		os.Remove(fifo)
		t.Fatal("Check hangs on a FIFO")
	}
}
