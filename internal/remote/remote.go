// Package remote builds the ssh command lines that attach to (or kill) a
// session's tmux session on the agent host.
//
// The remote side is reached through the user's own ssh client and config;
// this package only decides arguments. Every invocation forces agent and X11
// forwarding off so the isolated agent host never gets access to the host's
// credentials, whatever ~/.ssh/config says (command-line -o wins).
//
// Commands run inside the remote user's login shell ("$SHELL" -lc) so that
// PATH includes user-local installs such as ~/.local/bin/claude or a
// Homebrew tmux; a plain `ssh host cmd` does not read the login profile.
package remote

import (
	"fmt"
	"regexp"
	"strings"
)

// Mode selects what runs when the tmux session is first created.
type Mode string

const (
	ModeClaude         Mode = "claude"
	ModeClaudeContinue Mode = "claude-continue"
	ModeShell          Mode = "shell"
)

var idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidID reports whether id is safe to embed in a tmux session name.
func ValidID(id string) bool { return idRE.MatchString(id) }

// TmuxName is the tmux session name for a session id.
func TmuxName(id string) string { return "cw-" + id }

// Quote returns s as a single-quoted POSIX shell word. The '\” idiom also
// works in fish.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sshOptions are prepended to every invocation.
var sshOptions = []string{
	"-o", "ForwardAgent=no",
	"-o", "ForwardX11=no",
	"-o", "ForwardX11Trusted=no",
	"-o", "ClearAllForwardings=yes",
	"-o", "RemoteCommand=none", // a configured RemoteCommand would clash with ours
	"-o", "ServerAliveInterval=15", // notice dead connections within ~45 s
	"-o", "ServerAliveCountMax=3",
}

func checkTarget(target string) error {
	if target == "" || strings.HasPrefix(target, "-") || strings.ContainsAny(target, " \t\r\n\x00") {
		return fmt.Errorf("remote: invalid ssh target %q", target)
	}
	return nil
}

// paneCommand is what tmux runs in the session's first pane. When claude
// exits the pane falls back to a login shell instead of closing the session.
//
// claude starts from an interactive login shell (-lic), with the environment
// of a manual login: a non-interactive one skips most of ~/.bashrc (Ubuntu's
// returns early), where installers and users often add ~/.local/bin to PATH.
// The pane has a terminal, so the interactive shell is quiet; commands
// without one (the wrapper below, ScriptArgs) keep -lc.
func paneCommand(mode Mode) (string, error) {
	var cmd string
	switch mode {
	case ModeClaude:
		cmd = "claude"
	case ModeClaudeContinue:
		cmd = "claude --continue"
	case ModeShell:
		return `exec "$SHELL" -l`, nil
	default:
		return "", fmt.Errorf("remote: unknown start mode %q", mode)
	}
	return `exec "$SHELL" -lic ` + Quote(cmd+`; exec "$SHELL" -l`), nil
}

// ExitMissingDir is the exit status of an attach whose directory does not
// exist on the agent host (for example a working copy not sent yet).
const ExitMissingDir = 3

// CheckRemotePath accepts absolute paths and paths under the remote home
// ("~" or "~/...").
func CheckRemotePath(p string) error {
	if !(strings.HasPrefix(p, "/") || p == "~" || strings.HasPrefix(p, "~/")) || strings.ContainsAny(p, "\x00\n") {
		return fmt.Errorf("remote: remote path must be absolute or start with ~/: %q", p)
	}
	return nil
}

// ShellPath returns p as a shell word for the remote shell; a leading ~ is
// expanded through $HOME (a quoted ~ would not be).
func ShellPath(p string) string {
	switch {
	case p == "~":
		return `"$HOME"`
	case strings.HasPrefix(p, "~/"):
		return `"$HOME"/` + Quote(p[2:])
	}
	return Quote(p)
}

// AttachArgs returns the ssh arguments (without "ssh" itself) that attach to
// the session's tmux session, creating it in remotePath running mode's
// command if it does not exist yet. Run it in a pty.
func AttachArgs(target, id, remotePath string, mode Mode) ([]string, error) {
	if err := checkTarget(target); err != nil {
		return nil, err
	}
	if !ValidID(id) {
		return nil, fmt.Errorf("remote: invalid session id %q", id)
	}
	if err := CheckRemotePath(remotePath); err != nil {
		return nil, err
	}
	pane, err := paneCommand(mode)
	if err != nil {
		return nil, err
	}
	// The cd makes a missing directory a visible error (and exit status)
	// instead of tmux silently starting in $HOME. -A attaches if the session
	// exists, in which case -c and the pane command are ignored.
	tmux := fmt.Sprintf(`cd %s || { echo "session directory %s does not exist on this host" >&2; exit %d; }; `+
		`exec tmux new-session -A -s %s -c "$PWD" %s`,
		ShellPath(remotePath), strings.ReplaceAll(remotePath, `"`, ""), ExitMissingDir, TmuxName(id), Quote(pane))
	args := append([]string{"-t"}, sshOptions...)
	return append(args, "--", target, `exec "$SHELL" -lc `+Quote(tmux)), nil
}

// ScriptArgs returns ssh arguments that run script in the remote user's
// login shell without a terminal and without prompting (BatchMode).
func ScriptArgs(target, script string) ([]string, error) {
	if err := checkTarget(target); err != nil {
		return nil, err
	}
	args := append([]string{"-T", "-o", "BatchMode=yes"}, sshOptions...)
	return append(args, "--", target, `exec "$SHELL" -lc `+Quote(script)), nil
}

// SSHCommand is an ssh command line with the forced options, for tools that
// run ssh themselves (rsync -e, GIT_SSH_COMMAND). None of the words contain
// spaces, so both tools split it correctly.
func SSHCommand() string {
	return "ssh -o BatchMode=yes " + strings.Join(sshOptions, " ")
}

// SSHOptions returns the forced ssh options (for packages that build their
// own ssh command lines).
func SSHOptions() []string { return append([]string(nil), sshOptions...) }

// CheckTarget validates an ssh target (exported for packages that build
// their own command lines).
func CheckTarget(target string) error { return checkTarget(target) }

// KillArgs returns ssh arguments that kill the session's tmux session.
// It never prompts (BatchMode) since it runs without a terminal.
func KillArgs(target, id string) ([]string, error) {
	if err := checkTarget(target); err != nil {
		return nil, err
	}
	if !ValidID(id) {
		return nil, fmt.Errorf("remote: invalid session id %q", id)
	}
	// "=" makes tmux match the name exactly rather than as a prefix.
	tmux := "tmux kill-session -t " + Quote("="+TmuxName(id))
	args := append([]string{"-T", "-o", "BatchMode=yes"}, sshOptions...)
	return append(args, "--", target, `exec "$SHELL" -lc `+Quote(tmux)), nil
}
