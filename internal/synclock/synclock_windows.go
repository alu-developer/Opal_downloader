//go:build windows

package synclock

import (
	"time"

	"golang.org/x/sys/windows"
)

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
	h, ok := openForQuery(pid)
	if !ok {
		return false
	}
	defer windows.CloseHandle(h)

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// processStartedAt returns when the process identified by pid began
// running. It exists to close the PID-reuse hole in isProcessAlive alone:
// Windows recycles PIDs freely, so once the process that wrote a lock file
// has exited, any unrelated process can end up holding its number. A plain
// liveness probe then reports the lock as held forever, and the only symptom
// the user ever sees is a sync refusing to start with "a sync is already
// running (PID NNNNN)" naming a process that has nothing to do with
// opal-downloader. Comparing the live process's creation time against the
// time recorded in the lock distinguishes the real holder from an impostor
// that merely inherited its number.
//
// ok is false when the start time could not be determined at all (the
// process is gone, or the handle cannot be opened), in which case the caller
// must not treat the result as evidence either way.
func processStartedAt(pid int) (startedAt time.Time, ok bool) {
	h, opened := openForQuery(pid)
	if !opened {
		return time.Time{}, false
	}
	defer windows.CloseHandle(h)

	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return time.Time{}, false
	}
	return time.Unix(0, creation.Nanoseconds()).UTC(), true
}

func openForQuery(pid int) (windows.Handle, bool) {
	if pid <= 0 {
		return 0, false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0, false
	}
	return h, true
}
