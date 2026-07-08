package gui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
)

// TestHandleSettingsPostPreservesFieldsWithoutFormInputs is a regression test
// for a data-loss bug: saving the GUI Settings page used to build a fresh
// config.App{} from only the fields the form submits, silently deleting
// use_section_subfolders, section_folder_names, and subfolder_destinations
// (and any other field the Settings form has no input for) from config.yaml
// if they had been set by hand. This round-trips a config.yaml with those
// fields set through the real POST /settings handler and asserts they come
// back unchanged.
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
use_section_subfolders: true
section_folder_names:
  Vorlesung: Lectures
  Uebung: Exercises
subfolder_destinations:
  Analysis/Vorlesung: D:/Uni/Analysis/Lectures
  Analysis/Uebung: D:/Uni/Analysis/Exercises
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
	if !before.App.UseSectionSubfolders {
		t.Fatalf("precondition failed: initial config should have use_section_subfolders: true")
	}
	if len(before.App.SectionFolderNames) == 0 || len(before.App.SubfolderDestinations) == 0 {
		t.Fatalf("precondition failed: initial config should have non-empty section_folder_names/subfolder_destinations, got %+v / %+v",
			before.App.SectionFolderNames, before.App.SubfolderDestinations)
	}

	// Simulate the user submitting the Settings form as-is (values mirror
	// what loadedToViewData would have pre-filled from `before`), which is
	// the normal "just click Save" path that must not be destructive.
	form := url.Values{}
	form.Set("opal_url", before.Credentials.URL)
	form.Set("session_state_file", before.Credentials.StateFile)
	form.Set("browser_executable", before.Credentials.BrowserExecutable)
	form.Set("browser_user_data_dir", before.Credentials.BrowserUserDataDir)
	form.Set("browser_profile_directory", before.Credentials.BrowserProfileDir)
	form.Set("download_path", before.App.DownloadPath)
	form.Set("courses", "*")
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

	if after.App.UseSectionSubfolders != before.App.UseSectionSubfolders {
		t.Errorf("use_section_subfolders was not preserved: before=%v after=%v",
			before.App.UseSectionSubfolders, after.App.UseSectionSubfolders)
	}
	if !reflect.DeepEqual(after.App.SectionFolderNames, before.App.SectionFolderNames) {
		t.Errorf("section_folder_names was not preserved:\nbefore=%+v\nafter=%+v",
			before.App.SectionFolderNames, after.App.SectionFolderNames)
	}
	if !reflect.DeepEqual(after.App.SubfolderDestinations, before.App.SubfolderDestinations) {
		t.Errorf("subfolder_destinations was not preserved:\nbefore=%+v\nafter=%+v",
			before.App.SubfolderDestinations, after.App.SubfolderDestinations)
	}

	// Sanity: fields the form does submit were actually applied.
	if after.App.DownloadPath != before.App.DownloadPath {
		t.Errorf("download_path should still be settable via the form: before=%q after=%q",
			before.App.DownloadPath, after.App.DownloadPath)
	}
}
