package gui

// The first-run walk, in an actual browser.
//
// WHY THIS EXISTS SEPARATELY FROM first_run_journey_test.go
// ---------------------------------------------------------
// That test drives the handlers over httptest and covers what the server
// renders and stores. It cannot touch the settings page's JavaScript, and a
// large part of the course-selection UI *is* JavaScript: "+ Add course" and
// the "Find my courses" picker build their `course_row_name[]` inputs in the
// browser (settings.go's addCourseRow / applyDiscoveredCourses). Rows that
// only ever exist client-side are invisible to a handler-level test, and they
// are the rows a real user's selection actually travels in.
//
// The GUI normally opens a native WebView2 window, which cannot be driven from
// a test and must not be popped onto the maintainer's desktop unannounced. But
// the window is only a viewer for a plain local HTTP server - so this serves
// the real mux and points Playwright's already-bundled headless Chromium at
// it. Same pages, same JS, no window.
//
// NOT part of `dev.ps1 all`: it needs the Playwright browsers installed, which
// a plain `go test` on a fresh clone will not have. Opt in the same way
// internal/scraper's probe does:
//
//	OPAL_GUI_BROWSER_WALK=1 go test ./internal/gui/ -run TestBrowserFirstRunWalk -v
//
// Everything runs against a t.TempDir() config. See the header of
// first_run_journey_test.go for why that is not negotiable.

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
	"github.com/mxschmitt/playwright-go"
)

func TestBrowserFirstRunWalk(t *testing.T) {
	if os.Getenv("OPAL_GUI_BROWSER_WALK") == "" {
		t.Skip("set OPAL_GUI_BROWSER_WALK=1 to run the browser walk (needs Playwright browsers)")
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	downloadPath := filepath.Join(dir, "downloads")

	srv := &server{configPath: configPath, buildVersion: "test", feedback: &feedbackState{}}
	ts := httptest.NewServer(newMux(srv, configPath))
	defer ts.Close()

	scraper.EnsurePlaywrightBrowsersPath()
	pw, err := playwright.Run()
	if err != nil {
		t.Fatalf("playwright: %v", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)})
	if err != nil {
		t.Fatalf("launch chromium: %v", err)
	}
	defer browser.Close()

	page, err := browser.NewPage()
	if err != nil {
		t.Fatalf("new page: %v", err)
	}

	// A page that throws on load looks fine to a handler test and broken to a
	// user, so treat any uncaught error as a failure of the walk.
	var pageErrors []string
	page.On("pageerror", func(err error) { pageErrors = append(pageErrors, err.Error()) })

	mustClick := func(selector string) {
		t.Helper()
		if err := page.Click(selector); err != nil {
			t.Fatalf("click %s: %v", selector, err)
		}
	}
	mustFill := func(selector, value string) {
		t.Helper()
		if err := page.Fill(selector, value); err != nil {
			t.Fatalf("fill %s: %v", selector, err)
		}
	}

	// --- step 1: land on a first run -----------------------------------------
	if _, err := page.Goto(ts.URL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	}); err != nil {
		t.Fatalf("goto landing: %v", err)
	}

	setupCTA := page.Locator("text=Set up opal-downloader").First()
	if n, err := setupCTA.Count(); err != nil || n == 0 {
		body, _ := page.Content()
		t.Fatalf("no setup CTA on a first run (count=%d err=%v), page was:\n%s", n, err, body)
	}

	// --- step 2: click through to settings, as a user would ------------------
	if err := setupCTA.Click(); err != nil {
		t.Fatalf("click setup CTA: %v", err)
	}
	if err := page.WaitForURL("**/settings"); err != nil {
		t.Fatalf("setup CTA did not lead to /settings: %v", err)
	}

	mustFill("#download_path", downloadPath)

	// A first run has no config, so config.Load's default course list is the
	// wildcard and "Sync all courses" renders checked - which hides the whole
	// course picker behind JS. Picking specific courses therefore starts by
	// unticking it. That is the intended default (sync everything is the
	// low-friction path), but it does mean the picker is invisible until a
	// stranger thinks to uncheck a box, which is worth knowing.
	if picker, err := page.Locator("#courses-field").IsVisible(); err == nil && picker {
		t.Fatal("expected the course picker to start hidden behind 'Sync all courses' on a first run")
	}
	if err := page.Uncheck("#sync_all_courses"); err != nil {
		t.Fatalf("uncheck sync_all_courses: %v", err)
	}
	if visible, err := page.Locator("#courses-field").IsVisible(); err != nil || !visible {
		t.Fatalf("unticking 'Sync all courses' did not reveal the picker (visible=%v err=%v)", visible, err)
	}

	// --- step 3: add a course the JavaScript way -----------------------------
	// This is the part a handler-level test cannot reach. The row does not
	// exist in the server-rendered HTML at all; "+ Add course" creates it.
	before, err := page.Locator(`#courses-table input[name="course_row_name[]"]`).Count()
	if err != nil {
		t.Fatalf("count course rows: %v", err)
	}
	mustClick("#add-course-row")
	after, err := page.Locator(`#courses-table input[name="course_row_name[]"]`).Count()
	if err != nil {
		t.Fatalf("count course rows after add: %v", err)
	}
	if after != before+1 {
		t.Fatalf("'+ Add course' did not add a row: %d -> %d", before, after)
	}

	rowName := page.Locator(`#courses-table input[name="course_row_name[]"]`).Nth(after - 1)
	if err := rowName.Fill("Analysis I"); err != nil {
		t.Fatalf("fill new course row: %v", err)
	}
	rowFolder := page.Locator(`#courses-table input[name="course_row_folder[]"]`).Nth(after - 1)
	if err := rowFolder.Fill("Analysis"); err != nil {
		t.Fatalf("fill new folder cell: %v", err)
	}

	// --- step 4: save, and check the disk, not the page ----------------------
	mustClick(`#settings-form button[type="submit"]`)
	if err := page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateNetworkidle,
	}); err != nil {
		t.Fatalf("wait after save: %v", err)
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load after a browser save: %v", err)
	}
	if len(loaded.App.Courses) != 1 || loaded.App.Courses[0] != "Analysis I" {
		t.Fatalf("the JS-created course row did not survive a real form submit, got %#v", loaded.App.Courses)
	}
	if loaded.App.CourseFolders["Analysis I"] != "Analysis" {
		t.Fatalf("per-course folder from a JS-created row not saved, got %#v", loaded.App.CourseFolders)
	}

	// --- step 5: come back and change something ------------------------------
	// The returning-user path. The saved row must come back as a real control
	// so this submit carries it again; see first_run_journey_test.go step 5 for
	// why that invariant is the only thing preventing silent data loss.
	if _, err := page.Goto(ts.URL+"/settings", playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	}); err != nil {
		t.Fatalf("goto settings again: %v", err)
	}
	value, err := page.Locator(`#courses-table input[name="course_row_name[]"]`).First().InputValue()
	if err != nil {
		t.Fatalf("read course row on return: %v", err)
	}
	if value != "Analysis I" {
		t.Fatalf("saved course not shown on return, got %q", value)
	}

	movedPath := filepath.Join(dir, "downloads-moved")
	mustFill("#download_path", movedPath)
	mustClick(`#settings-form button[type="submit"]`)
	if err := page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateNetworkidle,
	}); err != nil {
		t.Fatalf("wait after second save: %v", err)
	}

	loaded, err = config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load after edit: %v", err)
	}
	if len(loaded.App.Courses) != 1 {
		t.Fatalf("editing the download path dropped the course selection, got %#v", loaded.App.Courses)
	}

	// --- step 6: the landing page has moved on -------------------------------
	if _, err := page.Goto(ts.URL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	}); err != nil {
		t.Fatalf("goto landing again: %v", err)
	}
	if n, _ := page.Locator("text=Set up opal-downloader").Count(); n != 0 {
		t.Fatal("landing page still asks for setup after a completed setup")
	}

	if len(pageErrors) > 0 {
		t.Fatalf("uncaught JavaScript errors during the walk: %v", pageErrors)
	}
}
