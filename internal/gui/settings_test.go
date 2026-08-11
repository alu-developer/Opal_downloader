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
	"github.com/alu-developer/opal-downloader/internal/scheduler"
	"github.com/alu-developer/opal-downloader/internal/syncer"
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
// The secondary buttons ("Browse...", "+ Add course", "Suggest folders",
// "+ Add rule") were unreadable: white text on a near-white fill. pageStyle's
// base `button` rule sets color:#fff for the blue primary buttons, and these
// class rules overrode only the background, so the inherited white text
// stayed. Found by screenshotting the page and looking at it (2026-07-26) -
// every assertion in this package passed the whole time, because the markup
// was never wrong.
//
// Any light-background button rule has to state its text colour explicitly.
func TestSettingsSecondaryButtonsSetTheirOwnTextColour(t *testing.T) {
	dir := t.TempDir()
	rec := httptest.NewRecorder()
	handleSettings(filepath.Join(dir, "config.yaml"))(
		rec, httptest.NewRequest(http.MethodGet, "/settings", nil))

	body := rec.Body.String()
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, ".") || !strings.Contains(trimmed, "{") {
			continue
		}
		// Only rules that repaint the background away from the primary blue;
		// those are the ones that strand the inherited white text.
		if !strings.Contains(trimmed, "background: #f5f5f5") {
			continue
		}
		if !strings.Contains(trimmed, "color:") {
			t.Fatalf("this button rule sets a light background but no text colour, "+
				"so it inherits color:#fff from pageStyle and renders white-on-white:\n  %s", trimmed)
		}
	}
}

// TestHandleSettingsFirstSaveKeepsDefaults is a regression test for the
// first-run path the Windows installer puts every new user on: setup.exe ->
// GUI -> Settings -> "Save settings", with no config.yaml in existence yet.
//
// loadSettingsViewData used to hand-build the App it renders in that case,
// listing three defaults (download path, courses, sync) and nothing else.
// Every other field therefore rendered as its zero value, and because Save
// writes every field it renders, the very first save wrote
// skip_enrollment_sections: false - which config.Load honours, since the
// key is a *bool where "absent" and "false" are different things. So a
// fresh install silently turned off a live-confirmed structural skip
// (config.DefaultSkipEnrollmentSections) for the user who never touched it.
//
// Found 2026-08-11 by installing a locally built opal-downloader-setup.exe
// and walking the first run, not by a failing test.
func TestHandleSettingsFirstSaveKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("precondition failed: config.yaml must not exist yet, got err=%v", err)
	}

	// What a first-run user submits: type a download path, click Save,
	// leave everything else at whatever the form came pre-filled with.
	form := url.Values{}
	form.Set("download_path", filepath.Join(dir, "downloads"))
	form.Set("sync_all_courses", "on")
	form.Set("sync", "on")

	req := httptest.NewRequest(http.MethodPost, "/settings", nil)
	req.PostForm = form
	req.Form = form
	rec := httptest.NewRecorder()

	handleSettings(configPath)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from first settings POST, got %d: %s", rec.Code, rec.Body.String())
	}

	after, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to load the config the first save wrote: %v", err)
	}

	if after.App.SkipEnrollmentSections != config.DefaultSkipEnrollmentSections {
		t.Errorf("first save flipped skip_enrollment_sections: got %v, want the default %v",
			after.App.SkipEnrollmentSections, config.DefaultSkipEnrollmentSections)
	}

	// The same class of bug for every other defaulted field: whatever the
	// first save writes must load back as the defaults a user would have
	// had if they had never opened the page at all.
	defaults := config.Defaults()
	if after.App.DownloadConcurrency != defaults.App.DownloadConcurrency {
		t.Errorf("download_concurrency: got %d, want default %d",
			after.App.DownloadConcurrency, defaults.App.DownloadConcurrency)
	}
	if after.App.CourseConcurrency != defaults.App.CourseConcurrency {
		t.Errorf("course_concurrency: got %d, want default %d",
			after.App.CourseConcurrency, defaults.App.CourseConcurrency)
	}
	if after.App.SectionConcurrency != defaults.App.SectionConcurrency {
		t.Errorf("section_concurrency: got %d, want default %d",
			after.App.SectionConcurrency, defaults.App.SectionConcurrency)
	}
	if after.Credentials.URL != defaults.Credentials.URL {
		t.Errorf("opal_url: got %q, want default %q", after.Credentials.URL, defaults.Credentials.URL)
	}
}

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

// The failure notification stopped being a preference on 2026-08-03 - it
// always fires. These two pin what that removal has to mean in the GUI:
// nothing on either page offers it as a choice, and neither page save
// resurrects the retired config key.
//
// This replaces a pair of tests that existed to protect the old checkbox
// from being silently cleared by an unrelated save (an unchecked box and an
// absent one are indistinguishable in a form post). That hazard is gone with
// the setting, but the "the schedule page must not touch settings it has no
// inputs for" half of it is still real, so it is kept below.
func TestNeitherPageOffersTheFailureNotificationAsAChoice(t *testing.T) {
	withScheduleFakes(t,
		func() (scheduler.Info, error) { return scheduler.Info{Registered: false}, nil },
		nil,
		func() error { return nil },
		func() (string, error) { return `C:\fake\opal-downloader.exe`, nil },
	)

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

	for _, page := range []struct {
		path    string
		handler http.HandlerFunc
	}{
		{"/settings", handleSettings(configPath)},
		{"/schedule", handleSchedulePage(configPath)},
	} {
		req := httptest.NewRequest(http.MethodGet, page.path, nil)
		rec := httptest.NewRecorder()
		page.handler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: expected 200, got %d", page.path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), `name="notify_on_scheduled_failure"`) {
			t.Errorf("GET %s still renders a notify_on_scheduled_failure input", page.path)
		}
	}
}

// The schedule page saves the schedule. It must not go anywhere near the
// course list or the folder mappings, which it has no inputs for - the whole
// reason it never went through parseSettingsForm.
func TestSchedulePageSaveLeavesCoursesAndFoldersAlone(t *testing.T) {
	withScheduleFakes(t,
		func() (scheduler.Info, error) { return scheduler.Info{Registered: false}, nil },
		nil,
		func() error { return nil },
		func() (string, error) { return `C:\fake\opal-downloader.exe`, nil },
	)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	initialYAML := `download_path: ./downloads
courses:
  - Analysis I
course_folders:
  Analysis I: Mathe/Analysis
sync: true
opal_url: https://bildungsportal.sachsen.de/opal/
session_state_file: ./state.json
`
	if err := os.WriteFile(configPath, []byte(initialYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/schedule", nil)
	req.PostForm = form
	req.Form = form
	rec := httptest.NewRecorder()

	handleSchedulePage(configPath)(rec, req)

	after, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config after save: %v", err)
	}
	if len(after.App.Courses) != 1 || after.App.Courses[0] != "Analysis I" {
		t.Errorf("saving from the schedule page changed the course list: %#v", after.App.Courses)
	}
	if after.App.CourseFolders["Analysis I"] != "Mathe/Analysis" {
		t.Errorf("saving from the schedule page changed the folder mappings: %#v", after.App.CourseFolders)
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

// TestHandleSettingsPostWarnsWhenTogglingSubfoldersWithExistingManifest
// covers the trigger for the whole manifest key-scheme failure mode: flipping
// "Organize downloads into a subfolder per OPAL section" re-keys every entry
// in an existing sync manifest, and used to do so through an unlabelled
// checkbox with no warning whatsoever.
func TestHandleSettingsPostWarnsWhenTogglingSubfoldersWithExistingManifest(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	downloadPath := filepath.Join(dir, "downloads")

	initialYAML := "download_path: " + filepath.ToSlash(downloadPath) + `
courses:
  - "*"
sync: true
opal_url: https://bildungsportal.sachsen.de/opal/
session_state_file: ./state.json
`
	if err := os.WriteFile(configPath, []byte(initialYAML), 0o644); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	// An already-populated sync history, written under the current (flat)
	// layout.
	manifest := &syncer.Manifest{
		Path: filepath.Join(downloadPath, syncer.ManifestFileName),
		Files: map[string]syncer.FileRecord{
			"Analysis I/sheet.pdf":   {},
			"Analysis I/skript.pdf":  {},
			"Lineare Algebra/l1.pdf": {},
		},
	}
	if err := manifest.Save(); err != nil {
		t.Fatalf("failed to write test manifest: %v", err)
	}

	post := func(useSubfolders bool) string {
		form := url.Values{}
		form.Set("download_path", filepath.ToSlash(downloadPath))
		form.Set("sync_all_courses", "on")
		form.Set("sync", "on")
		if useSubfolders {
			form.Set("use_section_subfolders", "on")
		}

		req := httptest.NewRequest(http.MethodPost, "/settings", nil)
		req.PostForm = form
		req.Form = form
		rec := httptest.NewRecorder()
		handleSettings(configPath)(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK from settings POST, got %d: %s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	body := post(true)
	if !strings.Contains(body, "existing sync history") {
		t.Errorf("expected a warning about existing sync history when enabling section subfolders, got body:\n%s", body)
	}
	if !strings.Contains(body, "3 files") {
		t.Errorf("expected the warning to state how many entries are affected, got body:\n%s", body)
	}

	// Saving again without changing the toggle must not re-warn - the
	// re-keying only happens on the change itself.
	body = post(true)
	if strings.Contains(body, "existing sync history") {
		t.Errorf("expected no warning when the toggle is unchanged, got body:\n%s", body)
	}
}
