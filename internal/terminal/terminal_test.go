//go:build !windows

package terminal

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
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

func TestDirThroughSymlinkKeepsLogicalPath(t *testing.T) {
	r := newRecorder()
	m := NewManager(r)
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(Spec{Command: "/bin/sh", Args: []string{"-c", "echo \"[$(pwd)]\""}, Dir: link}); err != nil {
		t.Fatal(err)
	}
	r.waitExit(t)
	if !strings.Contains(r.output(), "["+link+"]") {
		t.Errorf("pwd = %q, want %q", r.output(), link)
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

type tap struct {
	mu      sync.Mutex
	out, in bytes.Buffer
	exited  []string
}

func (t *tap) Output(id string, b []byte) { t.mu.Lock(); t.out.Write(b); t.mu.Unlock() }
func (t *tap) Input(id string, b []byte)  { t.mu.Lock(); t.in.Write(b); t.mu.Unlock() }
func (t *tap) Exit(id string)             { t.mu.Lock(); t.exited = append(t.exited, id); t.mu.Unlock() }

// The tap sees the same output as the frontend, the input, and the end,
// before the exit event.
func TestTap(t *testing.T) {
	r := newRecorder()
	m := NewManager(r)
	tp := &tap{}
	m.SetTap(tp)
	id, err := m.Start(Spec{Command: "/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	m.Write(id, []byte("echo tapped-$((6*7)); exit\n"))
	r.waitExit(t)
	tp.mu.Lock()
	defer tp.mu.Unlock()
	if tp.out.String() != r.output() || !strings.Contains(tp.out.String(), "tapped-42") {
		t.Errorf("tap output %q, frontend %q", tp.out.String(), r.output())
	}
	if tp.in.String() != "echo tapped-$((6*7)); exit\n" || len(tp.exited) != 1 || tp.exited[0] != id {
		t.Errorf("input %q, exited %v", tp.in.String(), tp.exited)
	}
}

// OnStart gets the id before the tap sees any output or the end, even of a
// process that exits at once.
func TestOnStart(t *testing.T) {
	r := newRecorder()
	m := NewManager(r)
	tp := &tap{}
	m.SetTap(tp)
	var started string
	id, err := m.Start(Spec{Command: "/bin/sh", Args: []string{"-c", "echo early"}, OnStart: func(id string) {
		time.Sleep(100 * time.Millisecond) // the process has exited by now
		tp.mu.Lock()
		defer tp.mu.Unlock()
		if tp.out.Len() > 0 || len(tp.exited) > 0 {
			t.Error("the tap saw the terminal before OnStart")
		}
		started = id
	}})
	if err != nil {
		t.Fatal(err)
	}
	r.waitExit(t)
	tp.mu.Lock()
	defer tp.mu.Unlock()
	if started != id || !strings.Contains(tp.out.String(), "early") {
		t.Errorf("OnStart got %q (id %q), tap output %q", started, id, tp.out.String())
	}
}
