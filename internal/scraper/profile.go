package scraper

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// tuFastDefaultProfileSubdir is the Chromium profile subfolder name inside
// the dedicated login user-data-dir (see LoginProfileDir) - Chromium always
// creates a "Default" profile subdirectory even for a single-profile
// launch. Matches internal/gui/tufast_setup.go's identically-named
// unexported constant (that package can't import this one back, so the
// name is duplicated, not shared - see HasTUFastExtension's doc comment).
const tuFastDefaultProfileSubdir = "Default"

// HasTUFastExtension reports whether a Chromium user-data-dir (profile
// root) has TU-Fast installed in its "Default" profile - a plain,
// read-only filesystem stat, matching the same check
// TransplantTUFastLoginData itself makes before allowing a copy into a
// target profile. This is the single, canonical implementation of that
// check; internal/gui's tufast_setup.go (the /tufast-setup page) and
// cmd/opal-downloader/root.go's checkLoginProfileHealth (the `status`
// command) both call this instead of re-deriving the same filesystem path,
// per this task's "extend/call the existing health-check logic, don't
// duplicate it" constraint.
func HasTUFastExtension(profileDir string) bool {
	if strings.TrimSpace(profileDir) == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(profileDir, tuFastDefaultProfileSubdir, "Extensions", TUFastExtensionID))
	return err == nil && info.IsDir()
}

// ErrTUFastNotReady is returned by EnsureTUFastPresent when the dedicated
// login profile is missing or doesn't have TU-Fast installed - see
// docs/scheduled-sync-plan.md section 2 ("Profile-lock precondition"). A
// human 2FA click will never arrive on an unattended machine, so an
// unattended (--scheduled) sync must fail fast here instead of opening a
// visible interactive-login browser and blocking for
// waitForLoggedInCourseLink's full 300s timeout for nothing.
//
// errors.Is(err, ErrTUFastNotReady) lets cmd/opal-downloader's Execute
// recognize this specific failure mode for a distinguishable exit code,
// per this task's "must be consumable ... without needing to parse
// free-text stdout" acceptance criterion (the actual surfacing of that
// distinction to the user - a status file/GUI banner - is the separate
// follow-up task add-scheduled-sync-failure-log-and-gui-banner).
var ErrTUFastNotReady = errors.New("dedicated login profile is not ready for unattended sync")

// EnsureTUFastPresent is the pre-flight gate `sync --scheduled` runs before
// attempting a login: it hard-requires the dedicated login profile (see
// LoginProfileDir) to exist and have TU-Fast installed, since that's the
// only way ensureSession's interactive-login fallback can complete 2FA with
// no human present. It performs no browser launch and no network call -
// same "filesystem-only, keeps this fast and offline" property as
// checkLoginProfileHealth (root.go's `status` check), which this doesn't
// replace (status's check is informational/non-fatal for any user; this one
// is a hard failure specific to unattended runs).
func EnsureTUFastPresent() error {
	profileDir, err := LoginProfileDir()
	if err != nil {
		return fmt.Errorf("%w: resolving login profile directory: %v", ErrTUFastNotReady, err)
	}
	if _, statErr := os.Stat(profileDir); statErr != nil {
		return fmt.Errorf("%w: %s does not exist yet - run `opal-downloader login` once, or set up TU-Fast via the GUI's /tufast-setup page, before enabling scheduled sync", ErrTUFastNotReady, profileDir)
	}
	if !HasTUFastExtension(profileDir) {
		return fmt.Errorf("%w: TU-Fast extension not detected in %s - scheduled sync needs it to complete 2FA unattended; set it up once via the GUI's /tufast-setup page", ErrTUFastNotReady, profileDir)
	}
	return nil
}
