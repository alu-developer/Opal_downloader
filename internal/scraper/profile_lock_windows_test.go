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
