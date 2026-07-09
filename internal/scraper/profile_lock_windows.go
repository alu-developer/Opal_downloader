//go:build windows

package scraper

import (
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// Root cause background (see profile.go's doc comment on isUserDataDirLocked
// for the full story): Chromium's ProcessSingleton has two completely
// different implementations depending on OS. On POSIX
// (process_singleton_posix.cc) it really does create "lockfile" /
// "SingletonLock" / "SingletonCookie" in the user-data-dir, which is what
// profile.go's marker-file check detects. On Windows
// (process_singleton_win.cc) none of those files are ever created; instead
// the running instance registers a hidden, message-only window (parent
// HWND_MESSAGE) whose window title is set to the exact user-data-dir path,
// and a new launch finds it via FindWindowEx(HWND_MESSAGE, ..., user_data_dir)
// to decide whether to hand off to the existing instance instead of starting
// a second one. That window exists for the entire lifetime of the browser
// process, not just briefly at startup - so unlike the marker-file approach,
// enumerating message-only windows is a reliable signal on Windows, which is
// this project's primary target platform.
//
// isWindowsSingletonWindowPresent looks for that window by enumerating every
// message-only window (regardless of its window class, since Chromium forks
// such as Brave are not guaranteed to keep Chrome's exact
// "Chrome_MessageWindow" class name) and comparing its title against
// userDataDir after normalizing both for path separator/case/trailing-slash
// differences.
func isWindowsSingletonWindowPresent(userDataDir string) (bool, error) {
	if userDataDir == "" {
		return false, nil
	}
	target := normalizeProfilePathForCompare(userDataDir)

	var prev uintptr
	for {
		// FindWindowEx returning NULL just means "no more message-only
		// windows to enumerate" (whether because we reached the end of the
		// list or none exist at all) - per Win32 convention for enumeration
		// APIs like this, a NULL/0 return is not itself a failure worth
		// surfacing, so we simply stop enumerating rather than trying to
		// classify the accompanying GetLastError value.
		hwnd, _, _ := procFindWindowExW.Call(hwndMessage, prev, 0, 0)
		if hwnd == 0 {
			break
		}

		title, err := getWindowTextW(hwnd)
		if err == nil && title != "" && normalizeProfilePathForCompare(title) == target {
			return true, nil
		}
		prev = hwnd
	}

	return false, nil
}

func normalizeProfilePathForCompare(p string) string {
	p = strings.TrimRight(p, `\/`)
	p = filepath.Clean(p)
	return strings.ToLower(p)
}

var (
	user32                = syscall.NewLazyDLL("user32.dll")
	procFindWindowExW     = user32.NewProc("FindWindowExW")
	procGetWindowTextW    = user32.NewProc("GetWindowTextW")
	procGetWindowTextLenW = user32.NewProc("GetWindowTextLengthW")
)

// hwndMessage is HWND_MESSAGE, the special parent handle ((HWND)-3) used for
// message-only windows. Message-only windows are not visible, do not appear
// in EnumWindows, and can only be found via FindWindowEx with this as the
// parent.
const hwndMessage = ^uintptr(2)

func getWindowTextW(hwnd uintptr) (string, error) {
	length, _, _ := procGetWindowTextLenW.Call(hwnd)
	if length == 0 {
		return "", nil
	}
	buf := make([]uint16, length+1)
	n, _, callErr := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return "", callErr
	}
	return syscall.UTF16ToString(buf[:n]), nil
}
