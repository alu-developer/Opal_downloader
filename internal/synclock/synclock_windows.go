//go:build windows

package synclock

import "golang.org/x/sys/windows"

// stillActive is the STILL_ACTIVE sentinel Win32's GetExitCodeProcess
// returns as the exit code of a process that hasn't terminated yet.
const stillActive = 259

// isProcessAlive reports whether pid identifies a currently-running
// process on Windows. It opens the process with only
// PROCESS_QUERY_LIMITED_INFORMATION (the minimal access right that still
// lets GetExitCodeProcess work, and one this process can request even
// against a process owned by a different user/elevation level) and checks
// its exit code: OpenProcess failing at all means no such process exists
// (Windows does not generally let you open an arbitrary dead PID - unlike
// POSIX, a PID is simply invalid once its process is gone and not yet
// reused), and a successful open with an exit code other than
// STILL_ACTIVE means the process object still exists (e.g. a handle table
// artifact) but has already finished running.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}
