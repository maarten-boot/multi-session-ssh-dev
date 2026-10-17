// Package gitstatus reads `git status` for a session directory so the Files
// tab can mark changed files. It only reads: no index lock is taken (so it
// never interferes with the user's own git commands).
//
// The repository may be shared with the agent host, which can then write
// its .git/config. git status runs programs named there (fsmonitor, and
// clean filters for files whose timestamps changed), so the repository's
// own config is checked first and status is skipped when it names any; see
// docs/CONNECTIVITY.md. Global and system config are the user's own and
// are trusted (e.g. git-lfs).
package gitstatus

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// File codes.
const (
	Modified  = "M"
	Added     = "A"
	Deleted   = "D"
	Renamed   = "R"
	Untracked = "?"
	Conflict  = "C"
	Ignored   = "I"
)

type Status struct {
	Repo bool `json:"repo"` // false: not inside a git work tree (or git missing)
	// Skipped explains why no status was read: the repository's own config
	// names programs that git status would run.
	Skipped  string `json:"skipped"`
	Branch   string `json:"branch"`
	Detached bool   `json:"detached"`
	Upstream string `json:"upstream"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	// Files maps paths relative to the session directory ("/"-separated)
	// to a code. Untracked and ignored directories appear as one entry.
	Files map[string]string `json:"files"`
	// Dirs maps directories containing changes to the strongest code below
	// them: C (conflict) > M (anything changed) > ? (only untracked).
	Dirs map[string]string `json:"dirs"`
}

// Configure is applied to every git command; the hook allows hiding the
// console window on Windows.
var Configure = func(*exec.Cmd) {}

func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{
		"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.quotePath=false",
	}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	Configure(cmd)
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("git %s: %s", args[0], msg)
		}
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out, nil
}

// GitDir finds the git directory of the repository containing dir, or ""
// outside a repository. It does not depend on the worktree: a planted
// core.worktree (pointing elsewhere, or nowhere) makes git refuse most
// commands, which must not hide the repository from the checks. So when git
// cannot say, it looks for .git upwards itself (a directory, or a "gitdir:"
// file for linked worktrees and submodules); nothing runs.
func GitDir(ctx context.Context, dir string) (string, error) {
	// A missing session directory is not in its parent's repository.
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	out, err := git(ctx, dir, "rev-parse", "--absolute-git-dir")
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	if strings.Contains(err.Error(), "not a git repository") {
		return "", nil
	}
	for d := dir; ; {
		p := filepath.Join(d, ".git")
		if fi, err := os.Stat(p); err == nil {
			if fi.IsDir() {
				return p, nil
			}
			if b, err := readSmall(p); err == nil {
				if rest, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:"); ok {
					g := strings.TrimSpace(rest)
					if !filepath.IsAbs(g) {
						g = filepath.Join(d, g)
					}
					return filepath.Clean(g), nil
				}
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", err // git failed and there is no .git: report git's error
		}
		d = parent
	}
}

// commonDir is where a linked worktree's shared config and hooks live.
func commonDir(gitDir string) string {
	if b, err := readSmall(filepath.Join(gitDir, "commondir")); err == nil {
		c := strings.TrimSpace(string(b))
		if !filepath.IsAbs(c) {
			c = filepath.Join(gitDir, c)
		}
		return filepath.Clean(c)
	}
	return gitDir
}

// readSmall reads a small regular file (a .git file, commondir, HEAD). Other
// kinds of file are refused: opening a FIFO the agent host planted would
// block forever.
func readSmall(p string) ([]byte, error) {
	if fi, err := os.Stat(p); err != nil {
		return nil, err
	} else if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file", p)
	}
	f, err := os.OpenFile(p, os.O_RDONLY|oNonblock, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 64<<10))
}

// configRec is one setting from the repository's own config.
type configRec struct {
	File  string // the config file it is set in
	Key   string // as git lists it: section and key lower-cased
	Value string
	Bare  bool // set without "= value" (true for booleans)
}

// configError is a config file git cannot read.
type configError struct {
	File string
	Err  error
}

// repoConfig reads the repository's own config: the shared config file and,
// when extensions.worktreeConfig is set, the worktree's config.worktree,
// without includes. It reads the files directly, from outside the
// repository, so neither includes nor core.worktree come into play; nothing
// runs. Missing files are skipped; files git cannot parse (or that are not
// regular files) are returned in bad, the others are still read. err is set
// only when ctx ends.
func repoConfig(ctx context.Context, gitDir string) (recs []configRec, bad []configError, err error) {
	read := func(f string) {
		fi, err := os.Stat(f)
		if err != nil {
			return
		}
		if !fi.Mode().IsRegular() {
			bad = append(bad, configError{f, errors.New("not a regular file")})
			return
		}
		out, err := git(ctx, os.TempDir(), "config", "--file", f, "--no-includes", "--list", "-z")
		if err != nil {
			if !strings.Contains(err.Error(), "exit status 1") { // 1: empty file
				bad = append(bad, configError{f, err})
			}
			return
		}
		for _, rec := range strings.Split(string(out), "\x00") {
			if rec != "" {
				k, v, ok := strings.Cut(rec, "\n")
				recs = append(recs, configRec{File: f, Key: k, Value: v, Bare: !ok})
			}
		}
	}
	read(filepath.Join(commonDir(gitDir), "config"))
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	// git reads config.worktree only with the extension on; when the main
	// config cannot be read, read it anyway rather than miss something.
	worktreeConfig := len(bad) > 0
	for _, r := range recs {
		if strings.ToLower(r.Key) == "extensions.worktreeconfig" {
			worktreeConfig = isTrue(r)
		}
	}
	if worktreeConfig {
		read(filepath.Join(gitDir, "config.worktree"))
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
	}
	return recs, bad, nil
}

// isTrue interprets a config value as git does for booleans.
func isTrue(r configRec) bool {
	if r.Bare {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(r.Value)) {
	case "true", "yes", "on":
		return true
	case "false", "no", "off", "":
		return false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(r.Value), 0, 64)
	return err == nil && n != 0
}

// RepoConfig lists the repository's own config (see repoConfig) as
// "key\nvalue" records. A config file git cannot read is an error.
func RepoConfig(ctx context.Context, gitDir string) ([]string, error) {
	recs, bad, err := repoConfig(ctx, gitDir)
	if err != nil {
		return nil, err
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("%s: %w", bad[0].File, bad[0].Err)
	}
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		if r.Bare {
			out = append(out, r.Key)
		} else {
			out = append(out, r.Key+"\n"+r.Value)
		}
	}
	return out, nil
}

// headBranch reads the branch from the git directory's HEAD file, for when
// git refuses to run (core.worktree planted): the branch name, or the short
// commit hash and detached.
func headBranch(gitDir string) (branch string, detached bool) {
	b, err := readSmall(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", false
	}
	head := strings.TrimSpace(string(b))
	if ref, ok := strings.CutPrefix(head, "ref:"); ok {
		ref = strings.TrimSpace(ref)
		return strings.TrimPrefix(ref, "refs/heads/"), false
	}
	if len(head) > 7 {
		head = head[:7]
	}
	if head == "" {
		return "HEAD", true
	}
	return head, true
}

// Get returns the status of dir. A directory outside any repository, or a
// missing git, is not an error: Repo is false.
func Get(ctx context.Context, dir string) (Status, error) {
	st := Status{Files: map[string]string{}, Dirs: map[string]string{}}
	if _, err := exec.LookPath("git"); err != nil {
		return st, nil
	}
	gd, err := GitDir(ctx, dir)
	if err != nil || gd == "" {
		return st, err
	}
	st.Repo = true
	if reason, err := unsafeConfig(ctx, gd); err != nil {
		return st, err
	} else if reason != "" {
		st.Skipped = reason
		st.Branch, st.Detached = headBranch(gd)
		return st, nil
	}
	prefix, err := git(ctx, dir, "rev-parse", "--show-prefix")
	if err != nil {
		return st, err
	}
	// Submodules are not entered: each has its own config to vet.
	out, err := git(ctx, dir, "status", "--porcelain=v2", "-z", "--branch",
		"--untracked-files=normal", "--ignored=matching", "--ignore-submodules=all", "--", ".")
	if err != nil {
		return st, err
	}
	parse(&st, out, strings.TrimSpace(string(prefix)))
	return st, nil
}

// unsafeConfig returns a reason if the repository's own config (not global
// or system config) sets something git status would execute, that pulls in
// other config, or that points git at another directory. Reading config
// runs nothing.
func unsafeConfig(ctx context.Context, gitDir string) (string, error) {
	recs, bad, err := repoConfig(ctx, gitDir)
	if err != nil {
		return "", err
	}
	if len(bad) > 0 { // what git would make of it is unknown
		return "repository config cannot be read (" + filepath.Base(bad[0].File) + ")", nil
	}
	for _, rec := range recs {
		key := rec.Key
		k := strings.ToLower(key)
		switch {
		case strings.HasPrefix(k, "filter."), // clean/smudge/process programs
			strings.HasPrefix(k, "include."), strings.HasPrefix(k, "includeif."), // more config
			k == "core.fsmonitor",
			k == "core.worktree": // status would describe another directory
			return "repository config sets " + key, nil
		}
	}
	return "", nil
}

// parse reads `git status --porcelain=v2 -z --branch` output. Paths in it
// are relative to the repository root; prefix is the session directory's
// position in the repository ("sub/dir/" or "").
func parse(st *Status, out []byte, prefix string) {
	recs := strings.Split(string(out), "\x00")
	for i := 0; i < len(recs); i++ {
		r := recs[i]
		if r == "" {
			continue
		}
		switch r[0] {
		case '#':
			header(st, r)
		case '1': // 1 XY sub mH mI mW hH hI path
			if f := strings.SplitN(r, " ", 9); len(f) == 9 {
				st.add(prefix, f[8], code(f[1]))
			}
		case '2': // 2 XY sub mH mI mW hH hI Xscore path, then origPath as its own record
			if f := strings.SplitN(r, " ", 10); len(f) == 10 {
				c := code(f[1])
				if c == Modified {
					c = Renamed
				}
				st.add(prefix, f[9], c)
			}
			i++ // skip origPath
		case 'u': // u XY sub m1 m2 m3 mW h1 h2 h3 path
			if f := strings.SplitN(r, " ", 11); len(f) == 11 {
				st.add(prefix, f[10], Conflict)
			}
		case '?':
			st.add(prefix, r[2:], Untracked)
		case '!':
			st.add(prefix, r[2:], Ignored)
		}
	}
}

func header(st *Status, r string) {
	f := strings.Fields(r)
	if len(f) < 3 {
		return
	}
	switch f[1] {
	case "branch.head":
		st.Branch = f[2]
		st.Detached = f[2] == "(detached)"
	case "branch.upstream":
		st.Upstream = f[2]
	case "branch.ab":
		if len(f) == 4 {
			st.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f[2], "+"))
			st.Behind, _ = strconv.Atoi(strings.TrimPrefix(f[3], "-"))
		}
	}
}

// code reduces a porcelain XY pair (index, worktree) to one letter.
func code(xy string) string {
	if len(xy) != 2 {
		return Modified
	}
	switch {
	case xy[0] == 'D' || xy[1] == 'D':
		return Deleted
	case xy[0] == 'A':
		return Added
	case xy[0] == 'R' || xy[0] == 'C':
		return Renamed
	}
	return Modified
}

var rank = map[string]int{Untracked: 1, Modified: 2, Conflict: 3}

// add records p (repository-relative) if it lies in the session directory,
// and marks its parent directories.
func (st *Status) add(prefix, p, c string) {
	if !strings.HasPrefix(p, prefix) {
		return
	}
	rel := strings.TrimSuffix(strings.TrimPrefix(p, prefix), "/")
	if rel == "" {
		return
	}
	st.Files[rel] = c
	if c == Ignored {
		return
	}
	dirCode := Modified
	switch c {
	case Untracked:
		dirCode = Untracked
	case Conflict:
		dirCode = Conflict
	}
	for d := path.Dir(rel); d != "." && d != "/"; d = path.Dir(d) {
		if rank[dirCode] > rank[st.Dirs[d]] {
			st.Dirs[d] = dirCode
		}
	}
}

// RemoteURL returns the repository's fetch URL for "origin", or else for
// the first remote, read from the repository's own config without includes
// (nothing runs). "" outside a repository or without remotes.
func RemoteURL(ctx context.Context, dir string) string {
	gd, err := GitDir(ctx, dir)
	if err != nil || gd == "" {
		return ""
	}
	recs, _, err := repoConfig(ctx, gd) // what can be read
	if err != nil {
		return ""
	}
	first := ""
	for _, rec := range recs {
		key, val := strings.ToLower(rec.Key), rec.Value
		if !strings.HasPrefix(key, "remote.") || !strings.HasSuffix(key, ".url") {
			continue
		}
		if key == "remote.origin.url" {
			return val
		}
		if first == "" {
			first = val
		}
	}
	return first
}
