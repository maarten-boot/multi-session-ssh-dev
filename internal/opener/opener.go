// Package opener opens files with the OS default application and reveals
// them in the file manager. It never knowingly executes anything: files the
// OS would run instead of open are refused (SPEC 6).
package opener

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var ErrExecutable = errors.New("refusing to open")

// Open opens path (a file, or a directory in the file manager) with the
// default application.
func Open(path string) error {
	if err := Check(runtime.GOOS, path); err != nil {
		return err
	}
	return openPlatform(path)
}

// Reveal shows path in the OS file manager, selected where supported.
func Reveal(path string) error {
	if _, err := os.Lstat(path); err != nil {
		return err
	}
	return revealPlatform(path)
}

// Executable extensions per OS: opening these runs code.
var execExt = map[string][]string{
	"windows": {".exe", ".com", ".bat", ".cmd", ".ps1", ".psm1", ".psd1", ".vbs", ".vbe", ".js", ".jse",
		".wsf", ".wsh", ".msi", ".msp", ".msc", ".scr", ".pif", ".lnk", ".hta", ".cpl", ".jar", ".reg",
		".inf", ".scf", ".url", ".application", ".appref-ms", ".gadget", ".settingcontent-ms", ".library-ms", ".appx", ".msix"},
	"darwin": {".app", ".command", ".tool", ".terminal", ".workflow", ".action", ".pkg", ".mpkg", ".jar", ".webloc", ".inetloc", ".fileloc"},
	"linux":  {".desktop", ".appimage", ".run", ".jar"},
}

// Check returns an ErrExecutable error if opening path on goos would run it.
func Check(goos, path string) error {
	refuse := func(why string) error {
		return fmt.Errorf("%w %s: %s", ErrExecutable, filepath.Base(path), why)
	}
	ext := strings.ToLower(filepath.Ext(path))
	list := execExt[goos]
	if list == nil {
		list = execExt["linux"] // other Unix desktops behave like Linux
	}
	for _, e := range list {
		if ext == e {
			return refuse("this file type is executed, not opened")
		}
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return nil // opens the file manager (".app" bundles caught above)
	}
	if goos == "darwin" && fi.Mode()&0o111 != 0 {
		return refuse("macOS runs executable files in Terminal")
	}
	if isBinaryExecutable(path) {
		return refuse("it is a program")
	}
	return nil
}

var magics = [][]byte{
	{0x7f, 'E', 'L', 'F'},    // ELF
	{'M', 'Z'},               // PE / DOS
	{0xfe, 0xed, 0xfa, 0xce}, // Mach-O 32
	{0xfe, 0xed, 0xfa, 0xcf}, // Mach-O 64
	{0xce, 0xfa, 0xed, 0xfe}, // Mach-O 32, little endian
	{0xcf, 0xfa, 0xed, 0xfe}, // Mach-O 64, little endian
	{0xca, 0xfe, 0xba, 0xbe}, // Mach-O universal (also Java .class, harmless to refuse)
}

func isBinaryExecutable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4)
	n, _ := io.ReadFull(f, head)
	for _, m := range magics {
		if n >= len(m) && bytes.Equal(head[:len(m)], m) {
			return true
		}
	}
	return false
}

// start runs a launcher command and reports its failure if it exits with
// an error quickly (e.g. "no application registered"); launchers that keep
// running are left alone.
func start(cmd *exec.Cmd) error {
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			msg := strings.TrimSpace(out.String())
			if msg == "" {
				msg = err.Error()
			}
			return fmt.Errorf("%s: %s", filepath.Base(cmd.Path), msg)
		}
	case <-time.After(3 * time.Second):
	}
	return nil
}
