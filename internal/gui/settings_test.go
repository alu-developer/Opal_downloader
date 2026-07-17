package gui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
)

// TestHandleSettingsPostPreservesFieldsWithoutFormInputs is a regression test
// for a data-loss bug: saving the GUI Settings page used to build a fresh
// config.App{} from only the fields the form submits, silently deleting any
// field the Settings form has no input for from config.yaml if it had been
// set by hand. use_section_subfolders/section_folder_names/
// subfolder_destinations have since gained real form inputs (see
// TestHandleSettingsPostRoundTripsSubfolderFields) so they're no longer part
// of what this test covers - it now exercises the fields that still have no
// form input (download_concurrency, course_concurrency), submitting a form
// that leaves them untouched and asserting they survive the save.
func TestHandleSettingsPostPreservesFieldsWithoutFormInputs(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	initialYAML := `download_path: ./downloads
courses:
  - "*"
sync: true
default_course_folder: ""
opal_url: https://bildungsportal.sachsen.de/opal/
session_state_file: ./state.json
download_concurrency: 5
course_concurrency: 2
`
	if err := os.WriteFile(configPath, []byte(initialYAML), 0o644); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	before, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to load initial config: %v", err)
	}
	if before.App.DownloadConcurrency != 5 || before.App.CourseConcurrency != 2 {
		t.Fatalf("precondition failed: expected download_concurrency=5 course_concurrency=2, got %+v",
			before.App)
	}

	// Simulate the user submitting the Settings form as-is (values mirror
	// what loadedToViewData would have pre-filled from `before`), which is
	// the normal "just click Save" path that must not be destructive.
	form := url.Values{}
	form.Set("download_path", before.App.DownloadPath)
	form.Set("sync_all_courses", "on")
	form.Set("sync", "on")
	form.Set("default_course_folder", before.App.DefaultCourseFolder)

	req := httptest.NewRequest(http.MethodPost, "/settings", nil)
	req.PostForm = form
	req.Form = form
	rec := httptest.NewRecorder()

	handleSettings(configPath)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from settings POST, got %d: %s", rec.Code, rec.Body.String())
	}

	after, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config after save: %v", err)
	}

	if after.App.DownloadConcurrency != before.App.DownloadConcurrency {
		t.Errorf("download_concurrency was not preserved: before=%v after=%v",
			before.App.DownloadConcurrency, after.App.DownloadConcurrency)
	}
	if after.App.CourseConcurrency != before.App.CourseConcurrency {
		t.Errorf("course_concurrency was not preserved: before=%v after=%v",
			before.App.CourseConcurrency, after.App.CourseConcurrency)
	}

	// Sanity: fields the form does submit were actually applied.
	if after.App.DownloadPath != before.App.DownloadPath {
		t.Errorf("download_path should still be settable via the form: before=%q after=%q",
			before.App.DownloadPath, after.App.DownloadPath)
	}

	// The three subfolder fields have no value in the initial config and no
	// form input was submitted for them either, so they should still be
	// absent/empty/false after save (not previously-silently-dropped values
	// reappearing from nowhere).
	if after.App.UseSectionSubfolders {
		t.Errorf("expected use_section_subfolders to remain false, got true")
	}
	if len(after.App.SectionFolderNames) != 0 {
		t.Errorf("expected section_folder_names to remain empty, got %+v", after.App.SectionFolderNames)
	}
	if len(after.App.SubfolderDestinations) != 0 {
		t.Errorf("expected subfolder_destinations to remain empty, got %+v", after.App.SubfolderDestinations)
	}
}

// TestHandleSettingsPostRoundTripsSubfolderFields verifies the new GUI
// editors for use_section_subfolders, section_folder_names, and
// subfolder_destinations actually save what the user submits - the point of
// this task is making these fields editable, not just non-destructive.
func TestHandleSettingsPostRoundTripsSubfolderFields(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	initialYAML := `download_path: ./downloads
courses:
  - "*"
sync: true
opal_url: https://bildungsportal.sachsen.de/opal/
session_state_file: ./state.json
`
	if err := os.WriteFile(configPath, []byte(initialYAML), 0o644); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	before, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to load initial config: %v", err)
	}
	if before.App.UseSectionSubfolders {
		t.Fatalf("precondition failed: expected use_section_subfolders to start false")
	}

	form := url.Values{}
	form.Set("download_path", before.App.DownloadPath)
	form.Set("sync_all_courses", "on")
	form.Set("sync", "on")
	form.Set("use_section_subfolders", "on")
	form["section_folder_pattern[]"] = []string{"Vorlesung", "Uebung"}
	form["section_folder_folder[]"] = []string{"Lectures", "Exercises"}
	form["subfolder_dest_key[]"] = []string{"*Analysis*/*Vorlesung*"}
	form["subfolder_dest_path[]"] = []string{"D:/Uni/Analysis/Lectures"}

	req := httptest.NewRequest(http.MethodPost, "/settings", nil)
	req.PostForm = form
	req.Form = form
	rec := httptest.NewRecorder()

	handleSettings(configPath)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from settings POST, got %d: %s", rec.Code, rec.Body.String())
	}

	after, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config after save: %v", err)
	}

	if !after.App.UseSectionSubfolders {
		t.Errorf("expected use_section_subfolders to be saved as true")
	}
	wantSectionFolders := map[string]string{"Vorlesung": "Lectures", "Uebung": "Exercises"}
	if !reflect.DeepEqual(after.App.SectionFolderNames, wantSectionFolders) {
		t.Errorf("section_folder_names mismatch: got %+v, want %+v", after.App.SectionFolderNames, wantSectionFolders)
	}
	wantDestinations := map[string]string{"*Analysis*/*Vorlesung*": "D:/Uni/Analysis/Lectures"}
	if !reflect.DeepEqual(after.App.SubfolderDestinations, wantDestinations) {
		t.Errorf("subfolder_destinations mismatch: got %+v, want %+v", after.App.SubfolderDestinations, wantDestinations)
	}

	// Saving with use_section_subfolders on and these fields populated
	// should not produce a misconfiguration warning.
	if warnings := config.Warnings(after.App); len(warnings) != 0 {
		t.Errorf("expected no config warnings when use_section_subfolders is true, got %v", warnings)
	}
}

// TestHandleSettingsPostRoundTripsNotifyOnScheduledFailure verifies the
// "Notify me if a scheduled sync fails" checkbox (internal/notify) saves and
// re-renders correctly, and - critically, per this field's whole reason for
// being separate from the "Enable daily automatic sync" toggle - that
// submitting the main settings form without schedule_enabled does not
// affect it either way (it lives in config.yaml, not Task Scheduler state).
func TestHandleSettingsPostRoundTripsNotifyOnScheduledFailure(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	initialYAML := `download_path: ./downloads
courses:
  - "*"
sync: true
opal_url: https://bildungsportal.sachsen.de/opal/
session_state_file: ./state.json
`
	if err := os.WriteFile(configPath, []byte(initialYAML), 0o644); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	before, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to load initial config: %v", err)
	}
	if before.App.NotifyOnScheduledFailure {
		t.Fatalf("precondition failed: expected notify_on_scheduled_failure to start false")
	}

	form := url.Values{}
	form.Set("download_path", before.App.DownloadPath)
	form.Set("sync_all_courses", "on")
	form.Set("sync", "on")
	form.Set("notify_on_scheduled_failure", "on")

	req := httptest.NewRequest(http.MethodPost, "/settings", nil)
	req.PostForm = form
	req.Form = form
	rec := httptest.NewRecorder()

	handleSettings(configPath)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from settings POST, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `id="notify_on_scheduled_failure" name="notify_on_scheduled_failure" checked`) {
		t.Errorf("expected the re-rendered page to show the checkbox checked, got body:\n%s", rec.Body.String())
	}

	after, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config after save: %v", err)
	}
	if !after.App.NotifyOnScheduledFailure {
		t.Errorf("expected notify_on_scheduled_failure to be saved as true")
	}
}

// TestHandleSettingsPostUncheckingSubfoldersProducesWarning verifies that
// saving with use_section_subfolders unchecked while section_folder_names/
// subfolder_destinations rows are still present surfaces the misconfiguration
// warning inline on the re-rendered page (the GUI counterpart of
// config.Warnings, which the CLI surfaces separately on config load).
func TestHandleSettingsPostUncheckingSubfoldersProducesWarning(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	initialYAML := `download_path: ./downloads
courses:
  - "*"
sync: true
opal_url: https://bildungsportal.sachsen.de/opal/
session_state_file: ./state.json
`
	if err := os.WriteFile(configPath, []byte(initialYAML), 0o644); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	form := url.Values{}
	form.Set("download_path", "./downloads")
	form.Set("sync_all_courses", "on")
	form.Set("sync", "on")
	// use_section_subfolders intentionally omitted (unchecked checkbox).
	form["section_folder_pattern[]"] = []string{"Vorlesung"}
	form["section_folder_folder[]"] = []string{"Lectures"}

	req := httptest.NewRequest(http.MethodPost, "/settings", nil)
	req.PostForm = form
	req.Form = form
	rec := httptest.NewRecorder()

	handleSettings(configPath)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from settings POST, got %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, "section_folder_names is set but use_section_subfolders is false") {
		t.Errorf("expected rendered page to contain the misconfiguration warning, got body:\n%s", body)
	}

	after, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config after save: %v", err)
	}
	warnings := config.Warnings(after.App)
	if len(warnings) != 1 {
		t.Fatalf("expected exactly one config warning after save, got %v", warnings)
	}
}

// TestHandleSettingsPostMergedCourseTableSingleCourse is a regression test
// for the merged "Courses" table (course name + optional folder override in
// one row) replacing the old separate Courses textarea and Course folder
// rules table. Entering one course name with one folder override, with
// "sync all courses" unchecked, must produce the same config.yaml shape the
// old two-field form produced: courses: [name] and
// course_folders: {name: folder}.
func TestHandleSettingsPostMergedCourseTableSingleCourse(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	form := url.Values{}
	form.Set("download_path", "./downloads")
	form.Set("sync", "on")
	// sync_all_courses intentionally omitted (unchecked): one specific
	// course only.
	form["course_row_name[]"] = []string{"Analysis I"}
	form["course_row_folder[]"] = []string{"Mathematik/Analysis"}

	req := httptest.NewRequest(http.MethodPost, "/settings", nil)
	req.PostForm = form
	req.Form = form
	rec := httptest.NewRecorder()

	handleSettings(configPath)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from settings POST, got %d: %s", rec.Code, rec.Body.String())
	}

	after, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config after save: %v", err)
	}

	wantCourses := []string{"Analysis I"}
	if !reflect.DeepEqual(after.App.Courses, wantCourses) {
		t.Errorf("courses mismatch: got %+v, want %+v", after.App.Courses, wantCourses)
	}
	wantFolders := map[string]string{"Analysis I": "Mathematik/Analysis"}
	if !reflect.DeepEqual(after.App.CourseFolders, wantFolders) {
		t.Errorf("course_folders mismatch: got %+v, want %+v", after.App.CourseFolders, wantFolders)
	}

	// OpalURL/SessionStateFile are no longer form fields - saving must
	// always fall back to the package defaults rather than leaving the
	// credentials empty/invalid.
	if after.Credentials.URL != config.DefaultOPALURL {
		t.Errorf("expected opal_url to default to %q, got %q", config.DefaultOPALURL, after.Credentials.URL)
	}
}

// TestHandleSettingsPostSyncAllCoursesIgnoresCourseNames verifies that
// checking "sync all courses" produces courses: ["*"] regardless of what
// (if anything) is in the merged course table's name column, while still
// preserving any folder overrides from that table.
func TestHandleSettingsPostSyncAllCoursesIgnoresCourseNames(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	form := url.Values{}
	form.Set("download_path", "./downloads")
	form.Set("sync", "on")
	form.Set("sync_all_courses", "on")
	form["course_row_name[]"] = []string{"Analysis I"}
	form["course_row_folder[]"] = []string{"Mathematik/Analysis"}

	req := httptest.NewRequest(http.MethodPost, "/settings", nil)
	req.PostForm = form
	req.Form = form
	rec := httptest.NewRecorder()

	handleSettings(configPath)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from settings POST, got %d: %s", rec.Code, rec.Body.String())
	}

	after, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config after save: %v", err)
	}

	if !reflect.DeepEqual(after.App.Courses, []string{"*"}) {
		t.Errorf("expected courses to be [\"*\"], got %+v", after.App.Courses)
	}
	wantFolders := map[string]string{"Analysis I": "Mathematik/Analysis"}
	if !reflect.DeepEqual(after.App.CourseFolders, wantFolders) {
		t.Errorf("course_folders mismatch: got %+v, want %+v", after.App.CourseFolders, wantFolders)
	}
}
