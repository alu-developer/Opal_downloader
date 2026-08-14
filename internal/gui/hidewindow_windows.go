//go:build windows

package gui

import (
	"os/exec"
	"syscall"
)

// hideWindow suppresses the console-window flash a spawned console
// subprocess (cmd.exe, explorer.exe, powershell.exe) would otherwise cause
// when opal-downloader has no console of its own for it to attach to - e.g.
// the native WebView2 window has none. Matches internal/notify's existing
// use of the same CREATE_NO_WINDOW flag for the same reason.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
