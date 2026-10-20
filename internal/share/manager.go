package share

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"

	"session-workbench/internal/remote"
)

// Exit statuses of the remote mount script.
const (
	exitNoSSHFS   = 4
	exitNoMountpt = 5
)

// Status of one share.
type Status struct {
	State string `json:"state"` // "starting", "mounted", "retrying", "failed", "stopped"
	Error string `json:"error"`
}

// Spec says what to share where.
type Spec struct {
	Target     string // ssh target
	LocalPath  string // served directory
	RemotePath string // mount point on the agent host (absolute or ~/...)
}

// Configure is applied to every ssh command (hides console windows on Windows).
var Configure = func(*exec.Cmd) {}

// Manager keeps one live share per session running while the app runs.
type Manager struct {
	notify func(id string, st Status)
	mu     sync.Mutex
	shares map[string]*running
}

type running struct {
	spec   Spec
	cancel context.CancelFunc
	status Status
	ready  chan struct{} // closed when the current attempt is mounted
	done   chan struct{} // closed when the loop has ended
}

// NewManager reports status changes through notify (may be nil).
func NewManager(notify func(id string, st Status)) *Manager {
	if notify == nil {
		notify = func(string, Status) {}
	}
	return &Manager{notify: notify, shares: map[string]*running{}}
}

// Ensure starts the share for id if needed and waits until it is mounted,
// failed for good, or the timeout passes.
func (m *Manager) Ensure(id string, spec Spec, timeout time.Duration) error {
	m.mu.Lock()
	r := m.shares[id]
	if r != nil && r.spec != spec { // the session moved: start over
		m.mu.Unlock()
		m.Stop(id)
		m.mu.Lock()
		r = nil
	}
	if r != nil {
		select {
		case <-r.done: // failed for good earlier: try again (e.g. sshfs installed since)
			r = nil
		default:
		}
	}
	if r == nil {
		ctx, cancel := context.WithCancel(context.Background())
		r = &running{spec: spec, cancel: cancel, ready: make(chan struct{}), done: make(chan struct{})}
		m.shares[id] = r
		go m.loop(ctx, id, r)
	}
	ready, done := r.ready, r.done
	m.mu.Unlock()
	select {
	case <-ready:
		return nil
	case <-done:
	case <-time.After(timeout):
	}
	st := m.Status(id)
	if st.State == "mounted" {
		return nil
	}
	if st.Error != "" {
		return errors.New(st.Error)
	}
	return fmt.Errorf("the shared directory is not mounted on the agent host yet (%s)", st.State)
}

// Status returns the share's current status ("stopped" if not running).
func (m *Manager) Status(id string) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.shares[id]; r != nil {
		return r.status
	}
	return Status{State: "stopped"}
}

// Stop ends a share; the agent host unmounts it.
func (m *Manager) Stop(id string) {
	m.mu.Lock()
	r := m.shares[id]
	delete(m.shares, id)
	m.mu.Unlock()
	if r != nil {
		r.cancel()
		<-r.done
		m.notify(id, Status{State: "stopped"})
	}
}

// StopAll ends every share (app shutdown).
func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.shares))
	for id := range m.shares {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Stop(id)
	}
}

func (m *Manager) set(id string, r *running, st Status) {
	m.mu.Lock()
	r.status = st
	m.mu.Unlock()
	m.notify(id, st)
}

// loop keeps the share mounted, reconnecting with backoff, until stopped
// or the agent host cannot mount it at all.
func (m *Manager) loop(ctx context.Context, id string, r *running) {
	defer close(r.done)
	attempt, noFUSE := 0, 0
	for {
		m.set(id, r, Status{State: "starting"})
		started := time.Now()
		err := m.attempt(ctx, id, r)
		if ctx.Err() != nil {
			return
		}
		// A host without FUSE fails the same way every time: give up after
		// it happened twice in a row, quickly.
		if err != nil && fuseMissing(err.Error()) && time.Since(started) < 10*time.Second {
			if noFUSE++; noFUSE >= 2 {
				err = &fatalError{err.Error() + " (FUSE is not available on the agent host)"}
			}
		} else {
			noFUSE = 0
		}
		var fatal *fatalError
		if errors.As(err, &fatal) {
			m.set(id, r, Status{State: "failed", Error: err.Error()})
			return
		}
		if time.Since(started) > time.Minute {
			attempt = 0
		}
		attempt++
		wait := min(30*time.Second, time.Second<<min(attempt-1, 5))
		msg := "the share was disconnected"
		if err != nil {
			msg = err.Error()
		}
		m.set(id, r, Status{State: "retrying", Error: fmt.Sprintf("%s; retrying in %s", msg, wait)})
		m.mu.Lock()
		r.ready = make(chan struct{}) // a new attempt must signal again
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// fuseMissing recognizes sshfs failing because the host has no FUSE.
func fuseMissing(msg string) bool {
	msg = strings.ToLower(msg)
	for _, s := range []string{"fuse: device not found", "/dev/fuse", "fusermount: not found", "fusermount3: not found", "fuse: failed to open"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

type fatalError struct{ msg string }

func (e *fatalError) Error() string { return e.msg }

// mountScript runs on the agent host with /bin/sh (not the login shell,
// whose profile might print text into the binary SFTP stream).
func mountScript(remotePath string) string {
	mp := remote.ShellPath(remotePath)
	return fmt.Sprintf(`PATH="$PATH:/usr/local/bin:/opt/homebrew/bin"
command -v sshfs >/dev/null 2>&1 || { echo "sshfs is not installed on this host (install sshfs and FUSE)" >&2; exit %d; }
# A mount left over from a lost connection reports "Transport endpoint is not connected".
(fusermount3 -u -z %[2]s || fusermount -u -z %[2]s || umount %[2]s) >/dev/null 2>&1
mkdir -p %[2]s || { echo "cannot create the mount point" >&2; exit %d; }
exec sshfs -f -o passive,uid="$(id -u)",gid="$(id -g)" share:/ %[2]s`, exitNoSSHFS, mp, exitNoMountpt)
}

func (m *Manager) attempt(ctx context.Context, id string, r *running) error {
	srv, err := NewServer(r.spec.LocalPath)
	if err != nil {
		return &fatalError{fmt.Sprintf("cannot share %s: %v", r.spec.LocalPath, err)}
	}
	defer srv.Close()
	if err := remote.CheckTarget(r.spec.Target); err != nil {
		return &fatalError{err.Error()}
	}
	if err := remote.CheckRemotePath(r.spec.RemotePath); err != nil {
		return &fatalError{err.Error()}
	}
	args := append([]string{"-T", "-o", "BatchMode=yes"}, remote.SSHOptions()...)
	args = append(args, "--", r.spec.Target, "exec /bin/sh -c "+remote.Quote(mountScript(r.spec.RemotePath)))
	cmd := exec.CommandContext(ctx, "ssh", args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	Configure(cmd)
	if err := cmd.Start(); err != nil {
		return &fatalError{fmt.Sprintf("starting ssh: %v", err)}
	}
	// sshfs sends its first file request right before it mounts; a moment
	// later the mount point is live. The timer belongs to this attempt: it
	// signals this attempt's ready channel, and nothing once the attempt has
	// ended (the loop then has a new channel for the next one).
	m.mu.Lock()
	ready := r.ready
	m.mu.Unlock()
	actx, acancel := context.WithCancel(ctx)
	defer func() {
		m.mu.Lock() // a timer that is setting "mounted" finishes first
		acancel()
		m.mu.Unlock()
	}()
	var once sync.Once
	onRequest := func() {
		once.Do(func() {
			time.AfterFunc(300*time.Millisecond, func() {
				m.mu.Lock()
				if actx.Err() != nil {
					m.mu.Unlock()
					return
				}
				st := Status{State: "mounted"}
				r.status = st
				close(ready)
				m.mu.Unlock()
				m.notify(id, st)
			})
		})
	}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.serve(rwc{stdout, stdin}, onRequest)
	}()
	werr := cmd.Wait()
	stdin.Close()
	<-serveErr
	msg := strings.TrimSpace(lastLine(stderr.String()))
	var ee *exec.ExitError
	if errors.As(werr, &ee) {
		switch ee.ExitCode() {
		case exitNoSSHFS, exitNoMountpt:
			return &fatalError{msg}
		case 255:
			if msg == "" {
				msg = "ssh failed"
			}
			return errors.New(msg)
		}
	}
	if msg != "" {
		return errors.New("sshfs: " + msg)
	}
	return nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}

type rwc struct {
	io.Reader
	io.WriteCloser
}

// serve is Serve with a callback on every request, used to notice that the
// client has connected.
func (s *Server) serve(c io.ReadWriteCloser, onRequest func()) error {
	h := notifying{s, onRequest}
	srv := sftp.NewRequestServer(c, sftp.Handlers{FileGet: h, FilePut: h, FileCmd: h, FileList: h})
	err := srv.Serve()
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return nil
	}
	return err
}

// notifying wraps the server's handlers to report activity.
type notifying struct {
	s  *Server
	on func()
}

// Every handler recovers: pkg/sftp runs them on its own goroutines, where
// a panic (a bug here or in pkg/sftp) would end the whole app and every
// terminal in it; the agent host gets an error for that request instead.
func guard(err *error) {
	if p := recover(); p != nil {
		*err = fmt.Errorf("share: internal error: %v", p)
	}
}

func (n notifying) Fileread(r *sftp.Request) (_ io.ReaderAt, err error) {
	defer guard(&err)
	n.on()
	return n.s.Fileread(r)
}
func (n notifying) Filewrite(r *sftp.Request) (_ io.WriterAt, err error) {
	defer guard(&err)
	n.on()
	return n.s.Filewrite(r)
}
func (n notifying) OpenFile(r *sftp.Request) (_ sftp.WriterAtReaderAt, err error) {
	defer guard(&err)
	n.on()
	return n.s.OpenFile(r)
}
func (n notifying) Filecmd(r *sftp.Request) (err error) {
	defer guard(&err)
	n.on()
	return n.s.Filecmd(r)
}
func (n notifying) PosixRename(r *sftp.Request) (err error) {
	defer guard(&err)
	n.on()
	return n.s.PosixRename(r)
}
func (n notifying) Filelist(r *sftp.Request) (_ sftp.ListerAt, err error) {
	defer guard(&err)
	n.on()
	return n.s.Filelist(r)
}
func (n notifying) Lstat(r *sftp.Request) (_ sftp.ListerAt, err error) {
	defer guard(&err)
	n.on()
	return n.s.Lstat(r)
}
func (n notifying) Readlink(p string) (_ string, err error) {
	defer guard(&err)
	n.on()
	return n.s.Readlink(p)
}
func (n notifying) RealPath(p string) (_ string, err error) {
	defer guard(&err)
	n.on()
	return n.s.RealPath(p)
}
