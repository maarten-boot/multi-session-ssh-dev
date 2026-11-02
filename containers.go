package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"session-workbench/internal/container"
	"session-workbench/internal/qa"
	"session-workbench/internal/remote"
	"session-workbench/internal/session"
)

// A container host's sessions each run in their own container (see
// internal/container); the Remote tab connects with ssh to the alias
// container.Name(id), whose address, key and host key come from
// containerSSHOptions.

// containerEngine is the engine named in Settings, else Podman or Docker.
func (a *App) containerEngine() (container.Engine, error) {
	return container.Find(a.sessions.Config().UI.ContainerEngine)
}

var userNameRE = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// containerUser is you inside the containers: your name (when Linux takes
// it, else "dev") and ids.
func containerUser() container.User {
	u := container.User{Name: "dev", UID: 1000, GID: 1000}
	cur, err := user.Current()
	if err != nil {
		return u
	}
	if n := strings.ToLower(cur.Username); userNameRE.MatchString(n) {
		u.Name = n
	}
	if id, err := strconv.Atoi(cur.Uid); err == nil {
		u.UID = id
	}
	if id, err := strconv.Atoi(cur.Gid); err == nil {
		u.GID = id
	}
	return u
}

// containerSpec is a container session's container.
func (a *App) containerSpec(r session.Resolved) container.Spec {
	s := container.Spec{ID: r.ID, LocalPath: r.LocalPath, Dir: filepath.Join(a.dataDir, "containers", r.ID),
		OwnVolume: r.OwnClaudeVolume, User: containerUser()}
	if runtime.GOOS == "linux" {
		// Your own Claude Code goes into the image (no download, which a
		// company proxy may block); a macOS one would not run there.
		if p, err := qa.FindClaude(a.sessions.Config().UI.QAClaude); err == nil {
			s.ClaudeBinary = p
		}
	}
	return s
}

// containerSession resolves a session of a container host.
func (a *App) containerSession(id string) (session.Resolved, error) {
	r, err := a.sessions.Resolve(id)
	if err != nil {
		return r, err
	}
	if r.Host.Kind != session.HostContainer {
		return r, fmt.Errorf("%s does not run in a container", r.Name)
	}
	if a.dataDir == "" {
		return r, fmt.Errorf("no data folder for the container's files")
	}
	return r, nil
}

// containerSSHOptions gives ssh what it needs for a running container's
// alias (remote.TargetOptions); nothing for other targets or a container
// that does not run.
func (a *App) containerSSHOptions(target string) []string {
	id, ok := strings.CutPrefix(target, "sw-")
	if !ok {
		return nil
	}
	r, err := a.containerSession(id)
	if err != nil {
		return nil
	}
	e, err := a.containerEngine()
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	port, err := e.Port(ctx, id)
	if err != nil {
		return nil
	}
	return container.SSHOptions(a.containerSpec(r), port)
}

// containerAttach is what the Remote tab's helper (-container-attach)
// needs: the engine, the container, and the ssh arguments without the
// container's options (they depend on its port).
type containerAttach struct {
	Engine container.Engine `json:"engine"`
	Spec   container.Spec   `json:"spec"`
	Args   []string         `json:"args"`
}

// containerHelper writes the helper's file and returns its command line:
// this program with -container-attach, run in the Remote tab's terminal,
// so the build's output shows there before ssh takes over.
func (a *App) containerHelper(r session.Resolved, sshArgs []string) (string, []string, error) {
	e, err := a.containerEngine()
	if err != nil {
		return "", nil, err
	}
	spec := a.containerSpec(r)
	if err := spec.Check(); err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(spec.Dir, 0o700); err != nil {
		return "", nil, err
	}
	b, err := json.Marshal(containerAttach{Engine: e, Spec: spec, Args: sshArgs})
	if err != nil {
		return "", nil, err
	}
	file := filepath.Join(spec.Dir, "attach.json")
	if err := os.WriteFile(file, b, 0o600); err != nil {
		return "", nil, err
	}
	self, err := os.Executable()
	if err != nil {
		return "", nil, err
	}
	return self, []string{"-container-attach", file}, nil
}

// runContainerAttach is the helper: it makes the container run (building
// it the first time, the output in the terminal), then becomes ssh.
func runContainerAttach(file string) int {
	fail := func(err error) int {
		fmt.Fprintf(os.Stdout, "\r\n\x1b[1;31m%s\x1b[0m\r\n", err)
		return 1
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return fail(err)
	}
	var at containerAttach
	if err := json.Unmarshal(b, &at); err != nil {
		return fail(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	port, err := at.Engine.Up(ctx, at.Spec, os.Stdout)
	if err != nil {
		return fail(err)
	}
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		return fail(fmt.Errorf("ssh client not found: %w", err))
	}
	stop()
	return fail(execInPlace(ssh, container.InsertOptions(at.Args, container.SSHOptions(at.Spec, port))))
}

// --- bindings ---

// ContainerEngineInfo describes the engine Settings would use (name, path,
// version), or why there is none.
func (a *App) ContainerEngineInfo(pref string) (string, error) {
	e, err := container.Find(pref)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	v, err := e.Version(ctx)
	if err != nil {
		return "", err
	}
	mode := ""
	if e.Rootless {
		mode = ", rootless"
	}
	return fmt.Sprintf("%s (%s%s)", v, e.Path, mode), nil
}

// ContainerFile is a session's Dockerfile for the Dockerfile window.
type ContainerFile struct {
	Path   string `json:"path"`
	Text   string `json:"text"`
	InRepo bool   `json:"in_repo"` // .session-workbench/Dockerfile in the session directory
	State  string `json:"state"`   // "running", "stopped", "none" (no container), or the engine problem
}

// ContainerDockerfile returns the session's Dockerfile (made from the
// template the first time).
func (a *App) ContainerDockerfile(id string) (ContainerFile, error) {
	r, err := a.containerSession(id)
	if err != nil {
		return ContainerFile{}, err
	}
	spec := a.containerSpec(r)
	if err := container.EnsureDockerfile(spec); err != nil {
		return ContainerFile{}, err
	}
	file, _, inRepo := spec.Dockerfile()
	b, err := os.ReadFile(file)
	if err != nil {
		return ContainerFile{}, err
	}
	return ContainerFile{Path: file, Text: string(b), InRepo: inRepo, State: a.containerState(id)}, nil
}

func (a *App) containerState(id string) string {
	e, err := a.containerEngine()
	if err != nil {
		return err.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	exists, running, err := e.State(ctx, id)
	switch {
	case err != nil:
		return err.Error()
	case running:
		return "running"
	case exists:
		return "stopped"
	}
	return "none"
}

// SaveContainerDockerfile writes the session's Dockerfile; with inRepo it
// goes to .session-workbench/Dockerfile in the session directory (made if
// needed), else to the app's copy.
func (a *App) SaveContainerDockerfile(id, text string, inRepo bool) (string, error) {
	r, err := a.containerSession(id)
	if err != nil {
		return "", err
	}
	spec := a.containerSpec(r)
	file := filepath.Join(spec.Dir, "Dockerfile")
	if inRepo {
		file = filepath.Join(r.LocalPath, filepath.FromSlash(container.RepoDockerfile))
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", err
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return "", err
	}
	return file, os.Rename(tmp, file)
}

// containerBuilds are the rebuilds running, one per session.
var containerBuilds = struct {
	sync.Mutex
	stop map[string]context.CancelFunc
}{stop: map[string]context.CancelFunc{}}

// ContainerRebuild builds the session's image again, its output as
// "container:build:<id>" events, then removes the container so the Remote
// tab's next connection starts it from the new image (tmux in the old one
// ends; the frontend asks first).
func (a *App) ContainerRebuild(id string) error {
	r, err := a.containerSession(id)
	if err != nil {
		return err
	}
	e, err := a.containerEngine()
	if err != nil {
		return err
	}
	containerBuilds.Lock()
	if containerBuilds.stop[id] != nil {
		containerBuilds.Unlock()
		return fmt.Errorf("the image of %s is being built", r.Name)
	}
	ctx, cancel := context.WithCancel(context.Background())
	containerBuilds.stop[id] = cancel
	containerBuilds.Unlock()
	defer func() {
		cancel()
		containerBuilds.Lock()
		delete(containerBuilds.stop, id)
		containerBuilds.Unlock()
	}()
	out := eventWriter(func(s string) { wruntime.EventsEmit(a.ctx, "container:build:"+id, s) })
	if err := e.Build(ctx, a.containerSpec(r), out); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("stopped")
		}
		return err
	}
	return e.Replace(context.Background(), id)
}

// ContainerStopBuild stops a running rebuild.
func (a *App) ContainerStopBuild(id string) {
	containerBuilds.Lock()
	defer containerBuilds.Unlock()
	if stop := containerBuilds.stop[id]; stop != nil {
		stop()
	}
}

// ContainerStop stops the session's container (its tmux sessions end).
func (a *App) ContainerStop(id string) error {
	if _, err := a.containerSession(id); err != nil {
		return err
	}
	e, err := a.containerEngine()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return e.Stop(ctx, id)
}

// ContainerRemove deletes the session's container and image, and with
// volume its own ~/.claude volume, and the app's files for it (keys; the
// Dockerfile in the data folder). Used when the session is deleted.
func (a *App) ContainerRemove(id string, volume bool) error {
	e, err := a.containerEngine()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := e.Remove(ctx, id, volume); err != nil {
		return err
	}
	if a.dataDir != "" && remote.ValidID(id) {
		return os.RemoveAll(filepath.Join(a.dataDir, "containers", id))
	}
	return nil
}

// eventWriter turns written text into calls of emit, a piece at a time.
type eventWriter func(string)

func (w eventWriter) Write(p []byte) (int, error) {
	w(string(p))
	return len(p), nil
}
