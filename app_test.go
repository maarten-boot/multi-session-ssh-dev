package main

import (
	"strings"
	"testing"

	"session-workbench/internal/remote"
)

func TestSessionID(t *testing.T) {
	cases := map[string]string{
		"/home/u/share/My Project.v2":         "my-project-v2-",
		"/home/u/share/multi-session-ssh-dev": "multi-session-ssh-dev-",
		"/home/u/share/日本":                    "",
		"/":                                   "",
	}
	for dir, prefix := range cases {
		id := sessionID(dir)
		if !remote.ValidID(id) {
			t.Errorf("sessionID(%q) = %q, not a valid id", dir, id)
		}
		if len(id) < len(prefix) || id[:len(prefix)] != prefix {
			t.Errorf("sessionID(%q) = %q, want prefix %q", dir, id, prefix)
		}
		if sessionID(dir) != id {
			t.Errorf("sessionID(%q) not stable", dir)
		}
	}
	if sessionID("/a/x") == sessionID("/b/x") {
		t.Error("same basename in different dirs collides")
	}
	// Truncation must not leave a trailing "-" before the hash ("--").
	if long := sessionID("/" + strings.Repeat("a", 39) + "-bbbbbbbbbb"); !remote.ValidID(long) || strings.Contains(long, "--") {
		t.Errorf("long id invalid: %q", long)
	}
}
