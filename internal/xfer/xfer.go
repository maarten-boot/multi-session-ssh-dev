// Package xfer moves files to and from the agent host over ssh without
// rsync, for computers that have none (Windows): tar on the agent host
// (standard on Linux and macOS), Go's archive/tar here, through the same
// ssh options as everything else. Claude backups and working copies use it
// where rsync is missing.
//
// Everything that comes from the agent host is unpacked through os.Root:
// no absolute paths or "..", symlinks only with relative targets that stay
// inside, no hard links, devices or other special files.
package xfer

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"session-workbench/internal/remote"
)

// Configure is applied to every command (hides console windows on Windows).
var Configure = func(*exec.Cmd) {}

// UseRsync reports whether rsync is used here: it is installed, and
// SW_NO_RSYNC is not set (which forces the built-in transfer, for tests).
func UseRsync() bool {
	if os.Getenv("SW_NO_RSYNC") != "" {
		return false
	}
	_, err := exec.LookPath("rsync")
	return err == nil
}

// ssh runs script on the agent host under /bin/sh (not the login shell:
// text a profile prints would corrupt the tar stream), with stdin and
// stdout connected as given. It returns ssh's exit status and stderr.
func ssh(ctx context.Context, target, script string, stdin io.Reader, stdout io.Writer) (int, string, error) {
	if err := remote.CheckTarget(target); err != nil {
		return 0, "", err
	}
	args := append([]string{"-T", "-o", "BatchMode=yes"}, remote.SSHOptions()...)
	args = append(args, "--", target, "exec /bin/sh -c "+remote.Quote(script))
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin, cmd.Stdout = stdin, stdout
	var errb bytes.Buffer
	cmd.Stderr = &errb
	Configure(cmd)
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), errb.String(), nil
	}
	return 0, errb.String(), err
}

// Run runs script on the agent host and returns its stdout.
func Run(ctx context.Context, target, script string, stdin io.Reader) ([]byte, int, string, error) {
	var out bytes.Buffer
	code, stderr, err := ssh(ctx, target, script, stdin, &out)
	return out.Bytes(), code, stderr, err
}

// Pull runs script on the agent host, which writes a tar stream, and
// unpacks it into dest (created). Entries for which skip returns true are
// dropped (names are slash-separated, relative). It returns the script's
// exit status and stderr; unsafe entries are skipped and listed in skipped.
func Pull(ctx context.Context, target, script, dest string, skip func(string) bool) (code int, stderr string, skipped []string, err error) {
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return 0, "", nil, err
	}
	root, err := os.OpenRoot(dest)
	if err != nil {
		return 0, "", nil, err
	}
	defer root.Close()
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		s, err := Unpack(root, pr, skip)
		skipped = s
		if err == nil {
			io.Copy(io.Discard, pr) // tar pads after the end marker: read it all
		}
		pr.CloseWithError(err) // stop ssh if unpacking failed
		done <- err
	}()
	code, stderr, err = ssh(ctx, target, script, nil, pw)
	pw.Close()
	if uerr := <-done; err == nil && uerr != nil {
		err = fmt.Errorf("unpacking: %w", uerr)
	}
	return code, stderr, skipped, err
}

// Unpack extracts a tar stream into root. Files are replaced, nothing is
// deleted. Unsafe entries are skipped and returned by name.
func Unpack(root *os.Root, r io.Reader, skip func(string) bool) ([]string, error) {
	tr := tar.NewReader(r)
	var skipped []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return skipped, nil
		}
		if err != nil {
			return skipped, err
		}
		name, ok := cleanName(h.Name)
		if !ok {
			skipped = append(skipped, h.Name)
			continue
		}
		if name == "." || (skip != nil && skip(name)) {
			continue
		}
		if err := unpackOne(root, h, name, tr); err != nil {
			if !escapes(err) {
				return skipped, fmt.Errorf("%s: %w", name, err)
			}
			skipped = append(skipped, h.Name) // through a symlink out of the tree
		} else if !kept(h) {
			skipped = append(skipped, h.Name)
		}
	}
}

// kept says whether unpackOne writes entries of this type here.
func kept(h *tar.Header) bool {
	switch h.Typeflag {
	case tar.TypeDir, tar.TypeReg, tar.TypeRegA:
		return true
	case tar.TypeSymlink:
		return runtime.GOOS != "windows" && safeLink(path.Clean(strings.ReplaceAll(h.Name, `\`, "/")), h.Linkname)
	}
	return false // hard links, devices, fifos
}

// escapes: os.Root refused a path that leads out of the tree.
func escapes(err error) bool { return strings.Contains(err.Error(), "escapes from parent") }

func unpackOne(root *os.Root, h *tar.Header, name string, tr io.Reader) error {
	if !kept(h) {
		return nil
	}
	mtime := h.ModTime
	switch h.Typeflag {
	case tar.TypeDir:
		return root.MkdirAll(filepath.FromSlash(name), 0o700|fs.FileMode(h.Mode)&0o755)
	case tar.TypeReg, tar.TypeRegA:
		if err := writeFile(root, name, fs.FileMode(h.Mode)&0o777, tr); err != nil {
			return err
		}
		root.Chtimes(filepath.FromSlash(name), mtime, mtime)
	case tar.TypeSymlink:
		p := filepath.FromSlash(name)
		if dir := path.Dir(name); dir != "." {
			if err := root.MkdirAll(filepath.FromSlash(dir), 0o700); err != nil {
				return err
			}
		}
		if fi, err := root.Lstat(p); err == nil && !fi.IsDir() {
			root.Remove(p)
		}
		return root.Symlink(h.Linkname, p)
	}
	return nil
}

func writeFile(root *os.Root, name string, mode fs.FileMode, r io.Reader) error {
	p := filepath.FromSlash(name)
	if dir := path.Dir(name); dir != "." {
		if err := root.MkdirAll(filepath.FromSlash(dir), 0o700); err != nil {
			return err
		}
	}
	// A symlink in the way is replaced, not written through.
	if fi, err := root.Lstat(p); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		root.Remove(p)
	}
	if mode&0o600 != 0o600 {
		mode |= 0o600
	}
	f, err := root.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return root.Chmod(p, mode)
}

// cleanName makes a tar member name relative and slash-separated; false if
// it is absolute or leaves the tree.
func cleanName(n string) (string, bool) {
	n = strings.ReplaceAll(n, `\`, "/")
	if strings.HasPrefix(n, "/") || (len(n) > 1 && n[1] == ':') {
		return "", false
	}
	c := path.Clean(n)
	if c == ".." || strings.HasPrefix(c, "../") {
		return "", false
	}
	return c, true
}

// safeLink allows a symlink at name only to a relative target inside the tree.
func safeLink(name, target string) bool {
	if target == "" || strings.HasPrefix(target, "/") || strings.Contains(target, `\`) {
		return false
	}
	c := path.Clean(path.Join(path.Dir(name), target))
	return c != ".." && !strings.HasPrefix(c, "../")
}

// File is one entry to push: a slash-separated path relative to the source
// directory, and the mode to give it (0 = from the file).
type File struct {
	Path string
	Mode fs.FileMode
}

// Push sends files from src to the agent host as a tar stream on the stdin
// of script (which unpacks it). Missing files are skipped.
func Push(ctx context.Context, target, script, src string, files []File) (int, string, error) {
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(Pack(pw, src, files)) }()
	code, stderr, err := ssh(ctx, target, script, pr, io.Discard)
	pr.Close()
	return code, stderr, err
}

// Pack writes files from src as a tar stream.
func Pack(w io.Writer, src string, files []File) error {
	bw := bufio.NewWriterSize(w, 256<<10)
	tw := tar.NewWriter(bw)
	for _, f := range files {
		p := filepath.Join(src, filepath.FromSlash(f.Path))
		fi, err := os.Lstat(p)
		if err != nil {
			continue // deleted meanwhile
		}
		h, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			continue
		}
		h.Name = f.Path
		h.Uid, h.Gid, h.Uname, h.Gname = 0, 0, "", ""
		switch {
		case fi.Mode()&fs.ModeSymlink != 0:
			if h.Linkname, err = os.Readlink(p); err != nil {
				continue
			}
		case fi.Mode().IsRegular():
		case fi.IsDir():
			h.Name += "/"
		default:
			continue
		}
		if f.Mode != 0 && !fi.IsDir() && fi.Mode()&fs.ModeSymlink == 0 {
			h.Mode = int64(f.Mode.Perm())
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if fi.Mode().IsRegular() {
			r, err := os.Open(p)
			if err != nil {
				return err
			}
			_, err = io.CopyN(tw, r, h.Size)
			r.Close()
			if err != nil {
				return err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return bw.Flush()
}

// crcTable is the POSIX cksum polynomial (0x04C11DB7, not reflected).
var crcTable = func() [256]uint32 {
	var t [256]uint32
	for i := range t {
		c := uint32(i) << 24
		for range 8 {
			if c&0x80000000 != 0 {
				c = c<<1 ^ 0x04C11DB7
			} else {
				c <<= 1
			}
		}
		t[i] = c
	}
	return t
}()

// Cksum is what POSIX cksum prints for r: the CRC and the length.
func Cksum(r io.Reader) (uint32, int64, error) {
	var crc uint32
	var n int64
	buf := make([]byte, 64<<10)
	for {
		k, err := r.Read(buf)
		for _, b := range buf[:k] {
			crc = crc<<8 ^ crcTable[byte(crc>>24)^b]
		}
		n += int64(k)
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, n, err
		}
	}
	for l := n; l != 0; l >>= 8 {
		crc = crc<<8 ^ crcTable[byte(crc>>24)^byte(l)]
	}
	return ^crc, n, nil
}
