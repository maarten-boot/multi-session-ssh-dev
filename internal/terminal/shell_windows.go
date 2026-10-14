//go:build windows

package terminal

import "os/exec"

// DefaultShell prefers PowerShell 7, then Windows PowerShell, then cmd.
func DefaultShell() (string, []string) {
	for _, sh := range []string{"pwsh.exe", "powershell.exe"} {
		if p, err := exec.LookPath(sh); err == nil {
			return p, []string{"-NoLogo"}
		}
	}
	return "cmd.exe", nil
}
