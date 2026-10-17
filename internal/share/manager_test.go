package share

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"session-workbench/internal/remote"
)

func TestMountScriptWithoutSSHFS(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell")
	}
	cmd := exec.Command("/bin/sh", "-c", mountScript("~/sw/x"))
	cmd.Env = []string{"PATH=/nonexistent", "HOME=" + t.TempDir()}
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != exitNoSSHFS || !strings.Contains(string(out), "sshfs is not installed") {
		t.Errorf("exit %v, output %q", err, out)
	}
}

func onAgent(t *testing.T, target, script string) (string, error) {
	t.Helper()
	args, _ := remote.ScriptArgs(target, script)
	out, err := exec.Command("ssh", args...).CombinedOutput()
	return string(out), err
}

// TestLiveShare needs an agent host with sshfs and FUSE, reachable without
// prompts: SW_SSH_TARGET=localhost go test ./internal/share -run LiveShare -v
func TestLiveShare(t *testing.T) {
	target := os.Getenv("SW_SSH_TARGET")
	if target == "" {
		t.Skip("SW_SSH_TARGET not set")
	}
	local := t.TempDir()
	os.WriteFile(filepath.Join(local, "a.txt"), []byte("from the host\n"), 0o644)
	base := fmt.Sprintf("sw-sharetest-%d", os.Getpid())
	mnt := "~/" + base + "/mnt"
	t.Cleanup(func() { onAgent(t, target, "fusermount3 -u -z ~/"+base+"/mnt; rm -rf ~/"+base) })

	var states []string
	m := NewManager(func(id string, st Status) { states = append(states, st.State) })
	spec := Spec{Target: target, LocalPath: local, RemotePath: mnt}
	if err := m.Ensure("s-1", spec, 20*time.Second); err != nil {
		t.Fatalf("mount: %v (states %v)", err, states)
	}
	out, err := onAgent(t, target, "cd "+mnt+" && cat a.txt && echo 'from the agent' >b.txt && mkdir sub && mv b.txt sub/ && ls -la sub")
	if err != nil || !strings.Contains(out, "from the host") {
		t.Fatalf("agent side: %v %s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(local, "sub", "b.txt")); string(b) != "from the agent\n" {
		t.Errorf("host sees %q", b)
	}
	// Owned by the agent-side user on the agent host.
	if out, _ := onAgent(t, target, "stat -c %U "+mnt+"/a.txt; id -un"); len(strings.Fields(out)) != 2 || strings.Fields(out)[0] != strings.Fields(out)[1] {
		t.Errorf("ownership on the agent: %q", out)
	}
	// Escapes are refused through sshfs too.
	if out, err := onAgent(t, target, "ln -s /etc "+mnt+"/etc-link"); err == nil {
		t.Errorf("absolute symlink created: %s", out)
	}
	if _, err := os.Lstat(filepath.Join(local, "etc-link")); err == nil {
		t.Error("symlink to /etc exists on the host")
	}

	// sshfs dies: the share comes back by itself.
	onAgent(t, target, "pkill -x sshfs")
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && m.Status("s-1").State != "retrying" {
		time.Sleep(100 * time.Millisecond)
	}
	if err := m.Ensure("s-1", spec, 20*time.Second); err != nil {
		t.Fatalf("remount: %v (states %v)", err, states)
	}
	if out, err := onAgent(t, target, "cat "+mnt+"/sub/b.txt"); err != nil || !strings.Contains(out, "from the agent") {
		t.Errorf("after remount: %v %s", err, out)
	}

	// Stopping unmounts.
	m.Stop("s-1")
	time.Sleep(time.Second)
	if out, _ := onAgent(t, target, "ls -A "+mnt+" | wc -l"); strings.TrimSpace(out) != "0" {
		t.Errorf("still mounted after stop: %q", out)
	}
	t.Logf("states: %v", states)
}

// A share that failed for good is tried again on the next Ensure (after the
// user fixed the cause), not reported from the old attempt forever.
func TestEnsureRetriesAfterFailure(t *testing.T) {
	m := NewManager(nil)
	local := filepath.Join(t.TempDir(), "later")
	spec := Spec{Target: "nohost.invalid", LocalPath: local, RemotePath: "/tmp/x"}
	err1 := m.Ensure("s-1", spec, 5*time.Second)
	if err1 == nil {
		t.Fatal("missing directory: no error")
	}
	if err := os.Mkdir(local, 0o755); err != nil {
		t.Fatal(err)
	}
	err2 := m.Ensure("s-1", spec, 2*time.Second)
	m.StopAll()
	if err2 == nil || err2.Error() == err1.Error() {
		t.Errorf("second Ensure reported the old failure again: %v", err2)
	}
}
