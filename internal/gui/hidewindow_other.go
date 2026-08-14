//go:build !windows

package gui

import "os/exec"

// hideWindow is a no-op on non-Windows platforms: CREATE_NO_WINDOW is a
// Windows-only concept, and the callers only need it for the console
// commands (cmd.exe, explorer.exe, powershell.exe) that appear in their
// windows-only branches.
func hideWindow(cmd *exec.Cmd) {}
