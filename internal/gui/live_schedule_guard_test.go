package gui

// The one piece of scheduling that CAN safely be verified against the real
// Windows Task Scheduler.
//
// WHY ONLY THIS PIECE
// -------------------
// scheduler.TaskName is a single global constant, so every registration this
// product makes lives under one name. On a machine where the user has a real
// daily sync registered - the maintainer's does - a real enable overwrites it
// with whatever binary is running (a test binary, deleted moments later) and a
// real disable deletes it outright. There is no guard on the disable path.
//
// What is safe, and worth checking, is the refusal. handleScheduleAction calls
// scheduler.CheckExecutableStable BEFORE scheduler.Enable, precisely so a
// binary that will not still be there tomorrow never becomes a scheduled job
// (see the #122 entry in docs/BACKLOG.md). A `go test` binary lives in the
// temp directory, so it is exactly that case - which means this test drives
// the real, unstubbed code path against the real Task Scheduler and the
// correct outcome is that nothing is written at all.
//
// The test proves that rather than assuming it: it reads the real
// registration before and after and fails if anything moved.
//
//	OPAL_GUI_LIVE_SCHEDULE=1 go test ./internal/gui/ -run TestSchedulingRefusesToRegisterADisposableBinaryForReal -v

import (
	"errors"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/scheduler"
)

func TestSchedulingRefusesToRegisterADisposableBinaryForReal(t *testing.T) {
	if os.Getenv("OPAL_GUI_LIVE_SCHEDULE") == "" {
		t.Skip("set OPAL_GUI_LIVE_SCHEDULE=1 to check the refusal against the real Task Scheduler")
	}

	// Deliberately NOT stubbed: the real scheduler, the real os.Executable.
	before, beforeErr := scheduler.Status()
	if beforeErr != nil && errors.Is(beforeErr, scheduler.ErrUnsupported) {
		t.Skip("Task Scheduler not supported on this platform")
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if stableErr := scheduler.CheckExecutableStable(exe); stableErr == nil {
		t.Skipf("this test binary (%s) does not look disposable, so there is no refusal to observe", exe)
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yaml := "download_path: " + filepath.ToSlash(filepath.Join(dir, "downloads")) + "\n" +
		"courses:\n  - Analysis I\nsync: true\n" +
		"opal_url: https://bildungsportal.sachsen.de/opal/\n" +
		"session_state_file: " + filepath.ToSlash(filepath.Join(dir, "state.json")) + "\n"
	if err := os.WriteFile(configPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	srv := &server{configPath: configPath, buildVersion: "test", feedback: &feedbackState{}}
	ts := httptest.NewServer(newMux(srv, configPath))
	defer ts.Close()

	form := url.Values{}
	form.Set("schedule_enabled", "on")
	form.Set("schedule_time", "23:59")

	resp, err := ts.Client().PostForm(ts.URL+"/settings/schedule", form)
	if err != nil {
		t.Fatalf("POST /settings/schedule: %v", err)
	}
	defer resp.Body.Close()

	body := make([]byte, 0, 64*1024)
	buf := make([]byte, 8192)
	for {
		n, readErr := resp.Body.Read(buf)
		body = append(body, buf[:n]...)
		if readErr != nil {
			break
		}
	}
	page := string(body)

	// It must refuse, and it must say why in terms the user can act on.
	if !strings.Contains(page, "permanent location") {
		t.Fatalf("expected a refusal explaining the binary is disposable; page was:\n%s", page)
	}

	after, afterErr := scheduler.Status()
	if afterErr != nil {
		t.Fatalf("re-reading the registration: %v", afterErr)
	}

	// The whole point: a refusal writes nothing.
	if after.Registered != before.Registered {
		t.Fatalf("the refusal changed whether a task is registered: %v -> %v", before.Registered, after.Registered)
	}
	if after.Time != before.Time {
		t.Fatalf("the refusal moved the scheduled time: %q -> %q", before.Time, after.Time)
	}
	if after.ExecutablePath != before.ExecutablePath {
		t.Fatalf("the refusal re-pointed the task: %q -> %q", before.ExecutablePath, after.ExecutablePath)
	}
	t.Logf("real Task Scheduler untouched: registered=%v time=%q exe=%q",
		after.Registered, after.Time, after.ExecutablePath)
}
