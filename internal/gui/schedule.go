package gui

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

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

// handleScheduleAction implements the Settings page's "Enable daily
// automatic sync" toggle: POST-only, opt-in (see this task's spec - the
// toggle starts unchecked/off, never flipped on by init/setup or any other
// code path). Checking the box and submitting calls scheduler.Enable
// (registering/updating the Windows Task Scheduler task); unchecking it and
// submitting calls scheduler.Disable (removing it). Either way, the whole
// Settings page is re-rendered afterward so the toggle's new (real,
// re-queried) state is immediately visible - same pattern as the main
// settings form's own POST handler.
func handleScheduleAction(configPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = r.ParseForm()

		enable := r.FormValue("schedule_enabled") == "on"
		timeArg := strings.TrimSpace(r.FormValue("schedule_time"))
		if timeArg == "" {
			timeArg = scheduler.DefaultTime
		}

		var actionErr error
		if enable {
			if err := scheduler.ValidateTime(timeArg); err != nil {
				actionErr = err
			} else if exePath, err := exeForScheduleFunc(); err != nil {
				actionErr = fmt.Errorf("resolving this program's own executable path: %w", err)
			} else if err := scheduler.CheckExecutableStable(exePath); err != nil {
				// Refuse rather than register a task that will silently stop
				// working - see ErrEphemeralExecutable's doc comment.
				actionErr = err
			} else {
				actionErr = scheduleEnableFunc(exePath, timeArg)
			}
		} else {
			actionErr = scheduleDisableFunc()
		}

		view := applyScheduleStatus(loadSettingsViewData(configPath))
		if actionErr != nil {
			view.ScheduleError = actionErr.Error()
		} else {
			view.ScheduleSaved = true
		}
		renderSettings(w, view)
	}
}
