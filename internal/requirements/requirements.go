// Package requirements reads a project's list of the software it needs on
// an agent host (.session-workbench/requirements, SPEC F16) and checks it
// there: one command per line, optionally with a version (`go >= 1.26`,
// `node`, `make`, `# comment`). The check runs `command -v` and the
// first of the command's `--version`, `version` and `-V` that succeeds, in
// one ssh call, and compares the
// first version number in that output.
package requirements

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// File is where a project keeps its list, relative to its directory.
const File = ".session-workbench/requirements"

// Template is what the Requirements window starts with.
const Template = `# The software this project needs on its agent host, one command per line,
# optionally with a version: >=, >, =, <=, <. For example:
#   go >= 1.26
#   node >= 20
#   make
`

// Req is one line of the list.
type Req struct {
	Line    int    `json:"line"`
	Name    string `json:"name"`
	Op      string `json:"op"`      // "" (any version), ">=", ">", "=", "<=", "<"
	Version string `json:"version"` // e.g. "1.26"
}

func (r Req) String() string {
	if r.Op == "" {
		return r.Name
	}
	return r.Name + " " + r.Op + " " + r.Version
}

var (
	nameRE    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	lineRE    = regexp.MustCompile(`^([^\s<>=]+)\s*(?:(>=|<=|==|=|>|<)\s*(\S+))?$`)
	versionRE = regexp.MustCompile(`\d+(?:\.\d+)+|\d+`)
)

// Parse reads the list; lines it cannot read come back as errors (with
// their line number), the rest as requirements.
func Parse(text string) ([]Req, []error) {
	var reqs []Req
	var errs []error
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		m := lineRE.FindStringSubmatch(line)
		switch {
		case m == nil:
			errs = append(errs, fmt.Errorf("line %d: %q is not \"command\" or \"command >= version\"", i+1, line))
		case !nameRE.MatchString(m[1]):
			errs = append(errs, fmt.Errorf("line %d: %q is not a command name", i+1, m[1]))
		case m[2] != "" && !versionRE.MatchString(m[3]):
			errs = append(errs, fmt.Errorf("line %d: %q is not a version", i+1, m[3]))
		default:
			op := m[2]
			if op == "==" {
				op = "="
			}
			reqs = append(reqs, Req{Line: i + 1, Name: m[1], Op: op, Version: m[3]})
		}
	}
	return reqs, errs
}

// Script is the shell script that checks reqs on an agent host: per
// requirement one line "name<TAB>found|missing<TAB>version output".
func Script(reqs []Req) string {
	var b strings.Builder
	for _, r := range reqs {
		// Names are checked by nameRE: safe as shell words.
		// The output of the first of --version, version, -V that succeeds
		// (never an error text: its numbers are no version).
		fmt.Fprintf(&b, `if command -v %[1]s >/dev/null 2>&1; then v=$(%[1]s --version </dev/null 2>&1) || v=$(%[1]s version </dev/null 2>&1) || v=$(%[1]s -V </dev/null 2>&1) || v=; v=$(printf '%%s' "$v" | head -n 3 | tr '\n\t' '  '); printf '%%s\tfound\t%%s\n' %[1]s "$v"; else printf '%%s\tmissing\t\n' %[1]s; fi
`, r.Name)
	}
	return b.String()
}

// Result is one requirement checked on a host.
type Result struct {
	Req
	Found   bool   `json:"found"`
	Have    string `json:"have"` // the version found ("" when none could be read)
	OK      bool   `json:"ok"`
	Problem string `json:"problem"` // why not OK
}

// Evaluate reads Script's output.
func Evaluate(reqs []Req, output string) []Result {
	got := map[string][2]string{}
	for _, line := range strings.Split(output, "\n") {
		f := strings.SplitN(line, "\t", 3)
		if len(f) == 3 {
			got[f[0]] = [2]string{f[1], f[2]}
		}
	}
	out := make([]Result, len(reqs))
	for i, r := range reqs {
		res := Result{Req: r}
		g, ok := got[r.Name]
		switch {
		case !ok:
			res.Problem = "not checked"
		case g[0] != "found":
			res.Problem = "not installed"
		default:
			res.Found = true
			res.Have = versionRE.FindString(g[1])
			switch {
			case r.Op == "":
				res.OK = true
			case res.Have == "":
				res.Problem = "its version could not be read"
			case Compare(res.Have, r.Version, r.Op):
				res.OK = true
			default:
				res.Problem = fmt.Sprintf("version %s, needs %s %s", res.Have, r.Op, r.Version)
			}
		}
		out[i] = res
	}
	return out
}

// Compare reports whether version have satisfies op want, comparing the
// dotted numbers part by part (missing parts count as 0).
func Compare(have, want, op string) bool {
	a, b := parts(have), parts(want)
	c := 0
	for i := 0; i < max(len(a), len(b)) && c == 0; i++ {
		x, y := at(a, i), at(b, i)
		if x < y {
			c = -1
		} else if x > y {
			c = 1
		}
	}
	switch op {
	case ">=":
		return c >= 0
	case ">":
		return c > 0
	case "=":
		// "= 1.26" accepts 1.26.3: only the given parts count.
		for i := range b {
			if at(a, i) != b[i] {
				return false
			}
		}
		return true
	case "<=":
		return c <= 0
	case "<":
		return c < 0
	}
	return true
}

func parts(v string) []int {
	var out []int
	for _, p := range strings.Split(versionRE.FindString(v), ".") {
		n, _ := strconv.Atoi(p)
		out = append(out, n)
	}
	return out
}

func at(p []int, i int) int {
	if i < len(p) {
		return p[i]
	}
	return 0
}
