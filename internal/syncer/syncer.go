// Package syncer implements the "working copy" file access mode
// (docs/CONNECTIVITY.md, mode C): the session directory on the host is the
// source of truth and is mirrored to the agent host with rsync; work done
// there comes back only as git commits, fetched by the host.
//
// Sending never silently destroys agent work: it is refused while the agent
// copy has commits the host has not fetched, and uncommitted agent changes
// are reported before they are overwritten.
package syncer

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"session-workbench/internal/remote"
	"session-workbench/internal/xfer"
)

// Configure is applied to every command (hides console windows on Windows).
var Configure = func(*exec.Cmd) {}

// Change is one entry of a send preview.
type Change struct {
	Op   string `json:"op"` // "new", "changed" or "deleted"
	Path string `json:"path"`
}

// AgentState is what the agent copy looks like.
type AgentState struct {
	Exists    bool   `json:"exists"`
	Repo      bool   `json:"repo"`
	Dirty     int    `json:"dirty"`     // files with uncommitted changes
	Unfetched int    `json:"unfetched"` // agent branch heads the host does not have
	Branch    string `json:"branch"`    // the branch checked out there ("" detached)
}

// Plan is the result of a send (or its dry run).
type Plan struct {
	Agent     AgentState `json:"agent"`
	Changes   []Change   `json:"changes"`
	New       int        `json:"new"`
	Changed   int        `json:"changed"`
	Deleted   int        `json:"deleted"`
	Truncated bool       `json:"truncated"`
	Blocked   string     `json:"blocked"` // why sending is not allowed now
	Note      string     `json:"note"`    // a caveat that does not block sending
	Done      bool       `json:"done"`    // false for a dry run
}

const maxListed = 500

// Session identifies what to sync.
type Session struct {
	ID         string // used for the fetched refs: refs/remotes/agent-<id>/*
	Target     string // ssh target
	LocalPath  string
	RemotePath string // absolute or ~/...
}

// rsyncPath returns the remote path as rsync and git expect it: relative to
// the remote home for ~/..., absolute otherwise.
func rsyncPath(p string) string {
	if p == "~" {
		return "."
	}
	return strings.TrimPrefix(p, "~/")
}

func run(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), append([]string{"LC_ALL=C"}, env...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	Configure(cmd)
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), errb.String(), fmt.Errorf("%s: %s", name, msg)
	}
	return out.String(), errb.String(), nil
}

// localGit runs git in the host repository with fsmonitor off. Note that
// FastForward's merge updates the work tree, so checkout filters and
// attributes of the host repository apply there.
func localGit(ctx context.Context, s Session, args ...string) (string, error) {
	out, _, err := run(ctx, s.LocalPath, []string{"GIT_SSH_COMMAND=" + remote.SSHCommand(), "GIT_TERMINAL_PROMPT=0"},
		"git", append([]string{"-c", "core.fsmonitor=false"}, args...)...)
	return out, err
}

// subdirOf returns the repository's top folder when the session directory
// is inside a repository but not its top: a working copy of it gets no
// .git, so claude cannot commit there and Get has nothing to fetch.
func subdirOf(ctx context.Context, s Session) string {
	out, err := localGit(ctx, s, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	top := strings.TrimSpace(out)
	// The same directory, however it is written (symlinks, C:/ vs C:\,
	// short names on Windows).
	a, errA := os.Stat(top)
	b, errB := os.Stat(s.LocalPath)
	if top == "" || errA != nil || errB != nil || os.SameFile(a, b) {
		return ""
	}
	return filepath.FromSlash(top)
}

// repoTop reports whether the session directory is the top of a git
// repository here.
func repoTop(ctx context.Context, s Session) bool {
	if _, err := localGit(ctx, s, "rev-parse", "--show-toplevel"); err != nil {
		return false
	}
	return subdirOf(ctx, s) == ""
}

func subdirProblem(top string) string {
	return fmt.Sprintf("this folder is part of the repository at %s; a working copy gets only this folder, "+
		"without .git, so claude's commits cannot come back with Get. Create a session for %s to use Get", top, top)
}

// probe inspects the agent copy. It also creates the parent directory so
// that rsync can create the copy itself.
func probe(ctx context.Context, s Session) (AgentState, []string, error) {
	dir := remote.ShellPath(s.RemotePath)
	parent := remote.ShellPath(s.RemotePath[:strings.LastIndex(s.RemotePath, "/")+1])
	script := fmt.Sprintf(`mkdir -p %s
cd %s 2>/dev/null || { echo NODIR; exit 0; }
git rev-parse --git-dir >/dev/null 2>&1 || { echo NOREPO; exit 0; }
echo "DIRTY $(git status --porcelain 2>/dev/null | wc -l)"
echo "BRANCH $(git symbolic-ref --short -q HEAD)"
git for-each-ref --format='HEAD %%(objectname)' refs/heads`, parent, dir)
	// /bin/sh, not the login shell: the script is POSIX (fish would fail).
	out, code, stderr, err := xfer.Run(ctx, s.Target, script, nil)
	if err == nil && code != 0 {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = fmt.Sprintf("exit %d", code)
		}
		err = fmt.Errorf("ssh: %s", msg)
	}
	if err != nil {
		return AgentState{}, nil, fmt.Errorf("checking the agent copy: %w", err)
	}
	st := AgentState{Exists: true, Repo: true}
	var heads []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		switch {
		case len(f) == 1 && f[0] == "NODIR":
			return AgentState{}, nil, nil
		case len(f) == 1 && f[0] == "NOREPO":
			st.Repo = false
		case len(f) == 2 && f[0] == "DIRTY":
			st.Dirty, _ = strconv.Atoi(f[1])
		case len(f) == 2 && f[0] == "BRANCH":
			st.Branch = f[1]
		case len(f) == 2 && f[0] == "HEAD":
			heads = append(heads, f[1])
		}
	}
	return st, heads, nil
}

// unknownCommits counts agent branch heads that the host repository does
// not contain (not fetched yet). A host without git cannot tell; then every
// head counts.
func unknownCommits(ctx context.Context, s Session, heads []string) int {
	n := 0
	for _, h := range heads {
		if _, err := localGit(ctx, s, "cat-file", "-e", h+"^{commit}"); err != nil {
			n++
		}
	}
	return n
}

// Send mirrors the host directory to the agent copy (dryRun: only report
// what would change). Files ignored by .gitignore are neither sent nor
// deleted on the agent host.
func Send(ctx context.Context, s Session, dryRun, overwriteDirty bool) (Plan, error) {
	if err := remote.CheckTarget(s.Target); err != nil {
		return Plan{}, err
	}
	agent, heads, err := probe(ctx, s)
	if err != nil {
		return Plan{}, err
	}
	p := Plan{Agent: agent, Changes: []Change{}}
	if top := subdirOf(ctx, s); top != "" {
		p.Note = subdirProblem(top)
	}
	if agent.Repo && len(heads) > 0 {
		p.Agent.Unfetched = unknownCommits(ctx, s, heads)
		if p.Agent.Unfetched > 0 {
			p.Blocked = "the agent copy has commits you have not fetched yet; use Get commits first"
		}
	}
	if p.Blocked == "" && agent.Dirty > 0 && !dryRun && !overwriteDirty {
		p.Blocked = fmt.Sprintf("the agent copy has uncommitted changes in %d file(s), which sending would overwrite", agent.Dirty)
	}
	if p.Blocked != "" && !dryRun {
		return p, nil
	}
	if !xfer.UseRsync() { // Windows: the built-in transfer
		if err := sendBuiltin(ctx, s, dryRun, &p); err != nil {
			return p, fmt.Errorf("sending: %w", err)
		}
		p.Done = !dryRun
		return p, nil
	}
	args := []string{"-a", "--delete", "--itemize-changes", "--filter=:- .gitignore", "-e", remote.SSHCommand()}
	if dryRun {
		args = append(args, "--dry-run")
	}
	src := filepath.ToSlash(filepath.Clean(s.LocalPath)) + "/"
	args = append(args, "--", src, s.Target+":"+rsyncPath(s.RemotePath)+"/")
	out, _, err := run(ctx, "", nil, "rsync", args...)
	if err != nil {
		return p, fmt.Errorf("sending: %w", err)
	}
	parseItemize(&p, out)
	p.Done = !dryRun
	return p, nil
}

// parseItemize reads rsync --itemize-changes output.
func parseItemize(p *Plan, out string) {
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		var c Change
		switch {
		case strings.HasPrefix(line, "*deleting "):
			c = Change{Op: "deleted", Path: strings.TrimSpace(line[len("*deleting "):])}
			p.Deleted++
		// < sent (our direction), > received, c created (dirs, links), h hard link;
		// "." means attributes only, which we do not list.
		case len(line) > 12 && strings.ContainsRune("<>ch", rune(line[0])) && line[11] == ' ':
			if line[1] == 'd' { // directories: their contents are listed separately
				continue
			}
			if strings.HasPrefix(line[2:11], "+++++++++") {
				c = Change{Op: "new", Path: line[12:]}
				p.New++
			} else {
				c = Change{Op: "changed", Path: line[12:]}
				p.Changed++
			}
		default:
			continue
		}
		if len(p.Changes) < maxListed {
			p.Changes = append(p.Changes, c)
		} else {
			p.Truncated = true
		}
	}
}

// Commit is one commit brought back from the agent.
type Commit struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
}

// Fetched is the result of Get.
type Fetched struct {
	Branch     string   `json:"branch"`      // the host's current branch
	AgentRef   string   `json:"agent_ref"`   // agent-<id>/<branch>, if the agent has that branch
	New        []Commit `json:"new"`         // agent commits not in the host branch
	HostAhead  int      `json:"host_ahead"`  // host commits the agent branch lacks
	CanFF      bool     `json:"can_ff"`      // the host branch can fast-forward
	Branches   []string `json:"branches"`    // all fetched agent branches
	AgentDirty int      `json:"agent_dirty"` // uncommitted changes left on the agent
	// Agent is every agent branch compared with the host's branch of the
	// same name.
	Agent       []AgentBranch `json:"agent"`
	AgentBranch string        `json:"agent_branch"` // checked out on the agent
}

// AgentBranch is one agent branch next to the host's.
type AgentBranch struct {
	Name     string `json:"name"`
	AgentRef string `json:"agent_ref"` // agent-<id>/<name>
	// State: "new" (no host branch), "ahead" (the host can fast-forward),
	// "same", "behind" (only the host has commits) or "diverged".
	State   string `json:"state"`
	Ahead   int    `json:"ahead"`   // agent commits the host branch lacks (new: in no host branch)
	Behind  int    `json:"behind"`  // host commits the agent branch lacks
	Current bool   `json:"current"` // checked out here
	// What taking it over brings (new, ahead, diverged): the agent's
	// commits (at most 20) and the files they change (at most 200),
	// compared with the host branch or, for a new branch, with where it
	// left the host's current branch.
	Commits []Commit `json:"commits"`
	Changes []Change `json:"changes"`
	More    bool     `json:"more"` // lists cut short
}

const (
	maxBranchCommits = 20
	maxBranchChanges = 200
)

// maxBranches bounds the branches compared one by one.
const maxBranches = 200

// RefPrefix is where agent branches are fetched to.
func RefPrefix(id string) string { return "refs/remotes/agent-" + id + "/" }

// Get fetches the agent copy's branches into refs/remotes/agent-<id>/ and
// compares the agent's version of the current branch with the host's.
// Hooks and config of the agent copy are never transferred.
func Get(ctx context.Context, s Session) (Fetched, error) {
	var f Fetched
	if _, err := localGit(ctx, s, "rev-parse", "--git-dir"); err != nil {
		return f, errors.New("the session directory is not a git repository on this computer")
	}
	if top := subdirOf(ctx, s); top != "" {
		return f, errors.New(subdirProblem(top))
	}
	agent, _, err := probe(ctx, s)
	if err != nil {
		return f, err
	}
	if !agent.Exists {
		return f, errors.New("there is no working copy on the agent host yet; use Send to agent first")
	}
	if !agent.Repo {
		return f, errors.New("the working copy on the agent host has no .git (removed there?); " +
			"Send to agent again to restore it")
	}
	f.AgentDirty = agent.Dirty
	f.AgentBranch = agent.Branch
	url := s.Target + ":" + rsyncPath(s.RemotePath)
	if _, err := localGit(ctx, s, "-c", "transfer.fsckObjects=true", "fetch", "--no-tags", "--prune", "--quiet", url,
		"+refs/heads/*:"+RefPrefix(s.ID)+"*"); err != nil {
		return f, fmt.Errorf("fetching from the agent copy: %w", err)
	}
	refs, _ := localGit(ctx, s, "for-each-ref", "--format=%(refname)", RefPrefix(s.ID))
	for _, r := range strings.Fields(refs) {
		f.Branches = append(f.Branches, strings.TrimPrefix(r, "refs/remotes/"))
	}
	if b, err := localGit(ctx, s, "symbolic-ref", "--short", "HEAD"); err == nil {
		f.Branch = strings.TrimSpace(b)
	}
	for _, r := range f.Branches {
		if len(f.Agent) == maxBranches {
			break
		}
		f.Agent = append(f.Agent, compare(ctx, s, r, f.Branch))
	}
	if f.Branch == "" {
		return f, nil // detached HEAD: branches fetched, nothing to compare
	}
	ref := RefPrefix(s.ID) + f.Branch
	if _, err := localGit(ctx, s, "rev-parse", "--verify", "--quiet", ref); err != nil {
		return f, nil
	}
	f.AgentRef = strings.TrimPrefix(ref, "refs/remotes/")
	log, _ := localGit(ctx, s, "log", "--format=%h %s", "--max-count=50", "HEAD.."+ref)
	for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
		if h, subj, ok := strings.Cut(line, " "); ok {
			f.New = append(f.New, Commit{Hash: h, Subject: subj})
		}
	}
	ahead, _ := localGit(ctx, s, "rev-list", "--count", ref+"..HEAD")
	f.HostAhead, _ = strconv.Atoi(strings.TrimSpace(ahead))
	f.CanFF = len(f.New) > 0 && f.HostAhead == 0
	return f, nil
}

// count runs git rev-list --count with args; -1 if git fails.
func count(ctx context.Context, s Session, args ...string) int {
	out, err := localGit(ctx, s, append([]string{"rev-list", "--count"}, args...)...)
	if err != nil {
		return -1
	}
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n
}

// compare puts agent branch agentRef (agent-<id>/<name>) next to the host's
// branch of the same name.
func compare(ctx context.Context, s Session, agentRef, current string) AgentBranch {
	full := "refs/remotes/" + agentRef
	b := AgentBranch{AgentRef: agentRef, Name: strings.TrimPrefix(full, RefPrefix(s.ID))}
	b.Current = b.Name == current
	local := "refs/heads/" + b.Name
	if _, err := localGit(ctx, s, "rev-parse", "--verify", "--quiet", local); err != nil {
		b.State = "new"
		b.Ahead = count(ctx, s, full, "--not", "--branches")
		base := "HEAD"
		if out, err := localGit(ctx, s, "merge-base", "HEAD", full); err == nil {
			base = strings.TrimSpace(out)
		}
		b.what(ctx, s, []string{full, "--not", "--branches"}, base, full)
		return b
	}
	b.Ahead, b.Behind = count(ctx, s, local+".."+full), count(ctx, s, full+".."+local)
	switch {
	case b.Ahead == 0 && b.Behind == 0:
		b.State = "same"
	case b.Behind == 0:
		b.State = "ahead"
		b.what(ctx, s, []string{local + ".." + full}, local, full)
	case b.Ahead == 0:
		b.State = "behind"
	default:
		b.State = "diverged"
		b.what(ctx, s, []string{local + ".." + full}, local+"...", full) // since the branches split
	}
	return b
}

// what lists the commits (git log revs) and the files changed from base to
// tip. A base ending in "..." compares from where it split off tip.
func (b *AgentBranch) what(ctx context.Context, s Session, revs []string, base, tip string) {
	args := append([]string{"log", "--format=%h %s", fmt.Sprintf("--max-count=%d", maxBranchCommits+1)}, revs...)
	if out, err := localGit(ctx, s, args...); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if h, subj, ok := strings.Cut(line, " "); ok {
				b.Commits = append(b.Commits, Commit{Hash: h, Subject: subj})
			}
		}
	}
	if len(b.Commits) > maxBranchCommits {
		b.Commits, b.More = b.Commits[:maxBranchCommits], true
	}
	// Names and status only: no diff drivers or textconv from the repository run.
	diff := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-status", "-z"}
	if strings.HasSuffix(base, "...") {
		diff = append(diff, base+tip)
	} else {
		diff = append(diff, base, tip)
	}
	out, err := localGit(ctx, s, diff...)
	if err != nil {
		return
	}
	f := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	for i := 0; i+1 < len(f); i += 2 {
		op := map[string]string{"A": "new", "D": "deleted"}[f[i]]
		if op == "" {
			op = "changed"
		}
		if len(b.Changes) == maxBranchChanges {
			b.More = true
			break
		}
		b.Changes = append(b.Changes, Change{Op: op, Path: f[i+1]})
	}
}

// Take makes the host's branches match the agent's where that loses
// nothing: an agent branch the host lacks becomes a new branch (without
// switching to it), a host branch behind its agent version is
// fast-forwarded (the checked-out one with git merge --ff-only, so the
// work tree follows). Names are agent branch names as Get reports them. It
// returns what it did per branch; a branch that cannot be taken over is an
// error, the others are still done.
func Take(ctx context.Context, s Session, names []string) ([]string, error) {
	cur, _ := localGit(ctx, s, "symbolic-ref", "--short", "-q", "HEAD")
	cur = strings.TrimSpace(cur)
	var done, failed []string
	for _, name := range names {
		msg, err := take(ctx, s, name, cur)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		done = append(done, msg)
	}
	if len(failed) > 0 {
		return done, errors.New(strings.Join(failed, "; "))
	}
	return done, nil
}

func take(ctx context.Context, s Session, name, current string) (string, error) {
	agent := RefPrefix(s.ID) + name
	if strings.HasPrefix(name, "-") || strings.Contains(name, "..") {
		return "", errors.New("not a branch name")
	}
	if _, err := localGit(ctx, s, "check-ref-format", "refs/heads/"+name); err != nil {
		return "", errors.New("not a branch name")
	}
	if _, err := localGit(ctx, s, "rev-parse", "--verify", "--quiet", agent); err != nil {
		return "", errors.New("not an agent branch of this session (Get again?)")
	}
	local := "refs/heads/" + name
	old, err := localGit(ctx, s, "rev-parse", "--verify", "--quiet", local)
	if err != nil {
		if _, err := localGit(ctx, s, "branch", "--no-track", name, agent); err != nil {
			return "", fmt.Errorf("creating the branch: %w", err)
		}
		return fmt.Sprintf("%s: new branch (from the agent)", name), nil
	}
	if _, err := localGit(ctx, s, "merge-base", "--is-ancestor", local, agent); err != nil {
		return "", errors.New("your branch has commits the agent's lacks; merge or rebase in the Local tab")
	}
	n := count(ctx, s, local+".."+agent)
	if n == 0 {
		return fmt.Sprintf("%s: already up to date", name), nil
	}
	if name == current {
		if _, err := localGit(ctx, s, "merge", "--ff-only", "--quiet", agent); err != nil {
			return "", fmt.Errorf("fast-forward: %w", err)
		}
	} else if _, err := localGit(ctx, s, "update-ref", "-m", "session-workbench: take agent branch", local, agent, strings.TrimSpace(old)); err != nil {
		return "", fmt.Errorf("fast-forward: %w", err)
	}
	return fmt.Sprintf("%s: fast-forwarded by %d commit(s)", name, n), nil
}
