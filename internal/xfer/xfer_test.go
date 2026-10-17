package xfer

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCksumMatchesPOSIX(t *testing.T) {
	if _, err := exec.LookPath("cksum"); err != nil {
		t.Skip("no cksum")
	}
	for _, data := range []string{"", "a", "hello world\n", strings.Repeat("xyz", 100000)} {
		cmd := exec.Command("cksum")
		cmd.Stdin = strings.NewReader(data)
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		crc, n, _ := Cksum(strings.NewReader(data))
		if got := fmt.Sprintf("%d %d", crc, n); !strings.HasPrefix(string(out), got) {
			t.Errorf("Cksum(%d bytes) = %s, cksum says %s", len(data), got, out)
		}
	}
}

// tarOf builds a tar stream from entries.
func tarOf(t *testing.T, entries ...*tar.Header) *bytes.Buffer {
	t.Helper()
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	for _, h := range entries {
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(h.Name))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			tw.Write([]byte(h.Name))
		}
	}
	tw.Close()
	return &b
}

// What the agent host sends cannot reach outside the destination.
func TestUnpackIsConfined(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	dest, outside := t.TempDir(), t.TempDir()
	os.Symlink(outside, filepath.Join(dest, "planted")) // a link already in the way
	root, _ := os.OpenRoot(dest)
	defer root.Close()
	stream := tarOf(t,
		&tar.Header{Name: ".claude/ok.json", Typeflag: tar.TypeReg, Mode: 0o644},
		&tar.Header{Name: "../escape", Typeflag: tar.TypeReg, Mode: 0o644},
		&tar.Header{Name: "/etc/abs", Typeflag: tar.TypeReg, Mode: 0o644},
		&tar.Header{Name: ".claude/out", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
		&tar.Header{Name: ".claude/up", Typeflag: tar.TypeSymlink, Linkname: "../../x"},
		&tar.Header{Name: ".claude/in", Typeflag: tar.TypeSymlink, Linkname: "ok.json"},
		&tar.Header{Name: ".claude/hard", Typeflag: tar.TypeLink, Linkname: "/etc/passwd"},
		&tar.Header{Name: ".claude/dev", Typeflag: tar.TypeChar},
		&tar.Header{Name: "planted/through", Typeflag: tar.TypeReg, Mode: 0o644},
		&tar.Header{Name: ".claude/.credentials.json", Typeflag: tar.TypeReg, Mode: 0o600},
		&tar.Header{Name: ".claude/after.json", Typeflag: tar.TypeReg, Mode: 0o644},
	)
	// One hostile entry must not stop the rest: everything after it still arrives.
	skipped, err := Unpack(root, stream, func(n string) bool { return n == ".claude/.credentials.json" })
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dest, ".claude", "ok.json")); err != nil || string(b) != ".claude/ok.json" {
		t.Errorf("regular file: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".claude", "after.json")); err != nil {
		t.Error("entries after a hostile one were not unpacked")
	}
	if l, err := os.Readlink(filepath.Join(dest, ".claude", "in")); err != nil || l != "ok.json" {
		t.Errorf("safe symlink: %q %v", l, err)
	}
	for _, p := range []string{".claude/out", ".claude/up", ".claude/hard", ".claude/dev", ".claude/.credentials.json"} {
		if _, err := os.Lstat(filepath.Join(dest, p)); err == nil {
			t.Errorf("%s was written", p)
		}
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Errorf("something was written outside: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "escape")); err == nil {
		t.Error("../escape was written")
	}
	if len(skipped) < 6 {
		t.Errorf("skipped %v", skipped)
	}
}

// A symlink placed through a symlinked directory is checked where it really
// lands: d -> . and then d/e -> ../secret would leave e -> ../secret at the top.
func TestUnpackLinkThroughLinkedDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	dest := t.TempDir()
	root, _ := os.OpenRoot(dest)
	defer root.Close()
	stream := tarOf(t,
		&tar.Header{Name: "d", Typeflag: tar.TypeSymlink, Linkname: "."},
		&tar.Header{Name: "d/e", Typeflag: tar.TypeSymlink, Linkname: "../secret"},
		&tar.Header{Name: "sub/", Typeflag: tar.TypeDir, Mode: 0o755},
		&tar.Header{Name: "sub/up", Typeflag: tar.TypeSymlink, Linkname: ".."},
		&tar.Header{Name: "f", Typeflag: tar.TypeSymlink, Linkname: "sub/up/../.."},
		&tar.Header{Name: "sub/in", Typeflag: tar.TypeSymlink, Linkname: "../d"},
	)
	skipped, err := Unpack(root, stream, nil)
	if err != nil {
		t.Fatal(err)
	}
	if l, err := os.Readlink(filepath.Join(dest, "e")); err == nil {
		t.Errorf("e -> %q was created at the top", l)
	}
	if l, err := os.Readlink(filepath.Join(dest, "f")); err != nil || l != "." {
		t.Errorf("f -> %q %v, want the cleaned target \".\"", l, err)
	}
	if l, err := os.Readlink(filepath.Join(dest, "sub", "in")); err != nil || l != "../d" {
		t.Errorf("ordinary link from a real directory: %q %v", l, err)
	}
	if len(skipped) != 1 || skipped[0] != "d/e" {
		t.Errorf("skipped %v", skipped)
	}
}

func TestPackUnpackRoundTrip(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(src, "a", "b"), 0o755)
	os.WriteFile(filepath.Join(src, "a", "b", "f.txt"), []byte("content"), 0o644)
	os.WriteFile(filepath.Join(src, "run.sh"), []byte("#!/bin/sh\n"), 0o644)
	var buf bytes.Buffer
	if err := Pack(&buf, src, []File{{Path: "a/b/f.txt"}, {Path: "run.sh", Mode: 0o755}, {Path: "gone.txt"}}); err != nil {
		t.Fatal(err)
	}
	root, _ := os.OpenRoot(dest)
	defer root.Close()
	if _, err := Unpack(root, &buf, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "a", "b", "f.txt")); string(b) != "content" {
		t.Errorf("f.txt = %q", b)
	}
	if fi, err := os.Stat(filepath.Join(dest, "run.sh")); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o755) {
		t.Errorf("run.sh mode: %v %v", fi, err)
	}
}

// TestSSHPullPush round-trips through a real agent host (SW_SSH_TARGET).
func TestSSHPullPush(t *testing.T) {
	target := os.Getenv("SW_SSH_TARGET")
	if target == "" || runtime.GOOS == "windows" {
		t.Skip("set SW_SSH_TARGET (the same paths on both sides, e.g. localhost)")
	}
	ctx := context.Background()
	src, there, back := t.TempDir(), t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(src, "one.txt"), []byte("1"), 0o644)
	os.MkdirAll(filepath.Join(src, "d"), 0o755)
	os.WriteFile(filepath.Join(src, "d", "two.txt"), []byte("2"), 0o644)
	code, stderr, err := Push(ctx, target, "cd "+there+" && tar -xf -", src, []File{{Path: "one.txt"}, {Path: "d/two.txt"}})
	if err != nil || code != 0 {
		t.Fatalf("push: %d %v %s", code, err, stderr)
	}
	code, stderr, _, err = Pull(ctx, target, "cd "+there+" && tar -cf - one.txt d", back, nil)
	if err != nil || code != 0 {
		t.Fatalf("pull: %d %v %s", code, err, stderr)
	}
	if b, _ := os.ReadFile(filepath.Join(back, "d", "two.txt")); string(b) != "2" {
		t.Errorf("round trip lost d/two.txt: %q", b)
	}
}
