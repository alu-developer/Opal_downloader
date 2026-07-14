package scraper

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// LoginProfileDir returns the single, hardcoded Playwright Chromium profile
// directory opal-downloader always logs in and syncs/lists against:
// ~/.opal-downloader/login-profile. There is no more configurable
// browser_user_data_dir/browser_executable - Chromium (Playwright's bundled
// build) is the only browser opal-downloader ever launches, and this
// dedicated profile is the only place it launches it against, with
// extensions (specifically TU-Fast) always enabled - see launchBrowser in
// session.go. The user either logs in manually here or, once, installs
// TU-Fast from the Chrome Web Store into this same profile (see
// gui.handleTUFastSetupOpen / OpenInteractiveBrowserAt), after which it
// auto-completes future logins exactly like it did against a real
// Brave/Chrome profile before this change.
func LoginProfileDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory for the login profile: %w", err)
	}
	return filepath.Join(home, ".opal-downloader", "login-profile"), nil
}

// profileLockFiles lists the marker files Chromium's *POSIX*
// (process_singleton_posix.cc) ProcessSingleton creates in a user-data-dir to
// guard against being opened by two running instances at once:
// "lockfile" is a symlink written on Linux, "SingletonLock"/"SingletonCookie"
// are used on macOS/Linux as well.
//
// IMPORTANT: none of these files are ever created by Chromium/Brave on
// Windows. Windows has a completely separate implementation
// (process_singleton_win.cc) that doesn't touch the user-data-dir at all -
// see profile_lock_windows.go for the Windows-specific check this is paired
// with. This POSIX marker-file check is kept for completeness/portability,
// but on Windows (this project's primary target) it will always return
// false, nil by itself; isUserDataDirLocked below only trusts it in
// combination with the Windows-specific check.
var profileLockFiles = []string{"lockfile", "SingletonLock", "SingletonCookie"}

// isUserDataDirLocked reports whether userDataDir appears to be in active use
// by another Chromium-based browser process (e.g. the user's real Brave
// window).
//
// This combines two checks:
//  1. The POSIX marker-file check (lockfile/SingletonLock/SingletonCookie) -
//     opens each marker with read-write access and no sharing allowed to
//     other writers; if the OS reports the file is already in use, the
//     profile is locked. This only ever fires on macOS/Linux, since Chromium
//     on Windows doesn't create these files (see profileLockFiles doc above).
//  2. isWindowsSingletonWindowPresent (profile_lock_windows.go / a no-op
//     stub on non-Windows) - the actual Windows-native check, which looks for
//     the hidden message-only window Chromium's Windows ProcessSingleton
//     keeps alive for the entire lifetime of the browser process.
//
// A missing userDataDir or missing lock files/window is not an error - it
// just means the profile is not currently locked (or doesn't exist yet).
func isUserDataDirLocked(userDataDir string) (bool, error) {
	if userDataDir == "" {
		return false, nil
	}
	if _, err := os.Stat(userDataDir); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	for _, name := range profileLockFiles {
		path := filepath.Join(userDataDir, name)
		info, statErr := os.Stat(path)
		if statErr != nil {
			continue
		}
		if info.IsDir() {
			continue
		}

		f, openErr := os.OpenFile(path, os.O_RDWR, 0)
		if openErr != nil {
			if isSharingViolation(openErr) {
				return true, nil
			}
			// Any other error (e.g. permissions) is not conclusive proof of a
			// lock; ignore and keep checking other markers.
			continue
		}
		_ = f.Close()
	}

	locked, err := isWindowsSingletonWindowPresent(userDataDir)
	if err != nil {
		// Inconclusive (e.g. an unexpected Win32 API error): don't silently
		// treat this as "not locked" and risk launching a second instance -
		// surface it so the caller can fail loud instead.
		return false, err
	}
	if locked {
		return true, nil
	}

	return false, nil
}

// isSharingViolation reports whether err indicates the file could not be
// opened because another process is holding it open exclusively. This is the
// case when Chromium/Brave is currently running against the profile.
func isSharingViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	// os.PathError.Err on Windows is a *syscall.Errno wrapping
	// ERROR_SHARING_VIOLATION (32) or ERROR_LOCK_VIOLATION (33) when another
	// process has the file open. Matching on the message keeps this portable
	// across Go versions without importing golang.org/x/sys/windows.
	msg := err.Error()
	return containsAny(msg, []string{
		"used by another process",
		"sharing violation",
		"lock violation",
		"resource temporarily unavailable",
	})
}

// ErrProfileLocked is returned when the dedicated login profile (see
// LoginProfileDir) is currently locked - in practice, by another
// opal-downloader process that already has a persistent Chromium context
// open against it.
var ErrProfileLocked = errors.New("browser profile is currently in use")
