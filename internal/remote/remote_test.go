package remote

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestValidID(t *testing.T) {
	for _, id := range []string{"a", "s-7f3a", "proj-1", strings.Repeat("a", 63)} {
		if !ValidID(id) {
			t.Errorf("ValidID(%q) = false", id)
		}
	}
	for _, id := range []string{"", "-a", "A", "a b", "a;b", "a.b", "a:b", "ä", strings.Repeat("a", 64)} {
		if ValidID(id) {
			t.Errorf("ValidID(%q) = true", id)
		}
	}
}

func TestArgsForceSafeOptions(t *testing.T) {
	attach, err := AttachArgs("user@host", "s1", "/srv/x", ModeClaude)
	if err != nil {
		t.Fatal(err)
	}
	kill, err := KillArgs("user@host", "s1")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{attach, kill} {
		for _, opt := range []string{"ForwardAgent=no", "ForwardX11=no", "ClearAllForwardings=yes", "RemoteCommand=none"} {
			i := slices.Index(args, opt)
			if i < 1 || args[i-1] != "-o" {
				t.Errorf("%q missing from %q", opt, args)
			}
		}
		// The target follows "--" so it can never be parsed as an option,
		// and the remote command is the only argument after it.
		if n := len(args); n < 3 || args[n-3] != "--" || args[n-2] != "user@host" {
			t.Errorf("bad tail: %q", args)
		}
	}
	if attach[0] != "-t" || kill[0] != "-T" || !slices.Contains(kill, "BatchMode=yes") {
		t.Errorf("tty/batch flags wrong: %q / %q", attach, kill)
	}
}

func TestArgsValidation(t *testing.T) {
	bad := []struct {
		target, id, path string
		mode             Mode
	}{
		{"", "s1", "/x", ModeShell},
		{"-oProxyCommand=evil", "s1", "/x", ModeShell},
		{"host extra", "s1", "/x", ModeShell},
		{"host", "S1", "/x", ModeShell},
		{"host", "s1;rm", "/x", ModeShell},
		{"host", "s1", "relative", ModeShell},
		{"host", "s1", "~user/x", ModeShell},
		{"host", "s1", "/a\nb", ModeShell},
		{"host", "s1", "/x", Mode("bash")},
	}
	for _, b := range bad {
		if _, err := AttachArgs(b.target, b.id, b.path, b.mode); err == nil {
			t.Errorf("AttachArgs(%q, %q, %q, %q) accepted", b.target, b.id, b.path, b.mode)
		}
	}
	if _, err := KillArgs("-x", "s1"); err == nil {
		t.Error("KillArgs accepted option-like target")
	}
}

// fakeRemote creates stand-ins for the remote login shell, tmux and claude
// that log what they receive, and returns an environment that uses them.
func fakeRemote(t *testing.T) (env []string, log string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	bin := t.TempDir()
	log = filepath.Join(t.TempDir(), "log")
	scripts := map[string]string{
		// Login shell: "-lc CMD" runs CMD, "-lic CMD" too but logs that it is
		// interactive; "-l" alone is the interactive fallback.
		"fakesh": `
case "$1" in
-lc) exec /bin/sh -c "$2" ;;
-lic) echo "interactive-login" >>"$LOG"; exec /bin/sh -c "$2" ;;
-l) echo "login-shell" >>"$LOG"; exit 0 ;;
esac
echo "unexpected shell args: $*" >>"$LOG"; exit 9`,
		// tmux logs cwd and args, then runs the pane command like tmux would.
		"tmux": `
echo "tmux cwd=$(pwd)" >>"$LOG"
for a in "$@"; do echo "tmux arg=$a" >>"$LOG"; done
for last in "$@"; do :; done
case "$1" in new-session) exec /bin/sh -c "$last" ;; esac`,
		// claude --continue fails like the real one when NO_CONVERSATION is set.
		"claude": `echo "claude cwd=$(pwd) args=$*" >>"$LOG"
if [ "$1" = --continue ] && [ -n "$NO_CONVERSATION" ]; then echo "No conversation found to continue" >&2; exit 1; fi`,
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env = append(os.Environ(), "SHELL="+filepath.Join(bin, "fakesh"), "PATH="+bin+":/usr/bin:/bin", "LOG="+log)
	return env, log
}

// runRemote executes the remote command (last ssh argument) the way sshd
// does: through the user's shell with -c.
func runRemote(t *testing.T, env, args []string) error {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", args[len(args)-1])
	cmd.Env = env
	cmd.Dir = "/"
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		t.Logf("remote output: %s", out)
	}
	return err
}

func readLog(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

func TestAttachCommandExecutes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell, and Windows forbids \" in file names")
	}
	// A directory name that breaks naive quoting in every way.
	dir := filepath.Join(t.TempDir(), `it's a "dir" with $HOME and `+"`id`"+` & ;`)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		mode Mode
		pane []string
	}{
		{ModeClaude, []string{"interactive-login", "claude cwd=" + dir + " args=", "login-shell"}},
		{ModeClaudeContinue, []string{"interactive-login", "claude cwd=" + dir + " args=--continue", "login-shell"}},
		{ModeShell, []string{"login-shell"}},
	}
	for _, c := range cases {
		t.Run(string(c.mode), func(t *testing.T) {
			env, log := fakeRemote(t)
			args, err := AttachArgs("host", "s-1", dir, c.mode)
			if err != nil {
				t.Fatal(err)
			}
			if err := runRemote(t, env, args); err != nil {
				t.Fatal(err)
			}
			got := readLog(t, log)
			want := []string{
				"tmux cwd=" + dir,
				"tmux arg=new-session", "tmux arg=-A", "tmux arg=-s", "tmux arg=cw-s-1",
				"tmux arg=-c", "tmux arg=" + dir,
			}
			if len(got) < len(want)+1 || !slices.Equal(got[:len(want)], want) {
				t.Fatalf("tmux invocation:\n got %q\nwant prefix %q", got, want)
			}
			// got[len(want)] is the pane command; what follows is its effect.
			if pane := got[len(want)+1:]; !slices.Equal(pane, c.pane) {
				t.Errorf("pane ran:\n got %q\nwant %q", pane, c.pane)
			}
		})
	}
}

// claude --continue without a conversation in the directory starts a new
// claude instead of leaving the user in a shell.
func TestAttachContinueWithoutConversation(t *testing.T) {
	env, log := fakeRemote(t)
	env = append(env, "NO_CONVERSATION=1")
	dir := t.TempDir()
	args, err := AttachArgs("host", "s-1", dir, ModeClaudeContinue)
	if err != nil {
		t.Fatal(err)
	}
	if err := runRemote(t, env, args); err != nil {
		t.Fatal(err)
	}
	var ran []string
	for _, l := range readLog(t, log) {
		if !strings.HasPrefix(l, "tmux ") {
			ran = append(ran, l)
		}
	}
	want := []string{"interactive-login", "claude cwd=" + dir + " args=--continue", "claude cwd=" + dir + " args=", "login-shell"}
	if !slices.Equal(ran, want) {
		t.Errorf("pane ran:\n got %q\nwant %q", ran, want)
	}
}

func TestAttachMissingDirFails(t *testing.T) {
	env, log := fakeRemote(t)
	args, err := AttachArgs("host", "s-1", filepath.Join(t.TempDir(), "missing"), ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	err = runRemote(t, env, args)
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != ExitMissingDir {
		t.Errorf("attach to missing directory: %v, want exit %d", err, ExitMissingDir)
	}
	if got := readLog(t, log); slices.ContainsFunc(got, func(s string) bool { return strings.HasPrefix(s, "tmux") }) {
		t.Errorf("tmux ran despite missing dir: %q", got)
	}
}

func TestKillCommandExecutes(t *testing.T) {
	env, log := fakeRemote(t)
	args, err := KillArgs("host", "s-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := runRemote(t, env, args); err != nil {
		t.Fatal(err)
	}
	want := []string{"tmux cwd=/", "tmux arg=kill-session", "tmux arg=-t", "tmux arg==cw-s-1"}
	if got := readLog(t, log); !slices.Equal(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestShellPath(t *testing.T) {
	for in, want := range map[string]string{
		"/a b/c": `'/a b/c'`,
		"~":      `"$HOME"`,
		"~/sw/x": `"$HOME"/'sw/x'`,
		"~/it's": `"$HOME"/'it'\''s'`,
	} {
		if got := ShellPath(in); got != want {
			t.Errorf("ShellPath(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestAttachHomeRelativePath(t *testing.T) {
	env, log := fakeRemote(t)
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, "sw", "proj-s-1"), 0o755)
	env = append(env, "HOME="+home)
	args, err := AttachArgs("host", "s-1", "~/sw/proj-s-1", ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if err := runRemote(t, env, args); err != nil {
		t.Fatal(err)
	}
	if got := readLog(t, log); len(got) < 7 || got[0] != "tmux cwd="+filepath.Join(home, "sw", "proj-s-1") ||
		got[6] != "tmux arg="+filepath.Join(home, "sw", "proj-s-1") {
		t.Errorf("log %q", got)
	}
}
