// Package notes stores notes as a tree of Markdown pages, one tree (a
// "book") per session and one for the whole app, next to the config file
// rather than in the project directory: notes stay out of the repository and
// are not visible to the agent host.
//
// Layout, like the Zim desktop wiki: page "Design/API" of session s-7f3a is
// notes/s-7f3a/Design/API.md; a page with subpages has a folder of the same
// name, and may have no file of its own.
package notes

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"session-workbench/internal/remote"
)

// MaxSize bounds a page.
const MaxSize = 4 << 20

// Limits on page names and trees.
const (
	MaxNameLen = 100  // characters per path element
	MaxDepth   = 10   // path elements
	MaxPages   = 5000 // per book, when listing
)

// AppID names the app-wide book: notes for all sessions and common
// prompts. Session ids cannot start with "_".
const AppID = "_app"

// Home is a book's first page; a session's notes from before pages existed
// become its Home page.
const Home = "Home"

// Prompts is the app book's page whose text (and subpages) holds the
// prompts; the app notes from before pages existed become this page.
const Prompts = "Prompts"

// Note is one page as stored.
type Note struct {
	Text  string `json:"text"`
	Path  string `json:"path"`  // the file
	Mtime int64  `json:"mtime"` // Unix milliseconds; 0 if there is no file yet
}

// Page is an entry of a book's tree.
type Page struct {
	Path     string `json:"path"` // "Design/API"
	Name     string `json:"name"` // "API"
	HasText  bool   `json:"has_text"`
	Children []Page `json:"children"`
}

// Store keeps the books in one directory.
type Store struct {
	Dir string
}

// mu serialises changes: the UI saves, renames and deletes one at a time,
// but a rename must not race a save of the same page.
var mu sync.Mutex

func bookDir(dir, book string) (string, error) {
	if book != AppID && !remote.ValidID(book) {
		return "", fmt.Errorf("notes: invalid session id %q", book)
	}
	return filepath.Join(dir, book), nil
}

// open returns the book's folder as an os.Root, creating it (owner-only)
// and moving notes from before pages existed into it first.
func (s Store) open(book string) (*os.Root, string, error) {
	d, err := bookDir(s.Dir, book)
	if err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return nil, "", err
	}
	s.migrate(book, d)
	r, err := os.OpenRoot(d)
	return r, d, err
}

// migrate moves notes/<book>.md to the book's Home page (Prompts for the
// app book) unless that page exists already.
func (s Store) migrate(book, d string) {
	old := filepath.Join(s.Dir, book+".md")
	if _, err := os.Stat(old); err != nil {
		return
	}
	target := filepath.Join(d, Home+".md")
	if book == AppID {
		target = filepath.Join(d, Prompts+".md")
	}
	if _, err := os.Stat(target); err == nil {
		return
	}
	os.Rename(old, target)
}

// CheckName reports whether p is a valid page path: elements separated by
// "/", each 1–100 characters, without characters file systems reserve, not
// starting with "." (hidden, temp files) and not a Windows device name.
func CheckName(p string) error {
	if p == "" {
		return errors.New("notes: empty page name")
	}
	parts := strings.Split(p, "/")
	if len(parts) > MaxDepth {
		return fmt.Errorf("notes: pages nest at most %d deep", MaxDepth)
	}
	for _, e := range parts {
		if err := checkElem(e); err != nil {
			return err
		}
	}
	return nil
}

func checkElem(e string) error {
	switch {
	case e == "":
		return errors.New("notes: empty page name")
	case !utf8.ValidString(e):
		return errors.New("notes: page name is not valid UTF-8")
	case utf8.RuneCountInString(e) > MaxNameLen:
		return fmt.Errorf("notes: page name longer than %d characters", MaxNameLen)
	case strings.HasPrefix(e, "."):
		return fmt.Errorf("notes: page name %q starts with a dot", e)
	case strings.TrimSpace(e) != e || strings.HasSuffix(e, "."):
		return fmt.Errorf("notes: page name %q starts or ends with a space or ends with a dot", e)
	case strings.ContainsAny(e, `/\:*?"<>|`):
		return fmt.Errorf(`notes: page name %q contains one of / \ : * ? " < > |`, e)
	case strings.IndexFunc(e, unicode.IsControl) >= 0:
		return fmt.Errorf("notes: page name %q contains a control character", e)
	case strings.EqualFold(path.Ext(e), ".md"):
		return fmt.Errorf("notes: page name %q ends in .md", e)
	}
	base := strings.ToUpper(e)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "COM¹", "COM²", "COM³",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9", "LPT¹", "LPT²", "LPT³":
		return fmt.Errorf("notes: %q is a reserved name on Windows", e)
	}
	return nil
}

func file(page string) string { return filepath.FromSlash(page) + ".md" }

// Tree lists a book's pages: Home first at the top, then by name
// (case-insensitive). Files and folders that are not valid page names are
// left out.
func (s Store) Tree(book string) ([]Page, error) {
	mu.Lock()
	defer mu.Unlock()
	r, d, err := s.open(book)
	if err != nil {
		return nil, err
	}
	r.Close()
	n := 0
	return list(d, "", &n), nil
}

func list(dir, prefix string, n *int) []Page {
	ents, _ := os.ReadDir(dir)
	byName := map[string]*Page{}
	var names []string
	for _, e := range ents {
		name, isFile := strings.CutSuffix(e.Name(), ".md")
		isFile = isFile && e.Type().IsRegular()
		if (!isFile && !e.IsDir()) || checkElem(name) != nil {
			continue
		}
		p := byName[name]
		if p == nil {
			if *n >= MaxPages {
				continue
			}
			*n++
			p = &Page{Path: path.Join(prefix, name), Name: name, Children: []Page{}}
			byName[name] = p
			names = append(names, name)
		}
		if isFile {
			p.HasText = true
		}
	}
	slices.SortFunc(names, func(a, b string) int {
		if prefix == "" && (a == Home) != (b == Home) {
			if a == Home {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	out := []Page{}
	for _, name := range names {
		p := byName[name]
		if fi, err := os.Stat(filepath.Join(dir, name)); err == nil && fi.IsDir() {
			p.Children = list(filepath.Join(dir, name), p.Path, n)
		}
		out = append(out, *p)
	}
	return out
}

// Load returns a page; a missing one is empty.
func (s Store) Load(book, page string) (Note, error) {
	if err := CheckName(page); err != nil {
		return Note{}, err
	}
	mu.Lock()
	defer mu.Unlock()
	r, d, err := s.open(book)
	if err != nil {
		return Note{}, err
	}
	defer r.Close()
	n := Note{Path: filepath.Join(d, file(page))}
	fi, err := r.Stat(file(page))
	if errors.Is(err, fs.ErrNotExist) {
		return n, nil
	}
	if err != nil {
		return n, err
	}
	if !fi.Mode().IsRegular() {
		return n, fmt.Errorf("notes: %s is not a file", n.Path)
	}
	if fi.Size() > MaxSize {
		return n, fmt.Errorf("notes: %s is larger than %d MB", n.Path, MaxSize>>20)
	}
	b, err := r.ReadFile(file(page))
	if err != nil {
		return n, err
	}
	n.Text, n.Mtime = string(b), fi.ModTime().UnixMilli()
	return n, nil
}

// Save writes a page atomically, owner-only (notes may contain anything).
// Empty text removes the page's file, and folders left empty by that.
func (s Store) Save(book, page, text string) (Note, error) {
	if err := CheckName(page); err != nil {
		return Note{}, err
	}
	if len(text) > MaxSize {
		return Note{}, fmt.Errorf("notes: larger than %d MB", MaxSize>>20)
	}
	mu.Lock()
	defer mu.Unlock()
	r, d, err := s.open(book)
	if err != nil {
		return Note{}, err
	}
	defer r.Close()
	return saveLocked(r, d, page, text)
}

// saveLocked is Save for the book r in folder d, with mu held.
func saveLocked(r *os.Root, d, page, text string) (Note, error) {
	p := filepath.Join(d, file(page))
	if text == "" {
		if err := r.Remove(file(page)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return Note{}, err
		}
		pruneDirs(r, path.Dir(page))
		return Note{Path: p}, nil
	}
	if err := write(r, page, text); err != nil {
		return Note{}, err
	}
	fi, err := r.Stat(file(page))
	if err != nil {
		return Note{}, err
	}
	return Note{Text: text, Path: p, Mtime: fi.ModTime().UnixMilli()}, nil
}

// write stores text as page through a temp file in the same folder.
func write(r *os.Root, page, text string) error {
	dir := filepath.Dir(file(page))
	if err := r.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b := make([]byte, 8)
	rand.Read(b)
	tmp := filepath.Join(dir, ".notes-"+hex.EncodeToString(b)+".md")
	f, err := r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer r.Remove(tmp) // fails harmlessly after the rename
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return r.Rename(tmp, file(page))
}

// pruneDirs removes dir and its parents (page paths) while they are empty.
func pruneDirs(r *os.Root, dir string) {
	for dir != "." && dir != "/" && dir != "" {
		if r.Remove(filepath.FromSlash(dir)) != nil {
			return // not empty (or gone)
		}
		dir = path.Dir(dir)
	}
}

// exists reports whether page has a file or a folder.
func exists(r *os.Root, page string) bool {
	_, err1 := r.Stat(file(page))
	_, err2 := r.Stat(filepath.FromSlash(page))
	return err1 == nil || err2 == nil
}

// Create makes a new page with its name as a heading; it is an error if the
// page exists (with a file or with subpages).
func (s Store) Create(book, page string) (Note, error) {
	if err := CheckName(page); err != nil {
		return Note{}, err
	}
	mu.Lock()
	defer mu.Unlock()
	r, d, err := s.open(book)
	if err != nil {
		return Note{}, err
	}
	defer r.Close()
	if exists(r, page) {
		return Note{}, fmt.Errorf("notes: page %q exists already", page)
	}
	return saveLocked(r, d, page, "# "+path.Base(page)+"\n\n")
}

// within reports whether page p lies inside page root (or is root),
// ignoring case: on Windows and macOS "design/X" is inside "Design".
func within(p, root string) bool {
	ps, rs := strings.Split(p, "/"), strings.Split(root, "/")
	if len(ps) < len(rs) {
		return false
	}
	for i, e := range rs {
		if !strings.EqualFold(ps[i], e) {
			return false
		}
	}
	return true
}

// depth is the number of elements of page path p.
func depth(p string) int { return strings.Count(p, "/") + 1 }

// subDepth returns how many levels of subpages page has (0: none).
func subDepth(r *os.Root, page string) int {
	dir := filepath.FromSlash(page)
	if fi, err := r.Stat(dir); err != nil || !fi.IsDir() {
		return 0
	}
	deepest := 0
	fs.WalkDir(r.FS(), page, func(p string, e fs.DirEntry, err error) error {
		if err == nil && p != page {
			deepest = max(deepest, strings.Count(strings.TrimPrefix(p, page), "/"))
		}
		return nil
	})
	return deepest
}

// same reports whether pages a and b are the same files on disk: a
// case-only rename on a case-insensitive file system. Every part of b (file,
// folder) that exists must be that part of a.
func same(r *os.Root, a, b string) bool {
	for _, name := range []func(string) string{file, filepath.FromSlash} {
		fb, err := r.Stat(name(b))
		if err != nil {
			continue
		}
		fa, err := r.Stat(name(a))
		if err != nil || !os.SameFile(fa, fb) {
			return false
		}
	}
	return true
}

// Rename moves a page with its subpages; the new name must be free and may
// not lie inside the page itself.
func (s Store) Rename(book, from, to string) error {
	if err := CheckName(from); err != nil {
		return err
	}
	if err := CheckName(to); err != nil {
		return err
	}
	if to == from || (within(to, from) && depth(to) > depth(from)) {
		return fmt.Errorf("notes: cannot move %q into itself", from)
	}
	mu.Lock()
	defer mu.Unlock()
	r, _, err := s.open(book)
	if err != nil {
		return err
	}
	defer r.Close()
	if !exists(r, from) {
		return fmt.Errorf("notes: page %q does not exist", from)
	}
	// On case-insensitive file systems "Design" and "design" are one name:
	// only refuse a target that is a different page. On case-sensitive ones
	// both may exist.
	if exists(r, to) && !(strings.EqualFold(to, from) && same(r, from, to)) {
		return fmt.Errorf("notes: page %q exists already", to)
	}
	if sub := subDepth(r, from); depth(to)+sub > MaxDepth {
		return fmt.Errorf("notes: pages nest at most %d deep: %q has %d levels of subpages", MaxDepth, from, sub)
	}
	if err := r.MkdirAll(filepath.Dir(file(to)), 0o700); err != nil {
		return err
	}
	if _, err := r.Stat(file(from)); err == nil {
		if err := r.Rename(file(from), file(to)); err != nil {
			return err
		}
	}
	if fi, err := r.Stat(filepath.FromSlash(from)); err == nil && fi.IsDir() {
		if err := r.Rename(filepath.FromSlash(from), filepath.FromSlash(to)); err != nil {
			return err
		}
	}
	pruneDirs(r, path.Dir(from))
	return nil
}

// Delete removes a page and its subpages.
func (s Store) Delete(book, page string) error {
	if err := CheckName(page); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	r, _, err := s.open(book)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := r.Remove(file(page)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := r.RemoveAll(filepath.FromSlash(page)); err != nil {
		return err
	}
	pruneDirs(r, path.Dir(page))
	return nil
}
