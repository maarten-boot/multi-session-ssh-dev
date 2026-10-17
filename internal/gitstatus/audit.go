package gitstatus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
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
var riskyKeys = []struct {
	re  *regexp.Regexp
	why string
}{
	{key("core.fsmonitor"), "runs a program on every git status"},
	{key("core.hookspath"), "moves the hooks folder, possibly into the shared files"},
	{key("core.sshcommand"), "replaces the ssh program used for fetch and push"},
	{key("core.gitproxy"), "runs a program for git:// connections"},
	{key("core.pager"), "runs a program to page output"},
	{key("core.editor"), "runs a program as the editor"},
	{key("core.askpass"), "runs a program to ask for passwords"},
	{key("sequence.editor"), "runs a program during rebase"},
	{key("credential.*"), "decides where your credentials go"},
	{key("filter.*"), "runs a program on file contents (status, add, checkout)"},
	{key("diff.external"), "runs a program for git diff"},
	{key("diff.*.textconv"), "runs a program for git diff"},
	{key("diff.*.command"), "runs a program for git diff"},
	{key("merge.*.driver"), "runs a program during merges"},
	{key("gpg.program"), "runs a program to sign commits"},
	{key("gpg.*.program"), "runs a program to sign commits"},
	{key("include.path"), "loads more config from another file"},
	{key("includeif.*"), "loads more config from another file"},
	{key("remote.*.pushurl"), "sends your pushes (and code) to another address"},
	{key("url.*.insteadof"), "rewrites the addresses you fetch from or push to"},
	{key("url.*.pushinsteadof"), "rewrites the addresses you push to"},
	{key("http.proxy"), "routes git's HTTP traffic through a proxy"},
}

func key(pattern string) *regexp.Regexp {
	parts := strings.Split(regexp.QuoteMeta(pattern), `\*`)
	return regexp.MustCompile("^" + strings.Join(parts, `[^\n]+`) + "$")
}

var urlCredentials = regexp.MustCompile(`(://)[^/@\s]+@`)

// RunAudit checks the repository containing dir for active hooks and risky
// repository config. Like Get it runs only git commands that execute
// nothing from the repository.
func RunAudit(ctx context.Context, dir string) (Audit, error) {
	a := Audit{Findings: []Finding{}}
	top, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		if strings.Contains(err.Error(), "not a git repository") || strings.Contains(err.Error(), "executable file not found") {
			return a, nil
		}
		return a, err
	}
	root := strings.TrimSpace(string(top))
	a.Repo = true
	var fp []string

	// Config: the repository's own file only, without following includes.
	if out, err := git(ctx, root, "config", "--local", "--no-includes", "--list", "-z"); err == nil {
		for _, rec := range strings.Split(string(out), "\x00") {
			name, value, _ := strings.Cut(rec, "\n")
			if name == "" {
				continue
			}
			k := strings.ToLower(name)
			for _, r := range riskyKeys {
				if !r.re.MatchString(k) {
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
			if strings.HasPrefix(k, "alias.") && strings.HasPrefix(value, "!") {
				a.Findings = append(a.Findings, Finding{
					Kind: "config", Name: name, Detail: value, Why: "a git alias that runs a shell command",
					Fix: "git config --local --unset-all " + quote(name),
				})
				fp = append(fp, "config "+name+"="+value)
			}
		}
	} else if !strings.Contains(err.Error(), "exit status 1") { // 1: no local config
		return a, err
	}

	// Hooks: the folder git would really use (follows core.hooksPath).
	if out, err := git(ctx, root, "rev-parse", "--git-path", "hooks"); err == nil {
		hooks := strings.TrimSpace(string(out))
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
