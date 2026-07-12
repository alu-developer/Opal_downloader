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
	user32                 = syscall.NewLazyDLL("user32.dll")
	procFindWindowExW      = user32.NewProc("FindWindowExW")
	procSendMessageTimeout = user32.NewProc("SendMessageTimeoutW")
)

// hwndMessage is HWND_MESSAGE, the special parent handle ((HWND)-3) used for
// message-only windows. Message-only windows are not visible, do not appear
// in EnumWindows, and can only be found via FindWindowEx with this as the
// parent.
const hwndMessage = ^uintptr(2)

// Windows message and SendMessageTimeout constants used by getWindowTextW.
// WM_GETTEXTLENGTH/WM_GETTEXT are the messages GetWindowTextLengthW/
// GetWindowTextW send under the hood; SMTO_ABORTIFHUNG makes
// SendMessageTimeoutW return immediately (rather than waiting out the full
// timeout) once Windows has already flagged the target window as not
// responding.
const (
	wmGetTextLength     = 0x000E
	wmGetText           = 0x000D
	smtoAbortIfHung     = 0x0002
	windowTextTimeoutMs = 300
)

// sendMessageTimeout wraps user32!SendMessageTimeoutW. It returns the
// message result and whether the call completed (false if the target
// window's message queue didn't respond within timeoutMs, e.g. because the
// owning process is hung) - see getWindowTextW for why this matters.
func sendMessageTimeout(hwnd uintptr, msg uintptr, wParam uintptr, lParam uintptr, timeoutMs uintptr) (result uintptr, ok bool) {
	var out uintptr
	ret, _, _ := procSendMessageTimeout.Call(
		hwnd, msg, wParam, lParam,
		smtoAbortIfHung, timeoutMs,
		uintptr(unsafe.Pointer(&out)),
	)
	if ret == 0 {
		// SendMessageTimeoutW returns 0 both on a real send failure and on a
		// timeout (GetLastError would distinguish them, but either way the
		// window isn't answering right now) - treat both as "no result",
		// which the caller treats as "not a match" rather than an error.
		return 0, false
	}
	return out, true
}

// getWindowTextW fetches a window's title the same way GetWindowTextW/
// GetWindowTextLengthW do, but via SendMessageTimeoutW with
// SMTO_ABORTIFHUNG so a single unresponsive window (belonging to some
// unrelated hung process elsewhere on the desktop) can't block
// isWindowsSingletonWindowPresent's enumeration forever. GetWindowTextW and
// GetWindowTextLengthW both work by synchronously sending WM_GETTEXT/
// WM_GETTEXTLENGTH to the target window and blocking until it replies -
// with no timeout, a hung window's message queue never replies and the call
// never returns. Sending the same messages ourselves via
// SendMessageTimeoutW gets the same information with a bounded wait.
//
// A timed-out or empty-title window is reported as "" with no error, which
// the caller (isWindowsSingletonWindowPresent) treats as "not a match" and
// moves on to the next window, rather than failing the whole scan.
func getWindowTextW(hwnd uintptr) (string, error) {
	length, ok := sendMessageTimeout(hwnd, wmGetTextLength, 0, 0, windowTextTimeoutMs)
	if !ok || length == 0 {
		return "", nil
	}
	buf := make([]uint16, length+1)
	n, ok := sendMessageTimeout(hwnd, wmGetText, uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])), windowTextTimeoutMs)
	if !ok || n == 0 {
		return "", nil
	}
	return syscall.UTF16ToString(buf[:n]), nil
}
