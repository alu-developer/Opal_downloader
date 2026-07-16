package scraper

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TUFastExtensionID is TU-Fast's Chrome Web Store extension ID - the single
// source of truth for this heuristic across the codebase (the CLI's
// checkBrowserProfileHealth in cmd/opal-downloader/root.go references this
// constant rather than keeping its own copy). Confirmed live in the task
// that first wired up TU-Fast auto-login - see
// docs/browser-profile-strategy.md's "Health-check design" section. Tied to
// the current TU-Fast build, not a stable protocol guarantee: if the ID ever
// changes, callers of this constant degrade to "soft warning" or "copy not
// found" behavior, never a false hard failure.
const TUFastExtensionID = "aheogihliekaafikeepfjngfegbnimbk"

// tuFastDataDirs lists the paths (relative to a Chromium profile directory,
// e.g. "<user-data-dir>/Default/...") that hold TU-Fast's own stored
// login/device-registration data.
//
// Confirmed live 2026-07-12 (see the task that added this file, and
// docs/browser-profile-strategy.md's "Transplanting TU-Fast login data"
// section) by reading TU-Fast's own bundled source
// (modules/credentials.js, modules/otp.js) from a real installed copy:
// TU-Fast stores the OPAL/Shibboleth username, password, and the full TOTP
// secret it uses to auto-generate 2FA codes entirely in
// chrome.storage.local - there is no server-side device token or
// certificate involved. Chromium persists chrome.storage.local as a leveldb
// directory at "Local Extension Settings/<extension-id>", which is what
// this copies. "Sync Extension Settings/<extension-id>" is included
// defensively in case a future TU-Fast version opts into chrome.storage.sync
// instead/as well - it was empty (didn't exist) in the version tested, so
// the copy step below silently skips any entry that isn't present rather
// than erroring.
var tuFastDataDirs = []string{
	filepath.Join("Local Extension Settings", TUFastExtensionID),
	filepath.Join("Sync Extension Settings", TUFastExtensionID),
}

// TransplantResult reports what TransplantTUFastLoginData actually did, so a
// caller (the GUI handler) can show the user exactly what changed rather
// than a bare "success".
type TransplantResult struct {
	// CopiedDirs lists the tuFastDataDirs entries (relative paths) that were
	// found in the source profile and copied into the target.
	CopiedDirs []string
	// BackedUpDirs lists any pre-existing target paths that were renamed out
	// of the way (not deleted) before being overwritten, so a bad copy can
	// be undone by hand. Empty when the target had no prior TU-Fast data.
	BackedUpDirs []string
}

// TransplantTUFastLoginData copies TU-Fast's own stored login/2FA data
// (chrome.storage.local, see tuFastDataDirs) from an existing, working
// browser profile into a different, already-TU-Fast-installed browser
// profile - skipping Chromium's own "Preferences"/"Secure Preferences"/
// "Local State"/"Extensions" files entirely, which is what keeps this safe:
// PR #20/#41's HMAC finding is specifically about *those* files getting
// copied into a directory they weren't generated for, which strips the
// extension's permissions the instant Chromium loads it. Since this never
// touches them - the target keeps whatever Preferences/Secure Preferences it
// already has from its own real install - that failure mode doesn't apply
// here.
//
// This is a 100% local, offline filesystem operation - no network calls, no
// browser launch - matching CLAUDE.md's "credentials/session data never
// leave the machine unscrubbed" principle (this data doesn't even leave the
// machine).
//
// Preconditions checked before anything is written: source and target must
// be different, existing, real-looking (has "Preferences") Chromium
// profiles; neither may currently be open in a running browser (reusing
// isUserDataDirLocked, the same lock check opal-downloader's own login path
// already respects, since copying in/out of a live LevelDB risks a torn
// read or a corrupted destination); the target must already have TU-Fast
// installed (its own "Extensions/<id>" present - this function only ever
// transplants the login *data*, never the extension install itself, which
// per this feature's design must stay a genuine one-time manual Web Store
// install in the target profile); and the source must actually have some
// TU-Fast data to copy.
//
// Any pre-existing TU-Fast data already in the target is renamed aside
// (never deleted) before being overwritten, so a mistaken copy can be
// manually undone.
func TransplantTUFastLoginData(sourceUserDataDir, sourceProfileDir, targetUserDataDir, targetProfileDir string) (TransplantResult, error) {
	var result TransplantResult

	sourceUserDataDir = strings.TrimSpace(sourceUserDataDir)
	targetUserDataDir = strings.TrimSpace(targetUserDataDir)
	if sourceUserDataDir == "" {
		return result, errors.New("source browser profile directory is required")
	}
	if targetUserDataDir == "" {
		return result, errors.New("could not determine the target login profile directory")
	}

	sourceDir := filepath.Join(sourceUserDataDir, profileSubdir(sourceProfileDir))
	targetDir := filepath.Join(targetUserDataDir, profileSubdir(targetProfileDir))

	if samePath(sourceDir, targetDir) {
		return result, errors.New("source and target are the same browser profile - nothing to copy")
	}

	if err := requireRealProfileDir(sourceDir); err != nil {
		return result, fmt.Errorf("source profile: %w", err)
	}
	if err := requireRealProfileDir(targetDir); err != nil {
		return result, fmt.Errorf("target profile: %w", err)
	}

	if locked, err := isUserDataDirLocked(sourceUserDataDir); err != nil {
		return result, fmt.Errorf("checking source profile lock: %w", err)
	} else if locked {
		return result, fmt.Errorf("the source browser profile (%s) is currently open in a running browser - close it and try again", sourceUserDataDir)
	}
	if locked, err := isUserDataDirLocked(targetUserDataDir); err != nil {
		return result, fmt.Errorf("checking target profile lock: %w", err)
	} else if locked {
		return result, fmt.Errorf("the target browser profile (%s) is currently open in a running browser - close it and try again", targetUserDataDir)
	}

	targetExtensionPath := filepath.Join(targetDir, "Extensions", TUFastExtensionID)
	if _, err := os.Stat(targetExtensionPath); err != nil {
		return result, fmt.Errorf("TU-Fast isn't installed in the target profile yet (%s not found) - install it from the Chrome Web Store in that profile first, then retry", targetExtensionPath)
	}

	for _, rel := range tuFastDataDirs {
		srcPath := filepath.Join(sourceDir, rel)
		info, err := os.Stat(srcPath)
		if err != nil || !info.IsDir() {
			continue
		}

		dstPath := filepath.Join(targetDir, rel)
		if _, err := os.Stat(dstPath); err == nil {
			backupPath := dstPath + ".opal-downloader-backup-" + time.Now().Format("20060102-150405")
			if err := os.Rename(dstPath, backupPath); err != nil {
				return result, fmt.Errorf("backing up existing %s before overwrite: %w", dstPath, err)
			}
			result.BackedUpDirs = append(result.BackedUpDirs, backupPath)
		}

		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return result, fmt.Errorf("creating %s: %w", filepath.Dir(dstPath), err)
		}
		if err := copyDirRecursive(srcPath, dstPath); err != nil {
			return result, fmt.Errorf("copying %s: %w", rel, err)
		}
		result.CopiedDirs = append(result.CopiedDirs, rel)
	}

	if len(result.CopiedDirs) == 0 {
		return result, fmt.Errorf("no TU-Fast login data found in the source profile (%s) - make sure TU-Fast is installed and logged in there", sourceDir)
	}

	return result, nil
}

// profileSubdir returns profileDir, defaulting to Chromium's own default
// profile subfolder name when empty - matching the same default already
// used by checkBrowserProfileHealth in cmd/opal-downloader/root.go and by
// session.go's launchBrowser.
func profileSubdir(profileDir string) string {
	if strings.TrimSpace(profileDir) == "" {
		return "Default"
	}
	return profileDir
}

// requireRealProfileDir checks profileDir exists and looks like a genuine
// Chromium profile (has a "Preferences" file) - the same heuristic
// checkBrowserProfileHealth uses.
func requireRealProfileDir(profileDir string) error {
	if _, err := os.Stat(profileDir); err != nil {
		return fmt.Errorf("%s doesn't exist", profileDir)
	}
	preferencesPath := filepath.Join(profileDir, "Preferences")
	if _, err := os.Stat(preferencesPath); err != nil {
		return fmt.Errorf("%s wasn't found - this doesn't look like a real browser profile", preferencesPath)
	}
	return nil
}

// samePath reports whether a and b resolve to the same filesystem location,
// comparing case-insensitively (safe on Windows, this project's primary
// target; harmless-but-slightly-loose on case-sensitive filesystems, where
// it can only make this check more conservative, never less).
func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return strings.EqualFold(absA, absB)
}

// copyDirRecursive copies every file under src into dst, creating
// directories as needed. Used only for TU-Fast's own small (kilobytes)
// leveldb storage directories - not a general-purpose large-tree copier.
func copyDirRecursive(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
