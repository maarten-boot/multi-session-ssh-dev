// Package profile is the name of a separate setup of the app: with
// -profile NAME (or SESSION_WORKBENCH_PROFILE=NAME) the app keeps
// everything under that name instead of "session-workbench" — its config
// and data folders, WebKit's storage — and tags what it makes elsewhere so
// two setups never meet: tmux sessions and the history folder on the agent
// hosts, container names. Without a name nothing changes.
package profile

import (
	"fmt"
	"regexp"
)

// Default is the folder name without a profile.
const Default = "session-workbench"

// EnvVar names the profile when -profile is not given.
const EnvVar = "SESSION_WORKBENCH_PROFILE"

var (
	name    string
	validRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)
)

// Set chooses the profile; "" and "session-workbench" are the default.
// Names are lower-case letters, digits, "-" and "_", up to 40 characters,
// starting with a letter or digit (they become folder, tmux and container
// names).
func Set(n string) error {
	if n == "" || n == Default {
		name = ""
		return nil
	}
	if !validRe.MatchString(n) {
		return fmt.Errorf("profile %q: use lower-case letters, digits, - and _ (up to 40, starting with a letter or digit)", n)
	}
	name = n
	return nil
}

// Name is the profile ("" for the default).
func Name() string { return name }

// Dir is the folder name for the app's config and data: the profile, else
// "session-workbench".
func Dir() string {
	if name == "" {
		return Default
	}
	return name
}

// Tag is "<name><sep>" for a profile, "" for the default: put in front of
// names the app makes outside its folders.
func Tag(sep string) string {
	if name == "" {
		return ""
	}
	return name + sep
}

// Title is the window title's app part: "Session Workbench", with the
// profile after a dash.
func Title() string {
	if name == "" {
		return "Session Workbench"
	}
	return "Session Workbench — " + name
}
