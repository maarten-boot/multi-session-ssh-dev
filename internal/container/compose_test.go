package container

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestComposeDoc: the app's dev service is the container RunArgs makes.
func TestComposeDoc(t *testing.T) {
	s := spec(t)
	b, _ := json.Marshal(Engine{Name: "docker"}.ComposeDoc(s))
	doc := string(b)
	for _, want := range []string{`"name":"sw-s-1"`, `"container_name":"sw-s-1"`, `"image":"sw-s-1"`, `"pull_policy":"never"`,
		`"host_ip":"127.0.0.1","target":22`, `"io.session-workbench.session":"s-1"`,
		`"source":"` + s.LocalPath + `","target":"` + s.LocalPath + `"`, `"target":"/home/ann/.claude"`,
		`"read_only":true,"source":"` + s.sshdDir() + `","target":"/etc/sw"`,
		`"claude":{"external":true,"name":"sw-claude"}`} {
		if !strings.Contains(doc, want) {
			t.Errorf("no %s in %s", want, doc)
		}
	}
	if strings.Contains(doc, "keep-id") {
		t.Error("docker with keep-id")
	}
	s.OwnVolume = true
	b, _ = json.Marshal(Engine{Name: "podman", Rootless: true}.ComposeDoc(s))
	if doc = string(b); !strings.Contains(doc, `"userns_mode":"keep-id"`) || !strings.Contains(doc, `"user":"0:0"`) ||
		!strings.Contains(doc, `"name":"sw-claude-s-1"`) {
		t.Errorf("rootless podman: %s", doc)
	}
}

// TestComposeFile: the repository's file wins over the app's; none means
// no compose.
func TestComposeFile(t *testing.T) {
	s := spec(t)
	if f, _ := s.ComposeFile(); f != "" {
		t.Errorf("found %s", f)
	}
	os.MkdirAll(s.Dir, 0o700)
	os.WriteFile(s.ComposePath(false), []byte("services: {}\n"), 0o600)
	if f, in := s.ComposeFile(); f != filepath.Join(s.Dir, "compose.yaml") || in {
		t.Errorf("app's: %s %v", f, in)
	}
	os.MkdirAll(filepath.Join(s.LocalPath, ".session-workbench"), 0o755)
	os.WriteFile(s.ComposePath(true), []byte("services: {}\n"), 0o644)
	if f, in := s.ComposeFile(); f != filepath.Join(s.LocalPath, ".session-workbench", "compose.yaml") || !in {
		t.Errorf("repo's: %s %v", f, in)
	}
	args := strings.Join(Engine{Name: "docker"}.ComposeArgs(s, s.ComposePath(true), "up", "-d"), " ")
	if want := "compose -p sw-s-1 --project-directory " + filepath.Join(s.LocalPath, ".session-workbench") +
		" -f " + filepath.Join(s.Dir, "sw-compose.json") + " -f " + s.ComposePath(true) + " up -d"; args != want {
		t.Errorf("args\n%s\nwant\n%s", args, want)
	}
}

// devConfig is compose's merged view of an untouched dev service.
func devConfig(s Spec) string {
	return `{"services":{"dev":{"image":"sw-s-1","container_name":"sw-s-1","labels":{"` + Label + `":"s-1"},
		"ports":[{"target":22,"host_ip":"127.0.0.1"},{"target":8080,"published":"8080"}],
		"volumes":[{"type":"bind","source":"` + s.LocalPath + `","target":"` + s.LocalPath + `"},
		{"type":"volume","source":"claude","target":"/home/ann/.claude"},{"type":"bind","source":"x","target":"/etc/sw"}]},
		"postgres":{"image":"docker.io/library/postgres:17"}}}`
}

// TestCheckDev: the dev service keeps what the app needs.
func TestCheckDev(t *testing.T) {
	s := spec(t)
	good := devConfig(s)
	for name, c := range map[string]struct{ from, to, want string }{
		"untouched":  {"", "", ""},
		"podman":     {`"image":"sw-s-1"`, `"image":"localhost/sw-s-1"`, ""},
		"image":      {`"image":"sw-s-1"`, `"image":"ubuntu"`, "image must stay sw-s-1"},
		"build":      {`"image":"sw-s-1"`, `"image":"sw-s-1","build":{"context":"."}`, "build section"},
		"name":       {`"container_name":"sw-s-1"`, `"container_name":"box"`, "container_name"},
		"network":    {`"image":"sw-s-1"`, `"image":"sw-s-1","network_mode":"host"`, "network_mode"},
		"label":      {`":"s-1"}`, `":"other"}`, "label"},
		"ssh port":   {`{"target":22,"host_ip":"127.0.0.1"},`, ``, "ssh port"},
		"public ssh": {`"host_ip":"127.0.0.1"`, `"host_ip":"0.0.0.0"`, "ssh port"},
		"mount":      {`"target":"/etc/sw"`, `"target":"/etc/other"`, "mount at /etc/sw"},
		"no dev":     {`"services":{"dev":`, `"services":{"devx":`, `"dev" is gone`},
	} {
		var cfg composeConfig
		if err := json.Unmarshal([]byte(strings.Replace(good, c.from, c.to, 1)), &cfg); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		err := checkDev(cfg, s)
		if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
}

// TestCheckCompose runs compose config (the fake engine) on the text,
// next to where it goes, and names the file in compose's error.
func TestCheckCompose(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("container hosts are not supported on Windows")
	}
	s := spec(t)
	t.Setenv("SW_FAKE_ENGINE", "1")
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("SW_FAKE_ARGS", argsFile)
	e := Engine{Name: "docker", Path: os.Args[0]}
	ctx := context.Background()
	t.Setenv("SW_FAKE_CONFIG", "")
	err := e.CheckCompose(ctx, s, "services:\n  x: {imag: a}\n", true)
	if err == nil || !strings.Contains(err.Error(), "compose does not accept it: services.x additional properties") {
		t.Errorf("bad file: %v", err)
	}
	t.Setenv("SW_FAKE_CONFIG", strings.Join(strings.Fields(devConfig(s)), ""))
	if err := e.CheckCompose(ctx, s, "services: {}\n", true); err != nil {
		t.Errorf("good file: %v", err)
	}
	b, _ := os.ReadFile(argsFile)
	repo := filepath.Join(s.LocalPath, ".session-workbench")
	if !strings.Contains(string(b), "--project-directory "+repo+" -f "+s.appCompose()+" -f "+repo+string(filepath.Separator)+".compose-check-") ||
		!strings.Contains(string(b), " config --format json") {
		t.Errorf("args: %s", b)
	}
	if left, _ := filepath.Glob(filepath.Join(repo, ".compose-check-*")); len(left) > 0 {
		t.Errorf("left %v", left)
	}
	if _, err := os.Stat(s.appCompose()); err != nil {
		t.Errorf("no app file: %v", err)
	}
}

// TestComposeEnv: Docker needs nothing; Podman on Linux gets an API
// socket: its own one when it answers, else one the app starts.
func TestComposeEnv(t *testing.T) {
	if env, err := (Engine{Name: "docker"}).composeEnv(); env != nil || err != nil {
		t.Errorf("docker: %v %v", env, err)
	}
	if runtime.GOOS != "linux" {
		t.Skip("Podman's socket is set up only on Linux")
	}
	run := t.TempDir()
	if len(run) > 80 {
		run = "/tmp"
	}
	run, _ = os.MkdirTemp(run, "rt")
	t.Cleanup(func() { os.RemoveAll(run) })
	t.Setenv("XDG_RUNTIME_DIR", run)
	t.Setenv("DOCKER_HOST", "unix:///elsewhere.sock")
	e := Engine{Name: "podman", Path: "podman"}
	if env, _ := e.composeEnv(); strings.Join(env, " ") != "PODMAN_COMPOSE_WARNING_LOGS=false" {
		t.Errorf("with DOCKER_HOST: %v", env)
	}
	os.Unsetenv("DOCKER_HOST")
	if os.Geteuid() == 0 {
		t.Skip("root uses /run/podman")
	}
	started := 0
	defer func(f func(Engine, string) error) { startService = f }(startService)
	startService = func(_ Engine, sock string) error {
		started++
		l, err := net.Listen("unix", sock)
		if err == nil {
			t.Cleanup(func() { l.Close() })
		}
		return err
	}
	env, err := e.composeEnv()
	if err != nil || started != 1 || env[1] != "DOCKER_HOST=unix://"+ownSocket() {
		t.Errorf("own service: %v %v %d", env, err, started)
	}
	if env, _ = e.composeEnv(); started != 1 || env[1] != "DOCKER_HOST=unix://"+ownSocket() {
		t.Errorf("own service again: %v %d", env, started)
	}
	os.MkdirAll(filepath.Join(run, "podman"), 0o700)
	l, err := net.Listen("unix", filepath.Join(run, "podman", "podman.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if env, _ = e.composeEnv(); env[1] != "DOCKER_HOST=unix://"+filepath.Join(run, "podman", "podman.sock") {
		t.Errorf("podman's socket: %v", env)
	}
}

// TestComposeProject: Stop and Remove reach the services of the session's
// Compose project (the fake engine lists c-postgres, n-1, v-1 for sw-s-1).
func TestComposeProject(t *testing.T) {
	t.Setenv("SW_FAKE_ENGINE", "1")
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("SW_FAKE_ARGS", argsFile)
	e := Engine{Name: "fake", Path: os.Args[0]}
	ctx := context.Background()
	if names, err := e.ComposeServices(ctx, "s-1"); err != nil || strings.Join(names, ",") != "sw-s-1-postgres-1" {
		t.Errorf("services %v %v", names, err)
	}
	if err := e.Remove(ctx, "s-1", false, false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(argsFile)
	for _, want := range []string{"rm -f sw-s-1\n", "rm -f c-postgres\n", "network rm n-1\n"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("no %q in:\n%s", want, b)
		}
	}
	if strings.Contains(string(b), "volume rm") {
		t.Errorf("removed volumes without data:\n%s", b)
	}
	os.Remove(argsFile)
	e.Remove(ctx, "s-1", false, true)
	if b, _ = os.ReadFile(argsFile); !strings.Contains(string(b), "volume rm v-1\n") || strings.Contains(string(b), "volume rm sw-claude") {
		t.Errorf("data volumes:\n%s", b)
	}
}

// TestSetEnv: the dev service's environment as sshd's SetEnv, sorted;
// unset variables are left out; what sshd cannot carry is refused.
func TestSetEnv(t *testing.T) {
	str := func(s string) *string { return &s }
	if got, err := setEnv(map[string]*string{"B": str("two words"), "A": str("1"), "UNSET": nil}); err != nil ||
		got != `SetEnv="A=1" "B=two words"` {
		t.Errorf("%q %v", got, err)
	}
	if got, err := setEnv(map[string]*string{"UNSET": nil}); got != "" || err != nil {
		t.Errorf("none: %q %v", got, err)
	}
	for _, bad := range []map[string]*string{{"A": str(`say "hi"`)}, {"A": str("a\nb")}, {"A-B": str("x")}, {"A": str(`c:\x`)}} {
		if _, err := setEnv(bad); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}

func TestNetnsDenied(t *testing.T) {
	if !netnsDenied(fmt.Errorf("podman stop: Error: removing container x network: 1 error occurred:\n\t* rootless netns: kill network process: permission denied")) ||
		netnsDenied(nil) || netnsDenied(fmt.Errorf("podman stop: no such container")) {
		t.Error("netnsDenied")
	}
}
