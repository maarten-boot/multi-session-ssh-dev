// Package container runs a session's agent host in a container on this
// computer, with Podman or Docker: an image built from the session's
// Dockerfile (.session-workbench/Dockerfile in its directory, else one in
// the app's data folder, from Template), a container with the directory
// bind-mounted at the same path, and an sshd reachable on 127.0.0.1 only
// with a key the app made. The app then connects with ssh to the alias
// Name(id), with SSHOptions, as to any agent host.
package container

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Template is the starting Dockerfile of a session (Ubuntu 24.04 with
// sshd, tmux, git and Claude Code, and the part the app needs).
//
//go:embed template/Dockerfile
var Template string

// Configure is applied to every command (hides console windows on Windows).
var Configure = func(*exec.Cmd) {}

// Engines are the container engines, in the order Find tries them.
var Engines = []string{"podman", "docker"}

// SharedVolume is the volume for ~/.claude that the containers share (one
// Claude Code login for all); a session can have its own (Volume).
const SharedVolume = "sw-claude"

// RepoDockerfile is where a session's Dockerfile lives in its directory.
const RepoDockerfile = ".session-workbench/Dockerfile"

// Label marks the app's images and containers with their session id.
const Label = "io.session-workbench.session"

// Name is the image, container and ssh alias of a session.
func Name(id string) string { return "sw-" + id }

// Volume is the ~/.claude volume of a session: the shared one, or with own
// its own.
func Volume(id string, own bool) string {
	if own {
		return SharedVolume + "-" + id
	}
	return SharedVolume
}

// Engine is Podman or Docker.
type Engine struct {
	Name     string `json:"name"` // podman or docker
	Path     string `json:"path"`
	Rootless bool   `json:"rootless"` // Podman without root: --userns=keep-id
}

// Find returns the engine named pref, or with "" the first of Engines that
// is installed.
func Find(pref string) (Engine, error) {
	names := Engines
	if pref != "" {
		names = []string{pref}
	}
	for _, n := range names {
		p, err := exec.LookPath(n)
		if err != nil {
			continue
		}
		e := Engine{Name: n, Path: p}
		if n == "podman" {
			e.Rootless = os.Geteuid() != 0
		}
		return e, nil
	}
	if pref != "" {
		return Engine{}, fmt.Errorf("%s is not installed", pref)
	}
	return Engine{}, errors.New("neither Podman nor Docker is installed (install one: e.g. sudo apt install podman)")
}

func (e Engine) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, e.Path, args...)
	Configure(cmd)
	return cmd
}

// output runs the engine and returns its output, with the error text of a
// failure.
func (e Engine) output(ctx context.Context, args ...string) (string, error) {
	var out, errb bytes.Buffer
	cmd := e.command(ctx, args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), fmt.Errorf("%s %s: %s", e.Name, args[0], msg)
	}
	return out.String(), nil
}

// Version is the engine's version line.
func (e Engine) Version(ctx context.Context) (string, error) {
	out, err := e.output(ctx, "--version")
	return strings.TrimSpace(out), err
}

// User is who the session's user is inside the container: your name and
// ids, with a home in /home.
type User struct {
	Name string `json:"name"`
	UID  int    `json:"uid"`
	GID  int    `json:"gid"`
}

// Home is the user's home inside the container.
func (u User) Home() string { return "/home/" + u.Name }

// Spec is a session's container.
type Spec struct {
	ID        string `json:"id"`
	LocalPath string `json:"local_path"` // mounted at the same path
	Dir       string `json:"dir"`        // <data dir>/containers/<id>: keys, known_hosts, the Dockerfile without one in the repository
	OwnVolume bool   `json:"own_volume"` // its own ~/.claude volume instead of SharedVolume
	// ClaudeBinary is this computer's Claude Code (Linux only: it runs in
	// the container), put into the image; "" lets the installer get it.
	ClaudeBinary string `json:"claude_binary"`
	User         User   `json:"user"`
}

// Dockerfile is the session's Dockerfile and its build context: the
// repository's, else the one in Dir; inRepo tells which.
func (s Spec) Dockerfile() (file, context string, inRepo bool) {
	repo := filepath.Join(s.LocalPath, filepath.FromSlash(RepoDockerfile))
	if fi, err := os.Stat(repo); err == nil && fi.Mode().IsRegular() {
		return repo, filepath.Dir(repo), true
	}
	return filepath.Join(s.Dir, "Dockerfile"), s.Dir, false
}

func (s Spec) sshdDir() string    { return filepath.Join(s.Dir, "sshd") }
func (s Spec) certsDir() string   { return filepath.Join(s.Dir, "certs") }
func (s Spec) toolsDir() string   { return filepath.Join(s.Dir, "tools") }
func (s Spec) key() string        { return filepath.Join(s.Dir, "id_ed25519") }
func (s Spec) knownHosts() string { return filepath.Join(s.Dir, "known_hosts") }
func (s Spec) hostKey() string    { return filepath.Join(s.sshdDir(), "ssh_host_ed25519_key") }
func (s Spec) authorizedKeys() string {
	return filepath.Join(s.sshdDir(), "authorized_keys")
}

// Check rejects what the engine's command line cannot carry.
func (s Spec) Check() error {
	switch {
	case s.ID == "" || strings.ContainsAny(s.ID, "/\\: "):
		return fmt.Errorf("container: invalid session id %q", s.ID)
	case !filepath.IsAbs(s.LocalPath):
		return fmt.Errorf("container: the session directory %q is not absolute", s.LocalPath)
	case strings.ContainsAny(s.LocalPath, ",:\n"):
		return fmt.Errorf("container: the session directory %q has a comma or colon, which a mount cannot carry", s.LocalPath)
	case s.Dir == "":
		return errors.New("container: no data folder")
	case s.User.Name == "" || strings.ContainsAny(s.User.Name, "/: \n"):
		return fmt.Errorf("container: invalid user name %q", s.User.Name)
	}
	return nil
}

// CertDirs are where this computer keeps the certificate authorities it
// trusts besides the system's (Debian/Ubuntu, Fedora/RHEL).
var CertDirs = []string{"/usr/local/share/ca-certificates", "/etc/pki/ca-trust/source/anchors"}

// Prepare makes the session's files in Dir: the client key, the sshd host
// key (pinned in known_hosts under the alias), authorized_keys, the extra
// certificate authorities (copyCerts), and the Dockerfile from Template
// unless the repository or Dir has one.
func Prepare(s Spec) error {
	if err := s.Check(); err != nil {
		return err
	}
	if err := copyCerts(s.certsDir()); err != nil {
		return err
	}
	if err := linkTools(s.toolsDir(), s.ClaudeBinary); err != nil {
		return err
	}
	if err := os.MkdirAll(s.sshdDir(), 0o700); err != nil {
		return err
	}
	for _, k := range []string{s.key(), s.hostKey()} {
		if _, err := os.Stat(k); err == nil {
			continue
		}
		cmd := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", Name(s.ID), "-f", k)
		Configure(cmd)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("making a key: %v: %s", err, strings.TrimSpace(string(out)))
		}
	}
	pub, err := os.ReadFile(s.key() + ".pub")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.authorizedKeys(), pub, 0o600); err != nil {
		return err
	}
	host, err := os.ReadFile(s.hostKey() + ".pub")
	if err != nil {
		return err
	}
	f := strings.Fields(string(host))
	if len(f) < 2 {
		return fmt.Errorf("unreadable host key %s.pub", s.hostKey())
	}
	if err := os.WriteFile(s.knownHosts(), []byte(Name(s.ID)+" "+f[0]+" "+f[1]+"\n"), 0o600); err != nil {
		return err
	}
	return EnsureDockerfile(s)
}

// EnsureDockerfile writes Template to Dir/Dockerfile when neither the
// repository nor Dir has a Dockerfile (all the Dockerfile window needs).
func EnsureDockerfile(s Spec) error {
	if err := s.Check(); err != nil {
		return err
	}
	file, _, inRepo := s.Dockerfile()
	if inRepo {
		return nil
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(file, []byte(Template), 0o600)
}

// copyCerts fills dir with the PEM certificates in CertDirs, as .crt files
// (what update-ca-certificates reads); dir stays, maybe empty.
func copyCerts(dir string) error {
	os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	n := 0
	for _, d := range CertDirs {
		filepath.WalkDir(d, func(p string, de os.DirEntry, err error) error {
			if err != nil || de.IsDir() {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil || !bytes.Contains(b, []byte("-----BEGIN CERTIFICATE-----")) {
				return nil
			}
			n++
			name := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
			return os.WriteFile(filepath.Join(dir, fmt.Sprintf("%02d-%s.crt", n, name)), b, 0o644)
		})
	}
	return nil
}

// linkTools fills dir (readable inside the build) with claude, the Claude
// Code binary: a hard link, or a copy across file systems; without one dir
// stays empty.
func linkTools(dir, claude string) error {
	os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	os.Chmod(dir, 0o755)
	if claude == "" {
		return nil
	}
	src, err := filepath.EvalSymlinks(claude)
	if err != nil {
		return err
	}
	dst := filepath.Join(dir, "claude")
	if os.Link(src, dst) == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// SSHOptions are the ssh options for the alias Name(id) of a container
// listening on port: the address, the app's key only, the pinned host key.
func SSHOptions(s Spec, port int) []string {
	return []string{
		"-o", "HostName=127.0.0.1",
		"-o", "Port=" + strconv.Itoa(port),
		"-o", "User=" + s.User.Name,
		"-o", "IdentityFile=" + s.key(),
		"-o", "IdentitiesOnly=yes",
		"-o", "PasswordAuthentication=no",
		"-o", "UserKnownHostsFile=" + s.knownHosts(),
		"-o", "GlobalKnownHostsFile=/dev/null",
		"-o", "HostKeyAlias=" + Name(s.ID),
		"-o", "StrictHostKeyChecking=yes",
	}
}

// InsertOptions puts opts into ssh arguments before the "--" that ends the
// options.
func InsertOptions(args, opts []string) []string {
	i := len(args)
	for j, a := range args {
		if a == "--" {
			i = j
			break
		}
	}
	out := append(append(append([]string{}, args[:i]...), opts...), args[i:]...)
	return out
}

// BuildArgs builds the session's image.
func (e Engine) BuildArgs(s Spec) []string {
	file, ctx, _ := s.Dockerfile()
	return []string{"build", "-t", Name(s.ID), "-f", file, "--label", Label + "=" + s.ID,
		"--build-arg", "SW_USER=" + s.User.Name,
		"--build-arg", "SW_UID=" + strconv.Itoa(s.User.UID),
		"--build-arg", "SW_GID=" + strconv.Itoa(s.User.GID),
		"--build-context", "sw-certs=" + s.certsDir(),
		"--build-context", "sw-tools=" + s.toolsDir(),
		ctx}
}

// RunArgs starts the session's container: the directory at the same path,
// the ~/.claude volume, the sshd files read-only, ssh on 127.0.0.1 only.
func (e Engine) RunArgs(s Spec) []string {
	args := []string{"run", "-d", "--name", Name(s.ID), "--hostname", Name(s.ID), "--label", Label + "=" + s.ID,
		"-p", "127.0.0.1::22",
		"--mount", "type=bind,src=" + s.LocalPath + ",dst=" + s.LocalPath,
		"--mount", "type=volume,src=" + Volume(s.ID, s.OwnVolume) + ",dst=" + s.User.Home() + "/.claude",
		"--mount", "type=bind,src=" + s.sshdDir() + ",dst=/etc/sw,readonly"}
	if e.Rootless {
		// Your ids inside are your ids outside (files keep their owner);
		// sshd still starts as root inside.
		args = append(args, "--userns=keep-id", "--user", "0:0")
	}
	return append(args, Name(s.ID))
}

// State reports whether the session's container exists and runs.
func (e Engine) State(ctx context.Context, id string) (exists, running bool, err error) {
	out, err := e.output(ctx, "ps", "-a", "--filter", "label="+Label+"="+id, "--format", "{{.Names}} {{.State}}")
	if err != nil {
		return false, false, err
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == Name(id) {
			return true, strings.EqualFold(f[1], "running"), nil
		}
	}
	return false, false, nil
}

// ImageExists reports whether the session's image is built.
func (e Engine) ImageExists(ctx context.Context, id string) bool {
	return e.command(ctx, "image", "inspect", Name(id)).Run() == nil
}

// Port is the 127.0.0.1 port of the container's sshd.
func (e Engine) Port(ctx context.Context, id string) (int, error) {
	out, err := e.output(ctx, "port", Name(id), "22/tcp")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if i := strings.LastIndex(line, ":"); i >= 0 {
			if p, err := strconv.Atoi(line[i+1:]); err == nil && p > 0 {
				return p, nil
			}
		}
	}
	return 0, fmt.Errorf("no ssh port for %s (%q)", Name(id), strings.TrimSpace(out))
}

// stream runs the engine with its output (both streams) to out.
func (e Engine) stream(ctx context.Context, out io.Writer, args ...string) error {
	cmd := e.command(ctx, args...)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	var last string
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			if t := strings.TrimSpace(sc.Text()); t != "" {
				last = t
			}
			fmt.Fprintf(out, "%s\r\n", sc.Text())
		}
		io.Copy(io.Discard, pr)
	}()
	err := cmd.Run()
	pw.Close()
	<-done
	if err != nil {
		return fmt.Errorf("%s %s failed (%v): %s", e.Name, args[0], err, last)
	}
	return nil
}

// Build builds the session's image, its output to out.
func (e Engine) Build(ctx context.Context, s Spec, out io.Writer) error {
	if err := Prepare(s); err != nil {
		return err
	}
	return e.stream(ctx, out, e.BuildArgs(s)...)
}

// Up makes the session's container run: built when there is no image,
// created when there is no container, started when it is stopped; then it
// waits for sshd and returns its port. Progress goes to out.
func (e Engine) Up(ctx context.Context, s Spec, out io.Writer) (int, error) {
	if err := Prepare(s); err != nil {
		return 0, err
	}
	exists, running, err := e.State(ctx, s.ID)
	if err != nil {
		return 0, err
	}
	switch {
	case running:
	case exists:
		fmt.Fprintf(out, "Starting container %s…\r\n", Name(s.ID))
		if _, err := e.output(ctx, "start", Name(s.ID)); err != nil {
			return 0, err
		}
	default:
		if !e.ImageExists(ctx, s.ID) {
			file, _, _ := s.Dockerfile()
			fmt.Fprintf(out, "Building image %s from %s (the first time takes a few minutes)…\r\n", Name(s.ID), file)
			if err := e.Build(ctx, s, out); err != nil {
				return 0, err
			}
		}
		fmt.Fprintf(out, "Starting container %s…\r\n", Name(s.ID))
		if _, err := e.output(ctx, e.RunArgs(s)...); err != nil {
			return 0, err
		}
	}
	port, err := e.Port(ctx, s.ID)
	if err != nil {
		return 0, err
	}
	if err := WaitSSH(ctx, port, 60*time.Second); err != nil {
		return 0, err
	}
	return port, nil
}

// Replace removes the session's container so that the next Up creates it
// from the current image (after a rebuild); a missing one is fine.
func (e Engine) Replace(ctx context.Context, id string) error {
	exists, _, err := e.State(ctx, id)
	if err != nil || !exists {
		return err
	}
	_, err = e.output(ctx, "rm", "-f", Name(id))
	return err
}

// Stop stops the session's container (its tmux sessions end).
func (e Engine) Stop(ctx context.Context, id string) error {
	exists, running, err := e.State(ctx, id)
	if err != nil || !exists || !running {
		return err
	}
	_, err = e.output(ctx, "stop", Name(id))
	return err
}

// Remove deletes the session's container and image, and with volume its
// own ~/.claude volume (never the shared one).
func (e Engine) Remove(ctx context.Context, id string, volume bool) error {
	if err := e.Replace(ctx, id); err != nil {
		return err
	}
	if e.ImageExists(ctx, id) {
		if _, err := e.output(ctx, "rmi", Name(id)); err != nil {
			return err
		}
	}
	if volume {
		if _, err := e.output(ctx, "volume", "inspect", Volume(id, true)); err == nil {
			if _, err := e.output(ctx, "volume", "rm", Volume(id, true)); err != nil {
				return err
			}
		}
	}
	return nil
}

// WaitSSH waits until an ssh server answers on 127.0.0.1:port (its
// version line), at most timeout.
func WaitSSH(ctx context.Context, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for {
		c, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			c.SetReadDeadline(time.Now().Add(3 * time.Second))
			b := make([]byte, 4)
			_, rerr := io.ReadFull(c, b)
			c.Close()
			if rerr == nil && string(b) == "SSH-" {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the container's ssh server did not answer on port %d within %s", port, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
