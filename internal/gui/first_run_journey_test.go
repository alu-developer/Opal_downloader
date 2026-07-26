package gui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
)

// The first run, walked as a sequence rather than as isolated handlers.
//
// Every other test in this package builds a config, hits one handler, and
// asserts on the response. That leaves the thing a stranger actually
// experiences untested: each step's real on-disk result being the next step's
// input. The GUI's native WebView2 window cannot be driven from a test, so
// this is the closest honest equivalent - the real handlers, the real
// template, a real config.yaml on disk, in order.
//
// Everything here runs against a t.TempDir() config path. That is not a
// stylistic choice: on 2026-07-23 a throwaway probe of this same journey was
// pointed at the live config.yaml and wiped its course_folders,
// subfolder_destinations and use_section_subfolders.

func TestFirstRunJourney(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	downloadPath := filepath.Join(dir, "downloads")

	srv := &server{configPath: configPath}
	settings := handleSettings(configPath)

	get := func(t *testing.T, h http.HandlerFunc, path string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d, body:\n%s", path, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	post := func(t *testing.T, form url.Values) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		settings(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /settings: status %d, body:\n%s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	// --- step 1: nothing exists yet ------------------------------------------
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("precondition: config should not exist yet, stat err = %v", err)
	}

	body := get(t, srv.handleLanding, "/")
	if !strings.Contains(body, "Set up opal-downloader") {
		t.Fatalf("first run should lead with setup, body was:\n%s", body)
	}

	// The settings page has to render with nothing to load. A stranger reaches
	// it before any config exists, so a nil-config crash here is the whole
	// first run.
	body = get(t, settings, "/settings")
	if !strings.Contains(body, `name="download_path"`) {
		t.Fatalf("settings form should render before any config exists, body was:\n%s", body)
	}

	// --- step 2: course selection, as the picker submits it ------------------
	// The picker ticks discovered courses and writes them into course_row_name[]
	// rows (settings.go's applyDiscoveredCourses); this is that submission.
	form := url.Values{}
	form.Set("download_path", downloadPath)
	form.Set("sync", "on")
	form.Add("course_row_name[]", "Analysis I")
	form.Add("course_row_folder[]", "Analysis")
	form.Add("course_row_name[]", "Softwaretechnologie")
	form.Add("course_row_folder[]", "")
	body = post(t, form)
	if strings.Contains(body, "class=\"error\"") {
		t.Fatalf("setup save reported an error, body was:\n%s", body)
	}

	// The save has to be real, not just rendered as saved.
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load after setup: %v", err)
	}
	if got := loaded.App.Courses; len(got) != 2 || got[0] != "Analysis I" || got[1] != "Softwaretechnologie" {
		t.Fatalf("selected courses not persisted, got %#v", got)
	}
	if loaded.App.CourseFolders["Analysis I"] != "Analysis" {
		t.Fatalf("per-course folder not persisted, got %#v", loaded.App.CourseFolders)
	}
	// A course with no folder must not invent an empty mapping - that would
	// read as "download this course to the root", not "unset".
	if _, ok := loaded.App.CourseFolders["Softwaretechnologie"]; ok {
		t.Fatalf("a blank folder should not be stored, got %#v", loaded.App.CourseFolders)
	}

	// --- step 3: the landing page has moved on -------------------------------
	body = get(t, srv.handleLanding, "/")
	if strings.Contains(body, "Set up opal-downloader") {
		t.Fatalf("landing page still asks for setup after a successful save, body was:\n%s", body)
	}

	// Same for /sync, which is one link away and gates on the same readiness.
	sp := newSyncPage(configPath, srv)
	syncBody := get(t, sp.handlePage, "/sync")
	if strings.Contains(syncBody, `<a href="/settings">Set up opal-downloader</a>`) {
		t.Fatalf("/sync still reports missing config after setup, body was:\n%s", syncBody)
	}

	// --- step 4: changing a setting afterwards -------------------------------
	// The returning-user path, and the one that has actually destroyed data.
	// A browser resubmits the entire rendered form, so the previously-saved
	// rows must come back with it.
	form.Set("download_path", filepath.Join(dir, "downloads-moved"))
	form.Add("subfolder_dest_key[]", "*Analysis*/*Vorlesung*")
	form.Add("subfolder_dest_path[]", filepath.Join(dir, "lectures"))
	form.Add("section_folder_pattern[]", "Exercises")
	form.Add("section_folder_folder[]", "Uebungen")
	post(t, form)

	loaded, err = config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load after edit: %v", err)
	}
	if len(loaded.App.Courses) != 2 {
		t.Fatalf("editing one field dropped the course selection, got %#v", loaded.App.Courses)
	}
	if loaded.App.SubfolderDestinations["*Analysis*/*Vorlesung*"] == "" {
		t.Fatalf("subfolder destination not persisted, got %#v", loaded.App.SubfolderDestinations)
	}
	if loaded.App.SectionFolderNames["Exercises"] != "Uebungen" {
		t.Fatalf("section folder name not persisted, got %#v", loaded.App.SectionFolderNames)
	}

	// --- step 5: the invariant the whole thing rests on ----------------------
	// parseSettingsForm rebuilds course_folders, subfolder_destinations and
	// section_folder_names from the submitted rows ALONE - whatever is not
	// resubmitted is gone. Nothing in the handler preserves them.
	//
	// So the only thing standing between a returning user and silent data loss
	// is that GET /settings renders those rows as real, server-rendered form
	// controls, which a browser then resubmits natively. That is exactly what
	// the 2026-07-23 probe got wrong about its own request and briefly
	// destroyed a live config over. If a template change ever dropped these
	// inputs, every save would quietly wipe the user's mappings and no other
	// test in this package would notice.
	body = get(t, settings, "/settings")
	for _, want := range []string{
		`name="course_row_name[]" value="Analysis I"`,
		`name="course_row_folder[]" value="Analysis"`,
		`name="subfolder_dest_key[]" value="*Analysis*/*Vorlesung*"`,
		`name="section_folder_pattern[]" value="Exercises"`,
		`name="section_folder_folder[]" value="Uebungen"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("settings page does not render %s as a resubmittable form control;\n"+
				"a browser save would drop it and silently wipe the setting. Body was:\n%s", want, body)
		}
	}
}
