// Package share implements the "live share" file access mode
// (docs/CONNECTIVITY.md, mode B): the app serves a session directory over
// SFTP through its own ssh connection, and the agent host mounts it with
// `sshfs -o passive`. The agent host never logs in to the host and needs
// no credentials for it.
//
// Every operation goes through os.Root, so paths cannot leave the session
// directory, also not via symlinks or "..". Symlinks may only be created
// with relative targets that stay inside it (an agent could otherwise plant
// "README" -> ~/.bashrc for the user to edit on the host); hard links are
// refused.
package share

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"syscall"

	"github.com/pkg/sftp"
)

// Server serves one directory.
type Server struct {
	root *os.Root
}

// NewServer opens dir as the root of the share.
func NewServer(dir string) (*Server, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &Server{root: r}, nil
}

func (s *Server) Close() error { return s.root.Close() }

// Serve speaks SFTP on rwc until the client goes away.
func (s *Server) Serve(rwc io.ReadWriteCloser) error {
	return s.serve(rwc, func() {})
}

var errEscape = errors.New("share: outside the shared directory")

// name maps a client path ("/a/b", relative to the share) to a name for
// os.Root. os.Root itself rejects anything that would leave the root.
func name(p string) string {
	c := path.Clean("/" + p)
	if c == "/" {
		return "."
	}
	return c[1:]
}

// --- reading and writing ---

func (s *Server) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	return s.root.Open(name(r.Filepath))
}

func (s *Server) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	return s.open(r)
}

// OpenFile handles opens for reading and writing at once.
func (s *Server) OpenFile(r *sftp.Request) (sftp.WriterAtReaderAt, error) {
	return s.open(r)
}

func (s *Server) open(r *sftp.Request) (*os.File, error) {
	pf := r.Pflags()
	flags := 0
	switch {
	case pf.Read && pf.Write:
		flags = os.O_RDWR
	case pf.Write:
		flags = os.O_WRONLY
	default:
		flags = os.O_RDONLY
	}
	if pf.Append {
		flags |= os.O_APPEND
	}
	if pf.Creat {
		flags |= os.O_CREATE
	}
	if pf.Trunc {
		flags |= os.O_TRUNC
	}
	if pf.Excl {
		flags |= os.O_EXCL
	}
	mode := fs.FileMode(0o644)
	if r.AttrFlags().Permissions {
		mode = fs.FileMode(r.Attributes().Mode) & fs.ModePerm
	}
	return s.root.OpenFile(name(r.Filepath), flags, mode)
}

// --- commands ---

func (s *Server) Filecmd(r *sftp.Request) error {
	n := name(r.Filepath)
	switch r.Method {
	case "Setstat":
		return s.setstat(r, n)
	case "Rename":
		// Plain SFTP rename must not overwrite an existing file.
		if _, err := s.root.Lstat(name(r.Target)); err == nil {
			return os.ErrExist
		}
		return s.root.Rename(n, name(r.Target))
	case "Rmdir":
		fi, err := s.root.Lstat(n)
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return syscall.ENOTDIR
		}
		return s.root.Remove(n)
	case "Mkdir":
		mode := fs.FileMode(0o755)
		if r.AttrFlags().Permissions {
			mode = fs.FileMode(r.Attributes().Mode) & fs.ModePerm
		}
		return s.root.Mkdir(n, mode)
	case "Remove":
		fi, err := s.root.Lstat(n)
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return syscall.EISDIR
		}
		return s.root.Remove(n)
	case "Symlink":
		// r.Filepath is the link's target (as given), r.Target the link.
		link := name(r.Target)
		if !insideRoot(link, r.Filepath) {
			return errEscape
		}
		return s.root.Symlink(r.Filepath, link)
	case "Link":
		return errors.New("share: hard links are not allowed")
	}
	return sftp.ErrSSHFxOpUnsupported
}

// PosixRename replaces the destination (sshfs uses it for atomic saves).
func (s *Server) PosixRename(r *sftp.Request) error {
	return s.root.Rename(name(r.Filepath), name(r.Target))
}

// insideRoot reports whether a symlink at link (root-relative) with the
// given target resolves inside the root, without following anything.
func insideRoot(link, target string) bool {
	if target == "" || path.IsAbs(target) || strings.Contains(target, "\x00") {
		return false
	}
	// Resolve relative to the root; a rooted path would hide the escape
	// (path.Clean("/..") is "/").
	full := path.Join(path.Dir(link), target)
	return full != ".." && !strings.HasPrefix(full, "../")
}

func (s *Server) setstat(r *sftp.Request, n string) error {
	flags, attrs := r.AttrFlags(), r.Attributes()
	if flags.Permissions {
		if err := s.root.Chmod(n, fs.FileMode(attrs.Mode)&(fs.ModePerm|fs.ModeSticky)); err != nil {
			return err
		}
	}
	if flags.Size {
		f, err := s.root.OpenFile(n, os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		err = f.Truncate(int64(attrs.Size))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	if flags.Acmodtime {
		if err := s.root.Chtimes(n, unixTime(attrs.Atime), unixTime(attrs.Mtime)); err != nil {
			return err
		}
	}
	// Ownership changes (UidGid) are ignored: files belong to the host user.
	return nil
}

// --- listing ---

func (s *Server) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	n := name(r.Filepath)
	switch r.Method {
	case "List":
		f, err := s.root.Open(n)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		entries, err := f.Readdir(-1)
		if err != nil {
			return nil, err
		}
		return lister(entries), nil
	case "Stat":
		fi, err := s.root.Stat(n)
		if err != nil {
			return nil, err
		}
		return lister{fi}, nil
	case "Readlink":
		t, err := s.root.Readlink(n)
		if err != nil {
			return nil, err
		}
		return lister{linkInfo(t)}, nil
	}
	return nil, sftp.ErrSSHFxOpUnsupported
}

func (s *Server) Lstat(r *sftp.Request) (sftp.ListerAt, error) {
	fi, err := s.root.Lstat(name(r.Filepath))
	if err != nil {
		return nil, err
	}
	return lister{fi}, nil
}

// Readlink returns a link's target as stored.
func (s *Server) Readlink(p string) (string, error) {
	return s.root.Readlink(name(p))
}

// RealPath canonicalizes a client path within the share.
func (s *Server) RealPath(p string) (string, error) {
	return path.Clean("/" + p), nil
}

type lister []os.FileInfo

func (l lister) ListAt(out []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(out, l[offset:])
	if int(offset)+n >= len(l) {
		return n, io.EOF
	}
	return n, nil
}
