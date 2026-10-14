//go:build !windows

package remote

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"session-workbench/internal/terminal"
)

// TestSSHTmuxRoundTrip runs against a real agent host. The target must accept
// non-interactive key auth, have tmux, and see the local temp dir at the same
// path (e.g. localhost):
//
//	SW_SSH_TARGET=localhost go test ./internal/remote -run SSH -v
func TestSSHTmuxRoundTrip(t *testing.T) {
	target := os.Getenv("SW_SSH_TARGET")
	if target == "" {
		t.Skip("SW_SSH_TARGET not set")
	}
	id := fmt.Sprintf("swtest-%d", os.Getpid())
	dir := t.TempDir()
	hasSession := func() bool {
		cmd := exec.Command("ssh", "-o", "BatchMode=yes", target, "exec \"$SHELL\" -lc "+Quote("tmux has-session -t "+Quote("="+TmuxName(id))))
		return cmd.Run() == nil
	}
	t.Cleanup(func() {
		if args, err := KillArgs(target, id); err == nil {
			exec.Command("ssh", args...).Run()
		}
	})

	r := &collector{exit: make(chan terminal.ExitInfo, 1)}
	m := terminal.NewManager(r)
	attach := func() string {
		args, err := AttachArgs(target, id, dir, ModeShell)
		if err != nil {
			t.Fatal(err)
		}
		tid, err := m.Start(terminal.Spec{Command: "ssh", Args: args, Cols: 100, Rows: 30})
		if err != nil {
			t.Fatal(err)
		}
		return tid
	}

	// First attach creates the session in dir.
	tid := attach()
	r.waitFor(t, "$ ", "# ", "% ", "> ") // any prompt
	m.Write(tid, []byte("echo \"marker-$((6*7))\"; pwd; tmux display -p 'name=#S'\r"))
	r.waitFor(t, "marker-42")
	r.waitFor(t, "name="+TmuxName(id))
	if !strings.Contains(r.text(), dir) {
		t.Errorf("pwd %q not in output", dir)
	}

	// Dropping the ssh connection must leave the tmux session alive.
	m.Kill(tid)
	r.waitExit(t)
	if !hasSession() {
		t.Fatal("tmux session gone after ssh disconnect")
	}

	// Re-attach finds the same shell, including its state.
	r.reset()
	tid = attach()
	m.Write(tid, []byte("echo \"again-$((7*8))\"\r"))
	r.waitFor(t, "again-56")
	m.Kill(tid)
	r.waitExit(t)

	// Kill removes it.
	args, _ := KillArgs(target, id)
	if out, err := exec.Command("ssh", args...).CombinedOutput(); err != nil {
		t.Fatalf("kill: %v: %s", err, out)
	}
	if hasSession() {
		t.Error("tmux session still exists after kill")
	}
}

type collector struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	exit chan terminal.ExitInfo
}

func (c *collector) Emit(name string, data ...any) {
	switch {
	case strings.HasPrefix(name, "term:data:"):
		b, _ := base64.StdEncoding.DecodeString(data[0].(string))
		c.mu.Lock()
		c.buf.Write(b)
		c.mu.Unlock()
	case strings.HasPrefix(name, "term:exit:"):
		c.exit <- data[0].(terminal.ExitInfo)
	}
}

func (c *collector) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

func (c *collector) reset() {
	c.mu.Lock()
	c.buf.Reset()
	c.mu.Unlock()
}

func (c *collector) waitFor(t *testing.T, any ...string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		s := c.text()
		for _, a := range any {
			if strings.Contains(s, a) {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q; output: %q", any, c.text())
}

func (c *collector) waitExit(t *testing.T) {
	t.Helper()
	select {
	case <-c.exit:
	case <-time.After(10 * time.Second):
		t.Fatal("no exit event")
	}
}
