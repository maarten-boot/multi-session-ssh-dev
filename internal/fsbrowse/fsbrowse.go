// Package fsbrowse lists directories inside a session's root for the Files
// tab. Paths from the frontend are relative, slash-separated, and may not
// leave the root.
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

// IsHidden reports whether a name is hidden by default (dot files, which
// includes .git).
func IsHidden(name string) bool { return strings.HasPrefix(name, ".") }

// List returns the entries of root/rel: directories first, then files,
// each case-insensitively by name. Hidden entries are left out unless
// showHidden is set.
func List(root, rel string, showHidden bool) (Listing, error) {
	dir, err := Resolve(root, rel)
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
			if fi, err := os.Stat(filepath.Join(dir, name)); err == nil {
				e.Dir = fi.IsDir() // broken links show as files
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
