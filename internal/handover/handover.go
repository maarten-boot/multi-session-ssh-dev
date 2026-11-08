// Package handover moves one project's Claude Code state from one agent
// host to another (SPEC F16): its conversations and memory, which Claude
// Code files under ~/.claude/projects/<the project's path, encoded>/, and
// the project's entry in ~/.claude.json (trust, allowed tools, MCP
// choices). Both sides are fetched to this computer with the built-in
// transfer (tar over ssh, internal/xfer); a file goes to the new host when
// it lacks it or has an older one (newer wins, nothing is deleted); the
// folder is renamed when the project's path differs there. The project's
// entry is merged into the new host's ~/.claude.json, which is replaced
// only if claude did not change it meanwhile.
package handover

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"session-workbench/internal/remote"
	"session-workbench/internal/xfer"
)

var notAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// Encode is Claude Code's folder name for a project path: every character
// but letters and digits becomes "-".
func Encode(path string) string { return notAlnum.ReplaceAllString(path, "-") }

// Side is one agent host and the project's path there.
type Side struct {
	Target string // ssh target
	Home   string // the home holding .claude ("" = $HOME)
	Path   string // the project's absolute path on that host
}

func (s Side) home() string {
	if s.Home == "" {
		return `"$HOME"`
	}
	return remote.Quote(s.Home)
}

func (s Side) folder() string { return ".claude/projects/" + Encode(s.Path) }

// Report says what a switch copied.
type Report struct {
	Found         bool     `json:"found"`         // the old host had Claude state for the project
	Conversations int      `json:"conversations"` // *.jsonl files copied (new or newer)
	Files         int      `json:"files"`         // all files copied
	Memory        int      `json:"memory"`        // of them in memory/
	Config        bool     `json:"config"`        // the project's ~/.claude.json entry was merged
	Notes         []string `json:"notes"`
}

// fetch brings side's project folder and ~/.claude.json to dir; false when
// the host has neither.
func fetch(ctx context.Context, s Side, dir string) (bool, error) {
	script := "cd " + s.home() + ` || exit 4
set --
[ -d ` + s.folder() + ` ] && set -- ` + s.folder() + `
[ -f .claude.json ] && set -- "$@" .claude.json
[ $# -eq 0 ] && exit 3
exec tar -cf - "$@"`
	code, stderr, _, err := xfer.Pull(ctx, s.Target, script, dir, nil)
	switch {
	case err != nil:
		return false, err
	case code == 3:
		return false, nil
	case code == 1 && strings.Contains(stderr, "file changed as we read it"):
		return true, nil
	case code != 0:
		return false, fmt.Errorf("%s: tar exit %d: %s", s.Target, code, lastLine(stderr))
	}
	return true, nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// Changes are the files (slash paths relative to from) that to lacks or
// has with an older modification time.
func Changes(from, to string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p == from {
				return fs.SkipAll
			}
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(from, p)
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if ti, err := os.Stat(filepath.Join(to, rel)); err == nil && !fi.ModTime().After(ti.ModTime()) {
			return nil // theirs is as new or newer
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	slices.Sort(out)
	return out, err
}

// MergeConfig puts the project's entry from fromJSON (under fromPath) into
// toJSON (under toPath): keys the new host lacks come along, allowedTools
// are joined, the trust is kept when either side gave it. It returns the
// new file and whether anything changed. toJSON may be empty (no file).
func MergeConfig(fromJSON, toJSON []byte, fromPath, toPath string) ([]byte, bool, error) {
	from, err := decode(fromJSON)
	if err != nil {
		return nil, false, fmt.Errorf("the old host's ~/.claude.json: %w", err)
	}
	src, _ := projects(from)[fromPath].(map[string]any)
	if src == nil {
		return nil, false, nil
	}
	to := map[string]any{}
	if len(bytes.TrimSpace(toJSON)) > 0 {
		if to, err = decode(toJSON); err != nil {
			return nil, false, fmt.Errorf("the new host's ~/.claude.json: %w", err)
		}
	}
	ps := projects(to)
	if ps == nil {
		ps = map[string]any{}
		to["projects"] = ps
	}
	dst, _ := ps[toPath].(map[string]any)
	if dst == nil {
		dst = map[string]any{}
	}
	before, _ := json.Marshal(dst)
	for k, v := range src {
		switch k {
		case "allowedTools":
			dst[k] = union(dst[k], v)
		case "hasTrustDialogAccepted":
			if v == true {
				dst[k] = true
			} else if _, ok := dst[k]; !ok {
				dst[k] = v
			}
		default:
			if _, ok := dst[k]; !ok {
				dst[k] = v
			}
		}
	}
	after, _ := json.Marshal(dst)
	if bytes.Equal(before, after) {
		return nil, false, nil
	}
	ps[toPath] = dst
	b, err := json.MarshalIndent(to, "", "  ")
	return append(b, '\n'), err == nil, err
}

func decode(b []byte) (map[string]any, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber() // numbers stay as written
	var m map[string]any
	if err := d.Decode(&m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func projects(m map[string]any) map[string]any {
	p, _ := m["projects"].(map[string]any)
	return p
}

// union joins two JSON lists of strings, in order, without duplicates.
func union(a, b any) any {
	var out []any
	seen := map[string]bool{}
	for _, l := range []any{a, b} {
		list, _ := l.([]any)
		for _, v := range list {
			key := fmt.Sprint(v)
			if !seen[key] {
				seen[key] = true
				out = append(out, v)
			}
		}
	}
	if out == nil {
		out = []any{}
	}
	return out
}

// ErrConfigChanged: claude wrote the new host's ~/.claude.json while the
// switch merged it.
var ErrConfigChanged = errors.New("claude changed ~/.claude.json on the new host meanwhile; nothing was replaced (end claude there and switch again)")

// putConfig replaces ~/.claude.json on s with b, if it still is what was
// fetched (its POSIX cksum; "none" when there was no file).
func putConfig(ctx context.Context, s Side, b []byte, was string) error {
	script := "cd " + s.home() + ` || exit 1
if [ -f .claude.json ]; then have=$(cksum < .claude.json | awk '{print $1" "$2}'); else have=none; fi
[ "$have" = ` + remote.Quote(was) + ` ] || exit 5
umask 077
cat > .claude.json.sw-new && mv -f .claude.json.sw-new .claude.json`
	_, code, stderr, err := xfer.Run(ctx, s.Target, script, bytes.NewReader(b))
	switch {
	case err != nil:
		return err
	case code == 5:
		return ErrConfigChanged
	case code != 0:
		return fmt.Errorf("writing ~/.claude.json on %s: exit %d: %s", s.Target, code, lastLine(stderr))
	}
	return nil
}

func cksumOf(file string) string {
	f, err := os.Open(file)
	if err != nil {
		return "none"
	}
	defer f.Close()
	crc, n, err := xfer.Cksum(f)
	if err != nil {
		return "none"
	}
	return strconv.FormatUint(uint64(crc), 10) + " " + strconv.FormatInt(n, 10)
}

// Switch copies the project's Claude state from one host to the other,
// with dir (made empty first) as the staging folder on this computer:
// dir/from and dir/to hold what both hosts had. Progress goes to out.
func Switch(ctx context.Context, from, to Side, dir string, out io.Writer) (Report, error) {
	var rep Report
	if err := os.RemoveAll(dir); err != nil {
		return rep, err
	}
	fromDir, toDir := filepath.Join(dir, "from"), filepath.Join(dir, "to")
	fmt.Fprintf(out, "Fetching Claude's state for %s from %s…\n", from.Path, from.Target)
	found, err := fetch(ctx, from, fromDir)
	if err != nil {
		return rep, err
	}
	if !found {
		fmt.Fprintf(out, "%s has no Claude state for this project (claude not used there yet?).\n", from.Target)
	}
	fmt.Fprintf(out, "Fetching %s's own for comparison…\n", to.Target)
	if _, err := fetch(ctx, to, toDir); err != nil {
		return rep, err
	}
	srcProj := filepath.Join(fromDir, filepath.FromSlash(from.folder()))
	if _, err := os.Stat(srcProj); err == nil {
		rep.Found = true
		// Named as Claude Code looks for it on the new host.
		if from.folder() != to.folder() {
			renamed := filepath.Join(fromDir, filepath.FromSlash(to.folder()))
			if err := os.Rename(srcProj, renamed); err != nil {
				return rep, err
			}
			rep.Notes = append(rep.Notes, fmt.Sprintf("the project's path differs (%s there): its folder is renamed", to.Path))
		}
		changes, err := Changes(filepath.Join(fromDir, filepath.FromSlash(to.folder())), filepath.Join(toDir, filepath.FromSlash(to.folder())))
		if err != nil {
			return rep, err
		}
		var files []xfer.File
		for _, c := range changes {
			files = append(files, xfer.File{Path: to.folder() + "/" + c})
			rep.Files++
			if strings.HasSuffix(c, ".jsonl") {
				rep.Conversations++
			}
			if strings.HasPrefix(c, "memory/") {
				rep.Memory++
			}
		}
		if len(files) > 0 {
			fmt.Fprintf(out, "Sending %d files to %s…\n", len(files), to.Target)
			script := "mkdir -p " + to.home() + "/.claude/projects && cd " + to.home() + ` || exit 1
exec tar -xf -`
			code, stderr, err := xfer.Push(ctx, to.Target, script, fromDir, files)
			if err != nil {
				return rep, err
			}
			if code != 0 {
				return rep, fmt.Errorf("tar on %s, exit %d: %s", to.Target, code, lastLine(stderr))
			}
		}
	}
	fromCfg, _ := os.ReadFile(filepath.Join(fromDir, ".claude.json"))
	toFile := filepath.Join(toDir, ".claude.json")
	toCfg, _ := os.ReadFile(toFile)
	if len(fromCfg) > 0 {
		merged, changed, err := MergeConfig(fromCfg, toCfg, from.Path, to.Path)
		if err != nil {
			return rep, err
		}
		if changed {
			rep.Found = true
			fmt.Fprintf(out, "Merging the project's settings into ~/.claude.json on %s…\n", to.Target)
			if err := putConfig(ctx, to, merged, cksumOf(toFile)); err != nil {
				return rep, err
			}
			rep.Config = true
		}
	}
	return rep, nil
}
