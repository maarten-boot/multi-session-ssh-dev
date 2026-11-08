package requirements

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	reqs, errs := Parse(Template + "go >= 1.26\nnode>=20 # js\nmake\npython3 == 3.12\n\nbad name!\nx >= abc\n")
	var got []string
	for _, r := range reqs {
		got = append(got, r.String())
	}
	if strings.Join(got, ",") != "go >= 1.26,node >= 20,make,python3 = 3.12" {
		t.Errorf("%v", got)
	}
	if len(errs) != 2 || !strings.Contains(errs[0].Error(), "line 11") || !strings.Contains(errs[1].Error(), "not a version") {
		t.Errorf("errors %v", errs)
	}
}

func TestCompare(t *testing.T) {
	for _, c := range []struct {
		have, op, want string
		ok             bool
	}{
		{"1.26.0", ">=", "1.26", true}, {"1.25.9", ">=", "1.26", false}, {"1.26", ">", "1.26", false},
		{"22.1.0", ">", "20", true}, {"3.12.3", "=", "3.12", true}, {"3.13.0", "=", "3.12", false},
		{"4.3", "<", "4.4", true}, {"4.4", "<=", "4.4", true}, {"10.0", ">=", "9.9", true},
	} {
		if Compare(c.have, c.want, c.op) != c.ok {
			t.Errorf("%s %s %s", c.have, c.op, c.want)
		}
	}
}

func TestEvaluate(t *testing.T) {
	reqs, _ := Parse("go >= 1.26\nnode >= 20\nmake\nrustc\nweird >= 1\n")
	out := "go\tfound\tflag provided but not defined: -version go version go1.26.0 linux/amd64 \n" +
		"node\tfound\tv18.19.1 \nmake\tfound\tGNU Make 4.3 \nrustc\tmissing\t\nweird\tfound\tno number here\n"
	var got []string
	for _, r := range Evaluate(reqs, out) {
		got = append(got, r.Name+"="+map[bool]string{true: "ok", false: r.Problem}[r.OK])
	}
	want := "go=ok,node=version 18.19.1, needs >= 20,make=ok,rustc=not installed,weird=its version could not be read"
	if strings.Join(got, ",") != want {
		t.Errorf("\n%s\nwant\n%s", strings.Join(got, ","), want)
	}
}

// TestScript runs the check here with sh: sh is there, a made-up command
// is not.
func TestScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no sh")
	}
	reqs, _ := Parse("sh\nno-such-command-xyz >= 1\n")
	out, err := exec.Command("sh", "-c", Script(reqs)).Output()
	if err != nil {
		t.Fatal(err)
	}
	res := Evaluate(reqs, string(out))
	if !res[0].Found || !res[0].OK || res[1].Found || res[1].Problem != "not installed" {
		t.Errorf("%+v\n%s", res, out)
	}
}
