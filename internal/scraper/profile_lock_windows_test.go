//go:build windows

package scraper

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
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

	// A window belongs to the OS thread that created it, and Windows destroys
	// every window a thread owns when that thread exits. A Go goroutine is not
	// pinned to a thread, and the runtime is free to retire an idle one - so
	// without this lock the window can vanish partway through the test, and
	// isUserDataDirLocked then correctly reports "not locked" for a profile
	// whose advertising window no longer exists.
	//
	// That is what made TestIsUserDataDirLocked_RunningWindowsSingletonWindowIsLocked
	// flaky on 2026-07-21: it failed once inside a full `scripts/dev.ps1 all`
	// run (lots of goroutines and browser launches, so lots of thread churn)
	// and never in isolation - 30 consecutive solo runs all passed. The
	// unresponsive-window test further down this file already locks its
	// thread for a related reason; this helper simply never did.
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)

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

// TestIsWindowsSingletonWindowPresent_UnresponsiveWindowDoesNotHang
// reproduces the original bug: a message-only window that exists but whose
// owning thread never pumps messages (i.e. never calls GetMessage/
// PeekMessage), the same way a hung/frozen desktop application would look
// from the outside. Before the SendMessageTimeoutW fix, getWindowTextW used
// GetWindowTextW/GetWindowTextLengthW directly, which send a synchronous
// message and block forever waiting for a reply that a non-pumping thread
// will never send - so isWindowsSingletonWindowPresent (and therefore
// isUserDataDirLocked, which gates every launchBrowser call) could hang
// indefinitely on any unrelated hung window anywhere on the desktop. This
// test creates exactly that kind of window - on a dedicated goroutine that
// creates it and then deliberately blocks without ever entering a message
// loop - and asserts the scan still returns within a small bounded time
// instead of hanging.
func TestIsWindowsSingletonWindowPresent_UnresponsiveWindowDoesNotHang(t *testing.T) {
	dir := t.TempDir()

	readyCh := make(chan uintptr, 1)
	unblockCh := make(chan struct{})
	ownerDoneCh := make(chan struct{})

	go func() {
		defer close(ownerDoneCh)
		// Locking this goroutine to its OS thread and then never running a
		// message loop on it is what makes the window "unresponsive": Win32
		// only delivers cross-thread SendMessage calls when the owning
		// thread is pumping messages, so a thread that never calls
		// GetMessage/PeekMessage will never reply to one.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		classPtr, err := syscall.UTF16PtrFromString("STATIC")
		if err != nil {
			close(readyCh)
			return
		}
		titlePtr, err := syscall.UTF16PtrFromString(dir)
		if err != nil {
			close(readyCh)
			return
		}
		procCreateWindowExW := user32.NewProc("CreateWindowExW")
		hwnd, _, _ := procCreateWindowExW.Call(
			0,
			uintptr(unsafe.Pointer(classPtr)),
			uintptr(unsafe.Pointer(titlePtr)),
			0,
			0, 0, 0, 0,
			hwndMessage,
			0,
			0,
			0,
		)
		readyCh <- hwnd
		if hwnd == 0 {
			return
		}

		// Deliberately do NOT pump messages here - just block, simulating a
		// hung GUI thread. Once the test signals it's done, clean up the
		// window.
		<-unblockCh
		procDestroyWindow := user32.NewProc("DestroyWindow")
		procDestroyWindow.Call(hwnd)
	}()
	t.Cleanup(func() {
		close(unblockCh)
		<-ownerDoneCh
	})

	hwnd := <-readyCh
	if hwnd == 0 {
		t.Fatal("setup: failed to create unresponsive test window")
	}

	type scanResult struct {
		present bool
		err     error
	}
	resultCh := make(chan scanResult, 1)
	start := time.Now()
	go func() {
		present, err := isWindowsSingletonWindowPresent(dir)
		resultCh <- scanResult{present, err}
	}()

	// The fix bounds each window's per-message wait to windowTextTimeoutMs
	// (300ms) with SMTO_ABORTIFHUNG, and this scenario only has one such
	// window, so the whole scan should complete in well under a second.
	// Before the fix this would hang forever (or until the original 10-
	// minute go test default timeout, as observed live during
	// ease-second-profile-tufast-setup testing) - 5s is a generous bound
	// that still fails fast and decisively if the fix regresses.
	select {
	case res := <-resultCh:
		elapsed := time.Since(start)
		t.Logf("scan completed in %v", elapsed)
		if res.err != nil {
			t.Fatalf("unexpected error: %v", res.err)
		}
		// The unresponsive window never actually reports a title (its
		// WM_GETTEXT/WM_GETTEXTLENGTH replies time out), so it can never be
		// matched as a "locked" singleton window - it should just be
		// skipped, not misreported as present.
		if res.present {
			t.Fatal("an unresponsive window with no readable title should not be reported as a singleton match")
		}
		if elapsed > 5*time.Second {
			t.Fatalf("scan took %v - expected the SendMessageTimeoutW fix to bound the wait to roughly 2*windowTextTimeoutMs", elapsed)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("isWindowsSingletonWindowPresent hung on an unresponsive window - SendMessageTimeoutW/SMTO_ABORTIFHUNG did not bound the wait as expected")
	}
}
