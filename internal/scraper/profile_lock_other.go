//go:build !windows

package scraper

// isWindowsSingletonWindowPresent is a no-op on non-Windows platforms. There
// is no Windows message-only-window mechanism to check there, and this
// project's Chromium-profile-locking concern is Windows-specific (see
// profile.go and profile_lock_windows.go) - on POSIX, Chromium's
// process_singleton_posix.cc really does create the marker files
// (lockfile/SingletonLock/SingletonCookie) that isUserDataDirLocked already
// checks in profile.go, so no additional platform-specific check is needed
// here.
func isWindowsSingletonWindowPresent(userDataDir string) (bool, error) {
	return false, nil
}
