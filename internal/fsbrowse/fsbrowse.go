// Package fsbrowse lists directories inside a session's root for the Files
// tab. Paths from the frontend are relative, slash-separated, and may not
// leave the root, neither by name nor through symlinks: the agent host can
// create links in a shared directory (README -> ~/.bashrc).
package fsbrowse

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// MaxEntries caps a single listing; huge directories (node_modules, build
// output) are truncated rather than freezing the UI.
const MaxEntries = 5000

type Entry struct {
	Name    string `json:"name"`
	Rel     string `json:"rel"` // path relative to the root, "/"-separated
	Dir     bool   `json:"dir"` // directory, or symlink to one
	Symlink bool   `json:"symlink"`
	Outside bool   `json:"outside"` // symlink leading out of the root: not listed or opened
	Size    int64  `json:"size"`
	ModTime int64  `json:"mtime"` // Unix seconds
	Hidden  bool   `json:"hidden"`
}

type Listing struct {
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated"`
}

var ErrOutsideRoot = errors.New("fsbrowse: path leaves the session directory")

// Resolve turns rel ("" for the root) into a native path under root.
func Resolve(root, rel string) (string, error) {
	if strings.ContainsRune(rel, 0) || strings.Contains(rel, `\`) {
		return "", fmt.Errorf("fsbrowse: invalid path %q", rel)
	}
	if rel == "" {
		return root, nil
	}
	c := path.Clean(rel)
	if path.IsAbs(c) || c == ".." || strings.HasPrefix(c, "../") || filepath.VolumeName(c) != "" {
		return "", fmt.Errorf("%w: %q", ErrOutsideRoot, rel)
	}
	return filepath.Join(root, filepath.FromSlash(c)), nil
}

// Inside checks that p, with symlinks resolved, is within root.
func Inside(root, p string) error {
	rr, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	rp, err := filepath.EvalSymlinks(p)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rr, rp)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("%w: %s leads to %s", ErrOutsideRoot, p, rp)
	}
	return nil
}

// ResolveInside is Resolve plus Inside: for listing and opening, where a
// symlink must not lead out of the root.
func ResolveInside(root, rel string) (string, error) {
	p, err := Resolve(root, rel)
	if err != nil {
		return "", err
	}
	return p, Inside(root, p)
}

// IsHidden reports whether a name is hidden by default (dot files, which
// includes .git).
func IsHidden(name string) bool { return strings.HasPrefix(name, ".") }

// List returns the entries of root/rel: directories first, then files,
// each case-insensitively by name. Hidden entries are left out unless
// showHidden is set.
func List(root, rel string, showHidden bool) (Listing, error) {
	dir, err := ResolveInside(root, rel)
	if err != nil {
		return Listing{}, err
	}
	des, err := os.ReadDir(dir)
	if err != nil {
		return Listing{}, err
	}
	l := Listing{Entries: []Entry{}}
	for _, de := range des {
		name := de.Name()
		hidden := IsHidden(name)
		if hidden && !showHidden {
			continue
		}
		if len(l.Entries) == MaxEntries {
			l.Truncated = true
			break
		}
		e := Entry{Name: name, Rel: path.Join(rel, name), Hidden: hidden, Dir: de.IsDir()}
		if de.Type()&fs.ModeSymlink != 0 {
			e.Symlink = true
			full := filepath.Join(dir, name)
			if fi, err := os.Stat(full); err == nil {
				e.Dir = fi.IsDir() // broken links show as files
			}
			if errors.Is(Inside(root, full), ErrOutsideRoot) {
				e.Outside, e.Dir = true, false
			}
		}
		if fi, err := de.Info(); err == nil {
			e.ModTime = fi.ModTime().Unix()
			if !e.Dir {
				e.Size = fi.Size()
			}
		}
		l.Entries = append(l.Entries, e)
	}
	slices.SortFunc(l.Entries, func(a, b Entry) int {
		if a.Dir != b.Dir {
			if a.Dir {
				return -1
			}
			return 1
		}
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	return l, nil
}
