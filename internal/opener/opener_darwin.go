package opener

import "os/exec"

// Paths are always absolute, so they cannot be mistaken for options.

func openPlatform(path string) error {
	return start(exec.Command("open", path))
}

func revealPlatform(path string) error {
	return start(exec.Command("open", "-R", path))
}
