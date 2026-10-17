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

// Symlinks the agent host may plant must not lead out of the root.
func TestSymlinksOutOfRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root, outside := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(outside, "bashrc"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(root, "real.md"), []byte("x"), 0o600)
	os.Mkdir(filepath.Join(root, "sub"), 0o755)
	os.Symlink(filepath.Join(outside, "bashrc"), filepath.Join(root, "README"))
	os.Symlink(outside, filepath.Join(root, "escape"))
	os.Symlink("real.md", filepath.Join(root, "inner"))
	os.Symlink("sub", filepath.Join(root, "subdir"))
	l, err := List(root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Entry{}
	for _, e := range l.Entries {
		got[e.Name] = e
	}
	if !got["README"].Outside || !got["escape"].Outside || got["escape"].Dir {
		t.Errorf("links out of the root not marked: %+v %+v", got["README"], got["escape"])
	}
	if got["inner"].Outside || got["subdir"].Outside || !got["subdir"].Dir {
		t.Errorf("links inside the root marked: %+v %+v", got["inner"], got["subdir"])
	}
	for _, rel := range []string{"README", "escape", "escape/bashrc"} {
		if _, err := ResolveInside(root, rel); !errors.Is(err, ErrOutsideRoot) {
			t.Errorf("ResolveInside(%q) = %v, want ErrOutsideRoot", rel, err)
		}
	}
	if _, err := List(root, "escape", false); !errors.Is(err, ErrOutsideRoot) {
		t.Errorf("listing a link out of the root: %v", err)
	}
	if _, err := ResolveInside(root, "inner"); err != nil {
		t.Errorf("link inside the root refused: %v", err)
	}
}

// A link inside the root shows its target's size; a broken link pointing
// out of the root is marked as outside.
func TestSymlinkSizeAndBrokenOutside(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root, outside := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(root, "big.txt"), make([]byte, 12345), 0o644)
	os.Mkdir(filepath.Join(root, "sub"), 0o755)
	os.Symlink(outside, filepath.Join(root, "esc"))
	os.Symlink("big.txt", filepath.Join(root, "inner"))
	os.Symlink(filepath.Join(outside, "missing"), filepath.Join(root, "abs-out"))
	os.Symlink("../../nowhere", filepath.Join(root, "sub", "up-out"))
	os.Symlink("../missing", filepath.Join(root, "sub", "up-in"))
	os.Symlink("esc/missing", filepath.Join(root, "via-link"))
	os.Symlink("esc/../missing", filepath.Join(root, "via-link-up"))
	os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "abs-in"))
	l, err := List(root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	sub, _ := List(root, "sub", false)
	got := map[string]Entry{}
	for _, e := range append(l.Entries, sub.Entries...) {
		got[e.Name] = e
	}
	if e := got["inner"]; e.Size != 12345 || e.Outside {
		t.Errorf("inner link: %+v, want the target's size", e)
	}
	for _, n := range []string{"abs-out", "up-out", "via-link", "via-link-up"} {
		if !got[n].Outside {
			t.Errorf("broken link %s out of the root not marked: %+v", n, got[n])
		}
	}
	for _, n := range []string{"up-in", "abs-in"} {
		if e, ok := got[n]; !ok || e.Outside {
			t.Errorf("broken link %s inside the root marked: %+v", n, e)
		}
	}
}
