package terminal

import (
	"encoding/base64"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ConPTY smoke test: output arrives, input is delivered, the exit code is
// reported and the default shell starts.

type winRecorder struct {
	mu   sync.Mutex
	out  strings.Builder
	exit chan ExitInfo
}

func (r *winRecorder) Emit(name string, data ...any) {
	switch {
	case strings.HasPrefix(name, "term:data:"):
		b, _ := base64.StdEncoding.DecodeString(data[0].(string))
		r.mu.Lock()
		r.out.Write(b)
		r.mu.Unlock()
	case strings.HasPrefix(name, "term:exit:"):
		r.exit <- data[0].(ExitInfo)
	}
}

func (r *winRecorder) text() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.out.String()
}

func waitFor(t *testing.T, r *winRecorder, want string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(r.text(), want) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q; output: %q", want, r.text())
}

func TestConPTYEchoAndExitCode(t *testing.T) {
	r := &winRecorder{exit: make(chan ExitInfo, 1)}
	m := NewManager(r)
	id, err := m.Start(Spec{Command: "cmd.exe", Args: []string{"/q"}, Cols: 100, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	m.Write(id, []byte("set /a 6*7\r")) // the answer is not in the echoed input
	waitFor(t, r, "42")
	m.Write(id, []byte("exit 3\r"))
	select {
	case e := <-r.exit:
		if e.Code != 3 {
			t.Errorf("exit code %d, want 3", e.Code)
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("no exit; output: %q", r.text())
	}
}

func TestDefaultShellStarts(t *testing.T) {
	sh, args := DefaultShell()
	r := &winRecorder{exit: make(chan ExitInfo, 1)}
	m := NewManager(r)
	id, err := m.Start(Spec{Command: sh, Args: args, Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("%s: %v", sh, err)
	}
	cmd := "6*7\r" // PowerShell evaluates expressions
	if strings.EqualFold(filepath.Base(sh), "cmd.exe") {
		cmd = "set /a 6*7\r"
	}
	m.Write(id, []byte(cmd))
	waitFor(t, r, "42")
	m.Kill(id)
	select {
	case <-r.exit:
	case <-time.After(20 * time.Second):
		t.Fatal("no exit after kill")
	}
}
