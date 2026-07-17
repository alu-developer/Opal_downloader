//go:build !windows

package synclock

import "syscall"

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
