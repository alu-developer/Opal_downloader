// Package notify shows a native Windows toast notification when a
// scheduled sync run fails (internal/statuslog.OutcomeFailure only - never
// "partial" or "success"), so a user can find out a scheduled run broke
// without opening the GUI. Windows-only (see notify_windows.go /
// notify_other.go), matching internal/scheduler's OS-gating pattern: the
// package still builds on every platform (the non-Windows stub returns
// ErrUnsupported) so callers don't need their own per-OS build tags.
//
// Investigation (queue task
// add-windows-toast-notification-for-scheduled-sync-failures, 2026-07-17):
// BurntToast (the PowerShell module commonly used for this) is NOT present
// by default on this machine's Windows 11 install (Get-Module -ListAvailable
// BurntToast returns nothing), and the task spec explicitly disqualifies
// silently installing a PowerShell module as a side effect of enabling a
// notification toggle. Instead this shells out to a small inline PowerShell
// script that calls the Windows Runtime toast API
// (Windows.UI.Notifications.ToastNotificationManager) directly - no
// PowerShell module and no new go.mod dependency needed. Live-verified on
// this machine (real Windows 11, build 26100): calling
// CreateToastNotifier with a plain, unregistered app ID string (no Start
// Menu shortcut/AUMID registration) succeeds with no exception, the shown
// toast is recorded in ToastNotificationManager.History with the exact
// content passed in (including quotes/ampersands/unicode, confirming the
// escaping is correct), and the notifier's own .Setting property reports
// "Enabled" (Windows considers the app allowed to show notifications, not
// suppressed) - see the PR description for the exact commands run.
package notify

import (
	"errors"

	"github.com/alu-developer/opal-downloader/internal/statuslog"
)

// ErrUnsupported is returned by ScheduledSyncFailure on any platform other
// than Windows.
var ErrUnsupported = errors.New("toast notifications are only supported on Windows")

// AppID identifies this notification's source to the Windows Notification
// Platform. It is a plain string, not registered via a Start Menu
// shortcut/AUMID - see this package's doc comment for the live
// investigation confirming an unregistered app ID still works.
const AppID = "OpalDownloader.ScheduledSync"

// scheduledFailureTitle is the toast's title line. Kept generic/short - the
// actionable detail goes in the body (message).
const scheduledFailureTitle = "Opal Downloader: scheduled sync failed"

// ScheduledSyncFailure shows a toast notification reporting that a
// scheduled sync run failed.
//
// message is expected to already be sanitized (see statuslog.SanitizeMessage)
// by the time it gets here - callers are expected to pass the same
// status.Message that was just written to the scheduled-run status file.
// message is re-sanitized here anyway, same as statuslog.Write does for the
// status file itself: CLAUDE.md's "credentials and session data never leave
// the machine unscrubbed" is this project's one non-negotiable safety bar,
// so a toast - which renders directly on screen, potentially visible to
// anyone glancing at the machine - gets the same last-line-of-defense
// treatment rather than trusting every call site got it right.
func ScheduledSyncFailure(message string) error {
	return show(scheduledFailureTitle, statuslog.SanitizeMessage(message))
}
