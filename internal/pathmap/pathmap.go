// Package pathmap translates a path on the host into the matching path on
// the agent host, given the two prefixes under which the same directory tree
// is visible (e.g. an NFS export and its mount point).
//
// Host paths may be Unix ("/home/u/share/x") or Windows ("C:\Users\u\share\x",
// "\\server\share\x") style regardless of the OS this runs on, so the logic
// is testable everywhere. Remote paths are always POSIX.
package pathmap

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

var (
	ErrNotAbsolute    = errors.New("pathmap: path is not absolute")
	ErrNotUnderPrefix = errors.New("pathmap: path is not under the local prefix")
)

// ToRemote returns remotePrefix + (localPath - localPrefix).
// Windows paths are compared case-insensitively.
func ToRemote(localPath, localPrefix, remotePrefix string) (string, error) {
	if !strings.HasPrefix(remotePrefix, "/") {
		return "", fmt.Errorf("%w: remote prefix %q", ErrNotAbsolute, remotePrefix)
	}
	lp, err := split(localPath)
	if err != nil {
		return "", err
	}
	pp, err := split(localPrefix)
	if err != nil {
		return "", err
	}
	if lp.root != pp.root || len(lp.parts) < len(pp.parts) {
		return "", fmt.Errorf("%w: %q not under %q", ErrNotUnderPrefix, localPath, localPrefix)
	}
	for i, p := range pp.parts {
		a := lp.parts[i]
		if lp.windows {
			if !strings.EqualFold(a, p) {
				return "", fmt.Errorf("%w: %q not under %q", ErrNotUnderPrefix, localPath, localPrefix)
			}
		} else if a != p {
			return "", fmt.Errorf("%w: %q not under %q", ErrNotUnderPrefix, localPath, localPrefix)
		}
	}
	rel := lp.parts[len(pp.parts):]
	return path.Join(append([]string{path.Clean(remotePrefix)}, rel...)...), nil
}

type parsed struct {
	root    string // "/" for Unix, "c:" for a drive, "//server/share" for UNC (lower-cased)
	parts   []string
	windows bool
}

func split(p string) (parsed, error) {
	if strings.ContainsRune(p, 0) {
		return parsed{}, fmt.Errorf("pathmap: NUL in path %q", p)
	}
	var r parsed
	s := p
	switch {
	case len(s) >= 2 && isLetter(s[0]) && s[1] == ':':
		if len(s) < 3 || (s[2] != '\\' && s[2] != '/') {
			return parsed{}, fmt.Errorf("%w: %q", ErrNotAbsolute, p) // "C:foo" is drive-relative
		}
		r.windows = true
		r.root = strings.ToLower(s[:2])
		s = s[3:]
	case strings.HasPrefix(s, `\\`) || strings.HasPrefix(s, `//`) && strings.Contains(s, `\`):
		r.windows = true
		f := strings.FieldsFunc(s, isSep)
		if len(f) < 2 {
			return parsed{}, fmt.Errorf("%w: incomplete UNC path %q", ErrNotAbsolute, p)
		}
		r.root = strings.ToLower("//" + f[0] + "/" + f[1])
		s = strings.Join(f[2:], "/")
	case strings.HasPrefix(s, "/"):
		r.root = "/"
	default:
		return parsed{}, fmt.Errorf("%w: %q", ErrNotAbsolute, p)
	}
	sep := func(c rune) bool { return c == '/' }
	if r.windows {
		sep = isSep
	}
	for _, c := range strings.FieldsFunc(s, sep) {
		switch c {
		case ".":
		case "..":
			if len(r.parts) == 0 {
				return parsed{}, fmt.Errorf("%w: %q escapes its root", ErrNotAbsolute, p)
			}
			r.parts = r.parts[:len(r.parts)-1]
		default:
			r.parts = append(r.parts, c)
		}
	}
	return r, nil
}

func isSep(c rune) bool    { return c == '/' || c == '\\' }
func isLetter(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }
