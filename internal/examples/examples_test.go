package examples

import (
	"regexp"
	"strings"
	"testing"

	"session-workbench/internal/config"
)

// The shipped commands pass the same check as Settings → Save.
func TestCommands(t *testing.T) {
	cs, err := Commands()
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) < 5 {
		t.Fatalf("only %d example commands", len(cs))
	}
	if err := config.ValidateCommands(cs); err != nil {
		t.Fatal(err)
	}
	groups := map[string]bool{}
	for _, c := range cs {
		groups[c.Group] = true
	}
	for _, g := range []string{"Claude", "Git", "Text"} {
		if !groups[g] {
			t.Errorf("no command in group %q", g)
		}
	}
}

// Every prompt is a "## name" heading with text, names unique (ignoring
// case, as the button compares them).
func TestPrompts(t *testing.T) {
	p := strings.ReplaceAll(Prompts(), "\r\n", "\n")
	names := regexp.MustCompile(`(?m)^## +(.*\S)\s*$`).FindAllStringSubmatch(p, -1)
	if len(names) < 5 {
		t.Fatalf("only %d example prompts", len(names))
	}
	seen := map[string]bool{}
	for _, n := range names {
		k := strings.ToLower(n[1])
		if seen[k] {
			t.Errorf("prompt %q twice", n[1])
		}
		seen[k] = true
	}
	if !strings.HasPrefix(p, "## ") {
		t.Error("the page should start with a prompt heading")
	}
	for _, s := range strings.Split(p, "\n## ")[1:] {
		if body := strings.TrimSpace(s[strings.Index(s+"\n", "\n"):]); body == "" {
			t.Errorf("prompt %q has no text", strings.SplitN(s, "\n", 2)[0])
		}
	}
}
