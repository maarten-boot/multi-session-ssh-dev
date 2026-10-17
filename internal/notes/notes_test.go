package notes

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadSave(t *testing.T) {
	s := Store{Dir: filepath.Join(t.TempDir(), "notes")}
	n, err := s.Load("s-1a2b")
	if err != nil || n.Text != "" || n.Mtime != 0 || !strings.HasSuffix(n.Path, "s-1a2b.md") {
		t.Fatalf("missing notes: %+v %v", n, err)
	}
	text := "# Plan\n\n- ask claude about the parser\n- héllo ✓ 日本\n"
	saved, err := s.Save("s-1a2b", text)
	if err != nil || saved.Mtime == 0 {
		t.Fatalf("save: %+v %v", saved, err)
	}
	got, err := s.Load("s-1a2b")
	if err != nil || got.Text != text || got.Mtime != saved.Mtime {
		t.Errorf("reload: %+v %v", got, err)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(got.Path)
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("permissions %v, want 0600", fi.Mode().Perm())
		}
	}
	// No temp files left; empty notes remove the file.
	if ents, _ := os.ReadDir(s.Dir); len(ents) != 1 {
		t.Errorf("%d files in notes dir", len(ents))
	}
	if _, err := s.Save("s-1a2b", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(got.Path); !os.IsNotExist(err) {
		t.Error("empty notes left a file")
	}
}

func TestInvalidIDs(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for _, id := range []string{"", "../x", "a/b", "A", `s-1\x`} {
		if _, err := s.Load(id); err == nil {
			t.Errorf("Load(%q) accepted", id)
		}
		if _, err := s.Save(id, "x"); err == nil {
			t.Errorf("Save(%q) accepted", id)
		}
	}
}

func TestSizeLimit(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.Save("s-big", strings.Repeat("x", MaxSize+1)); err == nil {
		t.Error("oversized notes saved")
	}
}

func TestAppNotes(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.Save(AppID, "## Review\nReview the diff.\n"); err != nil {
		t.Fatal(err)
	}
	n, err := s.Load(AppID)
	if err != nil || n.Text != "## Review\nReview the diff.\n" || filepath.Base(n.Path) != "_app.md" {
		t.Errorf("app notes: %+v %v", n, err)
	}
	if _, err := s.Load("_other"); err == nil {
		t.Error("an id starting with _ other than the app notes was accepted")
	}
}
