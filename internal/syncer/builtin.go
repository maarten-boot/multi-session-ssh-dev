package syncer

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"session-workbench/internal/remote"
	"session-workbench/internal/xfer"
)

// Send without rsync (Windows), with the same result: the agent copy
// becomes identical to the host directory, files git ignores are neither
// sent nor deleted there. Files are compared by POSIX cksum (CRC and size),
// computed on both sides; new and changed ones go over as a tar stream,
// deleted ones are removed there.

type localFile struct {
	mode fs.FileMode // 0: from the file
	link bool
}

// localFiles lists what a send copies: in a git repository the files git
// does not ignore plus .git itself, else every file.
func localFiles(ctx context.Context, s Session) (map[string]localFile, error) {
	files := map[string]localFile{}
	add := func(rel string) {
		fi, err := os.Lstat(filepath.Join(s.LocalPath, filepath.FromSlash(rel)))
		if err != nil {
			return // listed by git but deleted
		}
		switch {
		case fi.Mode()&fs.ModeSymlink != 0:
			files[rel] = localFile{link: true}
		case fi.Mode().IsRegular():
			files[rel] = localFile{}
		}
	}
	out, err := localGit(ctx, s, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		// Not a repository: everything.
		err := filepath.WalkDir(s.LocalPath, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(s.LocalPath, p)
			add(filepath.ToSlash(rel))
			return nil
		})
		return files, err
	}
	for _, rel := range strings.Split(out, "\x00") {
		if rel != "" {
			add(rel)
		}
	}
	// Windows files have no executable bit: take it from git's index.
	if runtime.GOOS == "windows" {
		if idx, err := localGit(ctx, s, "ls-files", "-z", "-s"); err == nil {
			for _, rec := range strings.Split(idx, "\x00") {
				meta, rel, ok := strings.Cut(rec, "\t")
				if f, in := files[rel]; ok && in && strings.HasPrefix(meta, "100755 ") {
					f.mode = 0o755
					files[rel] = f
				}
			}
		}
	}
	gitDir := filepath.Join(s.LocalPath, ".git")
	if fi, err := os.Stat(gitDir); err == nil && fi.IsDir() {
		filepath.WalkDir(gitDir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(s.LocalPath, p)
			add(filepath.ToSlash(rel))
			return nil
		})
	}
	return files, nil
}

type sum struct {
	crc  uint32
	size int64
}

// remoteSums lists the agent copy's files the same way, with their cksum.
func remoteSums(ctx context.Context, s Session) (map[string]sum, error) {
	dir := remote.ShellPath(s.RemotePath)
	script := `cd ` + dir + ` 2>/dev/null || exit 0
{ if git rev-parse --git-dir >/dev/null 2>&1; then
    git -c core.fsmonitor=false ls-files -z --cached --others --exclude-standard
    [ -d .git ] && find .git -type f -print0
  else
    find . -type f -print0
  fi; } | xargs -0 cksum 2>/dev/null
exit 0`
	out, code, stderr, err := xfer.Run(ctx, s.Target, script, nil)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("listing the agent copy: exit %d: %s", code, strings.TrimSpace(stderr))
	}
	sums := map[string]sum{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		f := strings.SplitN(sc.Text(), " ", 3)
		if len(f) != 3 { // cksum of empty input (no name)
			continue
		}
		crc, err1 := strconv.ParseUint(f[0], 10, 32)
		size, err2 := strconv.ParseInt(f[1], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		sums[strings.TrimPrefix(f[2], "./")] = sum{uint32(crc), size}
	}
	return sums, nil
}

func localSum(path string) (sum, error) {
	f, err := os.Open(path)
	if err != nil {
		return sum{}, err
	}
	defer f.Close()
	crc, n, err := xfer.Cksum(f)
	return sum{crc, n}, err
}

// sendBuiltin fills p's changes and, unless dryRun, applies them.
func sendBuiltin(ctx context.Context, s Session, dryRun bool, p *Plan) error {
	local, err := localFiles(ctx, s)
	if err != nil {
		return err
	}
	there, err := remoteSums(ctx, s)
	if err != nil {
		return err
	}
	var changes []Change
	var push []xfer.File
	for rel, lf := range local {
		if lf.link {
			push = append(push, xfer.File{Path: rel}) // cheap: always resent, not listed
			continue
		}
		r, ok := there[rel]
		if ok {
			l, err := localSum(filepath.Join(s.LocalPath, filepath.FromSlash(rel)))
			if err != nil || l == r {
				continue
			}
			changes = append(changes, Change{Op: "changed", Path: rel})
		} else {
			changes = append(changes, Change{Op: "new", Path: rel})
		}
		push = append(push, xfer.File{Path: rel, Mode: lf.mode})
	}
	var deleted []string
	for rel := range there {
		if _, ok := local[rel]; !ok {
			deleted = append(deleted, rel)
			changes = append(changes, Change{Op: "deleted", Path: rel})
		}
	}
	slices.SortFunc(changes, func(a, b Change) int { return strings.Compare(a.Path, b.Path) })
	for _, c := range changes {
		switch c.Op {
		case "new":
			p.New++
		case "changed":
			p.Changed++
		case "deleted":
			p.Deleted++
		}
		if len(p.Changes) < maxListed {
			p.Changes = append(p.Changes, c)
		} else {
			p.Truncated = true
		}
	}
	if dryRun {
		return nil
	}
	dir := remote.ShellPath(s.RemotePath)
	if len(push) > 0 || !p.Agent.Exists {
		code, stderr, err := xfer.Push(ctx, s.Target, "mkdir -p "+dir+" && cd "+dir+" && exec tar -xf -", s.LocalPath, push)
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("unpacking on the agent host: exit %d: %s", code, strings.TrimSpace(stderr))
		}
	}
	if len(deleted) > 0 {
		list := strings.Join(deleted, "\x00") + "\x00"
		_, code, stderr, err := xfer.Run(ctx, s.Target, "cd "+dir+" && xargs -0 rm -f --", strings.NewReader(list))
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("deleting on the agent host: exit %d: %s", code, strings.TrimSpace(stderr))
		}
	}
	return nil
}
