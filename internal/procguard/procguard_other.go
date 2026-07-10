//go:build !windows

// Package procguard ties child-process lifetime to this process's own
// lifetime on platforms that support it. See procguard_windows.go's doc
// comment for the full rationale (orphaned Chromium windows surviving a
// force-killed opal-downloader process).
package procguard

// EnsureChildProcessesDieWithParent is a no-op on non-Windows platforms.
// Windows is this project's primary target (see CLAUDE.md); POSIX process
// groups/job-control equivalents (e.g. Linux's PR_SET_PDEATHSIG, or placing
// the browser in its own process group and killing the group) are not
// implemented yet - tracked as a gap, not silently assumed safe.
func EnsureChildProcessesDieWithParent() {}
