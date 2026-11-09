package idle

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// TestProcsWithoutPS runs the process helpers where there is no ps (a
// plain ubuntu container): they read /proc.
func TestProcsWithoutPS(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc: Linux only")
	}
	bin := t.TempDir()
	for _, tool := range []string{"sed", "cut", "tr", "awk", "sleep"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Skip(tool, " not found")
		}
		os.Symlink(p, filepath.Join(bin, tool))
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	// $$ is the shell; a child marked by its arguments runs under it.
	script := sh + ` -c 'sleep 30' marker-xyz & child=$!
sleep 0.3
` + procs + `
command -v ps >/dev/null 2>&1 && echo HAS-PS
echo "args:$(args_of $child)"
echo "ppid:$(ppid_of $child)"
echo "desc:$(desc_of $$ | tr '\n' ' ')"
echo "self:$$ child:$child"
kill $(desc_of $$) 2>/dev/null`
	cmd := exec.Command(sh, "-c", script)
	cmd.Env = []string{"PATH=" + bin}
	out, err := cmd.CombinedOutput()
	got := string(out)
	if err != nil && !strings.Contains(got, "self:") {
		t.Fatalf("%v: %s", err, got)
	}
	if strings.Contains(got, "HAS-PS") {
		t.Fatal("ps still found")
	}
	var self, child int
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(l, "self:") {
			f := strings.Fields(strings.NewReplacer("self:", "", "child:", "").Replace(l))
			self, _ = strconv.Atoi(f[0])
			child, _ = strconv.Atoi(f[1])
		}
	}
	if !strings.Contains(got, "args:"+sh+" -c sleep 30 marker-xyz") {
		t.Errorf("args: %s", got)
	}
	if !strings.Contains(got, "ppid:"+strconv.Itoa(self)+"\n") {
		t.Errorf("ppid: %s", got)
	}
	if !strings.Contains(got, " "+strconv.Itoa(child)+" ") && !strings.Contains(got, "desc:"+strconv.Itoa(child)+" ") {
		t.Errorf("desc: %s", got)
	}
}
