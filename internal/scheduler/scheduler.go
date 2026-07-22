// Package scheduler registers/removes/queries a Windows Task Scheduler task
// that runs `opal-downloader.exe sync --scheduled` once a day. See
// docs/scheduled-sync-plan.md section 4 for the reasoning behind every
// design choice here: a fixed daily-time CalendarTrigger (not an "on logon"
// trigger, which can fire multiple times a day and would need its own
// dedup guard) with STARTWHENAVAILABLE enabled (Task Scheduler's own
// built-in "run as soon as possible if the scheduled time was missed"
// behavior, covering the "machine was off/asleep" case with no extra
// opal-downloader-side code), running only when the user is logged on
// (LogonType InteractiveToken - the simpler, safer default: no Windows
// credential storage needed for the task, at the cost of not running while
// fully logged out).
//
// This is deliberately Windows-only (see scheduler_windows.go /
// scheduler_other.go) - per this project's CLAUDE.md and this task's own
// non-goals, macOS/Linux scheduling (launchd/cron) is out of scope until
// there's a concrete need. The package still builds on every platform (the
// non-Windows stub returns ErrUnsupported) so cmd/opal-downloader and
// internal/gui don't need their own per-OS build tags just to reference it.
package scheduler

import (
	"errors"
	"regexp"
)

// TaskName is the single Task Scheduler task name this package ever
// creates/deletes/queries - there is only ever one scheduled-sync task per
// machine (matching this being a single-user desktop tool with one
// config.yaml/download_path), so no per-config-path naming is needed.
const TaskName = "OpalDownloaderScheduledSync"

// DefaultTime is the GUI/CLI default when the user enables scheduling
// without picking a specific time - early morning, to minimize the odds of
// the brief visible-browser flash (see docs/scheduled-sync-plan.md section
// 1) coinciding with the user actively watching their screen, while
// STARTWHENAVAILABLE still catches up shortly after they wake/log in if the
// machine was asleep at that time.
const DefaultTime = "06:00"

// ErrUnsupported is returned by Enable/Disable/Status on any platform other
// than Windows.
var ErrUnsupported = errors.New("scheduled sync registration is only supported on Windows (Task Scheduler)")

// timePattern validates the "HH:MM" 24-hour time-of-day format used
// throughout this package's public API (Enable's hhmm parameter, Info.Time,
// the GUI's time input, and `schedule enable --time`).
var timePattern = regexp.MustCompile(`^([01]\d|2[0-3]):([0-5]\d)$`)

// ValidateTime reports whether hhmm is a valid 24-hour "HH:MM" time of day
// (e.g. "06:00", "23:59"). Shared by both platform implementations' Enable
// and by callers (GUI form validation, CLI --time parsing) that want to
// reject a bad value before ever shelling out to schtasks.
func ValidateTime(hhmm string) error {
	if !timePattern.MatchString(hhmm) {
		return errInvalidTime(hhmm)
	}
	return nil
}

func errInvalidTime(hhmm string) error {
	return &invalidTimeError{hhmm: hhmm}
}

type invalidTimeError struct{ hhmm string }

func (e *invalidTimeError) Error() string {
	return "invalid time " + quote(e.hhmm) + ": expected 24-hour HH:MM (e.g. 06:00)"
}

func quote(s string) string {
	return "\"" + s + "\""
}

// Info describes the current registration state of the scheduled-sync task.
type Info struct {
	// Registered is true if the Task Scheduler task exists at all.
	Registered bool
	// Time is the task's daily trigger time in 24-hour "HH:MM" format.
	// Only meaningful when Registered is true.
	Time string

	// ExecutablePath is the binary the registered task actually runs, and
	// ExecutableMissing reports that this file no longer exists. Together
	// they surface the failure mode described on ErrEphemeralExecutable: a
	// task registered against a path that later disappears stays
	// "registered" forever while never running once. Nothing else in the
	// product can detect that, because the scheduled-status file is only
	// written by a run that actually happened - so a task that never
	// launches leaves the GUI cheerfully showing the last successful run.
	//
	// ExecutablePath is "" when it could not be determined, in which case
	// ExecutableMissing is always false: an unknown path must never be
	// reported as a missing one.
	ExecutablePath    string
	ExecutableMissing bool
}
