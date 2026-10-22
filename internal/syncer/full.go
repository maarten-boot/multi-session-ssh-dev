package syncer

// Full transfers, for the end of a work cycle and for an agent host that
// went away: Save copies the whole agent copy here, TakeFiles brings its
// files into the session directory, SendAll sends a directory in full.

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"session-workbench/internal/remote"
	"session-workbench/internal/xfer"
)

// SendAll makes the agent copy identical to src (dryRun: only report what
// would change), including the files .gitignore ignores, on both sides: it
// is "Send all" for the session directory and "Restore saved copy" for the
// folder Save fills. It also sends when Send would refuse (unfetched
// commits, uncommitted changes there); Plan.Agent says what is overwritten.
func SendAll(ctx context.Context, s Session, src string, dryRun bool) (Plan, error) {
	if err := remote.CheckTarget(s.Target); err != nil {
		return Plan{}, err
	}
	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		return Plan{}, fmt.Errorf("%s: no such folder", src)
	}
	if empty(src) {
		// Sending nothing would delete the whole agent copy.
		return Plan{}, fmt.Errorf("%s is empty; nothing to send", src)
	}
	agent, heads, err := probe(ctx, s)
	if err != nil {
		return Plan{}, err
	}
	from := s
	from.LocalPath = src
	p := Plan{Agent: agent, Changes: []Change{}}
	if agent.Repo && len(heads) > 0 {
		p.Agent.Unfetched = unknownCommits(ctx, from, heads)
	}
	return p, mirror(ctx, from, dryRun, true, &p)
}

func empty(dir string) bool {
	f, err := os.Open(dir)
	if err != nil {
		return true
	}
	defer f.Close()
	names, _ := f.Readdirnames(1)
	return len(names) == 0
}

// Save copies the agent copy in full (with .git and the files .gitignore
// ignores) to dest, which then holds exactly that: files gone there go
// here too. Symlinks that leave the copy, devices and other special files
// are left out; the note says so. progress (may be nil) gets the percentage
// done when rsync can tell (3.1 or newer), else nothing.
func Save(ctx context.Context, s Session, dest string, progress func(int)) (note string, err error) {
	if err := remote.CheckTarget(s.Target); err != nil {
		return "", err
	}
	agent, _, err := probe(ctx, s)
	if err != nil {
		return "", err
	}
	if !agent.Exists {
		return "", errors.New("there is no agent copy yet (Send first)")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	if !xfer.UseRsync() {
		return saveBuiltin(ctx, s, dest)
	}
	args := []string{"-a", "--delete", "--safe-links", "--no-devices", "--no-specials", "--timeout=120", "-e", remote.SSHCommand()}
	if progress != nil && rsyncProgress() {
		// The whole file list first, so the percentage is of everything.
		args = append(args, "--info=progress2", "--no-inc-recursive")
	} else {
		progress = nil
	}
	args = append(args, "--", s.Target+":"+rsyncPath(s.RemotePath)+"/", filepath.ToSlash(dest)+"/")
	cmd := exec.CommandContext(ctx, "rsync", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var errb bytes.Buffer
	cmd.Stderr = &errb
	Configure(cmd)
	if progress != nil {
		out, perr := cmd.StdoutPipe()
		if perr != nil {
			return "", perr
		}
		if err = cmd.Start(); err == nil {
			readProgress(out, progress)
			err = cmd.Wait()
		}
	} else {
		err = cmd.Run()
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 24 { // files vanished while copying (claude was writing)
		return "some files changed while saving; save again when claude is idle", nil
	}
	if err != nil {
		msg := lastLine(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("saving: rsync: %s", msg)
	}
	return "", nil
}

// rsyncProgress reports whether rsync here has --info=progress2 (3.1,
// protocol 31; macOS ships 2.6.9 or openrsync without it).
var rsyncProgress = sync.OnceValue(func() bool {
	out, err := exec.Command("rsync", "--version").Output()
	if err != nil {
		return false
	}
	m := regexp.MustCompile(`protocol version (\d+)`).FindSubmatch(out)
	n := 0
	if m != nil {
		n, _ = strconv.Atoi(string(m[1]))
	}
	return n >= 31 && !bytes.Contains(out, []byte("openrsync"))
})

var percent = regexp.MustCompile(`\s(\d{1,3})%\s`)

// readProgress passes the percentages of rsync's --info=progress2 lines
// (separated by \r) to progress, each new value once.
func readProgress(r io.Reader, progress func(int)) {
	sc := bufio.NewScanner(r)
	sc.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
			return i + 1, data[:i], nil
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	last := -1
	for sc.Scan() {
		if m := percent.FindSubmatch(append(sc.Bytes(), ' ')); m != nil {
			if n, _ := strconv.Atoi(string(m[1])); n != last && n <= 100 {
				last = n
				progress(n)
			}
		}
	}
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	return s[strings.LastIndex(s, "\n")+1:]
}

// saveBuiltin is Save without rsync: the agent copy as one tar stream into
// a new folder, which then replaces dest.
func saveBuiltin(ctx context.Context, s Session, dest string) (string, error) {
	tmp, old := dest+".new", dest+".old"
	os.RemoveAll(tmp)
	script := "cd " + remote.ShellPath(s.RemotePath) + " || exit 4\nexec tar -cf - ."
	code, stderr, skipped, err := xfer.Pull(ctx, s.Target, script, tmp, nil)
	if err == nil && code != 0 && !(code == 1 && strings.Contains(stderr, "file changed as we read it")) {
		err = fmt.Errorf("tar on the agent host, exit %d: %s", code, strings.TrimSpace(lastLine(stderr)))
	}
	if err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("saving: %w", err)
	}
	os.RemoveAll(old)
	if err := os.Rename(dest, old); err != nil && !errors.Is(err, os.ErrNotExist) {
		os.RemoveAll(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Rename(old, dest)
		return "", err
	}
	os.RemoveAll(old)
	switch len(skipped) {
	case 0:
		return "", nil
	case 1:
		return "1 unsafe entry left out: " + skipped[0], nil
	}
	return fmt.Sprintf("%d unsafe entries left out", len(skipped)), nil
}

// inGit reports whether a slash-separated path is or is inside a .git.
func inGit(rel string) bool {
	return slices.Contains(strings.Split(rel, "/"), ".git")
}

// TakeFiles copies the agent copy's files into the session directory
// (dryRun: only report what would change): new files and changed ones,
// never .git (at any depth); deletes: also delete files here the agent copy
// does not have (outside .git). Unlike Get it brings what the agent wrote
// unreviewed, and overwrites the host's own changes to the same files:
// Plan.Overwrites counts the changed or deleted ones with uncommitted
// changes here (git only).
// It is Get for a session directory that is not a git repository.
func TakeFiles(ctx context.Context, s Session, dryRun, deletes bool) (Plan, error) {
	if err := remote.CheckTarget(s.Target); err != nil {
		return Plan{}, err
	}
	agent, _, err := probe(ctx, s)
	if err != nil {
		return Plan{}, err
	}
	p := Plan{Agent: agent, Changes: []Change{}}
	if !agent.Exists {
		return p, errors.New("there is no agent copy yet (Send first)")
	}
	if !xfer.UseRsync() {
		err = takeBuiltin(ctx, s, dryRun, deletes, &p)
	} else {
		args := []string{"-a", "--itemize-changes", "--safe-links", "--no-devices", "--no-specials", "--exclude=.git",
			"-e", remote.SSHCommand()}
		if deletes {
			args = append(args, "--delete") // not what --exclude protects: .git stays
		}
		if dryRun {
			args = append(args, "--dry-run")
		}
		args = append(args, "--", s.Target+":"+rsyncPath(s.RemotePath)+"/", filepath.ToSlash(filepath.Clean(s.LocalPath))+"/")
		var out string
		if out, _, err = run(ctx, "", nil, "rsync", args...); err == nil {
			parseItemize(&p, out)
		}
	}
	if err != nil {
		return p, fmt.Errorf("taking the files: %w", err)
	}
	p.Overwrites = overwritten(ctx, s, p.Changes)
	p.Done = !dryRun
	return p, nil
}

// overwritten counts the files a take changes or deletes that have
// uncommitted changes here (staged or not; tracked files only). The paths
// are relative to the session directory, also in a repository's
// subdirectory (--relative); without git it is 0.
func overwritten(ctx context.Context, s Session, changes []Change) int {
	dirty := map[string]bool{}
	for _, staged := range []bool{false, true} {
		args := []string{"diff", "--name-only", "-z", "--relative", "--no-renames"}
		if staged {
			args = append(args, "--cached")
		}
		out, err := localGit(ctx, s, args...)
		if err != nil {
			return 0
		}
		for _, rel := range strings.Split(out, "\x00") {
			if rel != "" {
				dirty[rel] = true
			}
		}
	}
	n := 0
	for _, c := range changes {
		if c.Op != "new" && dirty[c.Path] {
			n++
		}
	}
	return n
}

// takeBuiltin is TakeFiles without rsync: files compared by cksum, then
// the agent copy as a tar stream of which only those are unpacked.
func takeBuiltin(ctx context.Context, s Session, dryRun, deletes bool, p *Plan) error {
	there, err := remoteSums(ctx, s, "find . -name .git -prune -o -type f -print0")
	if err != nil {
		return err
	}
	var gone []string
	if deletes {
		filepath.WalkDir(s.LocalPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && d.Name() == ".git" {
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(s.LocalPath, path)
			rel = filepath.ToSlash(rel)
			if !d.IsDir() && d.Name() != ".git" {
				if _, ok := there[rel]; !ok {
					gone = append(gone, rel)
				}
			}
			return nil
		})
	}
	for _, rel := range gone {
		p.Deleted++
		p.Changes = append(p.Changes, Change{Op: "deleted", Path: rel})
	}
	want := map[string]bool{}
	for rel, r := range there {
		if inGit(rel) {
			continue
		}
		l, err := localSum(filepath.Join(s.LocalPath, filepath.FromSlash(rel)))
		switch {
		case errors.Is(err, os.ErrNotExist):
			want[rel] = true
			p.New++
			p.Changes = append(p.Changes, Change{Op: "new", Path: rel})
		case err == nil && l != r:
			want[rel] = true
			p.Changed++
			p.Changes = append(p.Changes, Change{Op: "changed", Path: rel})
		}
	}
	slices.SortFunc(p.Changes, func(a, b Change) int { return strings.Compare(a.Path, b.Path) })
	if len(p.Changes) > maxListed {
		p.Changes, p.Truncated = p.Changes[:maxListed], true
	}
	if dryRun {
		return nil
	}
	if len(want) > 0 {
		// Folders on the way to a wanted file are unpacked too (their mode).
		parents := map[string]bool{}
		for rel := range want {
			for d := rel; strings.Contains(d, "/"); {
				d = d[:strings.LastIndex(d, "/")]
				parents[d] = true
			}
		}
		script := "cd " + remote.ShellPath(s.RemotePath) + " || exit 4\nexec tar -cf - --exclude=.git ."
		code, stderr, _, err := xfer.Pull(ctx, s.Target, script, s.LocalPath, func(name string) bool {
			return inGit(name) || (!want[name] && !parents[name])
		})
		if err == nil && code != 0 && !(code == 1 && strings.Contains(stderr, "file changed as we read it")) {
			err = fmt.Errorf("tar on the agent host, exit %d: %s", code, strings.TrimSpace(lastLine(stderr)))
		}
		if err != nil {
			return err
		}
	}
	if len(gone) > 0 {
		root, err := os.OpenRoot(s.LocalPath)
		if err != nil {
			return err
		}
		defer root.Close()
		for _, rel := range gone {
			if err := root.Remove(filepath.FromSlash(rel)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

// IsRepo reports whether the session directory is the top of a git
// repository, so Get can fetch commits; else TakeFiles is its Get.
func IsRepo(ctx context.Context, s Session) bool {
	return repoTop(ctx, s)
}

// SavedCount counts the files in a saved copy (for the dialogs).
func SavedCount(dir string) int {
	n := 0
	filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}
