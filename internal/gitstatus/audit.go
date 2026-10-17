package gitstatus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
// With a value prefix, only values starting with it count (lower-case).
var riskyKeys = []struct {
	re    *regexp.Regexp
	why   string
	value string
}{
	{key("core.fsmonitor"), "runs a program on every git status", ""},
	{key("core.hookspath"), "moves the hooks folder, possibly into the shared files", ""},
	{key("core.sshcommand"), "replaces the ssh program used for fetch and push", ""},
	{key("core.gitproxy"), "runs a program for git:// connections", ""},
	{key("core.pager"), "runs a program to page output", ""},
	{key("core.editor"), "runs a program as the editor", ""},
	{key("core.askpass"), "runs a program to ask for passwords", ""},
	{key("sequence.editor"), "runs a program during rebase", ""},
	{key("credential.*"), "decides where your credentials go", ""},
	{key("filter.*"), "runs a program on file contents (status, add, checkout)", ""},
	{key("diff.external"), "runs a program for git diff", ""},
	{key("diff.*.textconv"), "runs a program for git diff", ""},
	{key("diff.*.command"), "runs a program for git diff", ""},
	{key("merge.*.driver"), "runs a program during merges", ""},
	{key("gpg.program"), "runs a program to sign commits", ""},
	{key("gpg.*.program"), "runs a program to sign commits", ""},
	{key("include.path"), "loads more config from another file", ""},
	{key("includeif.*"), "loads more config from another file", ""},
	{key("remote.*.pushurl"), "sends your pushes (and code) to another address", ""},
	{key("url.*.insteadof"), "rewrites the addresses you fetch from or push to", ""},
	{key("url.*.pushinsteadof"), "rewrites the addresses you push to", ""},
	{key("http.proxy"), "routes git's HTTP traffic through a proxy", ""},
	{key("http.*.proxy"), "routes git's HTTP traffic through a proxy", ""},
	{key("remote.*.proxy"), "routes that remote's traffic through a proxy", ""},
	{key("alias.*"), "a git alias that runs a shell command", "!"},
	{key("pager.*"), "runs a program to page that command's output", ""},
	{key("core.worktree"), "makes git work on another directory", ""},
	{key("core.alternaterefscommand"), "runs a program to list refs of alternate repositories", ""},
	{key("interactive.difffilter"), "runs a program in git add -p", ""},
	{key("difftool.*.cmd"), "runs a program for git difftool", ""},
	{key("difftool.*.path"), "runs a program for git difftool", ""},
	{key("mergetool.*.cmd"), "runs a program for git mergetool", ""},
	{key("mergetool.*.path"), "runs a program for git mergetool", ""},
	{key("gpg.ssh.defaultkeycommand"), "runs a program to choose the signing key", ""},
	{key("submodule.*.update"), "runs a shell command on git submodule update", "!"},
	{key("remote.*.uploadpack"), "runs a program when fetching from a local remote", ""},
	{key("remote.*.receivepack"), "runs a program when pushing to a local remote", ""},
	{key("remote.*.url"), "an ext:: remote runs a shell command to fetch and push", "ext::"},
	{key("protocol.allow"), "allows transports git blocks by default (ext:: runs commands)", ""},
	{key("protocol.*.allow"), "allows transports git blocks by default (ext:: runs commands)", ""},
	{key("browser.*.cmd"), "runs a program to open web pages (git help -w, instaweb)", ""},
	{key("browser.*.path"), "runs a program to open web pages (git help -w, instaweb)", ""},
	{key("man.*.cmd"), "runs a program to show help pages", ""},
	{key("man.*.path"), "runs a program to show help pages", ""},
	{key("tar.*.command"), "runs a program for git archive", ""},
	{key("trailer.*.command"), "runs a command when trailers are added (git commit --trailer)", ""},
	{key("trailer.*.cmd"), "runs a command when trailers are added (git commit --trailer)", ""},
	{key("instaweb.httpd"), "runs a web server program for git instaweb", ""},
	{key("sendemail.*cmd"), "runs a command for git send-email", ""},
	{key("sendemail.smtpserver"), "runs a program to send mail (git send-email)", "/"},
	{key("sendemail.*.smtpserver"), "runs a program to send mail (git send-email)", "/"},
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
	recs, err := RepoConfig(ctx, gd)
	if err != nil {
		return a, err
	}
	for _, rec := range recs {
		name, value, _ := strings.Cut(rec, "\n")
		k := strings.ToLower(name)
		if k == "core.hookspath" {
			hooksPath = value
		}
		for _, r := range riskyKeys {
			if !r.re.MatchString(k) || !strings.HasPrefix(strings.ToLower(value), r.value) {
				continue
			}
			a.Findings = append(a.Findings, Finding{
				Kind: "config", Name: name, Why: r.why,
				Detail: urlCredentials.ReplaceAllString(value, "${1}***@"),
				Fix:    "git config --local --unset-all " + quote(name),
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
		if err != nil || (runtime.GOOS != "windows" && info.Mode()&0o111 == 0) {
			continue
		}
		data, _ := os.ReadFile(p)
		sum := sha256.Sum256(data)
		a.Findings = append(a.Findings, Finding{
			Kind: "hook", Name: e.Name(), Detail: p, Why: "runs when git performs that action on this machine",
			Fix: "cat " + quote(p) + "   # inspect;  rm " + quote(p) + "   # remove",
		})
		fp = append(fp, "hook "+e.Name()+" "+hex.EncodeToString(sum[:]))
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
