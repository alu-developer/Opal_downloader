package gui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/scheduler"
)

// TestMain overrides the schedule package-level fakes for every test in
// this package (not just schedule_test.go's own): handleSettings' GET/POST
// paths call applyScheduleStatus on every render (see settings.go), so
// without this default fake, every existing Settings-page test elsewhere in
// this package would silently shell out to a real schtasks.exe and depend
// on whatever scheduled-task state happens to exist on the machine running
// `go test`. Individual tests below save/restore these vars around their
// own fakes.
func TestMain(m *testing.M) {
	scheduleStatusFunc = func() (scheduler.Info, error) { return scheduler.Info{}, nil }
	os.Exit(m.Run())
}

func withScheduleFakes(t *testing.T, status func() (scheduler.Info, error), enable func(string, string) error, disable func() error, exe func() (string, error)) {
	t.Helper()
	origStatus, origEnable, origDisable, origExe := scheduleStatusFunc, scheduleEnableFunc, scheduleDisableFunc, exeForScheduleFunc
	if status != nil {
		scheduleStatusFunc = status
	}
	if enable != nil {
		scheduleEnableFunc = enable
	}
	if disable != nil {
		scheduleDisableFunc = disable
	}
	if exe != nil {
		exeForScheduleFunc = exe
	}
	t.Cleanup(func() {
		scheduleStatusFunc = origStatus
		scheduleEnableFunc = origEnable
		scheduleDisableFunc = origDisable
		exeForScheduleFunc = origExe
	})
}

func TestApplyScheduleStatusUnsupported(t *testing.T) {
	withScheduleFakes(t, func() (scheduler.Info, error) { return scheduler.Info{}, scheduler.ErrUnsupported }, nil, nil, nil)

	view := applyScheduleStatus(settingsViewData{})
	if view.ScheduleSupported {
		t.Fatalf("expected ScheduleSupported=false when scheduler.Status returns ErrUnsupported, got %+v", view)
	}
}

func TestApplyScheduleStatusRegistered(t *testing.T) {
	withScheduleFakes(t, func() (scheduler.Info, error) { return scheduler.Info{Registered: true, Time: "07:30"}, nil }, nil, nil, nil)

	view := applyScheduleStatus(settingsViewData{})
	if !view.ScheduleSupported || !view.ScheduleEnabled || view.ScheduleTime != "07:30" {
		t.Fatalf("expected Supported=true Enabled=true Time=07:30, got %+v", view)
	}
}

func TestApplyScheduleStatusNotRegisteredDefaultsTime(t *testing.T) {
	withScheduleFakes(t, func() (scheduler.Info, error) { return scheduler.Info{Registered: false}, nil }, nil, nil, nil)

	view := applyScheduleStatus(settingsViewData{})
	if view.ScheduleEnabled {
		t.Fatalf("expected ScheduleEnabled=false, got true")
	}
	if view.ScheduleTime != scheduler.DefaultTime {
		t.Fatalf("expected ScheduleTime to default to %q, got %q", scheduler.DefaultTime, view.ScheduleTime)
	}
}

func TestApplyScheduleStatusOtherErrorSurfaced(t *testing.T) {
	withScheduleFakes(t, func() (scheduler.Info, error) { return scheduler.Info{}, os.ErrPermission }, nil, nil, nil)

	view := applyScheduleStatus(settingsViewData{})
	if !view.ScheduleSupported {
		t.Fatalf("expected ScheduleSupported=true even on a non-ErrUnsupported error, got %+v", view)
	}
	if view.ScheduleError == "" {
		t.Fatalf("expected ScheduleError to be set, got %+v", view)
	}
}

// ephemeralExe is a path shaped like Go's build cache - what os.Executable()
// resolves to during a `go run .` session, and what the maintainer's own
// scheduled task was actually found pointing at.
const ephemeralExe = `C:\Users\someone\AppData\Local\go-build\1e\1ec79afd-d\opal-downloader.exe`

const stableExe = `C:\Program Files\opal-downloader\opal-downloader.exe`

// A task pointing into the build cache still resolves today and looks healthy
// - which is exactly why it has to be repaired now rather than reported when
// the file finally disappears.
func TestApplyScheduleStatusRepairsEphemeralExecutable(t *testing.T) {
	var gotExe, gotTime string
	withScheduleFakes(t,
		func() (scheduler.Info, error) {
			return scheduler.Info{Registered: true, Time: "07:30", ExecutablePath: ephemeralExe}, nil
		},
		func(exePath, hhmm string) error { gotExe, gotTime = exePath, hhmm; return nil },
		nil,
		func() (string, error) { return stableExe, nil },
	)

	view := applyScheduleStatus(settingsViewData{})

	if gotExe != stableExe {
		t.Errorf("expected the task to be re-registered against %q, got %q", stableExe, gotExe)
	}
	if gotTime != "07:30" {
		t.Errorf("repair must keep the user's trigger time, got %q", gotTime)
	}
	if view.ScheduleNotice == "" {
		t.Error("a repair the user did not ask for has to be reported")
	}
	if view.ScheduleError != "" {
		t.Errorf("nothing is wrong after a successful repair, but got error %q", view.ScheduleError)
	}
	if !view.ScheduleEnabled {
		t.Error("the schedule is still enabled after being repaired")
	}
}

func TestApplyScheduleStatusRepairsMissingExecutable(t *testing.T) {
	var enableCalled bool
	withScheduleFakes(t,
		func() (scheduler.Info, error) {
			return scheduler.Info{Registered: true, Time: "06:00", ExecutablePath: stableExe, ExecutableMissing: true}, nil
		},
		func(string, string) error { enableCalled = true; return nil },
		nil,
		func() (string, error) { return stableExe, nil },
	)

	view := applyScheduleStatus(settingsViewData{})

	if !enableCalled {
		t.Fatal("expected a registration whose binary is gone to be repaired")
	}
	if view.ScheduleNotice == "" {
		t.Error("expected the repair to be reported")
	}
}

// Repairing a doomed path with another doomed path would just reset the clock
// on the same silent failure, so the warning is the honest answer.
func TestApplyScheduleStatusWarnsInsteadOfRepairingFromAnotherEphemeralBinary(t *testing.T) {
	var enableCalled bool
	withScheduleFakes(t,
		func() (scheduler.Info, error) {
			return scheduler.Info{Registered: true, Time: "07:30", ExecutablePath: ephemeralExe}, nil
		},
		func(string, string) error { enableCalled = true; return nil },
		nil,
		func() (string, error) { return ephemeralExe, nil },
	)

	view := applyScheduleStatus(settingsViewData{})

	if enableCalled {
		t.Fatal("must not re-register against a binary that is itself disposable")
	}
	if view.ScheduleError == "" {
		t.Error("expected the unrepairable registration to be reported as an error")
	}
	if view.ScheduleNotice != "" {
		t.Errorf("nothing was repaired, so nothing should claim it was: %q", view.ScheduleNotice)
	}
}

func TestApplyScheduleStatusFallsBackToWarningWhenRepairFails(t *testing.T) {
	withScheduleFakes(t,
		func() (scheduler.Info, error) {
			return scheduler.Info{Registered: true, Time: "07:30", ExecutablePath: ephemeralExe}, nil
		},
		func(string, string) error { return os.ErrPermission },
		nil,
		func() (string, error) { return stableExe, nil },
	)

	view := applyScheduleStatus(settingsViewData{})

	if view.ScheduleError == "" {
		t.Error("a failed repair must still warn about the broken registration")
	}
	if view.ScheduleNotice != "" {
		t.Errorf("a failed repair must not report success: %q", view.ScheduleNotice)
	}
}

// The repair has to be a no-op for a healthy task, or every settings page
// load would rewrite the user's Task Scheduler entry for no reason.
func TestApplyScheduleStatusLeavesHealthyRegistrationAlone(t *testing.T) {
	var enableCalled bool
	withScheduleFakes(t,
		func() (scheduler.Info, error) {
			return scheduler.Info{Registered: true, Time: "07:30", ExecutablePath: stableExe}, nil
		},
		func(string, string) error { enableCalled = true; return nil },
		nil,
		func() (string, error) { return stableExe, nil },
	)

	view := applyScheduleStatus(settingsViewData{})

	if enableCalled {
		t.Fatal("a healthy registration must not be rewritten")
	}
	if view.ScheduleError != "" || view.ScheduleNotice != "" {
		t.Errorf("expected a quiet page for a healthy schedule, got error %q notice %q", view.ScheduleError, view.ScheduleNotice)
	}
}

func newScheduleTestConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yaml := "download_path: ./downloads\ncourses:\n  - \"*\"\nsync: true\n"
	if err := os.WriteFile(configPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	return configPath
}

func TestHandleScheduleActionEnableCallsEnableWithFormTime(t *testing.T) {
	var gotExe, gotTime string
	var enableCalled bool
	withScheduleFakes(t,
		func() (scheduler.Info, error) { return scheduler.Info{Registered: true, Time: gotTime}, nil },
		func(exePath, hhmm string) error {
			enableCalled = true
			gotExe = exePath
			gotTime = hhmm
			return nil
		},
		nil,
		func() (string, error) { return `C:\fake\opal-downloader.exe`, nil },
	)

	configPath := newScheduleTestConfig(t)
	form := url.Values{}
	form.Set("schedule_enabled", "on")
	form.Set("schedule_time", "07:15")

	req := httptest.NewRequest(http.MethodPost, "/schedule", nil)
	req.PostForm = form
	req.Form = form
	rec := httptest.NewRecorder()

	handleSchedulePage(configPath)(rec, req)

	if !enableCalled {
		t.Fatal("expected scheduleEnableFunc to be called when schedule_enabled=on is submitted")
	}
	if gotExe != `C:\fake\opal-downloader.exe` || gotTime != "07:15" {
		t.Fatalf("expected Enable called with (fake exe, 07:15), got (%q, %q)", gotExe, gotTime)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Saved.") {
		t.Fatalf("expected a success message in the response, got: %s", rec.Body.String())
	}
}

func TestHandleScheduleActionDisableCallsDisable(t *testing.T) {
	var disableCalled bool
	withScheduleFakes(t,
		func() (scheduler.Info, error) { return scheduler.Info{Registered: false}, nil },
		nil,
		func() error { disableCalled = true; return nil },
		nil,
	)

	configPath := newScheduleTestConfig(t)
	form := url.Values{} // schedule_enabled omitted = unchecked
	req := httptest.NewRequest(http.MethodPost, "/schedule", nil)
	req.PostForm = form
	req.Form = form
	rec := httptest.NewRecorder()

	handleSchedulePage(configPath)(rec, req)

	if !disableCalled {
		t.Fatal("expected scheduleDisableFunc to be called when schedule_enabled is not submitted")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleScheduleActionInvalidTimeDoesNotCallEnable(t *testing.T) {
	var enableCalled bool
	withScheduleFakes(t,
		func() (scheduler.Info, error) { return scheduler.Info{}, nil },
		func(string, string) error { enableCalled = true; return nil },
		nil,
		func() (string, error) { return `C:\fake\opal-downloader.exe`, nil },
	)

	configPath := newScheduleTestConfig(t)
	form := url.Values{}
	form.Set("schedule_enabled", "on")
	form.Set("schedule_time", "not-a-time")
	req := httptest.NewRequest(http.MethodPost, "/schedule", nil)
	req.PostForm = form
	req.Form = form
	rec := httptest.NewRecorder()

	handleSchedulePage(configPath)(rec, req)

	if enableCalled {
		t.Fatal("expected scheduleEnableFunc NOT to be called for an invalid time")
	}
	if !strings.Contains(rec.Body.String(), "Could not update") {
		t.Fatalf("expected an error message in the response, got: %s", rec.Body.String())
	}
}

func TestSchedulePageRendersOnGet(t *testing.T) {
	withScheduleFakes(t,
		func() (scheduler.Info, error) { return scheduler.Info{Registered: true, Time: "06:00"}, nil },
		nil, nil,
		func() (string, error) { return `C:\fake\opal-downloader.exe`, nil },
	)

	configPath := newScheduleTestConfig(t)
	req := httptest.NewRequest(http.MethodGet, "/schedule", nil)
	rec := httptest.NewRecorder()

	handleSchedulePage(configPath)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for a GET of the schedule page, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Automatic sync") {
		t.Fatalf("the schedule page did not render its own heading: %s", rec.Body.String())
	}
}

func TestSchedulePageRejectsOtherMethods(t *testing.T) {
	configPath := newScheduleTestConfig(t)
	req := httptest.NewRequest(http.MethodDelete, "/schedule", nil)
	rec := httptest.NewRecorder()

	handleSchedulePage(configPath)(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for DELETE, got %d", rec.Code)
	}
}

// A daily task registered against a missing config.yaml does not fail once, it
// fails every single morning - silently, unless the failure notification is on,
// in which case it becomes a daily toast about a job the user cannot tell they
// set up wrong. The page already says "set up first"; this is what makes that
// more than a suggestion.
func TestEnablingAScheduleWithNothingToSyncIsRefused(t *testing.T) {
	var enableCalled bool
	withScheduleFakes(t,
		func() (scheduler.Info, error) { return scheduler.Info{}, nil },
		func(string, string) error { enableCalled = true; return nil },
		nil,
		func() (string, error) { return `C:\fake\opal-downloader.exe`, nil },
	)

	// A path with no config.yaml at it - a first run that reached this page
	// before finishing Settings.
	missing := filepath.Join(t.TempDir(), "config.yaml")

	form := url.Values{}
	form.Set("schedule_enabled", "on")
	form.Set("schedule_time", "07:15")
	req := httptest.NewRequest(http.MethodPost, "/schedule", nil)
	req.PostForm = form
	req.Form = form
	rec := httptest.NewRecorder()

	handleSchedulePage(missing)(rec, req)

	if enableCalled {
		t.Fatal("a daily sync was registered against a config that does not exist; it would fail every morning")
	}
	if !strings.Contains(rec.Body.String(), "nothing to sync yet") {
		t.Errorf("the refusal does not explain what to do about it: %s", rec.Body.String())
	}
}

// And the refusal must not block the normal case.
func TestEnablingAScheduleWithAConfigStillWorks(t *testing.T) {
	var enableCalled bool
	withScheduleFakes(t,
		func() (scheduler.Info, error) { return scheduler.Info{}, nil },
		func(string, string) error { enableCalled = true; return nil },
		nil,
		func() (string, error) { return `C:\fake\opal-downloader.exe`, nil },
	)

	configPath := newScheduleTestConfig(t)
	form := url.Values{}
	form.Set("schedule_enabled", "on")
	form.Set("schedule_time", "07:15")
	req := httptest.NewRequest(http.MethodPost, "/schedule", nil)
	req.PostForm = form
	req.Form = form
	rec := httptest.NewRecorder()

	handleSchedulePage(configPath)(rec, req)

	if !enableCalled {
		t.Fatal("the guard blocked a perfectly normal enable")
	}
}

// Turning it OFF must never be blocked by a missing config. Someone whose
// config went away still has a registered task running every morning, and
// refusing to let them remove it would strand them with it.
func TestDisablingIsNeverBlockedByAMissingConfig(t *testing.T) {
	var disableCalled bool
	withScheduleFakes(t,
		func() (scheduler.Info, error) { return scheduler.Info{Registered: true}, nil },
		nil,
		func() error { disableCalled = true; return nil },
		nil,
	)

	missing := filepath.Join(t.TempDir(), "config.yaml")
	req := httptest.NewRequest(http.MethodPost, "/schedule", nil)
	req.PostForm = url.Values{}
	req.Form = url.Values{}
	rec := httptest.NewRecorder()

	handleSchedulePage(missing)(rec, req)

	if !disableCalled {
		t.Fatal("a missing config stopped the user removing a schedule they already have")
	}
}
