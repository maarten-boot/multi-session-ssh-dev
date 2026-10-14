//go:build !windows

package terminal

import (
	"bytes"
	"encoding/base64"
	"strings"
	"sync"
	"testing"
	"time"
)

type recorder struct {
	mu   sync.Mutex
	out  bytes.Buffer
	exit chan ExitInfo
}

func newRecorder() *recorder { return &recorder{exit: make(chan ExitInfo, 1)} }

func (r *recorder) Emit(name string, data ...any) {
	switch {
	case strings.HasPrefix(name, "term:data:"):
		b, err := base64.StdEncoding.DecodeString(data[0].(string))
		if err != nil {
			panic(err)
		}
		r.mu.Lock()
		r.out.Write(b)
		r.mu.Unlock()
	case strings.HasPrefix(name, "term:exit:"):
		r.exit <- data[0].(ExitInfo)
	}
}

func (r *recorder) output() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.out.String()
}

func (r *recorder) waitExit(t *testing.T) ExitInfo {
	t.Helper()
	select {
	case e := <-r.exit:
		return e
	case <-time.After(5 * time.Second):
		t.Fatalf("no exit event; output so far: %q", r.output())
		return ExitInfo{}
	}
}

func TestOutputAndExitCode(t *testing.T) {
	r := newRecorder()
	m := NewManager(r)
	if _, err := m.Start(Spec{Command: "/bin/sh", Args: []string{"-c", "echo hello; exit 3"}}); err != nil {
		t.Fatal(err)
	}
	e := r.waitExit(t)
	if e.Code != 3 {
		t.Errorf("exit code = %d, want 3", e.Code)
	}
	if !strings.Contains(r.output(), "hello") {
		t.Errorf("output %q missing hello", r.output())
	}
}

func TestLargeOutputIsComplete(t *testing.T) {
	r := newRecorder()
	m := NewManager(r)
	// ~600 KB, well past several flush batches.
	if _, err := m.Start(Spec{Command: "/bin/sh", Args: []string{"-c", "seq 1 100000; echo END"}}); err != nil {
		t.Fatal(err)
	}
	r.waitExit(t)
	out := r.output()
	if !strings.Contains(out, "\r\n99999\r\n100000\r\nEND") {
		t.Errorf("output truncated, tail: %q", out[max(0, len(out)-80):])
	}
}

func TestWriteDirAndEnv(t *testing.T) {
	r := newRecorder()
	m := NewManager(r)
	dir := t.TempDir()
	id, err := m.Start(Spec{Command: "/bin/sh", Dir: dir, Env: []string{"SW_TEST=xyz"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Write(id, []byte("echo \"[$SW_TEST][$TERM]\"; pwd; exit\n")); err != nil {
		t.Fatal(err)
	}
	r.waitExit(t)
	out := r.output()
	if !strings.Contains(out, "[xyz][xterm-256color]") {
		t.Errorf("env not applied: %q", out)
	}
	if !strings.Contains(out, dir) {
		t.Errorf("cwd %q not in output %q", dir, out)
	}
}

func TestResize(t *testing.T) {
	r := newRecorder()
	m := NewManager(r)
	id, err := m.Start(Spec{Command: "/bin/sh", Cols: 100, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	m.Write(id, []byte("stty size\n"))
	time.Sleep(200 * time.Millisecond)
	if err := m.Resize(id, 132, 43); err != nil {
		t.Fatal(err)
	}
	m.Write(id, []byte("stty size; exit\n"))
	r.waitExit(t)
	out := r.output()
	if !strings.Contains(out, "30 100") || !strings.Contains(out, "43 132") {
		t.Errorf("sizes not reported: %q", out)
	}
}

func TestKill(t *testing.T) {
	r := newRecorder()
	m := NewManager(r)
	id, err := m.Start(Spec{Command: "/bin/sh", Args: []string{"-c", "sleep 60"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Kill(id); err != nil {
		t.Fatal(err)
	}
	r.waitExit(t)
	if err := m.Write(id, []byte("x")); err == nil {
		t.Error("write to killed terminal succeeded")
	}
}

func TestStartErrors(t *testing.T) {
	m := NewManager(newRecorder())
	if _, err := m.Start(Spec{}); err == nil {
		t.Error("empty command accepted")
	}
	if _, err := m.Start(Spec{Command: "/nonexistent/binary"}); err == nil {
		t.Error("missing binary accepted")
	}
}
