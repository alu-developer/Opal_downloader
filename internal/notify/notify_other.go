//go:build !windows

package notify

// show is unsupported on non-Windows platforms - see this package's doc
// comment. Kept as a real (if trivial) function rather than build-tagging
// ScheduledSyncFailure itself out of existence entirely so callers can
// reference this package unconditionally without their own per-OS build
// tags (same reasoning as internal/scheduler's scheduler_other.go).
func show(title, message string) error {
	return ErrUnsupported
}
