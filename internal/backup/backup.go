// Package backup copies Claude Code's data from the agent hosts to this
// computer, so a crashed or reinstalled agent host can be restored:
// ~/.claude (settings, CLAUDE.md, memory, transcripts, skills, agents,
// history, checkpoints) and ~/.claude.json (per-project state, MCP servers).
//
// It uses rsync over the user's ssh with the forced options, as working
// copies do, or, where rsync is missing (Windows), the built-in transfer
// (internal/xfer: tar on the agent host, confined unpacking here), which
// copies everything each time instead of only what changed. A backup only grows: files removed on the agent host stay in
// it, so a broken or compromised host cannot empty it. Left out are
// claude's login (.credentials.json: the app stores no secrets; log in to
// claude again after a restore) and what claude downloads or recreates.
package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"session-workbench/internal/remote"
	"session-workbench/internal/xfer"
)

// Configure is applied to every command (hides console windows on Windows).
var Configure = func(*exec.Cmd) {}

// Exclude lists rsync patterns, relative to the remote home.
var Exclude = []string{
	"/.claude/.credentials.json", // claude's login token
	"/.claude/cache/",
	"/.claude/plugins/cache/", // plugin downloads; plugins/*.json (what is installed) is kept
	"/.claude/plugins/marketplaces/",
	"/.claude/shell-snapshots/",
	"/.claude/session-env/",
	"/.claude/statsig/",
	"/.claude/telemetry/",
	"/.claude/debug/",
}

// Host is an agent host to back up.
type Host struct {
	Name   string
	Target string // ssh target
	Home   string // remote directory holding .claude; "" = the remote home
}

// Status is the outcome of the last backup of a host.
type Status struct {
	Last   time.Time `json:"last"`    // last attempt
	LastOK time.Time `json:"last_ok"` // last successful backup
	Error  string    `json:"error"`   // of the last attempt; "" = it worked
	Busy   bool      `json:"busy"`
}

// rsyncMissing matches rsync's report of a missing source file.
var rsyncMissing = regexp.MustCompile(`link_stat "[^"]*?(\.claude(?:\.json)?)" failed: No such file`)

func remoteDir(h Host) string {
	if h.Home == "" {
		return "" // rsync paths without a slash are relative to the remote home
	}
	return strings.TrimSuffix(h.Home, "/") + "/"
}

func rsync(ctx context.Context, args ...string) (string, int, error) {
	cmd := exec.CommandContext(ctx, "rsync", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var errb bytes.Buffer
	cmd.Stderr = &errb
	Configure(cmd)
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		return errb.String(), -1, err
	}
	return errb.String(), code, nil
}

func commonArgs() []string {
	return []string{"-a", "--safe-links", "--no-devices", "--no-specials", "--timeout=120", "-e", remote.SSHCommand()}
}

// Run backs h up into dir/home (a copy of the files in the remote home).
func Run(ctx context.Context, h Host, dir string) error {
	if err := remote.CheckTarget(h.Target); err != nil {
		return err
	}
	dest := filepath.Join(dir, "home")
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	if !xfer.UseRsync() {
		return pull(ctx, h, dest)
	}
	args := commonArgs()
	for _, x := range Exclude {
		args = append(args, "--exclude="+x)
	}
	base := remoteDir(h)
	args = append(args, "--", h.Target+":"+base+".claude", h.Target+":"+base+".claude.json", dest+"/")
	stderr, code, err := rsync(ctx, args...)
	if err != nil {
		return err
	}
	return classify(code, stderr)
}

// classify turns a backup's rsync exit status into an error, or nil when
// the backup is good: 24 means files vanished while copying (claude was
// writing), and a missing ~/.claude.json is not a problem.
func classify(code int, stderr string) error {
	switch code {
	case 0, 24:
		return nil
	case 23:
		other := false
		for _, line := range strings.Split(stderr, "\n") {
			line = strings.TrimSpace(line)
			m := rsyncMissing.FindStringSubmatch(line)
			switch {
			case m != nil && m[1] == ".claude":
				return errors.New("no ~/.claude on the agent host (claude not used there yet?)")
			case line == "", m != nil, strings.HasPrefix(line, "rsync error: some files/attrs were not transferred"):
			default:
				other = true
			}
		}
		if !other {
			return nil
		}
	}
	return fmt.Errorf("rsync exit %d: %s", code, strings.TrimSpace(lastLines(stderr, 3)))
}

// excluded reports whether a path (relative to the remote home, slashes)
// is left out of backups.
func excluded(name string) bool {
	for _, x := range Exclude {
		x = strings.TrimPrefix(x, "/")
		if name == strings.TrimSuffix(x, "/") || strings.HasPrefix(name, strings.TrimSuffix(x, "/")+"/") {
			return true
		}
	}
	return false
}

// homeDir is the remote directory holding .claude, as a shell word.
func homeDir(h Host) string {
	if h.Home == "" {
		return `"$HOME"`
	}
	return remote.Quote(h.Home)
}

// pull is Run without rsync: tar on the agent host with the same
// exclusions, unpacked here through os.Root; the exclusions are applied
// here too, in case the agent host's tar ignored them.
func pull(ctx context.Context, h Host, dest string) error {
	var ex strings.Builder
	for _, x := range Exclude {
		ex.WriteString(" --exclude=" + remote.Quote(strings.Trim(x, "/")))
	}
	script := "cd " + homeDir(h) + ` || exit 1
[ -e .claude ] || { echo "no ~/.claude" >&2; exit 3; }
set -- .claude
[ -e .claude.json ] && set -- "$@" .claude.json
exec tar -cf -` + ex.String() + ` "$@"`
	code, stderr, _, err := xfer.Pull(ctx, h.Target, script, dest, excluded)
	if err != nil {
		return err
	}
	switch code {
	case 0, 1: // 1: GNU tar, a file changed while it was read (claude was writing)
		return nil
	case 3:
		return errors.New("no ~/.claude on the agent host (claude not used there yet?)")
	}
	return fmt.Errorf("tar on the agent host, exit %d: %s", code, strings.TrimSpace(lastLines(stderr, 3)))
}

// push is Restore without rsync: the backup as a tar stream, unpacked on
// the agent host without replacing existing files (GNU tar
// --skip-old-files, BSD tar -k).
func push(ctx context.Context, h Host, src string) error {
	var files []xfer.File
	for _, top := range []string{".claude", ".claude.json"} {
		filepath.WalkDir(filepath.Join(src, top), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(src, p)
			files = append(files, xfer.File{Path: filepath.ToSlash(rel)})
			return nil
		})
	}
	script := "mkdir -p " + homeDir(h) + " && cd " + homeDir(h) + ` || exit 1
if tar --version 2>/dev/null | grep -q GNU; then exec tar -xf - --skip-old-files; else exec tar -xkf -; fi`
	code, stderr, err := xfer.Push(ctx, h.Target, script, src, files)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("tar on the agent host, exit %d: %s", code, strings.TrimSpace(lastLines(stderr, 3)))
	}
	return nil
}

// Restore copies a backup to h without overwriting files that exist there:
// for a new or reinstalled agent host.
func Restore(ctx context.Context, h Host, dir string) error {
	if err := remote.CheckTarget(h.Target); err != nil {
		return err
	}
	src := filepath.Join(dir, "home")
	var sources []string
	for _, n := range []string{".claude", ".claude.json"} {
		if _, err := os.Lstat(filepath.Join(src, n)); err == nil {
			sources = append(sources, filepath.ToSlash(filepath.Join(src, n)))
		}
	}
	if len(sources) == 0 {
		return errors.New("there is no backup for this host yet")
	}
	if !xfer.UseRsync() {
		return push(ctx, h, src)
	}
	base := remoteDir(h)
	if base == "" {
		base = "./"
	}
	args := append(commonArgs(), "--ignore-existing", "--")
	args = append(append(args, sources...), h.Target+":"+base)
	stderr, code, err := rsync(ctx, args...)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("rsync exit %d: %s", code, strings.TrimSpace(lastLines(stderr, 3)))
	}
	return nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// safeName turns a host name into a directory name.
var unsafe = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func safeName(name string) string {
	n := unsafe.ReplaceAllString(name, "_")
	if n == "" || strings.Trim(n, ".") == "" {
		n = "_" + n
	}
	return n
}

// Runner backs up every host regularly while the app runs.
type Runner struct {
	Dir     string                       // backups go to Dir/<host name>/
	Hosts   func() []Host                // the current hosts
	Every   func() (time.Duration, bool) // interval, and whether backups are on
	Changed func(host string, s Status)  // after a backup starts or ends

	// StartDelay is the wait before the first round (default 1 minute).
	StartDelay time.Duration

	mu     sync.Mutex
	status map[string]Status
}

// HostDir is where h's backup lives.
func (r *Runner) HostDir(host string) string { return filepath.Join(r.Dir, safeName(host)) }

// Status returns the current status per host name (loaded from disk once).
func (r *Runner) Status() map[string]Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]Status{}
	for _, h := range r.Hosts() {
		out[h.Name] = r.statusLocked(h.Name)
	}
	return out
}

func (r *Runner) statusLocked(host string) Status {
	if r.status == nil {
		r.status = map[string]Status{}
	}
	s, ok := r.status[host]
	if !ok {
		if b, err := os.ReadFile(filepath.Join(r.HostDir(host), "status.json")); err == nil {
			json.Unmarshal(b, &s)
		}
		s.Busy = false
		r.status[host] = s
	}
	return s
}

func (r *Runner) set(host string, s Status) {
	r.mu.Lock()
	r.status[host] = s
	r.mu.Unlock()
	if !s.Busy {
		if b, err := json.MarshalIndent(s, "", "  "); err == nil {
			os.MkdirAll(r.HostDir(host), 0o700)
			os.WriteFile(filepath.Join(r.HostDir(host), "status.json"), append(b, '\n'), 0o600)
		}
	}
	if r.Changed != nil {
		r.Changed(host, s)
	}
}

// Now backs up one host and waits for it; an error if it is already busy.
func (r *Runner) Now(ctx context.Context, name string) error {
	for _, h := range r.Hosts() {
		if h.Name == name {
			return r.backup(ctx, h)
		}
	}
	return fmt.Errorf("unknown host %q", name)
}

func (r *Runner) backup(ctx context.Context, h Host) error {
	r.mu.Lock()
	s := r.statusLocked(h.Name)
	if s.Busy {
		r.mu.Unlock()
		return errors.New("a backup of this host is already running")
	}
	s.Busy = true
	r.status[h.Name] = s
	r.mu.Unlock()
	if r.Changed != nil {
		r.Changed(h.Name, s)
	}
	err := Run(ctx, h, r.HostDir(h.Name))
	s.Busy, s.Last, s.Error = false, time.Now(), ""
	if err != nil {
		s.Error = err.Error()
	} else {
		s.LastOK = s.Last
	}
	r.set(h.Name, s)
	return err
}

// due: the interval has passed since the last attempt; after a failure,
// retry every 15 minutes at most.
func (r *Runner) due(host string, every time.Duration, now time.Time) bool {
	r.mu.Lock()
	s := r.statusLocked(host)
	r.mu.Unlock()
	if s.Busy {
		return false
	}
	wait := every
	if s.Error != "" && wait > 15*time.Minute {
		wait = 15 * time.Minute
	}
	return now.Sub(s.Last) >= wait
}

// Start runs backups in the background until ctx ends.
func (r *Runner) Start(ctx context.Context) {
	delay := r.StartDelay
	if delay == 0 {
		delay = time.Minute
	}
	go func() {
		t := time.NewTimer(delay)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			if every, on := r.Every(); on {
				for _, h := range r.Hosts() {
					if r.due(h.Name, every, time.Now()) {
						go r.backup(ctx, h)
					}
				}
			}
			t.Reset(time.Minute)
		}
	}()
}
