//go:build windows

package scraper

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// openExclusiveForTest opens path with no sharing allowed to other readers or
// writers, the same way Chromium's ProcessSingleton holds its Windows
// "lockfile" open for the lifetime of the browser process. It's used to
// simulate "another process has the profile open" in tests without needing
// an actual running browser.
func openExclusiveForTest(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		0, // FILE_SHARE_NONE
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}

func TestIsUserDataDirLocked_OpenHandleIsLocked(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "lockfile")
	if err := os.WriteFile(lockPath, []byte(""), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Simulate a running browser by holding the file open exclusively, the
	// same way Chromium's ProcessSingleton does on Windows.
	f, err := openExclusiveForTest(lockPath)
	if err != nil {
		t.Fatalf("setup: could not open lock file: %v", err)
	}
	defer f.Close()

	locked, err := isUserDataDirLocked(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !locked {
		t.Fatal("expected an exclusively held lockfile to be reported as locked")
	}
}

func TestPrepareBrowserProfile_LockedSourceReturnsClearError(t *testing.T) {
	source := t.TempDir()
	lockPath := filepath.Join(source, "lockfile")
	if err := os.WriteFile(lockPath, []byte(""), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	f, err := openExclusiveForTest(lockPath)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer f.Close()

	s := &OpalScraper{browserUserDataDir: source, workingProfileDir: filepath.Join(t.TempDir(), "working")}
	_, err = s.prepareBrowserProfile()
	if err == nil {
		t.Fatal("expected an error when the source profile is locked")
	}
	if !errorIs(err, ErrProfileLocked) {
		t.Fatalf("expected error to wrap ErrProfileLocked, got: %v", err)
	}
}

// TestPrepareBrowserProfile_ExistingCopyIgnoresLockedSource is the regression
// test for the bug this file's changes fix: when the working profile copy
// already exists (marker present), prepareBrowserProfile must not even check
// whether the live source profile is locked - it should return the existing
// working copy path immediately. Previously the lock check ran first and
// would incorrectly block every run whenever Brave happened to be open, even
// though the tool had no need to touch the live profile at all.
func TestPrepareBrowserProfile_ExistingCopyIgnoresLockedSource(t *testing.T) {
	source := t.TempDir()
	lockPath := filepath.Join(source, "lockfile")
	if err := os.WriteFile(lockPath, []byte(""), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Simulate the live source profile being locked (as if Brave had it open
	// right now), the same way Chromium's ProcessSingleton does on Windows.
	f, err := openExclusiveForTest(lockPath)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer f.Close()

	// Sanity-check that the simulated lock is actually detected; otherwise
	// this test would pass for the wrong reason.
	if locked, lockErr := isUserDataDirLocked(source); lockErr != nil || !locked {
		t.Fatalf("failed to simulate a locked source profile (locked=%v, err=%v)", locked, lockErr)
	}

	dest := t.TempDir()
	// Simulate a working copy already completed in a previous run.
	if err := os.WriteFile(filepath.Join(dest, ".opal-copy-complete"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("setup marker: %v", err)
	}

	s := &OpalScraper{
		browserUserDataDir: source,
		browserProfileDir:  "Default",
		workingProfileDir:  dest,
	}

	got, err := s.prepareBrowserProfile()
	if err != nil {
		t.Fatalf("expected no error when working copy already exists (even with source locked), got: %v", err)
	}
	if got != dest {
		t.Fatalf("expected existing working copy path %q to be returned unchanged, got %q", dest, got)
	}
}
