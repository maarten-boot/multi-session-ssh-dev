package container

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"session-workbench/internal/profile"
)

// TestMain lets the test binary stand in for an engine (SW_FAKE_ENGINE=1):
// it notes its arguments in $SW_FAKE_ARGS and answers ps and port.
func TestMain(m *testing.M) {
	if os.Getenv("SW_FAKE_ENGINE") == "" {
		os.Exit(m.Run())
	}
	args := os.Args[1:]
	f, _ := os.OpenFile(os.Getenv("SW_FAKE_ARGS"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	fmt.Fprintln(f, strings.Join(args, " "))
	f.Close()
	switch args[0] {
	case "ps":
		fmt.Println("sw-s-1 running")
		fmt.Println("sw-s-10 exited")
	case "port":
		fmt.Println("127.0.0.1:41234")
		fmt.Println("[::1]:41234")
	case "fail":
		fmt.Fprintln(os.Stderr, "something broke")
		os.Exit(1)
	}
	os.Exit(0)
}

func spec(t *testing.T) Spec {
	dir := t.TempDir()
	proj := filepath.Join(dir, "proj")
	os.MkdirAll(proj, 0o755)
	return Spec{ID: "s-1", LocalPath: proj, Dir: filepath.Join(dir, "c"), User: User{Name: "ann", UID: 1001, GID: 1002}}
}

func TestArgs(t *testing.T) {
	s := spec(t)
	run := strings.Join(Engine{Name: "docker"}.RunArgs(s), " ")
	for _, want := range []string{"run -d --name sw-s-1", "-p 127.0.0.1::22",
		"type=bind,src=" + s.LocalPath + ",dst=" + s.LocalPath,
		"type=volume,src=sw-claude,dst=/home/ann/.claude", "dst=/etc/sw,readonly", Label + "=s-1"} {
		if !strings.Contains(run, want) {
			t.Errorf("run lacks %q: %s", want, run)
		}
	}
	if strings.Contains(run, "keep-id") || !strings.HasSuffix(run, " sw-s-1") {
		t.Errorf("docker run: %s", run)
	}
	s.OwnVolume = true
	run = strings.Join(Engine{Name: "podman", Rootless: true}.RunArgs(s), " ")
	if !strings.Contains(run, "--userns=keep-id --user 0:0 sw-s-1") || !strings.Contains(run, "src=sw-claude-s-1,") {
		t.Errorf("rootless podman run: %s", run)
	}
	build := strings.Join(Engine{Name: "podman"}.BuildArgs(s), " ")
	for _, want := range []string{"build -t sw-s-1 -f " + filepath.Join(s.Dir, "Dockerfile"), "SW_USER=ann", "SW_UID=1001",
		"SW_GID=1002", "sw-certs=" + filepath.Join(s.Dir, "certs"), "sw-tools=" + filepath.Join(s.Dir, "tools")} {
		if !strings.Contains(build, want) {
			t.Errorf("build lacks %q: %s", want, build)
		}
	}
	if !strings.HasSuffix(build, " "+s.Dir) {
		t.Errorf("build context: %s", build)
	}
	opts := strings.Join(SSHOptions(s, 41234), " ")
	for _, want := range []string{"HostName=127.0.0.1", "Port=41234", "User=ann", "IdentitiesOnly=yes",
		"HostKeyAlias=sw-s-1", "StrictHostKeyChecking=yes", "UserKnownHostsFile=" + filepath.Join(s.Dir, "known_hosts")} {
		if !strings.Contains(opts, want) {
			t.Errorf("ssh options lack %q: %s", want, opts)
		}
	}
	got := InsertOptions([]string{"-t", "-o", "A=1", "--", "sw-s-1", "cmd"}, []string{"-o", "Port=2"})
	if strings.Join(got, " ") != "-t -o A=1 -o Port=2 -- sw-s-1 cmd" {
		t.Errorf("insert: %q", got)
	}
}

func TestCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("container hosts are not supported on Windows (a path with C: cannot be mounted)")
	}
	good := spec(t)
	if err := good.Check(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []func(*Spec){
		func(s *Spec) { s.ID = "" }, func(s *Spec) { s.ID = "a/b" },
		func(s *Spec) { s.LocalPath = "rel/path" }, func(s *Spec) { s.LocalPath += ",x" }, func(s *Spec) { s.LocalPath += ":x" },
		func(s *Spec) { s.Dir = "" }, func(s *Spec) { s.User.Name = "" },
	} {
		s := good
		bad(&s)
		if s.Check() == nil {
			t.Errorf("accepted %+v", s)
		}
	}
}

func TestPrepare(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("container hosts are not supported on Windows (a path with C: cannot be mounted)")
	}
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("no ssh-keygen")
	}
	s := spec(t)
	certs := t.TempDir()
	os.WriteFile(filepath.Join(certs, "corp.crt"), []byte("-----BEGIN CERTIFICATE-----\nx\n-----END CERTIFICATE-----\n"), 0o644)
	os.WriteFile(filepath.Join(certs, "notes.txt"), []byte("not a certificate"), 0o644)
	old := CertDirs
	CertDirs = []string{certs, filepath.Join(certs, "missing")}
	defer func() { CertDirs = old }()
	bin := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	s.ClaudeBinary = bin
	if err := Prepare(s); err != nil {
		t.Fatal(err)
	}
	kh, _ := os.ReadFile(filepath.Join(s.Dir, "known_hosts"))
	host, _ := os.ReadFile(filepath.Join(s.Dir, "sshd", "ssh_host_ed25519_key.pub"))
	if f := strings.Fields(string(host)); !strings.HasPrefix(string(kh), "sw-s-1 ssh-ed25519 "+f[1]) {
		t.Errorf("known_hosts %q", kh)
	}
	auth, _ := os.ReadFile(filepath.Join(s.Dir, "sshd", "authorized_keys"))
	pub, _ := os.ReadFile(filepath.Join(s.Dir, "id_ed25519.pub"))
	if string(auth) != string(pub) || len(pub) == 0 {
		t.Error("authorized_keys is not the client key")
	}
	if df, _ := os.ReadFile(filepath.Join(s.Dir, "Dockerfile")); string(df) != Template {
		t.Error("no template Dockerfile")
	}
	if es, _ := os.ReadDir(filepath.Join(s.Dir, "certs")); len(es) != 1 || es[0].Name() != "01-corp.crt" {
		t.Errorf("certs %v", es)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "tools", "claude")); err != nil {
		t.Errorf("claude not in tools: %v", err)
	}
	// Again: the keys stay, an edited Dockerfile stays.
	os.WriteFile(filepath.Join(s.Dir, "Dockerfile"), []byte("FROM mine\n"), 0o600)
	if err := Prepare(s); err != nil {
		t.Fatal(err)
	}
	if pub2, _ := os.ReadFile(filepath.Join(s.Dir, "id_ed25519.pub")); string(pub2) != string(pub) {
		t.Error("the key changed")
	}
	if df, _ := os.ReadFile(filepath.Join(s.Dir, "Dockerfile")); string(df) != "FROM mine\n" {
		t.Error("the edited Dockerfile was replaced")
	}
	// The repository's Dockerfile wins.
	repo := filepath.Join(s.LocalPath, ".session-workbench", "Dockerfile")
	os.MkdirAll(filepath.Dir(repo), 0o755)
	os.WriteFile(repo, []byte("FROM repo\n"), 0o644)
	if file, ctx, inRepo := s.Dockerfile(); file != repo || ctx != filepath.Dir(repo) || !inRepo {
		t.Errorf("Dockerfile() = %s %s %v", file, ctx, inRepo)
	}
}

func TestFakeEngine(t *testing.T) {
	t.Setenv("SW_FAKE_ENGINE", "1")
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("SW_FAKE_ARGS", argsFile)
	e := Engine{Name: "fake", Path: os.Args[0]}
	ctx := context.Background()
	if ex, run, err := e.State(ctx, "s-1"); err != nil || !ex || !run {
		t.Errorf("state s-1: %v %v %v", ex, run, err)
	}
	if ex, run, _ := e.State(ctx, "s-10"); !ex || run {
		t.Errorf("state s-10: %v %v", ex, run)
	}
	if ex, _, _ := e.State(ctx, "s-2"); ex {
		t.Error("s-2 exists")
	}
	if p, err := e.Port(ctx, "s-1"); err != nil || p != 41234 {
		t.Errorf("port %d %v", p, err)
	}
	if _, err := e.output(ctx, "fail"); err == nil || !strings.Contains(err.Error(), "something broke") {
		t.Errorf("failure: %v", err)
	}
	if err := e.Stop(ctx, "s-1"); err != nil {
		t.Fatal(err)
	}
	if err := e.Remove(ctx, "s-10", true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(argsFile)
	for _, want := range []string{"stop sw-s-1", "rm -f sw-s-10", "volume rm sw-claude-s-10"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("no %q in:\n%s", want, b)
		}
	}
	if strings.Contains(string(b), "volume rm sw-claude\n") {
		t.Error("removed the shared volume")
	}
}

func TestFind(t *testing.T) {
	if _, err := Find("no-such-engine-xyz"); err == nil {
		t.Error("found a missing engine")
	}
	if Volume("s-1", false) != SharedVolume || Volume("s-1", true) != "sw-claude-s-1" || Name("s-1") != "sw-s-1" {
		t.Error("names")
	}
}

func TestEnsureDockerfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("container hosts are not supported on Windows (a path with C: cannot be mounted)")
	}
	s := spec(t)
	if err := EnsureDockerfile(s); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(s.Dir, "Dockerfile")); string(b) != Template {
		t.Error("no template")
	}
	// Nothing else is made: no keys, no certificates.
	if _, err := os.Stat(filepath.Join(s.Dir, "id_ed25519")); err == nil {
		t.Error("made keys")
	}
	os.WriteFile(filepath.Join(s.Dir, "Dockerfile"), []byte("FROM mine\n"), 0o600)
	EnsureDockerfile(s)
	if b, _ := os.ReadFile(filepath.Join(s.Dir, "Dockerfile")); string(b) != "FROM mine\n" {
		t.Error("replaced an existing Dockerfile")
	}
}

// TestProfileNames: a profile's containers and own volumes carry its name;
// the shared login volume stays shared.
func TestProfileNames(t *testing.T) {
	defer profile.Set("")
	profile.Set("ssh-switcher")
	if Name("s-1") != "sw-ssh-switcher-s-1" || Volume("s-1", true) != "sw-claude-ssh-switcher-s-1" || Volume("s-1", false) != SharedVolume {
		t.Errorf("%s %s %s", Name("s-1"), Volume("s-1", true), Volume("s-1", false))
	}
}
