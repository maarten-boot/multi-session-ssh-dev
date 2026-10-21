package syncer

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"session-workbench/internal/remote"
	"session-workbench/internal/xfer"
)

// Send without rsync (Windows), with the same result as
// rsync -a --delete --filter=':- .gitignore': the agent copy becomes
// identical to the host directory, files .gitignore ignores are neither
// sent nor deleted there. Files are compared by POSIX cksum (CRC and size),
// computed on both sides; new and changed ones go over as a tar stream,
// deleted ones are removed there.

type localFile struct {
	mode fs.FileMode // 0: from the file
	link bool
}

// errNoGit: git is not installed here, so .gitignore cannot be applied.
var errNoGit = errors.New("git is not installed")

// ignoreChecker decides which paths (slash-separated, relative to the
// session directory) are ignored here.
//
// At the top of a repository it asks that repository, as git ls-files
// --exclude-standard does. Anywhere else (a subdirectory of a repository,
// possibly one the repository ignores, or no repository at all) it applies
// only the .gitignore files inside the session directory, like rsync's
// filter: git check-ignore runs against an empty temporary repository whose
// work tree is the session directory. That needs git; without it every
// path counts as not ignored and errNoGit is returned.
type ignoreChecker struct {
	s   Session
	top bool // the session directory is a repository's top
}

func (ic ignoreChecker) ignored(ctx context.Context, paths []string) (map[string]bool, error) {
	res := map[string]bool{}
	if len(paths) == 0 {
		return res, nil
	}
	if _, err := exec.LookPath("git"); err != nil {
		return res, errNoGit
	}
	env := append(os.Environ(), "LC_ALL=C")
	if !ic.top {
		tmp, err := os.MkdirTemp("", "sw-ignore-")
		if err != nil {
			return res, err
		}
		defer os.RemoveAll(tmp)
		if _, _, err := run(ctx, "", nil, "git", "init", "-q", "--bare", tmp); err != nil {
			return res, err
		}
		env = append(env, "GIT_DIR="+tmp, "GIT_WORK_TREE="+ic.s.LocalPath)
	}
	cmd := exec.CommandContext(ctx, "git", "-c", "core.fsmonitor=false", "check-ignore", "--no-index", "-z", "--stdin")
	cmd.Dir = ic.s.LocalPath
	cmd.Env = env
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\x00") + "\x00")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	Configure(cmd)
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 { // nothing ignored
		return res, nil
	}
	if err != nil {
		return res, fmt.Errorf("git check-ignore: %v: %s", err, strings.TrimSpace(errb.String()))
	}
	for _, rel := range strings.Split(out.String(), "\x00") {
		if rel != "" {
			res[rel] = true
		}
	}
	return res, nil
}

// localFiles lists what a send copies: at the top of a git repository the
// files git does not ignore plus .git itself, else every file that no
// .gitignore inside the session directory ignores; all: every file.
func localFiles(ctx context.Context, s Session, ic ignoreChecker, all bool) (map[string]localFile, error) {
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
	if all {
		err := filepath.WalkDir(s.LocalPath, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				rel, _ := filepath.Rel(s.LocalPath, p)
				add(filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else if ic.top {
		out, err := localGit(ctx, s, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
		if err != nil {
			return nil, err
		}
		for _, rel := range strings.Split(out, "\x00") {
			if rel != "" {
				add(rel)
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
	} else {
		var all []string
		err := filepath.WalkDir(s.LocalPath, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(s.LocalPath, p)
			all = append(all, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
		ign, err := ic.ignored(ctx, all)
		if err != nil && !errors.Is(err, errNoGit) {
			return nil, err
		}
		for _, rel := range all {
			if !ign[rel] {
				add(rel)
			}
		}
		if len(files) == 0 && len(all) > 0 {
			// Sending nothing would delete the whole agent copy.
			return nil, fmt.Errorf("every file in %s is ignored by a .gitignore there; nothing to send", s.LocalPath)
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
	return files, nil
}

type sum struct {
	crc  uint32
	size int64
}

// remoteSums lists the agent copy's files the same way, with their cksum.
// list is a shell command printing NUL-separated paths instead ("": what a
// send would copy).
func remoteSums(ctx context.Context, s Session, list string) (map[string]sum, error) {
	dir := remote.ShellPath(s.RemotePath)
	if list == "" {
		list = `if git rev-parse --git-dir >/dev/null 2>&1; then
    git -c core.fsmonitor=false ls-files -z --cached --others --exclude-standard
    [ -d .git ] && find .git -type f -print0
  else
    find . -type f -print0
  fi`
	}
	script := `cd ` + dir + ` 2>/dev/null || exit 0
{ ` + list + `; } | xargs -0 cksum 2>/dev/null
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

// sendBuiltin fills p's changes and, unless dryRun, applies them. all:
// files .gitignore ignores too, on both sides.
func sendBuiltin(ctx context.Context, s Session, dryRun, all bool, p *Plan) error {
	ic := ignoreChecker{s: s, top: repoTop(ctx, s)}
	local, err := localFiles(ctx, s, ic, all)
	if err != nil {
		return err
	}
	list := ""
	if all {
		list = "find . -type f -print0"
	}
	there, err := remoteSums(ctx, s, list)
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
	var gone []string
	for rel := range there {
		if _, ok := local[rel]; !ok {
			gone = append(gone, rel)
		}
	}
	// What is ignored here stays on the agent host (rsync's filter protects
	// it too): the agent copy may lack the .git or .gitignore that hid it
	// from the listing there.
	ign, err := map[string]bool{}, error(nil)
	if !all {
		ign, err = ic.ignored(ctx, gone)
	}
	if errors.Is(err, errNoGit) && hasGitignore(s.LocalPath) {
		gone = nil // cannot tell what is ignored: delete nothing
		addNote(p, "git is not installed here, so files deleted here were not deleted on the agent host")
	} else if err != nil && !errors.Is(err, errNoGit) {
		return err
	}
	var deleted []string
	for _, rel := range gone {
		if !ign[rel] {
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

// hasGitignore reports whether dir contains a .gitignore anywhere.
func hasGitignore(dir string) bool {
	found := false
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == ".gitignore" {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func addNote(p *Plan, note string) {
	if p.Note != "" {
		p.Note += "; "
	}
	p.Note += note
}
