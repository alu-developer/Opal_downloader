//go:build windows

package scraper

import (
	"os"
	"syscall"
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
