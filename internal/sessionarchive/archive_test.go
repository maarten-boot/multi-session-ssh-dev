package sessionarchive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"session-workbench/internal/config"
)

func write(t *testing.T, p, text string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestKind(t *testing.T) {
	for name, want := range map[string]string{"a.zip": "zip", "A.ZIP": "zip", "a.tar.gz": "tar.gz", "a.tgz": "tar.gz"} {
		if k, err := Kind(name); err != nil || k != want {
			t.Errorf("Kind(%s) = %q, %v", name, k, err)
		}
	}
	if _, err := Kind("a.rar"); err == nil {
		t.Error("a.rar accepted")
	}
}

// TestRoundTrip writes both kinds and reads them back: the manifest, the
// files of a part (in subfolders), and that Extract keeps existing files.
func TestRoundTrip(t *testing.T) {
	src := t.TempDir()
	write(t, filepath.Join(src, "Home.md"), "# Home\n")
	write(t, filepath.Join(src, "Work", "Plan.md"), "plan\n")
	os.MkdirAll(filepath.Join(src, "Empty", "Inner"), 0o755)
	for _, kind := range []string{"zip", "tar.gz"} {
		t.Run(kind, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "s."+kind)
			f, _ := os.Create(file)
			w, err := NewWriter(f, kind)
			if err != nil {
				t.Fatal(err)
			}
			if n, err := w.AddDir("notes", src, nil); err != nil || n != 2 {
				t.Fatalf("AddDir: %d %v", n, err)
			}
			if n, err := w.AddDir("none", filepath.Join(src, "missing"), nil); err != nil || n != 0 {
				t.Fatalf("missing dir: %d %v", n, err)
			}
			if err := w.AddBytes("notes/Home.md", []byte("x")); err == nil {
				t.Error("a name twice")
			}
			m := Manifest{Format: Format, App: "test", Exported: time.Now(), Parts: []string{PartNotes},
				Session: config.Session{ID: "s-ab12", Name: "demo"}, Host: config.Host{Name: "box"}}
			mb, _ := json.Marshal(m)
			w.AddBytes(ManifestName, mb)
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			f.Close()
			arc, err := Open(file)
			if err != nil {
				t.Fatal(err)
			}
			if arc.Manifest.Session.Name != "demo" || !arc.Manifest.Has(PartNotes) || arc.Manifest.Has(PartProject) {
				t.Errorf("manifest %+v", arc.Manifest)
			}
			dest := t.TempDir()
			write(t, filepath.Join(dest, "Home.md"), "mine\n")
			if n, err := arc.Extract("notes", dest, false); err != nil || n != 1 {
				t.Fatalf("Extract: %d %v", n, err)
			}
			if b, _ := os.ReadFile(filepath.Join(dest, "Home.md")); string(b) != "mine\n" {
				t.Error("an existing file was replaced")
			}
			if b, _ := os.ReadFile(filepath.Join(dest, "Work", "Plan.md")); string(b) != "plan\n" {
				t.Errorf("Work/Plan.md: %q", b)
			}
			if fi, err := os.Stat(filepath.Join(dest, "Empty", "Inner")); err != nil || !fi.IsDir() {
				t.Errorf("the empty directory did not come back: %v", err)
			}
			if b, err := arc.ReadFile("notes/Work/Plan.md", 100); err != nil || string(b) != "plan\n" {
				t.Errorf("ReadFile: %q %v", b, err)
			}
			if b, err := arc.ReadFile("nothing", 100); err != nil || b != nil {
				t.Errorf("ReadFile of a missing file: %q %v", b, err)
			}
			if _, err := arc.ReadFile("notes/Work/Plan.md", 2); err == nil {
				t.Error("max not applied")
			}
		})
	}
}

// TestHostile: names with "..", absolute names and symlinks in an archive
// are never written; no manifest or a newer format is refused.
func TestHostile(t *testing.T) {
	dir := t.TempDir()
	zfile := filepath.Join(dir, "bad.zip")
	f, _ := os.Create(zfile)
	zw := zip.NewWriter(f)
	for _, n := range []string{"project/../../evil", "/abs/evil", "project/ok"} {
		w, _ := zw.Create(n)
		w.Write([]byte("x"))
	}
	h := &zip.FileHeader{Name: "project/link"}
	h.SetMode(os.ModeSymlink | 0o777)
	w, _ := zw.CreateHeader(h)
	w.Write([]byte("/etc/passwd"))
	w, _ = zw.Create(ManifestName)
	w.Write([]byte(`{"format":1}`))
	zw.Close()
	f.Close()

	tfile := filepath.Join(dir, "bad.tar.gz")
	f, _ = os.Create(tfile)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, n := range []string{"project/../../evil", "project/ok"} {
		tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
		tw.Write([]byte("x"))
	}
	tw.WriteHeader(&tar.Header{Name: "project/link", Linkname: "/etc/passwd", Typeflag: tar.TypeSymlink})
	tw.WriteHeader(&tar.Header{Name: ManifestName, Mode: 0o644, Size: 12, Typeflag: tar.TypeReg})
	tw.Write([]byte(`{"format":1}`))
	tw.Close()
	gz.Close()
	f.Close()

	for _, file := range []string{zfile, tfile} {
		arc, err := Open(file)
		if err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(t.TempDir(), "in")
		if n, err := arc.Extract("project", dest, false); err != nil || n != 1 {
			t.Errorf("%s: Extract %d %v", filepath.Base(file), n, err)
		}
		if _, err := os.Lstat(filepath.Join(dest, "link")); err == nil {
			t.Errorf("%s: a symlink was made", filepath.Base(file))
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "..", "evil")); err == nil {
			t.Errorf("%s: wrote outside", filepath.Base(file))
		}
	}

	nf := filepath.Join(dir, "nomanifest.zip")
	f, _ = os.Create(nf)
	zw = zip.NewWriter(f)
	zw.Create("x")
	zw.Close()
	f.Close()
	if _, err := Open(nf); err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Errorf("no manifest: %v", err)
	}
	ff := filepath.Join(dir, "future.zip")
	f, _ = os.Create(ff)
	zw = zip.NewWriter(f)
	w, _ = zw.Create(ManifestName)
	w.Write([]byte(`{"format":99}`))
	zw.Close()
	f.Close()
	if _, err := Open(ff); err == nil {
		t.Error("format 99 accepted")
	}
	tf := filepath.Join(dir, "text.zip")
	write(t, tf, "hello")
	if _, err := Open(tf); err == nil {
		t.Error("a text file accepted")
	}
}

// TestProjectFiles: in a repository the tracked and untracked files and
// .git, not the ignored ones; without git every file.
func TestProjectFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q")
	write(t, filepath.Join(dir, ".gitignore"), "build/\n*.o\n")
	write(t, filepath.Join(dir, "main.go"), "package main\n")
	run("add", ".")
	write(t, filepath.Join(dir, "new.txt"), "untracked\n")
	write(t, filepath.Join(dir, "build", "out"), "ignored\n")
	write(t, filepath.Join(dir, "x.o"), "ignored\n")
	files, err := ProjectFiles(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".gitignore", "main.go", "new.txt", ".git/HEAD", ".git/refs/heads/"} {
		if !slices.Contains(files, want) {
			t.Errorf("no %s in %v", want, files)
		}
	}
	for _, not := range []string{"build/out", "x.o"} {
		if slices.Contains(files, not) {
			t.Errorf("%s (ignored) is in", not)
		}
	}
	plain := t.TempDir()
	write(t, filepath.Join(plain, "a", "b.txt"), "b\n")
	write(t, filepath.Join(plain, "c.o"), "c\n")
	files, err = ProjectFiles(context.Background(), plain)
	if err != nil || !slices.Equal(files, []string{"a/b.txt", "c.o"}) {
		t.Errorf("without git: %v %v", files, err)
	}
}

// TestModes: an executable stays executable (not on Windows: no modes).
func TestModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no file modes on Windows")
	}
	src := t.TempDir()
	write(t, filepath.Join(src, "run.sh"), "#!/bin/sh\n")
	os.Chmod(filepath.Join(src, "run.sh"), 0o755)
	for _, kind := range []string{"zip", "tar.gz"} {
		file := filepath.Join(t.TempDir(), "m."+kind)
		f, _ := os.Create(file)
		w, _ := NewWriter(f, kind)
		w.AddDir("project", src, nil)
		w.AddBytes(ManifestName, []byte(`{"format":1}`))
		w.Close()
		f.Close()
		arc, err := Open(file)
		if err != nil {
			t.Fatal(err)
		}
		dest := t.TempDir()
		arc.Extract("project", dest, false)
		if fi, err := os.Stat(filepath.Join(dest, "run.sh")); err != nil || fi.Mode().Perm()&0o100 == 0 {
			t.Errorf("%s: run.sh not executable: %v", kind, fi.Mode())
		}
	}
}
