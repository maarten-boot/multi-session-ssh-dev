package fsbrowse

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func names(l Listing) []string {
	var out []string
	for _, e := range l.Entries {
		out = append(out, e.Name)
	}
	return out
}

func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"src/pkg", "Docs", "beta", ".git/objects", ".github"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	for _, f := range []string{"README.md", "alpha.go", "Zeta.txt", ".gitignore", "src/main.go"} {
		os.WriteFile(filepath.Join(root, f), []byte("hello"), 0o644)
	}
	return root
}

func TestListSortsAndHides(t *testing.T) {
	root := tree(t)
	l, err := List(root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(l), []string{"beta", "Docs", "src", "alpha.go", "README.md", "Zeta.txt"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	l, _ = List(root, "", true)
	if got, want := names(l), []string{".git", ".github", "beta", "Docs", "src", ".gitignore", "alpha.go", "README.md", "Zeta.txt"}; !slices.Equal(got, want) {
		t.Errorf("hidden: got %q, want %q", got, want)
	}
	for _, e := range l.Entries {
		if e.Hidden != (e.Name[0] == '.') {
			t.Errorf("%s: hidden=%v", e.Name, e.Hidden)
		}
		if e.Name == "alpha.go" && (e.Size != 5 || e.ModTime == 0 || e.Dir) {
			t.Errorf("file entry %+v", e)
		}
	}
}

func TestListSubdirRel(t *testing.T) {
	root := tree(t)
	l, err := List(root, "src", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != 2 || l.Entries[0].Rel != "src/pkg" || !l.Entries[0].Dir || l.Entries[1].Rel != "src/main.go" {
		t.Errorf("%+v", l.Entries)
	}
}

func TestOutsideRootRejected(t *testing.T) {
	root := tree(t)
	for _, rel := range []string{"..", "../x", "src/../../x", "/etc", `src\..\..`, "a\x00b"} {
		if _, err := List(root, rel, false); err == nil {
			t.Errorf("List(%q) succeeded", rel)
		}
	}
	if _, err := Resolve(root, "src/../Docs"); err != nil {
		t.Errorf("inner .. rejected: %v", err)
	}
	if _, err := Resolve(root, "../x"); !errors.Is(err, ErrOutsideRoot) {
		t.Errorf("want ErrOutsideRoot, got %v", err)
	}
}

func TestSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root := tree(t)
	os.Symlink(filepath.Join(root, "src"), filepath.Join(root, "link-dir"))
	os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "broken"))
	l, _ := List(root, "", false)
	got := map[string]Entry{}
	for _, e := range l.Entries {
		got[e.Name] = e
	}
	if e := got["link-dir"]; !e.Symlink || !e.Dir {
		t.Errorf("link-dir %+v", e)
	}
	if e := got["broken"]; !e.Symlink || e.Dir {
		t.Errorf("broken %+v", e)
	}
}

func TestTruncation(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < MaxEntries+10; i++ {
		os.WriteFile(filepath.Join(root, fmt.Sprintf("f%05d", i)), nil, 0o644)
	}
	l, err := List(root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !l.Truncated || len(l.Entries) != MaxEntries {
		t.Errorf("truncated=%v len=%d", l.Truncated, len(l.Entries))
	}
}

func TestMissingDir(t *testing.T) {
	if _, err := List(t.TempDir(), "nope", false); err == nil {
		t.Error("listing missing dir succeeded")
	}
}
