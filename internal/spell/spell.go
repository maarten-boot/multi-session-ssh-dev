// Package spell finds the Hunspell dictionaries the notes editor checks
// spelling with (the checking itself is in the frontend). English (en_US,
// from SCOWL, see dict/LICENSE) is built in; more languages come from a
// "dictionaries" folder next to the config file and the system's Hunspell
// folders (Linux: the hunspell-* / myspell-* packages; macOS:
// ~/Library/Spelling).
package spell

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"golang.org/x/text/encoding/htmlindex"
)

// Builtin is the language that needs no dictionary files.
const Builtin = "en_US"

//go:embed dict/en_US.aff dict/en_US.dic
var builtin embed.FS

// Dictionary is a Hunspell dictionary, both files as UTF-8 text.
type Dictionary struct {
	Lang string `json:"lang"`
	Aff  string `json:"aff"`
	Dic  string `json:"dic"`
}

var validLang = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_@.-]*$`)

// Dirs are the folders searched for <lang>.aff + <lang>.dic, own first.
func Dirs(own string) []string {
	dirs := []string{own}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "linux", "freebsd", "openbsd", "netbsd":
		if home != "" {
			dirs = append(dirs, filepath.Join(home, ".local", "share", "hunspell"))
		}
		dirs = append(dirs, "/usr/share/hunspell", "/usr/share/myspell", "/usr/share/myspell/dicts",
			"/usr/local/share/hunspell")
	case "darwin":
		if home != "" {
			dirs = append(dirs, filepath.Join(home, "Library", "Spelling"))
		}
		dirs = append(dirs, "/Library/Spelling")
	}
	return dirs
}

// Languages lists the languages there are dictionaries for: the built-in
// one, then those found in dirs, sorted.
func Languages(dirs []string) []string {
	var found []string
	for _, d := range dirs {
		names, _ := filepath.Glob(filepath.Join(d, "*.dic"))
		for _, n := range names {
			lang := strings.TrimSuffix(filepath.Base(n), ".dic")
			if !validLang.MatchString(lang) || lang == Builtin || slices.Contains(found, lang) {
				continue
			}
			if _, err := os.Stat(strings.TrimSuffix(n, ".dic") + ".aff"); err == nil {
				found = append(found, lang)
			}
		}
	}
	slices.Sort(found)
	return append([]string{Builtin}, found...)
}

// Load returns the dictionary for lang: the built-in one, else the first
// found in dirs.
func Load(lang string, dirs []string) (Dictionary, error) {
	if !validLang.MatchString(lang) {
		return Dictionary{}, fmt.Errorf("spelling: bad language name %q", lang)
	}
	var aff, dic []byte
	var err error
	if lang == Builtin {
		if aff, err = builtin.ReadFile("dict/en_US.aff"); err == nil {
			dic, err = builtin.ReadFile("dict/en_US.dic")
		}
	} else {
		err = os.ErrNotExist
		for _, d := range dirs {
			base := filepath.Join(d, lang)
			if aff, err = os.ReadFile(base + ".aff"); err == nil {
				dic, err = os.ReadFile(base + ".dic")
			}
			if err == nil {
				break
			}
		}
	}
	if errors.Is(err, os.ErrNotExist) {
		return Dictionary{}, fmt.Errorf("spelling: no dictionary for %q", lang)
	}
	if err != nil {
		return Dictionary{}, fmt.Errorf("spelling: %w", err)
	}
	return decode(lang, aff, dic)
}

var setLine = regexp.MustCompile(`(?m)^SET[ \t]+(\S+)`)

// decode converts both files to UTF-8 from the encoding named by the SET
// line of the .aff file (ISO8859-1 by default, as in Hunspell), with Unix
// line ends.
func decode(lang string, aff, dic []byte) (Dictionary, error) {
	enc := "ISO8859-1"
	if m := setLine.FindSubmatch(aff); m != nil {
		enc = string(m[1])
	}
	name := strings.TrimPrefix(strings.ToLower(enc), "microsoft-")
	if name != "utf-8" {
		e, err := htmlindex.Get(name)
		if err != nil {
			return Dictionary{}, fmt.Errorf("spelling: %s: unknown encoding %q", lang, enc)
		}
		d := e.NewDecoder()
		if aff, err = d.Bytes(aff); err == nil {
			dic, err = d.Bytes(dic)
		}
		if err != nil {
			return Dictionary{}, fmt.Errorf("spelling: %s: %w", lang, err)
		}
		aff = setLine.ReplaceAll(aff, []byte("SET UTF-8"))
	}
	crlf := func(b []byte) string { return string(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))) }
	return Dictionary{Lang: lang, Aff: crlf(aff), Dic: crlf(dic)}, nil
}
