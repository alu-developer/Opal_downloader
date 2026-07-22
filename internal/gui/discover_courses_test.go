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
