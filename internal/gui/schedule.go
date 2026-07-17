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
