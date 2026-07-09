//go:build windows

package scraper

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"
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

// createTestMessageWindow creates a real hidden, message-only window (parent
// HWND_MESSAGE) with its title set to title, using the always-registered
// built-in "STATIC" window class so the test doesn't need to register (and
// clean up) a custom window class/WNDPROC. This mirrors how Chromium's
// Windows ProcessSingleton advertises a running instance for a given
// user-data-dir: a message-only window whose title is the profile path,
// findable via FindWindowEx regardless of its window class.
func createTestMessageWindow(t *testing.T, title string) uintptr {
	t.Helper()

	classPtr, err := syscall.UTF16PtrFromString("STATIC")
	if err != nil {
		t.Fatalf("UTF16PtrFromString(class): %v", err)
	}
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		t.Fatalf("UTF16PtrFromString(title): %v", err)
	}

	procCreateWindowExW := user32.NewProc("CreateWindowExW")
	hwnd, _, callErr := procCreateWindowExW.Call(
		0,                                 // dwExStyle
		uintptr(unsafe.Pointer(classPtr)), // lpClassName
		uintptr(unsafe.Pointer(titlePtr)), // lpWindowName
		0,                                 // dwStyle
		0, 0, 0, 0,                        // x, y, nWidth, nHeight
		hwndMessage, // hWndParent
		0,           // hMenu
		0,           // hInstance
		0,           // lpParam
	)
	if hwnd == 0 {
		t.Fatalf("CreateWindowExW failed: %v", callErr)
	}
	t.Cleanup(func() {
		procDestroyWindow := user32.NewProc("DestroyWindow")
		procDestroyWindow.Call(hwnd)
	})
	return hwnd
}

func TestIsUserDataDirLocked_RunningWindowsSingletonWindowIsLocked(t *testing.T) {
	dir := t.TempDir()
	// Deliberately no marker files here at all. Real Chromium/Brave on
	// Windows never creates lockfile/SingletonLock/SingletonCookie - that's a
	// POSIX-only mechanism (process_singleton_posix.cc). On Windows
	// (process_singleton_win.cc), a running instance instead registers a
	// hidden message-only window whose title is the exact user-data-dir
	// path. Before this fix, isUserDataDirLocked only checked the marker
	// files above, so it could never detect a real running Brave instance on
	// Windows - reporting "not locked" even while Brave held the profile
	// open. This test reproduces that scenario and asserts it's now caught.
	createTestMessageWindow(t, dir)

	locked, err := isUserDataDirLocked(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !locked {
		t.Fatal("expected a running Windows singleton message window for this profile to be reported as locked")
	}
}

func TestIsWindowsSingletonWindowPresent_NormalizesTrailingSeparator(t *testing.T) {
	dir := t.TempDir()
	// Chromium/Brave may format the user-data-dir string with (or without) a
	// trailing separator depending on how it was invoked; the comparison
	// must not be sensitive to that.
	createTestMessageWindow(t, dir+string(filepath.Separator))

	present, err := isWindowsSingletonWindowPresent(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !present {
		t.Fatal("expected a trailing-separator title variant to still match")
	}
}

func TestIsWindowsSingletonWindowPresent_UnrelatedProfileNotMatched(t *testing.T) {
	dir := t.TempDir()
	otherDir := t.TempDir()
	createTestMessageWindow(t, otherDir)

	present, err := isWindowsSingletonWindowPresent(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if present {
		t.Fatal("should not report locked when only an unrelated profile's singleton window exists")
	}
}

func TestIsWindowsSingletonWindowPresent_NoWindowPresent(t *testing.T) {
	dir := t.TempDir()
	present, err := isWindowsSingletonWindowPresent(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if present {
		t.Fatal("expected no singleton window to be found for an unused profile dir")
	}
}
