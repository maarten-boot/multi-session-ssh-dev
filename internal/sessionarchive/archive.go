// Package sessionarchive writes one session's data into a .zip or .tar.gz
// archive and reads it back: manifest.json (what the session was, what is
// in the archive, what was missing), then one folder per part. Reading
// never trusts the archive: names must be local paths (no absolute paths,
// no ".."), only regular files are unpacked, through os.Root.
package sessionarchive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"session-workbench/internal/config"
)

// Format is the archive layout's version; a newer one is refused.
const Format = 1

// ManifestName is the manifest's name in the archive.
const ManifestName = "manifest.json"

// The parts of an archive: each is a folder (or a file) in it.
const (
	PartNotes      = "notes"      // notes/…: the session's note pages
	PartDockerfile = "dockerfile" // Dockerfile
	PartQA         = "qa"         // qa/conversation.json, qa/log/qa-*.md
	PartRecordings = "recordings" // recordings/…: the Remote tab's recordings
	PartHistory    = "history"    // history/local, history/remote
	PartCopies     = "copies"     // working-copies/…: saved agent copies
	PartProject    = "project"    // project/…: the session's directory
)

// AlwaysParts go in every archive; DefaultParts are ticked in the export
// dialog; the others are offered unticked.
var (
	AlwaysParts  = []string{PartNotes, PartDockerfile}
	DefaultParts = []string{PartQA, PartRecordings, PartHistory}
	OtherParts   = []string{PartCopies, PartProject}
)

// Manifest describes an archive.
type Manifest struct {
	Format   int            `json:"format"`
	App      string         `json:"app"` // the exporting app's version
	Exported time.Time      `json:"exported"`
	Session  config.Session `json:"session"`
	Host     config.Host    `json:"host"`
	// Parts are the parts in the archive; Missing says what was asked for
	// but could not be added (e.g. the Remote history of an unreachable host).
	Parts   []string `json:"parts"`
	Missing []string `json:"missing,omitempty"`
	// DockerfileInRepo: the Dockerfile was the project's
	// .session-workbench/Dockerfile (else the app's copy).
	DockerfileInRepo bool `json:"dockerfile_in_repo,omitempty"`
	// Skipped counts what could not go in: symlinks and special files.
	Skipped int `json:"skipped,omitempty"`
}

// Has reports whether the archive holds part.
func (m Manifest) Has(part string) bool {
	for _, p := range m.Parts {
		if p == part {
			return true
		}
	}
	return false
}

// Kind is an archive format, from the file name: "zip" or "tar.gz".
func Kind(name string) (string, error) {
	n := strings.ToLower(name)
	switch {
	case strings.HasSuffix(n, ".zip"):
		return "zip", nil
	case strings.HasSuffix(n, ".tar.gz"), strings.HasSuffix(n, ".tgz"):
		return "tar.gz", nil
	}
	return "", fmt.Errorf("%s: the name must end in .zip or .tar.gz", filepath.Base(name))
}

// Writer adds files to an archive.
type Writer struct {
	zw      *zip.Writer
	tw      *tar.Writer
	gz      *gzip.Writer
	names   map[string]bool
	Skipped int
}

// NewWriter writes an archive of kind ("zip" or "tar.gz") to w.
func NewWriter(w io.Writer, kind string) (*Writer, error) {
	a := &Writer{names: map[string]bool{}}
	switch kind {
	case "zip":
		a.zw = zip.NewWriter(w)
	case "tar.gz":
		a.gz = gzip.NewWriter(w)
		a.tw = tar.NewWriter(a.gz)
	default:
		return nil, fmt.Errorf("unknown archive kind %q", kind)
	}
	return a, nil
}

// Add writes one file: name is slash-separated and relative.
func (a *Writer) Add(name string, mode fs.FileMode, mtime time.Time, size int64, r io.Reader) error {
	if !fs.ValidPath(name) || name == "." {
		return fmt.Errorf("bad name in archive: %q", name)
	}
	if a.names[name] {
		return fmt.Errorf("twice in the archive: %s", name)
	}
	a.names[name] = true
	perm := mode.Perm() & 0o755
	if perm&0o400 == 0 {
		perm |= 0o600
	}
	if a.zw != nil {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: mtime}
		h.SetMode(perm)
		w, err := a.zw.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, r)
		return err
	}
	if err := a.tw.WriteHeader(&tar.Header{Name: name, Mode: int64(perm), Size: size, ModTime: mtime,
		Typeflag: tar.TypeReg, Format: tar.FormatPAX}); err != nil {
		return err
	}
	n, err := io.Copy(a.tw, r)
	if err == nil && n != size {
		err = fmt.Errorf("%s changed while it was read", name)
	}
	return err
}

// AddDirEntry writes a directory (so that empty ones, like a new
// repository's .git/refs/heads, come back).
func (a *Writer) AddDirEntry(name string, mtime time.Time) error {
	if !fs.ValidPath(name) || name == "." {
		return fmt.Errorf("bad name in archive: %q", name)
	}
	if a.names[name+"/"] {
		return nil
	}
	a.names[name+"/"] = true
	if a.zw != nil {
		h := &zip.FileHeader{Name: name + "/", Modified: mtime}
		h.SetMode(fs.ModeDir | 0o755)
		_, err := a.zw.CreateHeader(h)
		return err
	}
	return a.tw.WriteHeader(&tar.Header{Name: name + "/", Mode: 0o755, ModTime: mtime, Typeflag: tar.TypeDir,
		Format: tar.FormatPAX})
}

// AddBytes writes a file from memory.
func (a *Writer) AddBytes(name string, b []byte) error {
	return a.Add(name, 0o600, time.Now(), int64(len(b)), bytes.NewReader(b))
}

// AddFile writes the file at src as name. Symlinks and special files are
// counted in Skipped, not written.
func (a *Writer) AddFile(name, src string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		a.Skipped++
		return nil
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	return a.Add(name, fi.Mode(), fi.ModTime(), fi.Size(), f)
}

// AddDir writes every regular file under dir as prefix/<relative path>;
// keep, when set, chooses the files (slash-separated relative names). A
// missing dir adds nothing and is not an error.
func (a *Writer) AddDir(prefix, dir string, keep func(rel string) bool) (int, error) {
	n := 0
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if keep != nil && !keep(rel) {
			return nil
		}
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return err
			}
			return a.AddDirEntry(prefix+"/"+rel, fi.ModTime())
		}
		if !d.Type().IsRegular() {
			a.Skipped++
			return nil
		}
		n++
		return a.AddFile(prefix+"/"+rel, p)
	})
	return n, err
}

// AddFiles writes the files rels (slash-separated, relative to dir) as
// prefix/<rel>; files gone since they were listed are left out.
func (a *Writer) AddFiles(prefix, dir string, rels []string) error {
	for _, rel := range rels {
		if d, ok := strings.CutSuffix(rel, "/"); ok {
			if err := a.AddDirEntry(prefix+"/"+d, time.Now()); err != nil {
				return err
			}
			continue
		}
		err := a.AddFile(prefix+"/"+rel, filepath.Join(dir, filepath.FromSlash(rel)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Close finishes the archive.
func (a *Writer) Close() error {
	if a.zw != nil {
		return a.zw.Close()
	}
	if err := a.tw.Close(); err != nil {
		return err
	}
	return a.gz.Close()
}

// Configure is applied to every git command (hides console windows on Windows).
var Configure = func(*exec.Cmd) {}

func gitCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "-c", "core.fsmonitor=false"}, args...)...)
	Configure(cmd)
	return cmd
}

// ProjectFiles lists what of dir goes into an archive: in a git work tree
// the tracked and untracked files git does not ignore, plus everything in
// .git when dir is the top of the repository (its directories too, as
// "name/": git needs the empty ones); without git every file. Names are
// slash-separated and relative to dir, sorted.
func ProjectFiles(ctx context.Context, dir string) ([]string, error) {
	var files []string
	out, err := gitCommand(ctx, dir, "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		// Not a repository (or no git): every file.
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, err := filepath.Rel(dir, p)
			files = append(files, filepath.ToSlash(rel))
			return err
		})
		sort.Strings(files)
		return files, err
	}
	seen := map[string]bool{}
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" && !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}
	if fi, err := os.Stat(filepath.Join(dir, ".git")); err == nil && fi.IsDir() {
		git := filepath.Join(dir, ".git")
		err := filepath.WalkDir(git, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(dir, p)
			if d.IsDir() {
				rel += "/"
			}
			files = append(files, filepath.ToSlash(rel))
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

// Archive is an archive opened for reading.
type Archive struct {
	Path     string
	Manifest Manifest
	kind     string
}

// MaxManifest bounds manifest.json (it holds config, not data).
const MaxManifest = 4 << 20

// Open reads an archive's manifest; the kind comes from its content.
func Open(name string) (*Archive, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	head := make([]byte, 4)
	_, err = io.ReadFull(f, head)
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("%s: not a session archive", filepath.Base(name))
	}
	a := &Archive{Path: name}
	switch {
	case bytes.HasPrefix(head, []byte("PK\x03\x04")):
		a.kind = "zip"
	case bytes.HasPrefix(head, []byte{0x1f, 0x8b}):
		a.kind = "tar.gz"
	default:
		return nil, fmt.Errorf("%s: neither a .zip nor a .tar.gz file", filepath.Base(name))
	}
	found := false
	err = a.Each(func(n string, _ fs.FileMode, size int64, r io.Reader) error {
		if n != ManifestName {
			return nil
		}
		if size > MaxManifest {
			return errors.New("manifest.json is too large")
		}
		found = true
		return json.NewDecoder(io.LimitReader(r, MaxManifest)).Decode(&a.Manifest)
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(name), err)
	}
	if !found {
		return nil, fmt.Errorf("%s: no manifest.json — not a Session Workbench session archive", filepath.Base(name))
	}
	if a.Manifest.Format < 1 || a.Manifest.Format > Format {
		return nil, fmt.Errorf("%s: archive format %d is not supported by this version (it knows %d)",
			filepath.Base(name), a.Manifest.Format, Format)
	}
	return a, nil
}

// Each calls fn for every regular file and directory (mode has
// fs.ModeDir, name without the trailing "/") in the archive, in archive
// order. Names that are not local paths and other entries are skipped.
func (a *Archive) Each(fn func(name string, mode fs.FileMode, size int64, r io.Reader) error) error {
	if a.kind == "zip" {
		zr, err := zip.OpenReader(a.Path)
		if err != nil {
			return err
		}
		defer zr.Close()
		for _, f := range zr.File {
			if f.Mode().IsDir() {
				if n := strings.TrimSuffix(f.Name, "/"); fs.ValidPath(n) && n != "." {
					if err := fn(n, fs.ModeDir|0o755, 0, bytes.NewReader(nil)); err != nil {
						return err
					}
				}
				continue
			}
			if !f.Mode().IsRegular() || !fs.ValidPath(f.Name) {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			err = fn(f.Name, f.Mode(), int64(f.UncompressedSize64), rc)
			rc.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}
	f, err := os.Open(a.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeDir {
			if n := strings.TrimSuffix(h.Name, "/"); fs.ValidPath(n) && n != "." {
				if err := fn(n, fs.ModeDir|0o755, 0, bytes.NewReader(nil)); err != nil {
					return err
				}
			}
			continue
		}
		if h.Typeflag != tar.TypeReg || !fs.ValidPath(h.Name) {
			continue
		}
		if err := fn(h.Name, fs.FileMode(h.Mode).Perm(), h.Size, tr); err != nil {
			return err
		}
	}
}

// Extract unpacks the files under prefix/ into dest (created, 0700),
// without the prefix. Existing files are kept unless replace is set. It
// returns how many files it wrote.
func (a *Archive) Extract(prefix, dest string, replace bool) (int, error) {
	n := 0
	var root *os.Root
	defer func() {
		if root != nil {
			root.Close()
		}
	}()
	err := a.Each(func(name string, mode fs.FileMode, _ int64, r io.Reader) error {
		rel, ok := strings.CutPrefix(name, prefix+"/")
		if !ok || rel == "" {
			return nil
		}
		if root == nil {
			if err := os.MkdirAll(dest, 0o700); err != nil {
				return err
			}
			var err error
			if root, err = os.OpenRoot(dest); err != nil {
				return err
			}
		}
		p := filepath.FromSlash(rel)
		if mode.IsDir() {
			return root.MkdirAll(p, 0o755)
		}
		if d := path.Dir(rel); d != "." {
			if err := root.MkdirAll(filepath.FromSlash(d), 0o700); err != nil {
				return err
			}
		}
		flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
		if replace {
			flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		}
		f, err := root.OpenFile(p, flags, mode.Perm()&0o755|0o600)
		if errors.Is(err, fs.ErrExist) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = io.Copy(f, r)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		n++
		return err
	})
	return n, err
}

// ReadFile returns one file of the archive (nil when it is not there),
// at most max bytes.
func (a *Archive) ReadFile(name string, max int64) ([]byte, error) {
	var b []byte
	err := a.Each(func(n string, _ fs.FileMode, size int64, r io.Reader) error {
		if n != name {
			return nil
		}
		if size > max {
			return fmt.Errorf("%s is too large", name)
		}
		var err error
		b, err = io.ReadAll(io.LimitReader(r, max))
		return err
	})
	return b, err
}
