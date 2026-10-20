// Package terminal runs processes attached to pseudo-terminals and streams
// their output to an Emitter (the Wails event bus in the app, a recorder in
// tests).
package terminal

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/aymanbagabas/go-pty"
)

const (
	flushInterval = 8 * time.Millisecond
	flushSize     = 32 * 1024
	// drainTimeout bounds how long we wait for output after the process
	// exits; a background child can keep the tty open indefinitely.
	drainTimeout = 2 * time.Second
)

// Emitter delivers events to the frontend.
type Emitter interface {
	Emit(name string, data ...any)
}

// EmitterFunc adapts a function to Emitter.
type EmitterFunc func(name string, data ...any)

func (f EmitterFunc) Emit(name string, data ...any) { f(name, data...) }

// Tap sees every terminal's output and input as they pass, and when a
// terminal ends (the Remote tab recorder). It must not block.
type Tap interface {
	Output(id string, b []byte)
	Input(id string, b []byte)
	Exit(id string)
}

// Spec describes the process to start.
type Spec struct {
	Command string
	Args    []string
	Dir     string
	Env     []string // appended to the current environment
	Cols    int
	Rows    int
	// OnStart, if set, gets the new terminal's id before its output is
	// read or its exit reported (to tell the tap what it is).
	OnStart func(id string)
}

// ExitInfo is the payload of the exit event.
type ExitInfo struct {
	Code  int    `json:"code"`
	Error string `json:"error,omitempty"`
}

// DataEvent and ExitEvent return the event names used for terminal id.
// Data payloads are base64 so that chunk boundaries inside multi-byte UTF-8
// sequences survive the JSON bridge; the frontend decodes to bytes.
func DataEvent(id string) string { return "term:data:" + id }
func ExitEvent(id string) string { return "term:exit:" + id }

// Manager owns all running terminals.
type Manager struct {
	emit  Emitter
	tap   Tap // set before the first Start
	mu    sync.Mutex
	next  int
	terms map[string]*term
}

func NewManager(emit Emitter) *Manager {
	return &Manager{emit: emit, terms: map[string]*term{}}
}

// SetTap installs tap; call it before starting terminals.
func (m *Manager) SetTap(tap Tap) { m.tap = tap }

type term struct {
	id     string
	pty    pty.Pty
	cmd    *pty.Cmd
	out    chan []byte
	once   sync.Once
	exited chan struct{} // closed when the process has ended
}

// Start launches spec in a new pty and returns the terminal id.
func (m *Manager) Start(spec Spec) (string, error) {
	if spec.Command == "" {
		return "", errors.New("terminal: empty command")
	}
	if spec.Cols <= 0 || spec.Rows <= 0 {
		spec.Cols, spec.Rows = 80, 24
	}
	p, err := pty.New()
	if err != nil {
		return "", fmt.Errorf("terminal: open pty: %w", err)
	}
	if err := p.Resize(spec.Cols, spec.Rows); err != nil {
		p.Close()
		return "", fmt.Errorf("terminal: resize: %w", err)
	}
	cmd := p.Command(spec.Command, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	if spec.Dir != "" {
		// Shells trust PWD when it names the cwd, so a Dir reached through a
		// symlink is shown as given rather than as its resolved path.
		cmd.Env = append(cmd.Env, "PWD="+spec.Dir)
	}
	cmd.Env = append(cmd.Env, spec.Env...)
	if err := cmd.Start(); err != nil {
		p.Close()
		return "", fmt.Errorf("terminal: start %s: %w", spec.Command, err)
	}
	// The child has its own copy of the slave end; dropping ours lets
	// reads on the master return EOF/EIO once the child side is gone.
	if up, ok := p.(pty.UnixPty); ok {
		up.Slave().Close()
	}

	m.mu.Lock()
	m.next++
	t := &term{id: fmt.Sprintf("t%d", m.next), pty: p, cmd: cmd, out: make(chan []byte, 64), exited: make(chan struct{})}
	m.terms[t.id] = t
	m.mu.Unlock()
	if spec.OnStart != nil {
		spec.OnStart(t.id)
	}

	readDone := make(chan struct{})
	go t.readLoop(readDone)
	pumpDone := make(chan struct{})
	go t.pump(m.emit, m.tap, pumpDone)
	go m.waitLoop(t, readDone, pumpDone)
	return t.id, nil
}

func (t *term) readLoop(done chan struct{}) {
	defer close(done)
	defer close(t.out)
	buf := make([]byte, flushSize)
	for {
		n, err := t.pty.Read(buf)
		if n > 0 {
			b := make([]byte, n)
			copy(b, buf[:n])
			t.out <- b
		}
		if err != nil {
			return
		}
	}
}

// pump coalesces output into batches so a chatty process does not flood
// the event bus with tiny messages.
func (t *term) pump(emit Emitter, tap Tap, done chan struct{}) {
	defer close(done)
	var pending []byte
	var tick <-chan time.Time
	flush := func() {
		if len(pending) > 0 {
			emit.Emit(DataEvent(t.id), base64.StdEncoding.EncodeToString(pending))
			pending = nil
		}
		tick = nil
	}
	for {
		select {
		case b, ok := <-t.out:
			if !ok {
				flush()
				return
			}
			if tap != nil {
				tap.Output(t.id, b)
			}
			pending = append(pending, b...)
			if len(pending) >= flushSize {
				flush()
			} else if tick == nil {
				tick = time.After(flushInterval)
			}
		case <-tick:
			flush()
		}
	}
}

func (m *Manager) waitLoop(t *term, readDone, pumpDone chan struct{}) {
	err := t.cmd.Wait()
	close(t.exited)
	info := ExitInfo{}
	if t.cmd.ProcessState != nil {
		info.Code = t.cmd.ProcessState.ExitCode()
	}
	var exitErr interface{ ExitCode() int }
	if err != nil && !errors.As(err, &exitErr) {
		info.Error = err.Error()
	}
	// ConPTY never signals EOF on its own; only Unix needs to drain.
	if _, ok := t.pty.(pty.UnixPty); ok {
		select {
		case <-readDone:
		case <-time.After(drainTimeout):
		}
	}
	t.close() // unblocks the reader on Windows / lingering children
	<-pumpDone
	m.mu.Lock()
	delete(m.terms, t.id)
	m.mu.Unlock()
	if m.tap != nil {
		m.tap.Exit(t.id)
	}
	m.emit.Emit(ExitEvent(t.id), info)
}

func (t *term) close() {
	t.once.Do(func() { t.pty.Close() })
}

func (m *Manager) get(id string) (*term, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.terms[id]
	if !ok {
		return nil, fmt.Errorf("terminal: unknown id %q", id)
	}
	return t, nil
}

// Write sends input (keystrokes, paste) to the terminal.
func (m *Manager) Write(id string, data []byte) error {
	t, err := m.get(id)
	if err != nil {
		return err
	}
	_, err = t.pty.Write(data)
	if err == nil && m.tap != nil {
		m.tap.Input(id, data)
	}
	return err
}

// Resize changes the pty window size; the kernel delivers SIGWINCH.
func (m *Manager) Resize(id string, cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	t, err := m.get(id)
	if err != nil {
		return err
	}
	return t.pty.Resize(cols, rows)
}

// hangupWait is how long a hung-up process gets to end by itself.
const hangupWait = 500 * time.Millisecond

// Kill ends the process; the exit event follows asynchronously. On Unix
// it first hangs up (SIGHUP, as closing a terminal window does), so a
// shell can save its history; whatever still runs after hangupWait is
// killed.
func (m *Manager) Kill(id string) error {
	t, err := m.get(id)
	if err != nil {
		return err
	}
	if p := t.cmd.Process; p != nil {
		if hangup(p) == nil {
			select {
			case <-t.exited:
			case <-time.After(hangupWait):
				p.Kill()
			}
		} else {
			p.Kill()
		}
	}
	t.close()
	return nil
}

// KillAll ends every terminal (app shutdown), all at once.
func (m *Manager) KillAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.terms))
	for id := range m.terms {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Kill(id)
		}()
	}
	wg.Wait()
}
