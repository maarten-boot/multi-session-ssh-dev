package container

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestRealEngine builds the template image with a real engine, starts the
// container and runs a command in it over ssh. It downloads packages and
// Claude Code, so it runs only with SW_CONTAINER_TEST=podman (or docker).
func TestRealEngine(t *testing.T) {
	name := os.Getenv("SW_CONTAINER_TEST")
	if name == "" {
		t.Skip("set SW_CONTAINER_TEST=podman or docker to build and run a real container")
	}
	e, err := Find(name)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := user.Current()
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	dir := t.TempDir()
	proj := filepath.Join(dir, "proj")
	os.MkdirAll(proj, 0o755)
	os.WriteFile(filepath.Join(proj, "hello.txt"), []byte("hi\n"), 0o644)
	s := Spec{ID: "t" + strconv.FormatInt(time.Now().Unix()%100000, 10), LocalPath: proj, Dir: filepath.Join(dir, "c"),
		OwnVolume: true, User: User{Name: u.Username, UID: uid, GID: gid}}
	if p, err := exec.LookPath("claude"); err == nil {
		s.ClaudeBinary = p // the installer may be blocked (a company proxy)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	t.Cleanup(func() { e.Remove(context.Background(), s.ID, true, true) })
	var log strings.Builder
	port, err := e.Up(ctx, s, &log)
	if err != nil {
		t.Fatalf("up: %v\n%s", err, log.String())
	}
	ssh := func(cmd string) string {
		args := InsertOptions([]string{"-T", "-o", "BatchMode=yes", "--", Name(s.ID), cmd}, SSHOptions(s, port))
		out, err := exec.Command("ssh", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("ssh %q: %v: %s", cmd, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if got := ssh("id -u; id -g; cat " + proj + "/hello.txt; touch " + proj + "/made-inside; command -v tmux"); got !=
		strconv.Itoa(uid)+"\n"+strconv.Itoa(gid)+"\nhi\n/usr/bin/tmux" {
		t.Errorf("inside: %q", got)
	}
	if fi, err := os.Stat(filepath.Join(proj, "made-inside")); err != nil {
		t.Errorf("file made inside: %v", err)
	} else if st := fileOwner(fi); st != uid {
		t.Errorf("file made inside belongs to %d, not %d", st, uid)
	}
	if got := ssh(`bash -lc 'echo $CLAUDE_CONFIG_DIR; command -v claude'`); !strings.HasSuffix(strings.Split(got, "\n")[0], "/.claude") ||
		!strings.Contains(got, "/.local/bin/claude") {
		t.Errorf("claude: %q", got)
	}
	// Up again: running, same port.
	if p2, err := e.Up(ctx, s, &log); err != nil || p2 != port {
		t.Errorf("second up: %d %v", p2, err)
	}
	if err := e.Stop(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, running, _ := e.State(ctx, s.ID); running {
		t.Error("still running after Stop")
	}
	if _, err := e.Up(ctx, s, &log); err != nil {
		t.Errorf("up after stop: %v", err)
	}
}

// TestRealCompose runs a session with a compose file with a real engine
// (SW_CONTAINER_TEST=podman or docker): the dev container reaches the
// service by name and has the environment the file adds to it; a second
// Up keeps the port; Stop stops both; Remove takes the service, its
// network and (with data) its volume.
func TestRealCompose(t *testing.T) {
	name := os.Getenv("SW_CONTAINER_TEST")
	if name == "" {
		t.Skip("set SW_CONTAINER_TEST=podman or docker to run a real Compose project")
	}
	e, err := Find(name)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if _, err := e.ComposeVersion(ctx); err != nil {
		t.Skip(err)
	}
	u, _ := user.Current()
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	dir := t.TempDir()
	proj := filepath.Join(dir, "proj")
	os.MkdirAll(filepath.Join(proj, ".session-workbench"), 0o755)
	os.WriteFile(filepath.Join(proj, ".session-workbench", "note.txt"), []byte("from the project\n"), 0o644)
	s := Spec{ID: "t" + strconv.FormatInt(time.Now().Unix()%100000, 10), LocalPath: proj, Dir: filepath.Join(dir, "c"),
		OwnVolume: true, User: User{Name: u.Username, UID: uid, GID: gid}}
	if p, err := exec.LookPath("claude"); err == nil {
		s.ClaudeBinary = p
	}
	file := "services:\n" +
		"  dev:\n    environment: {DB_HOST: db}\n    depends_on: [db]\n" +
		"  db:\n    image: docker.io/library/ubuntu:24.04\n    command: [sleep, infinity]\n" +
		"    volumes: [dbdata:/data, ./note.txt:/note.txt:ro]\n" +
		"volumes:\n  dbdata:\n"
	if err := e.CheckCompose(ctx, s, file, true); err != nil {
		t.Fatal(err)
	}
	if err := e.CheckCompose(ctx, s, "services:\n  dev:\n    container_name: other\n", true); err == nil ||
		!strings.Contains(err.Error(), "container_name") {
		t.Errorf("renamed dev accepted: %v", err)
	}
	os.WriteFile(s.ComposePath(true), []byte(file), 0o644)
	t.Cleanup(func() { e.Remove(context.Background(), s.ID, true, true) })
	var log strings.Builder
	port, err := e.Up(ctx, s, &log)
	if err != nil {
		t.Fatalf("up: %v\n%s", err, log.String())
	}
	ssh := func(cmd string) string {
		args := InsertOptions([]string{"-T", "-o", "BatchMode=yes", "--", Name(s.ID), cmd}, SSHOptions(s, port))
		out, err := exec.Command("ssh", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("ssh %q: %v: %s", cmd, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if got := ssh("id -u; echo DB_HOST=$DB_HOST; getent hosts db >/dev/null && echo db-found"); got !=
		strconv.Itoa(uid)+"\nDB_HOST=db\ndb-found" {
		t.Errorf("inside: %q", got)
	}
	db := Name(s.ID) + "-db-1"
	if out, err := e.output(ctx, "exec", db, "cat", "/note.txt"); err != nil || strings.TrimSpace(out) != "from the project" {
		t.Errorf("relative path in the file: %q %v", out, err)
	}
	if names, _ := e.ComposeServices(ctx, s.ID); strings.Join(names, ",") != db {
		t.Errorf("services: %v", names)
	}
	if p2, err := e.Up(ctx, s, &log); err != nil || p2 != port {
		t.Errorf("second up: %d %v", p2, err)
	}
	if err := e.Stop(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if ids, _ := e.projectIDs(ctx, s.ID, "ps"); len(ids) > 0 {
		t.Errorf("still running after Stop: %v", ids)
	}
	if _, err := e.Up(ctx, s, &log); err != nil {
		t.Errorf("up after stop: %v\n%s", err, log.String())
	}
	if err := e.Remove(ctx, s.ID, true, true); err != nil {
		t.Fatal(err)
	}
	for _, list := range [][]string{{"ps", "-a"}, {"network", "ls"}, {"volume", "ls"}} {
		if ids, _ := e.projectIDs(ctx, s.ID, list...); len(ids) > 0 {
			t.Errorf("%s left: %v", list[0], ids)
		}
	}
	if _, err := e.output(ctx, "volume", "inspect", SharedVolume); err != nil && !s.OwnVolume {
		t.Errorf("the shared volume went: %v", err)
	}
}
