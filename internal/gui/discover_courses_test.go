package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverCoursesRejectsGet(t *testing.T) {
	rec := httptest.NewRecorder()
	handleDiscoverCourses("whatever.yaml")(rec, httptest.NewRequest(http.MethodGet, "/settings/discover-courses", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for GET, got %d", rec.Code)
	}
}

// An unreadable config must come back as a readable JSON error with HTTP
// 200, not a transport-level failure: the page renders j.error inline, and
// during setup "things aren't configured yet" is a normal state rather than
// a fault.
func TestDiscoverCoursesReportsConfigErrorAsJSON(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.yaml")
	rec := httptest.NewRecorder()
	handleDiscoverCourses(missing)(rec, httptest.NewRequest(http.MethodPost, "/settings/discover-courses", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 so the page can render the message, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected a JSON content type, got %q", ct)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response was not valid JSON: %v (body %q)", err, rec.Body.String())
	}
	msg, ok := payload["error"].(string)
	if !ok || msg == "" {
		t.Fatalf("expected a non-empty error field, got %v", payload)
	}
	if _, hasCourses := payload["courses"]; hasCourses {
		t.Fatal("an error response must not also carry a courses list")
	}
}

// The settings page must offer the picker and keep the manual escape hatch:
// a course discovery misses still has to be addable by hand.
// The picker is hidden behind "Sync all courses", which is ticked by default
// on a first run - so the whole course-selection feature is invisible to
// anyone who doesn't think to untick a box. Found by walking the first run in
// a browser (2026-07-26); the maintainer's answer was to explain it rather
// than change the default.
func TestSettingsPageExplainsThatUntickingRevealsTheCoursePicker(t *testing.T) {
	dir := t.TempDir()
	rec := httptest.NewRecorder()
	handleSettings(filepath.Join(dir, "does-not-exist.yaml"))(
		rec, httptest.NewRequest(http.MethodGet, "/settings", nil))

	body := rec.Body.String()
	if !strings.Contains(body, "Untick it to pick specific") {
		t.Fatalf("nothing tells the user that unticking reveals the picker, body was:\n%s", body)
	}
	// Saying "untick to choose" is only useful if it also says why the list
	// isn't there in the first place.
	if !strings.Contains(body, "hidden until you untick it") {
		t.Fatalf("the hidden course list is not explained, body was:\n%s", body)
	}
}

// Clicking "Set up opal-downloader" lands a stranger on a form with five
// sections - browser, folders, subfolder rules, notifications, scheduling -
// almost none of which needs a decision on day one. Seen by screenshotting the
// real page (2026-07-26); without a word saying so, the only way to learn that
// one field matters is to read all of it.
func TestSettingsPageTellsAFirstTimerWhatActuallyMatters(t *testing.T) {
	dir := t.TempDir()
	rec := httptest.NewRecorder()
	handleSettings(filepath.Join(dir, "does-not-exist.yaml"))(
		rec, httptest.NewRequest(http.MethodGet, "/settings", nil))

	body := rec.Body.String()
	if !strings.Contains(body, "Only one field needs you") {
		t.Fatalf("no first-run guidance on the settings form, body was:\n%s", body)
	}

	// And it must not linger once the tool is configured - it would be a lie
	// on every later visit, when the other settings are exactly why you came.
	configPath := filepath.Join(dir, "config.yaml")
	yaml := "download_path: ./downloads\ncourses:\n  - Analysis\nsync: true\n" +
		"opal_url: https://bildungsportal.sachsen.de/opal/\nsession_state_file: ./state.json\n"
	if err := os.WriteFile(configPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	rec = httptest.NewRecorder()
	handleSettings(configPath)(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	if strings.Contains(rec.Body.String(), "Only one field needs you") {
		t.Fatal("the first-run guidance is still shown once a config exists")
	}
}

func TestSettingsPageOffersCoursePickerAndManualEntry(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("download_path: ./downloads\ncourses:\n  - Analysis\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	rec := httptest.NewRecorder()
	handleSettings(configPath)(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	body := rec.Body.String()

	for _, want := range []string{
		`id="find-courses-btn"`,
		`id="discovered-courses"`,
		`/settings/discover-courses`,
		`id="add-course-row"`, // manual entry must survive
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("settings page is missing %q", want)
		}
	}
}
