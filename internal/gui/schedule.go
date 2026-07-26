package gui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scheduler"
)

// scheduleStatusFunc, scheduleEnableFunc, scheduleDisableFunc, and
// exeForScheduleFunc are package-level indirections over
// scheduler.Status/Enable/Disable and os.Executable (matching this
// package's existing test-override convention - see e.g.
// detectBrowserUserDataDir in tufast_setup.go) so `go test` never shells
// out to a real schtasks.exe or depends on os.Executable() resolving to a
// real, meaningful path for the test binary.
var (
	scheduleStatusFunc  = scheduler.Status
	scheduleEnableFunc  = scheduler.Enable
	scheduleDisableFunc = scheduler.Disable
	exeForScheduleFunc  = os.Executable
)

// applyScheduleStatus queries the current Task Scheduler registration state
// (or the injected fakes in tests) and fills view's Schedule* fields.
// Called on every Settings page render (GET and POST) so the toggle always
// reflects the real, live OS-level state rather than a persisted guess that
// could drift (e.g. if the task was removed by hand in Task Scheduler's own
// UI).
func applyScheduleStatus(view settingsViewData) settingsViewData {
	info, err := scheduleStatusFunc()
	if err != nil {
		if errors.Is(err, scheduler.ErrUnsupported) {
			view.ScheduleSupported = false
			return view
		}
		view.ScheduleSupported = true
		view.ScheduleError = err.Error()
		view.ScheduleTime = scheduler.DefaultTime
		return view
	}

	view.ScheduleSupported = true
	view.ScheduleEnabled = info.Registered
	view.ScheduleTime = info.Time
	if view.ScheduleTime == "" {
		view.ScheduleTime = scheduler.DefaultTime
	}
	if info.Registered {
		view = repairDoomedSchedule(view, info)
	}
	return view
}

// scheduleIsDoomed reports whether a registered task's executable will not be
// there when it matters. Two shapes of the same failure: the binary is
// already gone, or it is still present but sits somewhere disposable (a Go
// build-cache entry from a `go run .` session, say) and will be cleaned up
// without warning.
//
// The second case is the one #122 could not fix. It only stopped *new*
// registrations being created that way; a registration made before it shipped
// still points at a doomed path and looks perfectly healthy until the day it
// isn't.
func scheduleIsDoomed(info scheduler.Info) bool {
	if info.ExecutableMissing {
		return true
	}
	if strings.TrimSpace(info.ExecutablePath) == "" {
		return false
	}
	return errors.Is(scheduler.CheckExecutableStable(info.ExecutablePath), scheduler.ErrEphemeralExecutable)
}

// repairDoomedSchedule re-points a doomed registration at the running
// executable, keeping its trigger time, and says so. Telling the user to
// "re-enable the schedule to repair it" was the previous behaviour, and it
// asks them to perform a repair they did not cause, do not understand, and
// cannot verify - for a feature whose entire promise is that they never have
// to think about it again.
//
// Yes, this writes to Task Scheduler during a page render. That is deliberate
// and bounded: it only fires for a registration that is already broken, it
// restores exactly what the user asked for rather than changing it, it is
// idempotent (the repaired path is stable, so the next render finds nothing
// to do), and it never happens silently - the page always reports it.
//
// Repair is skipped when the running executable is itself disposable:
// swapping one doomed path for another would only reset the clock on the same
// silent failure, and the user is better served by the warning.
func repairDoomedSchedule(view settingsViewData, info scheduler.Info) settingsViewData {
	if !scheduleIsDoomed(info) {
		return view
	}

	exePath, err := exeForScheduleFunc()
	if err == nil && scheduler.CheckExecutableStable(exePath) == nil {
		if enableErr := scheduleEnableFunc(exePath, view.ScheduleTime); enableErr == nil {
			view.ScheduleNotice = fmt.Sprintf(
				"Your daily sync pointed at a program that would not have kept working (%s), so it has been repaired to run this copy (%s) at %s. Nothing else changed.",
				info.ExecutablePath, exePath, view.ScheduleTime)
			return view
		}
	}

	view.ScheduleError = fmt.Sprintf(
		"Your daily sync is registered but points at a program that will not keep working (%s), so it is not running reliably. "+
			"Install opal-downloader somewhere permanent and enable the schedule below from there to repair it.",
		info.ExecutablePath)
	return view
}

// applyScheduleRegistration registers or removes the Windows Task Scheduler
// task. Opt-in throughout: nothing here is ever reached except by someone
// submitting the form, and no other code path turns scheduling on.
//
// Extracted from what used to be the Settings page's POST handler when
// automatic sync moved onto its own page (see schedule_page.go) - the
// registration logic itself is unchanged.
func applyScheduleRegistration(enable bool, at string) error {
	if !enable {
		return scheduleDisableFunc()
	}
	if err := scheduler.ValidateTime(at); err != nil {
		return err
	}
	exePath, err := exeForScheduleFunc()
	if err != nil {
		return fmt.Errorf("resolving this program's own executable path: %w", err)
	}
	// Refuse rather than register a task that will silently stop working -
	// see ErrEphemeralExecutable's doc comment.
	if err := scheduler.CheckExecutableStable(exePath); err != nil {
		return err
	}
	return scheduleEnableFunc(exePath, at)
}

// saveNotifyPreference persists just the "tell me if it failed" checkbox.
//
// It reads the config, changes one field and writes it back, rather than
// going through the settings form's parser: that parser rebuilds the course
// list and every folder mapping from submitted form rows, so handing it a
// form that contains none of them would wipe all of it. This page has no
// business touching any of those settings.
func saveNotifyPreference(configPath string, notify bool) error {
	loaded, err := config.Load(configPath)
	if err != nil {
		// No config yet means nothing to attach the preference to. The page
		// already tells the user to set up first; silently creating a config
		// from a notification checkbox would be worse than doing nothing.
		return nil
	}
	if loaded.App.NotifyOnScheduledFailure == notify {
		return nil
	}
	loaded.App.NotifyOnScheduledFailure = notify
	return config.Save(configPath, loaded)
}
