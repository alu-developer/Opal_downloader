package scraper

import (
	"errors"
	"os"
	"path/filepath"
)

// profileLockFiles lists the files Chromium-based browsers use to guard a
// user-data-dir against being opened by two running instances at once.
//
// "lockfile" is the marker used on Windows (Chromium's ProcessSingleton opens
// it with an exclusive share mode for the lifetime of the browser process).
// "SingletonLock"/"SingletonCookie" are the equivalents used on macOS/Linux.
// We check all of them so the detection works regardless of platform, even
// though this project's primary target is Windows.
var profileLockFiles = []string{"lockfile", "SingletonLock", "SingletonCookie"}

// isUserDataDirLocked reports whether userDataDir appears to be in active use
// by another Chromium-based browser process (e.g. the user's real Brave
// window). It works by trying to open the known lock marker files with
// read-write access and no sharing allowed to other writers; if the OS
// reports the file is already in use, the profile is locked.
//
// A missing userDataDir or missing lock files is not an error - it just means
// the profile is not currently locked (or doesn't exist yet).
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

// ErrProfileLocked is returned when the configured browser_user_data_dir is
// currently locked by another running browser instance (e.g. the user's real
// Brave window).
var ErrProfileLocked = errors.New("browser profile is currently in use")
