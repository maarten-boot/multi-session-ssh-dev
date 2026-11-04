package container

// Services next to the dev container (SPEC C7): a session with a compose
// file (.session-workbench/compose.yaml in its directory, else compose.yaml
// in the app's data folder) runs as a Compose project Name(id). The app
// writes the dev service itself (appCompose: the image it built, the ssh
// port on 127.0.0.1, the mounts, the label); the session's file adds the
// other services and may add to "dev" (environment, depends_on, ports).
// Both go to compose: -f app -f yours, with your file's folder as the
// project directory (its relative paths and .env resolve there).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// RepoCompose is where a session's compose file lives in its directory.
const RepoCompose = ".session-workbench/compose.yaml"

// DevService is the dev container's service in the Compose project.
const DevService = "dev"

// ComposeProjectLabel is the label compose puts on its containers,
// networks and volumes.
const ComposeProjectLabel = "com.docker.compose.project"

// ComposeTemplate is what the Services window starts with.
const ComposeTemplate = `# Services next to this session's dev container, in Docker Compose format.
# The dev container is the service "dev" (Session Workbench makes it); it
# reaches the services here by their names, e.g. postgres:5432.
# Add to "dev" what it needs from them, for example:
#   dev:
#     environment:
#       DATABASE_URL: postgres://dev:dev@postgres:5432/dev
#     depends_on: [postgres]
services: {}
`

func regularFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// ComposeFile is the session's compose file: the repository's, else the
// one in Dir; "" when it has none (the container then runs on its own).
func (s Spec) ComposeFile() (file string, inRepo bool) {
	if repo := s.repoCompose(); regularFile(repo) {
		return repo, true
	}
	if app := s.dirCompose(); regularFile(app) {
		return app, false
	}
	return "", false
}

func (s Spec) repoCompose() string {
	return filepath.Join(s.LocalPath, filepath.FromSlash(RepoCompose))
}
func (s Spec) dirCompose() string { return filepath.Join(s.Dir, "compose.yaml") }

// ComposePath is where the compose file is saved: in the repository or in Dir.
func (s Spec) ComposePath(inRepo bool) string {
	if inRepo {
		return s.repoCompose()
	}
	return s.dirCompose()
}

// appCompose is the app's own compose file (written before each use).
func (s Spec) appCompose() string { return filepath.Join(s.Dir, "sw-compose.json") }

// ComposeDoc is the app's part of the project: the dev service as RunArgs
// makes the container (JSON is YAML too, and quotes paths safely).
func (e Engine) ComposeDoc(s Spec) map[string]any {
	dev := map[string]any{
		"image":          Name(s.ID),
		"pull_policy":    "never", // built by the app (Build), never pulled
		"container_name": Name(s.ID),
		"hostname":       Name(s.ID),
		"labels":         map[string]string{Label: s.ID},
		"ports":          []any{map[string]any{"target": 22, "host_ip": "127.0.0.1"}},
		"volumes": []any{
			map[string]any{"type": "bind", "source": s.LocalPath, "target": s.LocalPath},
			map[string]any{"type": "volume", "source": "claude", "target": s.User.Home() + "/.claude"},
			map[string]any{"type": "bind", "source": s.sshdDir(), "target": "/etc/sw", "read_only": true},
		},
	}
	if e.Rootless {
		dev["userns_mode"] = "keep-id"
		dev["user"] = "0:0"
	}
	return map[string]any{
		"name":     Name(s.ID),
		"services": map[string]any{DevService: dev},
		// External: compose never makes or removes the login volume
		// (ensureVolume makes it; down -v leaves it).
		"volumes": map[string]any{"claude": map[string]any{"external": true, "name": Volume(s.ID, s.OwnVolume)}},
	}
}

func (e Engine) writeAppCompose(s Spec) error {
	b, err := json.MarshalIndent(e.ComposeDoc(s), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(s.appCompose(), append(b, '\n'), 0o600)
}

// envCompose is the app's file that gives the dev container's sshd the
// environment the user's file sets (written by writeEnvCompose).
func (s Spec) envCompose() string { return filepath.Join(s.Dir, "sw-compose-env.json") }

// ComposeArgs is a compose command for the session with the user's file
// (and the environment file when there is one).
func (e Engine) ComposeArgs(s Spec, user string, args ...string) []string {
	out := []string{"compose", "-p", Name(s.ID), "--project-directory", filepath.Dir(user),
		"-f", s.appCompose(), "-f", user}
	if regularFile(s.envCompose()) {
		out = append(out, "-f", s.envCompose())
	}
	return append(out, args...)
}

// podmanSocket is where Podman's API answers by default; compose (the
// docker-compose provider of "podman compose") talks to it.
func podmanSocket() string {
	if os.Geteuid() == 0 {
		return "/run/podman/podman.sock"
	}
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "podman", "podman.sock")
	}
	return ""
}

// ownSocket is the API socket the app starts itself when Podman's own does
// not run (a short path: a Unix socket's path is at most 107 bytes).
func ownSocket() string {
	d := os.Getenv("XDG_RUNTIME_DIR")
	if d == "" {
		d = os.TempDir()
	}
	return filepath.Join(d, fmt.Sprintf("sw-podman-%d.sock", os.Getuid()))
}

func answers(sock string) bool {
	if sock == "" {
		return false
	}
	c, err := net.DialTimeout("unix", sock, time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// startService runs "podman system service" on sock; it ends by itself a
// minute after its last client.
var startService = func(e Engine, sock string) error {
	cmd := exec.Command(e.Path, "system", "service", "--time=60", "unix://"+sock)
	Configure(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// composeEnv is what compose needs in its environment. Docker needs
// nothing; Podman on Linux an API socket (DOCKER_HOST): its own when it
// runs (systemctl --user enable --now podman.socket), else one the app
// starts. macOS' Podman machine provides its socket itself.
func (e Engine) composeEnv() ([]string, error) {
	if e.Name != "podman" {
		return nil, nil
	}
	env := []string{"PODMAN_COMPOSE_WARNING_LOGS=false"}
	if runtime.GOOS != "linux" || os.Getenv("DOCKER_HOST") != "" {
		return env, nil
	}
	if sock := podmanSocket(); answers(sock) {
		return append(env, "DOCKER_HOST=unix://"+sock), nil
	}
	sock := ownSocket()
	if !answers(sock) {
		os.Remove(sock) // left by a service that ended
		if err := startService(e, sock); err != nil {
			return nil, fmt.Errorf("starting Podman's API service for compose: %w", err)
		}
		for i := 0; !answers(sock); i++ {
			if i == 100 {
				return nil, errors.New("Podman's API service for compose did not start (try: systemctl --user enable --now podman.socket)")
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	return append(env, "DOCKER_HOST=unix://"+sock), nil
}

// compose is the engine's compose command with its environment.
func (e Engine) compose(ctx context.Context, args ...string) (*exec.Cmd, error) {
	env, err := e.composeEnv()
	if err != nil {
		return nil, err
	}
	cmd := e.command(ctx, args...)
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	return cmd, nil
}

// ComposeVersion is compose's version line, or why there is none.
func (e Engine) ComposeVersion(ctx context.Context) (string, error) {
	cmd, err := e.compose(ctx, "compose", "version")
	if err != nil {
		return "", err
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		hint := "install Docker's compose plugin (e.g. sudo apt install docker-compose-plugin)"
		if e.Name == "podman" {
			hint = "install a compose provider for Podman: docker-compose (e.g. sudo apt install docker-compose-plugin) or podman-compose"
		}
		return "", fmt.Errorf("%s compose is not available (%s): %s", e.Name, hint, lastLine(errb.String(), err))
	}
	return lastLine(out.String(), nil), nil
}

func lastLine(s string, err error) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

// composeConfig is what CheckCompose reads of compose's merged project.
type composeConfig struct {
	Services map[string]struct {
		Image         string             `json:"image"`
		Environment   map[string]*string `json:"environment"`
		ContainerName string             `json:"container_name"`
		NetworkMode   string             `json:"network_mode"`
		Build         any                `json:"build"`
		Labels        map[string]string  `json:"labels"`
		Ports         []struct {
			Target int    `json:"target"`
			HostIP string `json:"host_ip"`
		} `json:"ports"`
		Volumes []struct {
			Type   string `json:"type"`
			Source string `json:"source"`
			Target string `json:"target"`
		} `json:"volumes"`
	} `json:"services"`
}

// CheckCompose checks text as the session's compose file (saved where
// inRepo says): compose must read it with the app's part, and the dev
// service must keep what the app needs. The error is compose's own or
// names what changed.
func (e Engine) CheckCompose(ctx context.Context, s Spec, text string, inRepo bool) error {
	if err := s.Check(); err != nil {
		return err
	}
	if err := e.writeAppCompose(s); err != nil {
		return err
	}
	dest := s.ComposePath(inRepo)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	// Next to where it goes, so its relative paths mean the same.
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".compose-check-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.WriteString(tmp, text); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	cmd, err := e.compose(ctx, e.ComposeArgs(s, tmp.Name(), "config", "--format", "json")...)
	if err != nil {
		return err
	}
	cfg, err := readConfig(cmd)
	if err != nil {
		return errors.New(strings.ReplaceAll(err.Error(), tmp.Name(), filepath.Base(dest)))
	}
	if err := checkDev(cfg, s); err != nil {
		return err
	}
	_, err = setEnv(cfg.Services[DevService].Environment)
	return err
}

// readConfig runs a compose config command and reads its JSON.
func readConfig(cmd *exec.Cmd) (composeConfig, error) {
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	var cfg composeConfig
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return cfg, fmt.Errorf("compose does not accept it: %s", msg)
	}
	if err := json.Unmarshal(out.Bytes(), &cfg); err != nil {
		return cfg, fmt.Errorf("reading compose's view of it: %w", err)
	}
	return cfg, nil
}

var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// setEnv is sshd's SetEnv option for the dev service's environment: a
// container's environment reaches its main process (sshd) only, not the
// ssh logins, which sshd gives what SetEnv names. "" without any.
func setEnv(env map[string]*string) (string, error) {
	var names []string
	for k, v := range env {
		if v != nil {
			names = append(names, k)
		}
	}
	if len(names) == 0 {
		return "", nil
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, k := range names {
		v := *env[k]
		if !envNameRE.MatchString(k) || strings.ContainsAny(v, "\"\n\r\\") {
			return "", fmt.Errorf("the dev service's environment variable %s cannot be passed to ssh logins (a name with odd characters, or a quote, backslash or line break in its value)", k)
		}
		parts[i] = `"` + k + "=" + v + `"`
	}
	return "SetEnv=" + strings.Join(parts, " "), nil
}

// writeEnvCompose writes (or removes) envCompose: the dev container's
// command is the image's sshd command with -o SetEnv=… for the
// environment the merged project gives the dev service. An image whose
// command is not sshd keeps its command (the logins then lack it).
func (e Engine) writeEnvCompose(ctx context.Context, s Spec, user string, out io.Writer) error {
	os.Remove(s.envCompose())
	cmd, err := e.compose(ctx, e.ComposeArgs(s, user, "config", "--format", "json")...)
	if err != nil {
		return err
	}
	cfg, err := readConfig(cmd)
	if err != nil {
		return err
	}
	opt, err := setEnv(cfg.Services[DevService].Environment)
	if err != nil || opt == "" {
		return err
	}
	raw, err := e.output(ctx, "image", "inspect", "--format", "{{json .Config.Cmd}}", Name(s.ID))
	if err != nil {
		return err
	}
	var run []string
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &run); len(run) == 0 || filepath.Base(run[0]) != "sshd" {
		fmt.Fprintf(out, "The image's command is not sshd (%s): the ssh logins do not get the dev service's environment.\r\n", strings.TrimSpace(raw))
		return nil
	}
	doc := map[string]any{"services": map[string]any{DevService: map[string]any{"command": append(run, "-o", opt)}}}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.envCompose(), append(b, '\n'), 0o600)
}

// checkDev: the dev service as the app made it.
func checkDev(cfg composeConfig, s Spec) error {
	dev, ok := cfg.Services[DevService]
	if !ok {
		return fmt.Errorf("the service %q is gone (it is the dev container)", DevService)
	}
	bad := func(what string) error {
		return fmt.Errorf("the dev service's %s must stay as Session Workbench sets it", what)
	}
	switch {
	case dev.Image != Name(s.ID) && !strings.HasSuffix(dev.Image, "/"+Name(s.ID)):
		return fmt.Errorf("the dev service's image must stay %s (change the dev container in the Dockerfile window)", Name(s.ID))
	case dev.Build != nil:
		return errors.New("the dev service cannot have a build section: the dev container is built from the Dockerfile window")
	case dev.ContainerName != Name(s.ID):
		return bad("container_name")
	case dev.NetworkMode != "":
		return bad("network (network_mode)")
	case dev.Labels[Label] != s.ID:
		return bad("label " + Label)
	}
	ssh := false
	for _, p := range dev.Ports {
		ssh = ssh || (p.Target == 22 && p.HostIP == "127.0.0.1")
	}
	if !ssh {
		return errors.New("the dev service must keep its ssh port (22 on 127.0.0.1): the Remote tab connects through it")
	}
	need := map[string]bool{s.LocalPath: false, s.User.Home() + "/.claude": false, "/etc/sw": false}
	for _, v := range dev.Volumes {
		if _, ok := need[v.Target]; ok {
			need[v.Target] = true
		}
	}
	for _, t := range []string{s.LocalPath, s.User.Home() + "/.claude", "/etc/sw"} {
		if !need[t] {
			return fmt.Errorf("the dev service must keep its mount at %s", t)
		}
	}
	return nil
}

// ensureVolume makes the ~/.claude volume (external in the project).
func (e Engine) ensureVolume(ctx context.Context, s Spec) error {
	v := Volume(s.ID, s.OwnVolume)
	if _, err := e.output(ctx, "volume", "inspect", v); err == nil {
		return nil
	}
	_, err := e.output(ctx, "volume", "create", v)
	return err
}

// upCompose is Up for a session with a compose file: the image as
// before, then compose up for the dev container and its services. While
// the dev container runs, nothing is made again (a change of the file
// waits for ComposeApply or the next start), so a reconnection never ends
// its tmux session.
func (e Engine) upCompose(ctx context.Context, s Spec, user string, out io.Writer) error {
	exists, running, err := e.State(ctx, s.ID)
	if err != nil {
		return err
	}
	if !exists && !e.ImageExists(ctx, s.ID) {
		file, _, _ := s.Dockerfile()
		fmt.Fprintf(out, "Building image %s from %s (the first time takes a few minutes)…\r\n", Name(s.ID), file)
		if err := e.Build(ctx, s, out); err != nil {
			return err
		}
	}
	if err := e.ensureVolume(ctx, s); err != nil {
		return err
	}
	if err := e.writeAppCompose(s); err != nil {
		return err
	}
	if err := e.writeEnvCompose(ctx, s, user, out); err != nil {
		return err
	}
	args := []string{"up", "-d", "--remove-orphans"}
	if running {
		args = []string{"up", "-d", "--no-recreate"}
	} else {
		fmt.Fprintf(out, "Starting %s and its services (%s)…\r\n", Name(s.ID), user)
	}
	return e.streamCompose(ctx, out, e.ComposeArgs(s, user, args...)...)
}

// ComposeApply brings the running project in line with the compose file:
// services that changed are made again, removed ones go (the dev
// container too, when its part changed: its tmux session ends).
func (e Engine) ComposeApply(ctx context.Context, s Spec, out io.Writer) error {
	user, _ := s.ComposeFile()
	if user == "" {
		return errors.New("the session has no compose file")
	}
	if err := Prepare(s); err != nil {
		return err
	}
	if !e.ImageExists(ctx, s.ID) {
		return fmt.Errorf("the image %s is not built yet: the Remote tab builds it when it connects", Name(s.ID))
	}
	if err := e.ensureVolume(ctx, s); err != nil {
		return err
	}
	if err := e.writeAppCompose(s); err != nil {
		return err
	}
	if err := e.writeEnvCompose(ctx, s, user, out); err != nil {
		return err
	}
	return e.streamCompose(ctx, out, e.ComposeArgs(s, user, "up", "-d", "--remove-orphans")...)
}

func (e Engine) streamCompose(ctx context.Context, out io.Writer, args ...string) error {
	cmd, err := e.compose(ctx, args...)
	if err != nil {
		return err
	}
	return streamCmd(cmd, out, e.Name+" compose")
}

// projectIDs are the ids of the objects (ps, network ls, volume ls) of the
// session's Compose project.
func (e Engine) projectIDs(ctx context.Context, id string, list ...string) ([]string, error) {
	args := append(list, "-q", "--filter", "label="+ComposeProjectLabel+"="+Name(id))
	out, err := e.output(ctx, args...)
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// ComposeServices are the names of the session's running service
// containers besides the dev container.
func (e Engine) ComposeServices(ctx context.Context, id string) ([]string, error) {
	out, err := e.output(ctx, "ps", "-a", "--filter", "label="+ComposeProjectLabel+"="+Name(id), "--format", "{{.Names}}")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, n := range strings.Fields(out) {
		if n != Name(id) {
			names = append(names, n)
		}
	}
	return names, nil
}

// netnsDenied: on Ubuntu 26.04, AppArmor's profile for pasta refuses
// Podman's SIGTERM when the last container of a user-defined network
// stops ("rootless netns: kill network process: permission denied"; the
// fix is "signal (receive) peer=podman," in
// /etc/apparmor.d/local/usr.bin.pasta). The containers stop or go all
// the same; only Podman's network helper stays.
func netnsDenied(err error) bool {
	return err != nil && strings.Contains(err.Error(), "rootless netns: kill network process")
}
