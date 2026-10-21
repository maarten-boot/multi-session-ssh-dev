package spell

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBuiltin(t *testing.T) {
	d, err := Load(Builtin, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(d.Aff, "SET UTF-8\n") || !strings.Contains(d.Dic, "\nparse/") || strings.Contains(d.Dic, "\r") {
		t.Errorf("built-in dictionary: aff %q…, dic %d bytes", d.Aff[:20], len(d.Dic))
	}
}

func TestFolders(t *testing.T) {
	own, sys := t.TempDir(), t.TempDir()
	write := func(dir, name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Dutch in the own folder (UTF-8, Windows line ends), German in the
	// system one in Latin-1; one without .aff, one with a bad name.
	write(own, "nl.aff", "SET UTF-8\r\nTRY e\r\n")
	write(own, "nl.dic", "1\r\nfiets\r\n")
	write(sys, "de_DE.aff", "SET ISO8859-1\nTRY e\n")
	write(sys, "de_DE.dic", "1\nStra\xdfe\n")
	write(sys, "nl.aff", "SET UTF-8\n")
	write(sys, "nl.dic", "1\nother\n")
	write(sys, "fr.dic", "1\nvélo\n")
	write(sys, "-x.aff", "")
	write(sys, "-x.dic", "")
	dirs := []string{own, sys}

	if got, want := Languages(dirs), []string{"en_US", "de_DE", "nl"}; !slices.Equal(got, want) {
		t.Errorf("Languages = %q, want %q", got, want)
	}
	nl, err := Load("nl", dirs)
	if err != nil || nl.Dic != "1\nfiets\n" || nl.Aff != "SET UTF-8\nTRY e\n" {
		t.Errorf("nl: %+v, %v (the own folder first, Unix line ends)", nl, err)
	}
	de, err := Load("de_DE", dirs)
	if err != nil || de.Dic != "1\nStraße\n" || de.Aff != "SET UTF-8\nTRY e\n" {
		t.Errorf("de_DE: %+v, %v (converted to UTF-8)", de, err)
	}
	for _, lang := range []string{"fr", "xx", "../nl", "-x", ""} {
		if _, err := Load(lang, dirs); err == nil {
			t.Errorf("Load(%q) gave no error", lang)
		}
	}
	write(own, "ru.aff", "SET KOI8-Q\n")
	write(own, "ru.dic", "1\n")
	if _, err := Load("ru", dirs); err == nil || !strings.Contains(err.Error(), "unknown encoding") {
		t.Errorf("unknown encoding: %v", err)
	}
}
