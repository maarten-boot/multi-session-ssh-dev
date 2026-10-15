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
	"fmt"
	"os"
	"os/exec"
	"path"
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

// Get returns the status of dir. A directory outside any repository, or a
// missing git, is not an error: Repo is false.
func Get(ctx context.Context, dir string) (Status, error) {
	st := Status{Files: map[string]string{}, Dirs: map[string]string{}}
	if _, err := exec.LookPath("git"); err != nil {
		return st, nil
	}
	prefix, err := git(ctx, dir, "rev-parse", "--show-prefix")
	if err != nil {
		if strings.Contains(err.Error(), "not a git repository") {
			return st, nil
		}
		return st, err
	}
	st.Repo = true
	if reason, err := unsafeConfig(ctx, dir); err != nil {
		return st, err
	} else if reason != "" {
		st.Skipped = reason
		if b, err := git(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
			st.Branch = strings.TrimSpace(string(b))
		}
		return st, nil
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
// or system config) sets something git status would execute or that pulls
// in other config. Reading config with --no-includes runs nothing.
func unsafeConfig(ctx context.Context, dir string) (string, error) {
	out, err := git(ctx, dir, "config", "--local", "--no-includes", "--name-only", "--list")
	if err != nil {
		if strings.Contains(err.Error(), "exit status 1") {
			return "", nil // no local config at all
		}
		return "", err
	}
	for _, key := range strings.Fields(string(out)) {
		k := strings.ToLower(key)
		switch {
		case strings.HasPrefix(k, "filter."), // clean/smudge/process programs
			strings.HasPrefix(k, "include."), strings.HasPrefix(k, "includeif."), // more config
			k == "core.fsmonitor":
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
