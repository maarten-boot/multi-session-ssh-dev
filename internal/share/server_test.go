package share

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

// client connects an sftp client to a Server on dir over in-memory pipes.
func client(t *testing.T, dir string) *sftp.Client {
	t.Helper()
	srv, err := NewServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	cr, sw := io.Pipe() // server -> client
	sr, cw := io.Pipe() // client -> server
	go func() {
		if err := srv.Serve(struct {
			io.Reader
			io.Writer
			io.Closer
		}{sr, sw, sw}); err != nil {
			t.Logf("server: %v", err)
		}
		sw.Close() // never leave the client waiting
		srv.Close()
	}()
	c, err := sftp.NewClientPipe(cr, cw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close(); cw.Close() })
	return c
}

func TestFileOperations(t *testing.T) {
	dir := t.TempDir()
	c := client(t, dir)

	f, err := c.Create("/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("hello world"))
	f.Close()
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "hello world" {
		t.Errorf("written %q", b)
	}
	r, _ := c.Open("a.txt") // relative works too
	b, _ := io.ReadAll(r)
	r.Close()
	if string(b) != "hello world" {
		t.Errorf("read %q", b)
	}

	if err := c.Mkdir("/sub"); err != nil {
		t.Fatal(err)
	}
	if err := c.Rename("/a.txt", "/sub/b.txt"); err != nil {
		t.Fatal(err)
	}
	if err := c.Truncate("/sub/b.txt", 5); err != nil {
		t.Fatal(err)
	}
	mtime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := c.Chtimes("/sub/b.txt", mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := c.Chmod("/sub/b.txt", 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fi, err := c.Stat("/sub/b.txt")
	if err != nil || fi.Size() != 5 || !fi.ModTime().Equal(mtime) || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Errorf("stat %v %v", fi, err)
	}
	ents, err := c.ReadDir("/")
	if err != nil || len(ents) != 1 || ents[0].Name() != "sub" || !ents[0].IsDir() {
		t.Errorf("readdir %v %v", ents, err)
	}

	// Plain rename refuses to overwrite; POSIX rename (atomic save) replaces.
	c.Create("/c.txt")
	if err := c.Rename("/c.txt", "/sub/b.txt"); err == nil {
		t.Error("rename overwrote an existing file")
	}
	if err := c.PosixRename("/c.txt", "/sub/b.txt"); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove("/sub"); err == nil {
		t.Error("remove on a directory succeeded")
	}
	if err := c.Remove("/sub/b.txt"); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveDirectory("/sub"); err != nil {
		t.Fatal(err)
	}
}

func TestCannotLeaveTheShare(t *testing.T) {
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("s3cret"), 0o600)
	dir := t.TempDir()
	c := client(t, dir)

	// ".." and absolute paths stay inside: /../x is /x of the share.
	if _, err := c.Stat("/../" + filepath.Base(outside) + "/secret"); err == nil {
		t.Error("reached a sibling directory through ..")
	}
	if f, err := c.Create("/../escaped.txt"); err == nil {
		f.Close()
		if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escaped.txt")); err == nil {
			t.Error("created a file outside the share")
		}
	}

	if runtime.GOOS == "windows" {
		return // symlinks need privileges there
	}
	// A symlink pointing outside (e.g. made on the host) is not followed.
	os.Symlink(outside, filepath.Join(dir, "out"))
	if _, err := c.Open("/out/secret"); err == nil {
		t.Error("followed a symlink out of the share")
	}
	if f, err := c.Create("/out/new"); err == nil {
		f.Close()
		t.Error("wrote through a symlink out of the share")
	}

	// Creating symlinks: only relative targets that stay inside.
	for _, target := range []string{"/etc/passwd", "../outside", "a/../../x", filepath.Join(outside, "secret")} {
		if err := c.Symlink(target, "/link"); err == nil {
			os.Remove(filepath.Join(dir, "link"))
			t.Errorf("symlink to %q created", target)
		}
	}
	os.MkdirAll(filepath.Join(dir, "docs"), 0o755)
	os.MkdirAll(filepath.Join(dir, "a"), 0o755)
	if err := c.Symlink("../docs", "/a/docs"); err != nil { // a/../docs = docs
		t.Errorf("inside symlink refused: %v", err)
	}
	if err := c.Symlink("../docs", "/docs-up"); err == nil { // from the top, .. leaves
		t.Error("symlink out of the share from its top level created")
	}
	if err := c.Symlink("docs/../docs", "/d2"); err != nil {
		t.Errorf("inside symlink refused: %v", err)
	}
	if got, err := c.ReadLink("/d2"); err != nil || got != "docs/../docs" {
		t.Errorf("readlink %q %v", got, err)
	}
	os.WriteFile(filepath.Join(dir, "f"), nil, 0o644)
	if err := c.Link("/f", "/hard"); err == nil {
		t.Error("hard link created")
	}
	var names []string
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	t.Logf("share contains %v", names)
}

func TestInsideRoot(t *testing.T) {
	for _, c := range []struct {
		link, target string
		ok           bool
	}{
		{"l", "x", true}, {"a/l", "../x", true}, {"a/b/l", "../../x", true},
		{"l", "../x", false}, {"a/l", "../../x", false}, {"l", "/x", false}, {"l", "", false},
		{"l", "a/../../x", false},
	} {
		if got := insideRoot(c.link, c.target); got != c.ok {
			t.Errorf("insideRoot(%q, %q) = %v", c.link, c.target, got)
		}
	}
}
