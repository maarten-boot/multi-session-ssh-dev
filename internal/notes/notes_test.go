package notes

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

const book = "s-1a2b"

func newStore(t *testing.T) Store { return Store{Dir: filepath.Join(t.TempDir(), "notes")} }

// paths flattens a tree into "path" or "path*" (has text), depth first.
func paths(ps []Page) []string {
	var out []string
	for _, p := range ps {
		s := p.Path
		if p.HasText {
			s += "*"
		}
		out = append(out, s)
		out = append(out, paths(p.Children)...)
	}
	return out
}

func tree(t *testing.T, s Store, b string) []string {
	t.Helper()
	ps, err := s.Tree(b)
	if err != nil {
		t.Fatal(err)
	}
	return paths(ps)
}

func TestLoadSave(t *testing.T) {
	s := newStore(t)
	n, err := s.Load(book, Home)
	if err != nil || n.Text != "" || n.Mtime != 0 || !strings.HasSuffix(n.Path, filepath.Join(book, "Home.md")) {
		t.Fatalf("missing page: %+v %v", n, err)
	}
	text := "# Plan\n\n- ask claude about the parser\n- héllo ✓ 日本\n"
	saved, err := s.Save(book, "Design/API", text)
	if err != nil || saved.Mtime == 0 {
		t.Fatalf("save: %+v %v", saved, err)
	}
	got, err := s.Load(book, "Design/API")
	if err != nil || got.Text != text || got.Mtime != saved.Mtime {
		t.Errorf("reload: %+v %v", got, err)
	}
	if runtime.GOOS != "windows" {
		for _, p := range []string{got.Path, filepath.Dir(got.Path), filepath.Join(s.Dir, book)} {
			fi, _ := os.Stat(p)
			if fi.Mode().Perm()&0o077 != 0 {
				t.Errorf("%s: permissions %v, want owner-only", p, fi.Mode().Perm())
			}
		}
	}
	// No temp files left.
	if ents, _ := os.ReadDir(filepath.Dir(got.Path)); len(ents) != 1 {
		t.Errorf("%d files next to the page", len(ents))
	}
	// Empty text removes the page and the folders it leaves empty.
	if _, err := s.Save(book, "Design/API", ""); err != nil {
		t.Fatal(err)
	}
	if got := tree(t, s, book); len(got) != 0 {
		t.Errorf("after emptying: %v", got)
	}
}

func TestTree(t *testing.T) {
	s := newStore(t)
	for _, p := range []string{"zeta", "Alpha", "Home", "Alpha/two", "Alpha/One", "Beta/Deep/x"} {
		if _, err := s.Save(book, p, "x"); err != nil {
			t.Fatal(err)
		}
	}
	// Not pages: hidden or temp files, other extensions, bad names.
	d := filepath.Join(s.Dir, book)
	os.WriteFile(filepath.Join(d, ".notes-123.md"), nil, 0o600)
	os.WriteFile(filepath.Join(d, "README.txt"), nil, 0o600)
	os.WriteFile(filepath.Join(d, "a:b.md"), nil, 0o600)
	want := []string{"Home*", "Alpha*", "Alpha/One*", "Alpha/two*", "Beta", "Beta/Deep", "Beta/Deep/x*", "zeta*"}
	if got := tree(t, s, book); !slices.Equal(got, want) {
		t.Errorf("tree:\n got %v\nwant %v", got, want)
	}
}

func TestCreateRenameDelete(t *testing.T) {
	s := newStore(t)
	n, err := s.Create(book, "Design")
	if err != nil || n.Text != "# Design\n\n" {
		t.Fatalf("create: %+v %v", n, err)
	}
	if _, err := s.Create(book, "Design"); err == nil {
		t.Error("created a page that exists")
	}
	s.Create(book, "Design/API")
	s.Create(book, "Design/API/v2")
	if _, err := s.Create(book, "Design/API"); err == nil {
		t.Error("created a page that exists")
	}
	// A page with subpages but no text exists too.
	s.Save(book, "Folder/Sub", "x")
	if _, err := s.Create(book, "Folder"); err == nil {
		t.Error("created a page that has subpages")
	}
	s.Delete(book, "Folder")
	// Rename moves the page with its subpages.
	if err := s.Rename(book, "Design", "Architecture"); err != nil {
		t.Fatal(err)
	}
	want := []string{"Architecture*", "Architecture/API*", "Architecture/API/v2*"}
	if got := tree(t, s, book); !slices.Equal(got, want) {
		t.Errorf("after rename: %v", got)
	}
	// Into another page, and out again; the empty parent folder goes.
	if err := s.Rename(book, "Architecture/API", "Old/API"); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename(book, "Old/API", "API"); err != nil {
		t.Fatal(err)
	}
	want = []string{"API*", "API/v2*", "Architecture*"}
	if got := tree(t, s, book); !slices.Equal(got, want) {
		t.Errorf("after moves: %v", got)
	}
	for _, c := range [][2]string{{"API", "API/v2/x"}, {"API", "API"}, {"API", "Architecture"}, {"Nope", "X"}, {"API", "../x"}} {
		if err := s.Rename(book, c[0], c[1]); err == nil {
			t.Errorf("Rename(%q, %q) accepted", c[0], c[1])
		}
	}
	// Changing only the case is a rename (one name on Windows and macOS).
	if err := s.Rename(book, "Architecture", "architecture"); err != nil {
		t.Errorf("case-only rename: %v", err)
	}
	// Delete takes the subpages along.
	if err := s.Delete(book, "API"); err != nil {
		t.Fatal(err)
	}
	if got := tree(t, s, book); !slices.Equal(got, []string{"architecture*"}) {
		t.Errorf("after delete: %v", got)
	}
}

func TestNames(t *testing.T) {
	for _, p := range []string{"Home", "Design/API v2", "héllo ✓ 日本", "a.b", "C++", strings.Repeat("x", 100)} {
		if err := CheckName(p); err != nil {
			t.Errorf("CheckName(%q): %v", p, err)
		}
	}
	for _, p := range []string{"", "/a", "a/", "a//b", "..", "../x", ".hidden", "a\\b", "a:b", "a*", "a?", `a"`, "a<b", "a|b",
		" a", "a ", "a.", "x\x01", "notes.md", "CON", "nul.txt", "com1", "CONIN$", "conout$.txt", "COM¹", "lpt³", "LPT².x",
		strings.Repeat("x", 101),
		strings.Repeat("a/", 10) + "a"} {
		if err := CheckName(p); err == nil {
			t.Errorf("CheckName(%q) accepted", p)
		}
	}
	s := newStore(t)
	if _, err := s.Save(book, "../escape", "x"); err == nil {
		t.Error("saved outside the book")
	}
	for _, b := range []string{"", "../x", "a/b", "A", "_other"} {
		if _, err := s.Load(b, Home); err == nil {
			t.Errorf("book %q accepted", b)
		}
	}
}

func TestSizeLimit(t *testing.T) {
	s := newStore(t)
	if _, err := s.Save(book, Home, strings.Repeat("x", MaxSize+1)); err == nil {
		t.Error("oversized page saved")
	}
	// One written by something else is not loaded.
	d := filepath.Join(s.Dir, book)
	os.MkdirAll(d, 0o700)
	if err := os.WriteFile(filepath.Join(d, "Big.md"), make([]byte, MaxSize+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(book, "Big"); err == nil {
		t.Error("oversized page loaded")
	}
}

// A symlink out of the book is not followed.
func TestSymlinkOutside(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	s := newStore(t)
	outside := filepath.Join(t.TempDir(), "secret.md")
	os.WriteFile(outside, []byte("secret"), 0o600)
	d := filepath.Join(s.Dir, book)
	os.MkdirAll(d, 0o700)
	if err := os.Symlink(outside, filepath.Join(d, "Home.md")); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Load(book, Home); err == nil {
		t.Errorf("loaded through a symlink out of the book: %+v", n)
	}
}

// caseSensitive reports whether the file system under dir tells "a" from "A".
func caseSensitive(t *testing.T, dir string) bool {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "a"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := os.Stat(filepath.Join(dir, "A"))
	return err != nil
}

func TestRenameCase(t *testing.T) {
	s := newStore(t)
	if _, err := s.Create(book, "Design"); err != nil {
		t.Fatal(err)
	}
	// "design/X" lies inside "Design" on Windows and macOS.
	if err := s.Rename(book, "Design", "design/X"); err == nil {
		t.Error("moved a page into itself (other case)")
	}
	if got := tree(t, s, book); !slices.Equal(got, []string{"Design*"}) {
		t.Errorf("after refused move: %v", got)
	}
	if !caseSensitive(t, t.TempDir()) {
		t.Skip("case-insensitive file system")
	}
	// Here "Design" and "design" are two pages: no overwriting.
	if _, err := s.Save(book, "design", "keep me"); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename(book, "Design", "design"); err == nil {
		t.Error("rename overwrote a page that differs only in case")
	}
	if n, _ := s.Load(book, "design"); n.Text != "keep me" {
		t.Errorf("design: %q", n.Text)
	}
}

func TestRenameDepth(t *testing.T) {
	s := newStore(t)
	for _, p := range []string{"A/B/C/D/E", "P/Q/R/S/T/U/V"} {
		if _, err := s.Save(book, p, "x"); err != nil {
			t.Fatal(err)
		}
	}
	// A has 4 levels below it: at depth 8 that is 12.
	if err := s.Rename(book, "A", "P/Q/R/S/T/U/V/A"); err == nil {
		t.Error("moved pages deeper than MaxDepth")
	}
	if err := s.Rename(book, "A", "P/Q/R/S/T/A"); err != nil {
		t.Errorf("move to depth 10: %v", err)
	}
	if _, err := s.Tree(book); err != nil {
		t.Error(err)
	}
	if n, err := s.Load(book, "P/Q/R/S/T/A/B/C/D/E"); err != nil || n.Text != "x" {
		t.Errorf("deepest page: %+v %v", n, err)
	}
}

// Notes from before pages existed: a session's become its Home page, the
// app's its Prompts page; an existing page is not overwritten.
func TestMigrate(t *testing.T) {
	s := newStore(t)
	os.MkdirAll(s.Dir, 0o700)
	os.WriteFile(filepath.Join(s.Dir, book+".md"), []byte("old session notes"), 0o600)
	os.WriteFile(filepath.Join(s.Dir, AppID+".md"), []byte("## Review\nReview it."), 0o600)
	if n, _ := s.Load(book, Home); n.Text != "old session notes" {
		t.Errorf("session notes: %+v", n)
	}
	if n, _ := s.Load(AppID, Prompts); n.Text != "## Review\nReview it." {
		t.Errorf("app notes: %+v", n)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, book+".md")); !os.IsNotExist(err) {
		t.Error("old file left")
	}
	os.WriteFile(filepath.Join(s.Dir, book+".md"), []byte("second"), 0o600)
	if n, _ := s.Load(book, Home); n.Text != "old session notes" {
		t.Errorf("existing Home overwritten: %+v", n)
	}
}

func TestCountTodos(t *testing.T) {
	for text, want := range map[string]int{
		"TODO: fix the parser":                         1,
		"- TODO check logs\n- [ ] TODO two\nok":        2,
		"two on one line: TODO and TODO":               1, // lines count
		"(TODO) and «TODO»\r\nTODO.":                   2,
		"todo, Todo, TODOs, MYTODO, TODO_LIST, X1TODO": 0,
		"ÄTODO and TODOé":                              0,
		"":                                             0,
	} {
		if got := CountTodos(text); got != want {
			t.Errorf("CountTodos(%q) = %d, want %d", text, got, want)
		}
	}
}

func TestTodos(t *testing.T) {
	s := newStore(t)
	for page, text := range map[string]string{
		"Home":        "Plans.\nTODO: ask about the API\n",
		"Design/API":  "TODO one\nTODO two\n",
		"Design":      "nothing to do",
		"Prompts/Old": "done",
	} {
		if _, err := s.Save(book, page, text); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Todos(book)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["Home"] != 1 || got["Design/API"] != 2 {
		t.Errorf("Todos = %v", got)
	}
	if got, err := s.Todos("s-none"); err != nil || len(got) != 0 {
		t.Errorf("a book without notes: %v, %v", got, err)
	}
}
