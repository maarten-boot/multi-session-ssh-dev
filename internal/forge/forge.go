// Package forge knows about the web side of a repository (GitHub, GitLab):
// links derived from its git remote, and GitHub Actions runs read with the
// GitHub CLI (gh) the user has installed and logged in to. The app itself
// stores no tokens.
package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Configure is applied to every command (hides console windows on Windows).
var Configure = func(*exec.Cmd) {}

// Repo is a repository on a forge.
type Repo struct {
	Kind  string `json:"kind"` // "github", "gitlab" or "generic"
	Host  string `json:"host"`
	Path  string `json:"path"` // owner/name (GitLab: group/subgroup/name)
	Web   string `json:"web"`  // https://host/path
	Label string `json:"label"`
}

// Link is a named web address.
type Link struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

var scpLike = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):(.+)$`) // git@github.com:owner/repo.git

// Parse understands the usual remote URL forms: git@host:path,
// ssh://[user@]host[:port]/path, https://[user[:token]@]host/path.
// Credentials in the URL are dropped.
func Parse(remote string) (Repo, bool) {
	remote = strings.TrimSpace(remote)
	var host, p string
	if u, err := url.Parse(remote); err == nil && u.Scheme != "" && u.Host != "" {
		switch u.Scheme {
		case "https", "http", "ssh", "git":
		default:
			return Repo{}, false
		}
		host, p = u.Hostname(), u.Path
	} else if m := scpLike.FindStringSubmatch(remote); m != nil && !strings.Contains(remote, "://") && !strings.Contains(m[1], `\`) {
		host, p = m[1], m[2]
	} else {
		return Repo{}, false
	}
	p = strings.Trim(strings.TrimSuffix(strings.Trim(p, "/"), ".git"), "/")
	if host == "" || !strings.Contains(p, "/") || strings.ContainsAny(p, " \t\n?#") {
		return Repo{}, false
	}
	r := Repo{Kind: "generic", Host: strings.ToLower(host), Path: p, Label: p}
	switch {
	case r.Host == "github.com":
		r.Kind = "github"
	case strings.Contains(r.Host, "gitlab"):
		r.Kind = "gitlab"
	}
	r.Web = "https://" + r.Host + "/" + r.Path
	return r, true
}

// Links returns the useful pages of a repository.
func (r Repo) Links() []Link {
	switch r.Kind {
	case "github":
		return []Link{
			{"Repository", r.Web}, {"Actions", r.Web + "/actions"}, {"Pull requests", r.Web + "/pulls"},
			{"Issues", r.Web + "/issues"}, {"Releases", r.Web + "/releases"},
		}
	case "gitlab":
		return []Link{
			{"Repository", r.Web}, {"Pipelines", r.Web + "/-/pipelines"}, {"Merge requests", r.Web + "/-/merge_requests"},
			{"Issues", r.Web + "/-/issues"}, {"Releases", r.Web + "/-/releases"},
		}
	}
	return []Link{{"Repository", r.Web}}
}

// ValidLink checks a user-entered link: a name and an http(s) URL.
func ValidLink(l Link) error {
	if strings.TrimSpace(l.Name) == "" {
		return errors.New("a link needs a name")
	}
	u, err := url.Parse(strings.TrimSpace(l.URL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("%q is not an http(s) address", l.URL)
	}
	return nil
}

// Run is one GitHub Actions workflow run.
type Run struct {
	ID         int64  `json:"databaseId"`
	Workflow   string `json:"workflowName"`
	Title      string `json:"displayTitle"`
	Branch     string `json:"headBranch"`
	Event      string `json:"event"`
	Status     string `json:"status"`     // queued, in_progress, completed, ...
	Conclusion string `json:"conclusion"` // success, failure, cancelled, skipped, ... (when completed)
	Created    string `json:"createdAt"`
	URL        string `json:"url"`
}

var (
	ErrNoGH     = errors.New("the GitHub CLI (gh) is not installed")
	ErrNotGitHb = errors.New("not a GitHub repository")
)

// HaveGH reports whether gh is on PATH.
func HaveGH() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// Runs lists the latest workflow runs of a GitHub repository. The
// repository is passed explicitly (-R), so gh does not run git in the
// project directory.
func Runs(ctx context.Context, r Repo, limit int) ([]Run, error) {
	if r.Kind != "github" {
		return nil, ErrNotGitHb
	}
	if !HaveGH() {
		return nil, ErrNoGH
	}
	cmd := exec.CommandContext(ctx, "gh", "run", "list", "-R", r.Path, "-L", fmt.Sprint(limit),
		"--json", "databaseId,workflowName,displayTitle,headBranch,event,status,conclusion,createdAt,url")
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	Configure(cmd)
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("gh: %s", msg)
	}
	runs := []Run{}
	if err := json.Unmarshal(out.Bytes(), &runs); err != nil {
		return nil, fmt.Errorf("gh: unexpected output: %w", err)
	}
	return runs, nil
}
