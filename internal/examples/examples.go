// Package examples holds the example commands and prompts the app ships
// with (SPEC T5, T6): a first start without a config gets them, and
// existing users add the ones they lack from Settings → Commands and the
// prompts list. They are never merged into an existing setup by themselves.
package examples

import (
	_ "embed"
	"fmt"

	"github.com/BurntSushi/toml"

	"session-workbench/internal/config"
)

//go:embed commands.toml
var commandsTOML string

//go:embed prompts.md
var prompts string

// Commands returns the example commands of the command menu.
func Commands() ([]config.Command, error) {
	var f struct {
		UI struct {
			Commands []config.Command `toml:"commands"`
		} `toml:"ui"`
	}
	if _, err := toml.Decode(commandsTOML, &f); err != nil {
		return nil, fmt.Errorf("example commands: %w", err)
	}
	return f.UI.Commands, nil
}

// Prompts returns the example Prompts page: a "## name" heading per prompt.
func Prompts() string { return prompts }
