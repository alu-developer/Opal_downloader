package scraper

import (
	"errors"
	"fmt"
	"io"
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

// ErrProfileLocked is returned by prepareBrowserProfile when the source
// user-data-dir is currently locked by another running browser instance.
var ErrProfileLocked = errors.New("browser profile is currently in use")

// workingProfileParentDirName is the folder created under the user's home
// directory to hold opal-downloader's private copy of the browser profile.
const workingProfileParentDirName = ".opal-downloader"

// defaultWorkingProfileDir returns the default destination for the working
// copy of the browser profile, derived from the user's home directory so it
// does not collide with the project checkout or the real browser profile.
func defaultWorkingProfileDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, workingProfileParentDirName, "browser-profile"), nil
}

// profileCopyEntries lists the paths (relative to the user-data-dir root,
// using "/" separators) that are actually needed for a persistent Chromium
// context to have working cookies, saved logins, and installed/enabled
// extensions (specifically TU-Fast). Caches, history, favicons, GPU shader
// caches, etc. are intentionally excluded - they are multiple GB on a real
// profile and add nothing the scraper needs.
//
// Paths are grouped: entries without a "/" are copied from the user-data-dir
// root; entries prefixed with the profile directory name are resolved once
// the profile directory (e.g. "Default") is known.
var profileRootEntries = []string{
	"Local State", // holds the OS-encryption key used to decrypt cookies/passwords
}

var profileDirEntries = []string{
	"Preferences",              // enabled extensions, settings
	"Secure Preferences",       // extension permissions/settings (integrity-checked)
	"Extensions",               // installed extension code (e.g. TU-Fast)
	"Local Extension Settings", // extension chrome.storage.local data
	"Extension State",          // extension background/service worker state
	"Extension Rules",
	"Extension Scripts",
	"Network",    // Cookies, TransportSecurity, etc.
	"Login Data", // saved logins
	"Login Data-journal",
	"Login Data For Account",
	"Web Data", // autofill/web data used by some extensions
	"Local Storage",
	"IndexedDB",
	"Session Storage",
}

// prepareBrowserProfile ensures a private, opal-downloader-owned copy of the
// configured browser profile exists and is populated, then returns the
// user-data-dir that should actually be passed to
// playwright.Chromium.LaunchPersistentContext.
//
// Rationale: launching Playwright directly against the user's real
// browser_user_data_dir means the tool and a manually-opened Brave window
// fight over the same Chromium profile lock (see isUserDataDirLocked above).
// Copying the profile once into a dedicated working directory lets Brave stay
// fully usable while opal-downloader runs, at the cost of the copy going
// stale if the user reinstalls/updates the TU-Fast extension or logs into
// OPAL again from their everyday browser.
//
// Trade-off (deliberate, documented here and in config.example.yaml): this is
// a one-time copy, refreshed only when missing. We do NOT resync on every
// run, because doing so would re-introduce the lock conflict (we'd need to
// read the live profile while it might be open) and would slow down every
// invocation for a case (extension updates) that is rare in practice. If the
// copy goes stale - e.g. after a TU-Fast update, or after the user changes
// browser_user_data_dir/browser_profile_directory - deleting the working
// profile directory (printed at launch, also derivable via
// defaultWorkingProfileDir) forces a fresh copy on the next run.
func (s *OpalScraper) prepareBrowserProfile() (string, error) {
	sourceUserDataDir := s.browserUserDataDir
	if sourceUserDataDir == "" {
		return "", nil
	}

	destUserDataDir := s.workingProfileDir
	if destUserDataDir == "" {
		var err error
		destUserDataDir, err = defaultWorkingProfileDir()
		if err != nil {
			return "", fmt.Errorf("could not determine working profile directory: %w", err)
		}
	}

	profileDirName := s.browserProfileDir
	if profileDirName == "" {
		profileDirName = "Default"
	}

	// Check for an already-completed working copy *before* checking whether
	// the live source profile is locked. If the copy already exists, we have
	// no need to touch the live source at all, so whether Brave happens to
	// have it open right now is irrelevant - checking the lock first would
	// defeat the entire purpose of the one-time-copy design (see trade-off
	// note above), which is to let Brave stay open while opal-downloader
	// runs. The lock check only matters when a fresh copy actually needs to
	// be read from the live source (i.e. the marker is absent, below).
	marker := filepath.Join(destUserDataDir, ".opal-copy-complete")
	if _, err := os.Stat(marker); err == nil {
		// Already copied in a previous run; reuse it as-is (see trade-off
		// note above).
		return destUserDataDir, nil
	}

	locked, err := isUserDataDirLocked(sourceUserDataDir)
	if err != nil {
		return "", err
	}
	if locked {
		return "", fmt.Errorf("%w: %s appears to be open in Brave (or another Chromium-based browser) right now — please close it before running opal-downloader, or point browser_user_data_dir at a separate profile", ErrProfileLocked, sourceUserDataDir)
	}

	fmt.Printf("Preparing a private copy of the browser profile (one-time) so Brave stays usable while opal-downloader runs: %s\n", destUserDataDir)

	if err := os.RemoveAll(destUserDataDir); err != nil {
		return "", fmt.Errorf("could not clear stale working profile directory: %w", err)
	}
	if err := os.MkdirAll(destUserDataDir, 0o755); err != nil {
		return "", err
	}

	for _, name := range profileRootEntries {
		if err := copyProfilePath(filepath.Join(sourceUserDataDir, name), filepath.Join(destUserDataDir, name)); err != nil {
			return "", fmt.Errorf("copying %s: %w", name, err)
		}
	}

	destProfileDir := filepath.Join(destUserDataDir, profileDirName)
	if err := os.MkdirAll(destProfileDir, 0o755); err != nil {
		return "", err
	}
	for _, name := range profileDirEntries {
		if err := copyProfilePath(filepath.Join(sourceUserDataDir, profileDirName, name), filepath.Join(destProfileDir, name)); err != nil {
			return "", fmt.Errorf("copying %s/%s: %w", profileDirName, name, err)
		}
	}

	if err := os.WriteFile(marker, []byte("ok"), 0o644); err != nil {
		return "", fmt.Errorf("could not finalize working profile directory: %w", err)
	}

	return destUserDataDir, nil
}

// copyProfilePath copies a file or directory tree from src to dst. Missing
// source paths are silently skipped (not every profile has every optional
// file, e.g. "Login Data For Account" only exists once Chrome sync has been
// used).
func copyProfilePath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if info.IsDir() {
		return copyDirRecursive(src, dst, info.Mode())
	}
	return copyFileContents(src, dst, info.Mode())
}

func copyDirRecursive(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(dst, mode.Perm()|0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDirRecursive(srcPath, dstPath, entry.Type().Perm()); err != nil {
				return err
			}
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if err := copyFileContents(srcPath, dstPath, info.Mode()); err != nil {
			return err
		}
	}
	return nil
}

func copyFileContents(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm()|0o200)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
