package gitstatus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// Finding is one thing in a repository that git on the host would run or
// that redirects pushes. See docs/CONNECTIVITY.md.
type Finding struct {
	Kind   string `json:"kind"`   // "hook" or "config"
	Name   string `json:"name"`   // hook file name or config key
	Detail string `json:"detail"` // config value (URL credentials masked) or hook path
	Why    string `json:"why"`
	Fix    string `json:"fix"` // a command that inspects or removes it
}

// Audit is the result of checking a repository.
type Audit struct {
	Repo     bool      `json:"repo"`
	Findings []Finding `json:"findings"`
	// Fingerprint identifies the exact set of findings (including hook
	// contents and config values); "" when there are none. Trusting a
	// fingerprint silences the warning until something changes.
	Fingerprint string `json:"fingerprint"`
}

// Risky repository config keys, as lower-case patterns (* = any part).
// With value prefixes, only values starting with one of them count
// (lower-case).
var riskyKeys = []struct {
	re     *regexp.Regexp
	why    string
	values []string
}{
	{key("core.fsmonitor"), "runs a program on every git status", nil},
	{key("core.hookspath"), "moves the hooks folder, possibly into the shared files", nil},
	{key("core.sshcommand"), "replaces the ssh program used for fetch and push", nil},
	{key("core.gitproxy"), "runs a program for git:// connections", nil},
	{key("core.pager"), "runs a program to page output", nil},
	{key("core.editor"), "runs a program as the editor", nil},
	{key("core.askpass"), "runs a program to ask for passwords", nil},
	{key("sequence.editor"), "runs a program during rebase", nil},
	{key("credential.*"), "decides where your credentials go", nil},
	{key("filter.*"), "runs a program on file contents (status, add, checkout)", nil},
	{key("diff.external"), "runs a program for git diff", nil},
	{key("diff.*.textconv"), "runs a program for git diff", nil},
	{key("diff.*.command"), "runs a program for git diff", nil},
	{key("merge.*.driver"), "runs a program during merges", nil},
	{key("gpg.program"), "runs a program to sign commits", nil},
	{key("gpg.*.program"), "runs a program to sign commits", nil},
	{key("include.path"), "loads more config from another file", nil},
	{key("includeif.*"), "loads more config from another file", nil},
	{key("remote.*.pushurl"), "sends your pushes (and code) to another address", nil},
	{key("url.*.insteadof"), "rewrites the addresses you fetch from or push to", nil},
	{key("url.*.pushinsteadof"), "rewrites the addresses you push to", nil},
	{key("http.proxy"), "routes git's HTTP traffic through a proxy", nil},
	{key("http.*.proxy"), "routes git's HTTP traffic through a proxy", nil},
	{key("remote.*.proxy"), "routes that remote's traffic through a proxy", nil},
	{key("alias.*"), "a git alias that runs a shell command or sets options (-c …) that can run programs", []string{"!", "-", `"`, "'"}}, // git splits aliases like a shell: "-c" too
	{key("pager.*"), "runs a program to page that command's output", nil},
	{key("core.worktree"), "makes git work on another directory", nil},
	{key("core.alternaterefscommand"), "runs a program to list refs of alternate repositories", nil},
	{key("interactive.difffilter"), "runs a program in git add -p", nil},
	{key("guitool.*.cmd"), "runs a program from git gui's Tools menu", nil},
	{key("difftool.*.cmd"), "runs a program for git difftool", nil},
	{key("difftool.*.path"), "runs a program for git difftool", nil},
	{key("mergetool.*.cmd"), "runs a program for git mergetool", nil},
	{key("mergetool.*.path"), "runs a program for git mergetool", nil},
	{key("gpg.ssh.defaultkeycommand"), "runs a program to choose the signing key", nil},
	{key("submodule.*.update"), "runs a shell command on git submodule update", []string{"!"}},
	{key("remote.*.uploadpack"), "runs a program when fetching from a local remote", nil},
	{key("remote.*.receivepack"), "runs a program when pushing to a local remote", nil},
	{key("remote.*.url"), "an ext:: remote runs a shell command to fetch and push", []string{"ext::"}},
	{key("protocol.allow"), "allows transports git blocks by default (ext:: runs commands)", nil},
	{key("protocol.*.allow"), "allows transports git blocks by default (ext:: runs commands)", nil},
	{key("browser.*.cmd"), "runs a program to open web pages (git help -w, instaweb)", nil},
	{key("browser.*.path"), "runs a program to open web pages (git help -w, instaweb)", nil},
	{key("man.*.cmd"), "runs a program to show help pages", nil},
	{key("man.*.path"), "runs a program to show help pages", nil},
	{key("tar.*.command"), "runs a program for git archive", nil},
	{key("trailer.*.command"), "runs a command when trailers are added (git commit --trailer)", nil},
	{key("trailer.*.cmd"), "runs a command when trailers are added (git commit --trailer)", nil},
	{key("instaweb.httpd"), "runs a web server program for git instaweb", nil},
	{key("sendemail.*cmd"), "runs a command for git send-email", nil},
	{key("sendemail.smtpserver"), "runs a program to send mail (git send-email)", []string{"/"}},
	{key("sendemail.*.smtpserver"), "runs a program to send mail (git send-email)", []string{"/"}},
}

func key(pattern string) *regexp.Regexp {
	parts := strings.Split(regexp.QuoteMeta(pattern), `\*`)
	return regexp.MustCompile("^" + strings.Join(parts, `[^\n]+`) + "$")
}

var urlCredentials = regexp.MustCompile(`(://)[^/@\s]+@`)

// RunAudit checks the repository containing dir for active hooks and risky
// repository config. Like Get it runs only git commands that execute
// nothing from the repository, and it finds the repository through its git
// directory, so a planted core.worktree cannot hide the findings.
func RunAudit(ctx context.Context, dir string) (Audit, error) {
	a := Audit{Findings: []Finding{}}
	if _, err := exec.LookPath("git"); err != nil {
		return a, nil
	}
	gd, err := GitDir(ctx, dir)
	if err != nil || gd == "" {
		return a, err
	}
	a.Repo = true
	// The worktree root, for a relative core.hooksPath; git may refuse to
	// say (core.worktree planted), then the folder holding the git dir.
	root := filepath.Dir(commonDir(gd))
	if top, err := git(ctx, dir, "rev-parse", "--show-toplevel"); err == nil {
		root = strings.TrimSpace(string(top))
	}
	var fp []string
	hooksPath := ""

	// Config: the repository's own files only, without following includes.
	// A file git cannot read is a finding of its own; the hooks are still
	// checked.
	recs, bad, err := repoConfig(ctx, gd)
	if err != nil {
		return a, err
	}
	for _, b := range bad {
		a.Findings = append(a.Findings, Finding{
			Kind: "config", Name: b.File, Detail: b.Err.Error(),
			Why: "the repository's config cannot be read; git may behave unexpectedly",
			Fix: "cat " + quote(b.File) + "   # inspect",
		})
		fp = append(fp, "unreadable "+b.File+" "+hashFile(b.File))
	}
	for _, rec := range recs {
		name, value := rec.Key, rec.Value
		k := strings.ToLower(name)
		if k == "core.hookspath" {
			hooksPath = value
		}
		for _, r := range riskyKeys {
			if !r.re.MatchString(k) || !hasPrefix(strings.ToLower(strings.TrimLeft(value, " \t")), r.values) {
				continue
			}
			// --file and -C /: works whichever file it is in, and even when
			// a planted core.worktree makes git refuse the repository.
			a.Findings = append(a.Findings, Finding{
				Kind: "config", Name: name, Why: r.why,
				Detail: urlCredentials.ReplaceAllString(value, "${1}***@"),
				Fix:    "git -C / config --file " + quote(rec.File) + " --unset-all " + quote(name),
			})
			fp = append(fp, "config "+name+"="+value)
			break
		}
	}

	// Hooks: the folder git would really use (follows core.hooksPath and a
	// worktree's common dir); worked out here when git refuses to say.
	hooks := hooksPath
	if out, err := git(ctx, os.TempDir(), "--git-dir="+gd, "rev-parse", "--git-path", "hooks"); err == nil {
		hooks = strings.TrimSpace(string(out))
	} else if hooks == "" {
		hooks = filepath.Join(commonDir(gd), "hooks")
	}
	if !filepath.IsAbs(hooks) {
		hooks = filepath.Join(root, hooks)
	}
	entries, _ := os.ReadDir(hooks)
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".sample") {
			continue
		}
		p := filepath.Join(hooks, e.Name())
		info, err := os.Stat(p)
		// git skips hooks without the executable bit, except on Windows.
		// Only regular files (or links to them) run; reading a FIFO would
		// block forever.
		if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode()&0o111 == 0) {
			continue
		}
		a.Findings = append(a.Findings, Finding{
			Kind: "hook", Name: e.Name(), Detail: p, Why: "runs when git performs that action on this machine",
			Fix: "cat " + quote(p) + "   # inspect;  rm " + quote(p) + "   # remove",
		})
		fp = append(fp, "hook "+e.Name()+" "+hashFile(p))
	}

	sort.Slice(a.Findings, func(i, j int) bool {
		if a.Findings[i].Kind != a.Findings[j].Kind {
			return a.Findings[i].Kind < a.Findings[j].Kind
		}
		return a.Findings[i].Name < a.Findings[j].Name
	})
	if len(fp) > 0 {
		sort.Strings(fp)
		sum := sha256.Sum256([]byte(strings.Join(fp, "\n")))
		a.Fingerprint = hex.EncodeToString(sum[:16])
	}
	return a, nil
}

// hasPrefix reports whether s starts with one of prefixes; no prefixes
// match anything.
func hasPrefix(s string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// maxHashed caps how much of a file is hashed for the fingerprint; beyond
// it the size stands in for the rest.
const maxHashed = 1 << 20

// hashFile returns the hex SHA-256 of a regular file's contents (the first
// maxHashed bytes, plus the size of a larger file); "" if it cannot be read.
func hashFile(p string) string {
	if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
		return ""
	}
	f, err := os.OpenFile(p, os.O_RDONLY|oNonblock, 0)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if n, _ := io.Copy(h, io.LimitReader(f, maxHashed)); n == maxHashed {
		if fi, err := f.Stat(); err == nil {
			fmt.Fprintf(h, "\x00size %d", fi.Size())
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// quote makes s one shell word for the fix commands the user may paste into
// a shell: names and paths come from the repository, which the agent host
// may have written, so anything beyond plain characters is single-quoted.
var plainWord = regexp.MustCompile(`^[A-Za-z0-9._/@%+=:,-]+$`)

func quote(s string) string {
	if plainWord.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
