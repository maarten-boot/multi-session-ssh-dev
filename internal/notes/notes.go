// Package notes stores a free-text notes file per session, next to the
// config file (notes/<session id>.md) rather than in the project directory:
// notes stay out of the repository and are not visible to the agent host.
package notes

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"session-workbench/internal/remote"
)

// MaxSize bounds a notes file; the editor is a plain text area.
const MaxSize = 4 << 20

// Note is a session's notes as stored.
type Note struct {
	Text  string `json:"text"`
	Path  string `json:"path"`
	Mtime int64  `json:"mtime"` // Unix milliseconds; 0 if there is no file yet
}

// Store keeps notes files in one directory.
type Store struct {
	Dir string
}

func (s Store) path(id string) (string, error) {
	if !remote.ValidID(id) {
		return "", fmt.Errorf("notes: invalid session id %q", id)
	}
	return filepath.Join(s.Dir, id+".md"), nil
}

// Load returns a session's notes; a missing file is empty notes.
func (s Store) Load(id string) (Note, error) {
	p, err := s.path(id)
	if err != nil {
		return Note{}, err
	}
	n := Note{Path: p}
	fi, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return n, nil
	}
	if err != nil {
		return n, err
	}
	if fi.Size() > MaxSize {
		return n, fmt.Errorf("notes: %s is larger than %d MB", p, MaxSize>>20)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return n, err
	}
	n.Text, n.Mtime = string(b), fi.ModTime().UnixMilli()
	return n, nil
}

// Save writes a session's notes atomically (owner-only permissions: notes
// may contain anything). Empty notes remove the file.
func (s Store) Save(id, text string) (Note, error) {
	p, err := s.path(id)
	if err != nil {
		return Note{}, err
	}
	if len(text) > MaxSize {
		return Note{}, fmt.Errorf("notes: larger than %d MB", MaxSize>>20)
	}
	if text == "" {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return Note{}, err
		}
		return Note{Path: p}, nil
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return Note{}, err
	}
	f, err := os.CreateTemp(s.Dir, ".notes-*.md")
	if err != nil {
		return Note{}, err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op after the rename
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return Note{}, err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return Note{}, err
	}
	if err := f.Close(); err != nil {
		return Note{}, err
	}
	if err := os.Rename(tmp, p); err != nil {
		return Note{}, err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return Note{}, err
	}
	return Note{Text: text, Path: p, Mtime: fi.ModTime().UnixMilli()}, nil
}
