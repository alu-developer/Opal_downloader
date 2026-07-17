//go:build !windows

package scheduler

// Enable, Disable, and Status are all unsupported on non-Windows platforms
// - see this package's doc comment. Kept as real (if trivial) functions
// rather than build-tagging them out of existence entirely so
// cmd/opal-downloader and internal/gui can call this package
// unconditionally without their own per-OS build tags.

func Enable(exePath, hhmm string) error {
	return ErrUnsupported
}

func Disable() error {
	return ErrUnsupported
}

func Status() (Info, error) {
	return Info{}, ErrUnsupported
}
