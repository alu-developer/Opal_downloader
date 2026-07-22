//go:build !windows

package synclock

import (
	"syscall"
	"time"
)

// isProcessAlive reports whether pid identifies a currently-running
// process on POSIX platforms, via the conventional "signal 0" probe:
// sending signal 0 performs all of kill(2)'s validity/permission checks
// without actually delivering a signal, so a nil error means the process
// exists (and is signalable by this user); ESRCH means it does not.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// processStartedAt is the POSIX counterpart of the Windows implementation
// (see synclock_windows.go for what this guards against). Reading a
// process's start time portably across POSIX platforms means parsing
// /proc/<pid>/stat on Linux, sysctl on BSD/macOS, and so on - none of which
// is worth carrying here, since opal-downloader's scheduled-sync path is
// Windows-only in practice (Task Scheduler; see docs/scheduled-sync-plan.md).
//
// Returning ok=false makes the caller fall back to the plain liveness check,
// i.e. exactly the behaviour every platform had before. That is the safe
// direction: it can only ever be over-cautious about a lock being held, and
// never reclaims a lock that is genuinely still in use.
func processStartedAt(pid int) (time.Time, bool) {
	return time.Time{}, false
}
