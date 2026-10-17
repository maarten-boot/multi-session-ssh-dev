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

// fakeSSH puts an ssh on PATH that acts as sshfs: it sends an SFTP init and
// one STAT request (the share's first request, after which it counts as
// mounted), then runs body. $n is the number of the attempt, from 1.
func fakeSSH(t *testing.T, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell")
	}
	bin := t.TempDir()
	script := `#!/bin/sh
n=$(cat "` + bin + `/n" 2>/dev/null || echo 0); n=$((n+1)); echo $n >"` + bin + `/n"
printf '\000\000\000\005\001\000\000\000\003\000\000\000\012\021\000\000\000\001\000\000\000\001/'
` + body + "\n"
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// An attempt that dies right after its first request must not report the
// next attempt as mounted, nor close its ready channel (which the next
// attempt's own timer closed again: a panic).
func TestQuickFailureIsNotMounted(t *testing.T) {
	fakeSSH(t, `if [ "$n" = 1 ]; then sleep 0.1; echo "connection reset" >&2; exit 1; fi
exec cat >/dev/null`)
	m := NewManager(nil)
	defer m.StopAll()
	spec := Spec{Target: "fakehost", LocalPath: t.TempDir(), RemotePath: "/tmp/x"}
	// The first attempt is gone before its timer fires; the second starts
	// after a second of backoff.
	if err := m.Ensure("s-1", spec, 700*time.Millisecond); err == nil {
		t.Errorf("mounted while the share was retrying (%v)", m.Status("s-1"))
	}
	if err := m.Ensure("s-1", spec, 5*time.Second); err != nil {
		t.Fatalf("second attempt: %v", err)
	}
	time.Sleep(500 * time.Millisecond) // a stale timer would fire here
	if st := m.Status("s-1"); st.State != "mounted" {
		t.Errorf("status %v", st)
	}
}

// A host without FUSE fails for good instead of retrying forever.
func TestNoFUSEIsFatal(t *testing.T) {
	fakeSSH(t, `sleep 0.1; echo "fuse: device not found, try 'modprobe fuse' first" >&2; exit 1`)
	m := NewManager(nil)
	defer m.StopAll()
	spec := Spec{Target: "fakehost", LocalPath: t.TempDir(), RemotePath: "/tmp/x"}
	err := m.Ensure("s-1", spec, 10*time.Second)
	if st := m.Status("s-1"); err == nil || st.State != "failed" || !strings.Contains(st.Error, "FUSE") {
		t.Errorf("Ensure: %v, status %v", err, st)
	}
}
