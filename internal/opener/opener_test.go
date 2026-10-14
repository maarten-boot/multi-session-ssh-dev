package opener

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
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
		err := Check(c.goos, c.path)
		if refused := errors.Is(err, ErrExecutable); refused != c.refused || (err != nil && !refused) {
			t.Errorf("Check(%s, %s) = %v, want refused=%v", c.goos, filepath.Base(c.path), err, c.refused)
		}
	}
	if err := Check("linux", filepath.Join(dir, "missing")); err == nil || errors.Is(err, ErrExecutable) {
		t.Errorf("missing file: %v", err)
	}
}
