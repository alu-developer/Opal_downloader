//go:build !windows

package scraper

import (
	"sync"
	"time"
)

// acquireSessionLock is a same-process-only fallback on non-Windows
// platforms: it does not serialize across separate opal-downloader
// processes/worktrees, only across concurrent goroutines within this one
// process. This mirrors the existing platform split in profile.go/
// profile_lock_windows.go/profile_lock_other.go - the cross-process races
// this package's session lock exists to close are only reproducible via
// Chromium's Windows-specific ProcessSingleton behavior
// (isWindowsSingletonWindowPresent), and Windows is this project's primary
// (and, in practice, only tested) target platform. A real cross-process lock
// here would need a POSIX-appropriate mechanism (e.g. flock on a lock file)
// that nobody has verified against this project's actual Chromium-launch
// behavior on macOS/Linux; since no non-Windows session_lock races have ever
// been reported, this in-process mutex is a deliberately conservative stand-in
// that keeps the build and single-process tests working rather than a claim
// that cross-process safety is covered on these platforms too.
var sessionLockMu sync.Mutex

func acquireSessionLock(profileDir, stateFile string, timeout time.Duration) (release func(), err error) {
	sessionLockMu.Lock()
	return sessionLockMu.Unlock, nil
}
