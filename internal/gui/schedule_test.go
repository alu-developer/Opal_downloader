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

	req := httptest.NewRequest(http.MethodPost, "/settings/schedule", nil)
	req.PostForm = form
	req.Form = form
	rec := httptest.NewRecorder()

	handleScheduleAction(configPath)(rec, req)

	if !enableCalled {
		t.Fatal("expected scheduleEnableFunc to be called when schedule_enabled=on is submitted")
	}
	if gotExe != `C:\fake\opal-downloader.exe` || gotTime != "07:15" {
		t.Fatalf("expected Enable called with (fake exe, 07:15), got (%q, %q)", gotExe, gotTime)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Schedule updated") {
		t.Fatalf("expected success message in response, got: %s", rec.Body.String())
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
	req := httptest.NewRequest(http.MethodPost, "/settings/schedule", nil)
	req.PostForm = form
	req.Form = form
	rec := httptest.NewRecorder()

	handleScheduleAction(configPath)(rec, req)

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
	req := httptest.NewRequest(http.MethodPost, "/settings/schedule", nil)
	req.PostForm = form
	req.Form = form
	rec := httptest.NewRecorder()

	handleScheduleAction(configPath)(rec, req)

	if enableCalled {
		t.Fatal("expected scheduleEnableFunc NOT to be called for an invalid time")
	}
	if !strings.Contains(rec.Body.String(), "Could not update schedule") {
		t.Fatalf("expected an error message in the response, got: %s", rec.Body.String())
	}
}

func TestHandleScheduleActionRejectsNonPost(t *testing.T) {
	configPath := newScheduleTestConfig(t)
	req := httptest.NewRequest(http.MethodGet, "/settings/schedule", nil)
	rec := httptest.NewRecorder()

	handleScheduleAction(configPath)(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for GET, got %d", rec.Code)
	}
}
