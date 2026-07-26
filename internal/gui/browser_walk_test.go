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
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
	"github.com/mxschmitt/playwright-go"
)

func TestBrowserFirstRunWalk(t *testing.T) {
	if os.Getenv("OPAL_GUI_BROWSER_WALK") == "" {
		t.Skip("set OPAL_GUI_BROWSER_WALK=1 to run the browser walk (needs Playwright browsers)")
	}

	stubScheduler(t)

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

// Turning the daily automatic sync on and off, clicked in a real browser.
//
// Approved by the maintainer on 2026-07-26 ("jo, mach mal"), including that it
// touches scheduling. It nevertheless runs against stubbed scheduler seams,
// because of something they could not have known when approving:
// scheduler.TaskName is a single global constant, and their own live daily
// sync is registered under it. A real enable would overwrite that task with
// one pointing at the test binary, and a real disable would simply delete it -
// there is no guard on the disable path at all. Nothing worth learning here is
// worth deleting somebody's scheduled job for.
//
// What is verified for real against Task Scheduler, separately and safely, is
// that enabling from a disposable binary REFUSES before writing anything -
// see TestSchedulingRefusesToRegisterADisposableBinaryForReal below.
func TestBrowserSchedulingWalk(t *testing.T) {
	if os.Getenv("OPAL_GUI_BROWSER_WALK") == "" {
		t.Skip("set OPAL_GUI_BROWSER_WALK=1 to run the browser walk (needs Playwright browsers)")
	}

	calls := stubScheduler(t)

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

	// Automatic sync has its own page now; it used to be a second form at the
	// bottom of Settings.
	goSchedule := func() {
		t.Helper()
		if _, err := page.Goto(ts.URL+"/schedule", playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateLoad,
		}); err != nil {
			t.Fatalf("goto /schedule: %v", err)
		}
	}

	goSchedule()

	// Off by default, and it must stay that way: nothing should register a
	// scheduled job on somebody's machine without them asking.
	if checked, err := page.Locator("#schedule_enabled").IsChecked(); err != nil || checked {
		t.Fatalf("daily sync starts enabled (checked=%v err=%v); it is opt-in", checked, err)
	}

	// --- turn it on -----------------------------------------------------------
	if err := page.Check("#schedule_enabled"); err != nil {
		t.Fatalf("check schedule_enabled: %v", err)
	}
	if err := page.Fill("#schedule_time", "07:30"); err != nil {
		t.Fatalf("fill schedule_time: %v", err)
	}
	if err := page.Click(`#schedule-form button[type="submit"]`); err != nil {
		t.Fatalf("submit schedule form: %v", err)
	}
	if err := page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateLoad,
	}); err != nil {
		t.Fatalf("wait after enabling: %v", err)
	}

	if calls.enabled != 1 {
		t.Fatalf("expected exactly one enable, got %d", calls.enabled)
	}
	if calls.at != "07:30" {
		t.Fatalf("the time typed into the form did not reach the scheduler, got %q", calls.at)
	}

	// The toggle is re-queried from the scheduler on every render rather than
	// remembered, so this is the page agreeing with the OS, not with itself.
	if checked, err := page.Locator("#schedule_enabled").IsChecked(); err != nil || !checked {
		t.Fatalf("the toggle does not reflect the registration it just made (checked=%v err=%v)", checked, err)
	}
	if v, err := page.Locator("#schedule_time").InputValue(); err != nil || v != "07:30" {
		t.Fatalf("the registered time is not shown back, got %q err=%v", v, err)
	}

	// A reload must still show it on - the state lives in Task Scheduler, not
	// in the response to the POST.
	goSchedule()
	if checked, err := page.Locator("#schedule_enabled").IsChecked(); err != nil || !checked {
		t.Fatalf("the schedule looks off again after a reload (checked=%v err=%v)", checked, err)
	}

	// --- turn it off ----------------------------------------------------------
	if err := page.Uncheck("#schedule_enabled"); err != nil {
		t.Fatalf("uncheck schedule_enabled: %v", err)
	}
	if err := page.Click(`#schedule-form button[type="submit"]`); err != nil {
		t.Fatalf("submit schedule form to disable: %v", err)
	}
	if err := page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateLoad,
	}); err != nil {
		t.Fatalf("wait after disabling: %v", err)
	}

	if calls.disabled != 1 {
		t.Fatalf("expected exactly one disable, got %d", calls.disabled)
	}
	if checked, err := page.Locator("#schedule_enabled").IsChecked(); err != nil || checked {
		t.Fatalf("the toggle still looks enabled after disabling (checked=%v err=%v)", checked, err)
	}
	if calls.enabled != 1 {
		t.Fatalf("disabling should not have re-registered anything, enables=%d", calls.enabled)
	}
}

// Every page a stranger can reach from the nav, loaded in a real browser.
//
// A page that throws on load, or strands the user with no way back, is invisible
// to a handler test asserting on substrings - the HTML is there either way. This
// is the cheap check that each page actually runs and offers a way home.
//
// Same opt-in as TestBrowserFirstRunWalk.
func TestBrowserEveryPageLoads(t *testing.T) {
	if os.Getenv("OPAL_GUI_BROWSER_WALK") == "" {
		t.Skip("set OPAL_GUI_BROWSER_WALK=1 to run the browser walk (needs Playwright browsers)")
	}

	stubScheduler(t)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	// Configured, so pages render their normal state rather than their
	// nothing-set-up-yet state - the one a returning user actually sees.
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

	// "/" is excluded from the back-link requirement: it is home.
	pages := []struct {
		path        string
		wantHomeway bool
	}{
		{"/", false},
		{"/settings", true},
		{"/schedule", true},
		{"/sync", true},
		{"/tufast-setup", true},
		{"/update", true},
		{"/feedback", true},
	}

	for _, p := range pages {
		t.Run(p.path, func(t *testing.T) {
			page, err := browser.NewPage()
			if err != nil {
				t.Fatalf("new page: %v", err)
			}
			defer page.Close()

			var pageErrors []string
			page.On("pageerror", func(err error) { pageErrors = append(pageErrors, err.Error()) })

			// "load", not "networkidle": /sync opens its live-progress SSE
			// stream (/sync/stream) as soon as it renders and holds it open,
			// so the network never goes idle there. That is the page working,
			// not hanging - waiting for idle would time out on a healthy page.
			resp, err := page.Goto(ts.URL+p.path, playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateLoad,
			})
			if err != nil {
				t.Fatalf("goto %s: %v", p.path, err)
			}
			if resp.Status() != 200 {
				t.Fatalf("%s returned HTTP %d", p.path, resp.Status())
			}
			if len(pageErrors) > 0 {
				t.Fatalf("%s threw on load: %v", p.path, pageErrors)
			}

			// Every page carries the scheduled-status banner script, so a page
			// with no <h1> at all is a rendering failure rather than a design
			// choice.
			if n, _ := page.Locator("h1").Count(); n == 0 {
				t.Fatalf("%s rendered no heading", p.path)
			}

			if p.wantHomeway {
				// The GUI has no browser chrome in its real WebView2 window -
				// no address bar, no back button - so a page without a link
				// home is a dead end the user cannot leave.
				n, err := page.Locator(`a[href="/"]`).Count()
				if err != nil {
					t.Fatalf("count home links on %s: %v", p.path, err)
				}
				if n == 0 {
					t.Fatalf("%s offers no link back to the start page; in the native window there is no back button either", p.path)
				}
			}
		})
	}
}

// Leaving the settings page with unsaved edits, in a real browser.
//
// Reported by the maintainer (2026-07-26): "when going out of settings,
// nothing is saved without a warning... like, if you change something you may
// just forget to click: save settings". Saving is a separate deliberate click
// and the page has enough fields that "did I already save?" cannot be answered
// by looking at it.
//
// This has to be a browser test. The guard is entirely client-side - it
// compares a snapshot of every form against its current state - so a
// handler-level test sees nothing but an unchanged template.
func TestBrowserUnsavedChangesWalk(t *testing.T) {
	if os.Getenv("OPAL_GUI_BROWSER_WALK") == "" {
		t.Skip("set OPAL_GUI_BROWSER_WALK=1 to run the browser walk (needs Playwright browsers)")
	}

	stubScheduler(t)

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

	var pageErrors []string
	page.On("pageerror", func(err error) { pageErrors = append(pageErrors, err.Error()) })

	// Answer the leave-confirmation the way the test needs at the time. Default
	// is "stay", which is both the safe answer and the one that proves the
	// guard actually blocked the navigation.
	var dialogs []string
	leaveAnyway := false
	page.On("dialog", func(d playwright.Dialog) {
		dialogs = append(dialogs, d.Message())
		if leaveAnyway {
			_ = d.Accept()
		} else {
			_ = d.Dismiss()
		}
	})

	gotoSettings := func() {
		t.Helper()
		if _, err := page.Goto(ts.URL+"/settings", playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateNetworkidle,
		}); err != nil {
			t.Fatalf("goto settings: %v", err)
		}
	}
	barVisible := func() bool {
		t.Helper()
		visible, err := page.Locator("#unsaved-bar").IsVisible()
		if err != nil {
			t.Fatalf("read unsaved bar: %v", err)
		}
		return visible
	}

	gotoSettings()
	if barVisible() {
		t.Fatal("the unsaved-changes bar is showing on a freshly loaded page, before anything was edited")
	}

	// --- an edit raises the bar ---------------------------------------------
	original, err := page.Locator("#download_path").InputValue()
	if err != nil {
		t.Fatalf("read download path: %v", err)
	}
	if err := page.Fill("#download_path", filepath.Join(dir, "somewhere-else")); err != nil {
		t.Fatalf("edit download path: %v", err)
	}
	if !barVisible() {
		t.Fatal("edited a field and the unsaved-changes bar stayed hidden")
	}

	// --- undoing the edit lowers it again ------------------------------------
	// The bar tracks a snapshot, not "has anyone typed". Typing a character and
	// removing it must leave the page clean, or the warning becomes noise that
	// people learn to click through.
	if err := page.Fill("#download_path", original); err != nil {
		t.Fatalf("restore download path: %v", err)
	}
	if barVisible() {
		t.Fatal("restoring a field to its original value left the unsaved-changes bar up")
	}

	// --- leaving with unsaved edits asks first --------------------------------
	if err := page.Fill("#download_path", filepath.Join(dir, "somewhere-else")); err != nil {
		t.Fatalf("re-edit download path: %v", err)
	}
	if err := page.Click("p.back a"); err != nil {
		t.Fatalf("click the back link: %v", err)
	}
	if len(dialogs) == 0 {
		t.Fatal("navigating away with unsaved edits did not ask for confirmation")
	}
	if url := page.URL(); !strings.HasSuffix(url, "/settings") {
		t.Fatalf("declining the leave-confirmation still navigated away, now at %s", url)
	}

	// --- saving clears it -----------------------------------------------------
	if err := page.Click(`#settings-form button[type="submit"]`); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateNetworkidle,
	}); err != nil {
		t.Fatalf("wait after save: %v", err)
	}
	if barVisible() {
		t.Fatal("the unsaved-changes bar survived a save")
	}

	// Submitting must not itself trip the guard: the page unloads on save, and a
	// confirmation there would fire on every single save.
	if n := len(dialogs); n != 1 {
		t.Fatalf("expected exactly one confirmation (the declined one), got %d: %v", n, dialogs)
	}

	// --- and accepting really does leave --------------------------------------
	leaveAnyway = true
	if err := page.Fill("#download_path", filepath.Join(dir, "third-place")); err != nil {
		t.Fatalf("edit again: %v", err)
	}
	if err := page.Click("p.back a"); err != nil {
		t.Fatalf("click the back link again: %v", err)
	}
	if err := page.WaitForURL(ts.URL + "/"); err != nil {
		t.Fatalf("accepting the leave-confirmation did not navigate home: %v", err)
	}

	if len(pageErrors) > 0 {
		t.Fatalf("uncaught JavaScript errors during the walk: %v", pageErrors)
	}
}

// What the sync log shows a user, in a real browser.
//
// The maintainer's complaint (2026-07-26) was that the run's output is written
// for a developer: their account is ~345 files of which almost none change, so
// a routine sync produced ~345 "skipped: course / file" rows and the live
// status line sat on one arbitrary filename for minutes - which reads as a
// hang, and was in fact reported as one.
//
// Driven by publishing synthetic events into the real job, which the page
// receives over the real SSE stream and renders with the real JavaScript. A
// live sync would take minutes and could not produce an error on demand.
func TestBrowserSyncLogIsWrittenForAUser(t *testing.T) {
	if os.Getenv("OPAL_GUI_BROWSER_WALK") == "" {
		t.Skip("set OPAL_GUI_BROWSER_WALK=1 to run the browser walk (needs Playwright browsers)")
	}

	stubScheduler(t)

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
	var pageErrors []string
	page.On("pageerror", func(err error) { pageErrors = append(pageErrors, err.Error()) })

	// The /sync page holds its SSE stream open forever, so it never reaches
	// network-idle even when nothing is running. Waiting for the DOM is right.
	if _, err := page.Goto(ts.URL+"/sync", playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		t.Fatalf("goto sync: %v", err)
	}

	// The stream has to be attached before events are published, or they are
	// only seen via replay and the test proves nothing about live rendering.
	if _, err := page.WaitForFunction(`document.getElementById('status').textContent === 'Idle.'`, nil); err != nil {
		t.Fatalf("sync page never settled to idle: %v", err)
	}

	srv.syncJob.publish(jobEvent{Kind: "course_started", Course: "Analysis I", CourseIndex: 1, TotalCourses: 1})
	for _, name := range []string{"woche01.pdf", "woche02.pdf", "hybrid_quicksort.ipynb"} {
		srv.syncJob.publish(jobEvent{Kind: "file_skipped", Course: "Analysis I", File: name})
	}
	srv.syncJob.publish(jobEvent{Kind: "file_downloaded", Course: "Analysis I", File: "woche03.pdf"})

	// --- the status line counts, it does not name a file ---------------------
	if _, err := page.WaitForFunction(
		`document.getElementById('status').textContent.indexOf('4 files checked') !== -1`, nil,
	); err != nil {
		status, _ := page.Locator("#status").TextContent()
		t.Fatalf("status line never showed a running total (was %q): %v", status, err)
	}
	status, err := page.Locator("#status").TextContent()
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	if strings.Contains(status, "hybrid_quicksort.ipynb") {
		t.Fatalf("the status line is still parked on a filename, which is what read as a hang: %q", status)
	}
	if !strings.Contains(status, "1 downloaded") {
		t.Fatalf("status line does not say what the run achieved: %q", status)
	}

	// --- an up-to-date file is counted, not listed ---------------------------
	logText, err := page.Locator("#log").TextContent()
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	for _, name := range []string{"woche01.pdf", "woche02.pdf", "hybrid_quicksort.ipynb"} {
		if strings.Contains(logText, name) {
			t.Fatalf("an already-up-to-date file still gets its own log row (%q found in the log)", name)
		}
	}
	if !strings.Contains(logText, "woche03.pdf") {
		t.Fatalf("a downloaded file is missing from the log, which is the one thing the log is for: %q", logText)
	}

	// --- the summary is a sentence, not a field dump -------------------------
	srv.syncJob.publish(jobEvent{Kind: "done", Downloaded: 1, Skipped: 3, Errors: 0,
		Message: "Done. 1 downloaded, 3 already up to date, 0 failed."})

	if _, err := page.WaitForFunction(
		`document.getElementById('summary').textContent.length > 0`, nil,
	); err != nil {
		t.Fatalf("no summary after the run finished: %v", err)
	}
	summary, err := page.Locator("#summary").TextContent()
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	if strings.Contains(summary, "skipped=") || strings.Contains(summary, "downloaded=") {
		t.Fatalf("the summary is still a field dump: %q", summary)
	}
	if !strings.Contains(summary, "already up to date") {
		t.Fatalf("the summary does not explain what happened to the unchanged files: %q", summary)
	}

	if len(pageErrors) > 0 {
		t.Fatalf("uncaught JavaScript errors: %v", pageErrors)
	}
}

// Picking courses, in a real browser.
//
// The maintainer's complaint (2026-07-26) was that adding a course involved
// "mehrere Stellen" - several places. It did: a box of discovered checkboxes,
// a separate table of configured rows, a "+ Add course" button producing a
// third kind of thing, and the user expected to join them up mentally.
//
// Everything asserted here is client-side, so it cannot be reached from a
// handler test: the rows, the tickboxes and the drop-on-submit all live in
// JavaScript.
func TestBrowserCoursePickerIsOneList(t *testing.T) {
	if os.Getenv("OPAL_GUI_BROWSER_WALK") == "" {
		t.Skip("set OPAL_GUI_BROWSER_WALK=1 to run the browser walk (needs Playwright browsers)")
	}

	stubScheduler(t)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yaml := "download_path: " + filepath.ToSlash(filepath.Join(dir, "downloads")) + "\n" +
		"courses:\n  - Analysis I\n  - Algorithmen\n" +
		"course_folders:\n  Analysis I: Mathe/Analysis\n" +
		"sync: true\n" +
		"opal_url: https://bildungsportal.sachsen.de/opal/\n" +
		"session_state_file: " + filepath.ToSlash(filepath.Join(dir, "state.json")) + "\n"
	if err := os.WriteFile(configPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

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
	var pageErrors []string
	page.On("pageerror", func(err error) { pageErrors = append(pageErrors, err.Error()) })

	if _, err := page.Goto(ts.URL+"/settings", playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	}); err != nil {
		t.Fatalf("goto settings: %v", err)
	}

	// --- every configured course is on the list, ticked ----------------------
	ticks := page.Locator("#courses-table tbody .course-on")
	n, err := ticks.Count()
	if err != nil || n != 2 {
		t.Fatalf("expected both configured courses on one list, got %d (err=%v)", n, err)
	}
	for i := 0; i < n; i++ {
		if checked, err := ticks.Nth(i).IsChecked(); err != nil || !checked {
			t.Fatalf("a configured course is on the list but not ticked (row %d)", i)
		}
	}

	// --- unticking keeps the row, so the choice stays reversible -------------
	// The old picker deleted the row, which is why it had to refuse with an
	// alert() when the row carried a folder override.
	if err := ticks.Nth(0).Uncheck(); err != nil {
		t.Fatalf("untick a course: %v", err)
	}
	if n2, err := page.Locator("#courses-table tbody tr").Count(); err != nil || n2 != 2 {
		t.Fatalf("unticking removed the row instead of keeping it (rows=%d err=%v)", n2, err)
	}
	folder, err := page.Locator(`#courses-table input[name="course_row_folder[]"]`).Nth(0).InputValue()
	if err != nil {
		t.Fatalf("read folder: %v", err)
	}
	if folder != "Mathe/Analysis" {
		t.Fatalf("unticking discarded the folder override, got %q", folder)
	}

	// --- ticking it again restores it, with no dialog in the way -------------
	var dialogs []string
	page.On("dialog", func(d playwright.Dialog) {
		dialogs = append(dialogs, d.Message())
		_ = d.Dismiss()
	})
	if err := ticks.Nth(0).Check(); err != nil {
		t.Fatalf("re-tick a course: %v", err)
	}
	if len(dialogs) > 0 {
		t.Fatalf("ticking a course popped a dialog at the user: %v", dialogs)
	}

	// --- only ticked courses are saved ---------------------------------------
	if err := ticks.Nth(1).Uncheck(); err != nil {
		t.Fatalf("untick the second course: %v", err)
	}
	if err := page.Click(`#settings-form button[type="submit"]`); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateNetworkidle,
	}); err != nil {
		t.Fatalf("wait after save: %v", err)
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(loaded.App.Courses) != 1 || loaded.App.Courses[0] != "Analysis I" {
		t.Fatalf("unticking a course did not remove it from the saved list, got %#v", loaded.App.Courses)
	}
	if loaded.App.CourseFolders["Analysis I"] != "Mathe/Analysis" {
		t.Fatalf("the surviving course lost its folder override, got %#v", loaded.App.CourseFolders)
	}

	// --- adding one by hand still works --------------------------------------
	before, err := page.Locator("#courses-table tbody tr").Count()
	if err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if err := page.Click("#add-course-row"); err != nil {
		t.Fatalf("add by hand: %v", err)
	}
	after, err := page.Locator("#courses-table tbody tr").Count()
	if err != nil || after != before+1 {
		t.Fatalf("'Add one by hand' did not add a row: %d -> %d (err=%v)", before, after, err)
	}
	// A row added on purpose starts ticked; making someone add it and then
	// tick it would be two steps for one decision.
	if checked, err := page.Locator("#courses-table tbody .course-on").Nth(after - 1).IsChecked(); err != nil || !checked {
		t.Fatalf("a hand-added course did not start ticked (checked=%v err=%v)", checked, err)
	}

	if len(pageErrors) > 0 {
		t.Fatalf("uncaught JavaScript errors: %v", pageErrors)
	}
}

// A run that has stopped producing events should say so.
//
// A sync was reported stuck once (2026-07-26) and the only evidence was a
// status line that had not changed. Nothing noticed, and nothing could have:
// the page rendered the last event it received and had no opinion about how
// long ago that was.
//
// The threshold is three minutes in production, which no test should sit
// through, so the page's own constant is overridden in the browser first.
func TestBrowserSyncPageNoticesAStalledRun(t *testing.T) {
	if os.Getenv("OPAL_GUI_BROWSER_WALK") == "" {
		t.Skip("set OPAL_GUI_BROWSER_WALK=1 to run the browser walk (needs Playwright browsers)")
	}

	stubScheduler(t)

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
	var pageErrors []string
	page.On("pageerror", func(err error) { pageErrors = append(pageErrors, err.Error()) })

	// Set before the page script runs, or it would read the real three
	// minutes and this test would have to wait them out.
	if err := page.AddInitScript(playwright.Script{Content: playwright.String("window.OPAL_STALE_AFTER_MS = 300;")}); err != nil {
		t.Fatalf("shorten the stale threshold: %v", err)
	}
	if _, err := page.Goto(ts.URL+"/sync", playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		t.Fatalf("goto sync: %v", err)
	}
	if _, err := page.WaitForFunction(`document.getElementById('status').textContent === 'Idle.'`, nil); err != nil {
		t.Fatalf("sync page never settled to idle: %v", err)
	}

	// Nothing is running, so silence means nothing and must not be reported.
	time.Sleep(1200 * time.Millisecond)
	if visible, err := page.Locator("#stale").IsVisible(); err != nil || visible {
		t.Fatalf("an idle page reported itself as stalled (visible=%v err=%v)", visible, err)
	}

	// Now a run starts and immediately goes quiet.
	srv.syncJob.start(jobKindSync, func() {})
	defer srv.syncJob.finish()
	srv.syncJob.publish(jobEvent{Kind: "course_started", Course: "Analysis I", CourseIndex: 1, TotalCourses: 1})

	if _, err := page.WaitForFunction(`document.getElementById('stale').style.display === 'block'`,
		nil, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(15000)}); err != nil {
		text, _ := page.Locator("#stale").TextContent()
		t.Fatalf("a silent run was never reported as stalled (notice was %q): %v", text, err)
	}

	notice, err := page.Locator("#stale").TextContent()
	if err != nil {
		t.Fatalf("read the stall notice: %v", err)
	}
	// It has to stay honest: a long section legitimately goes quiet, so this
	// reports elapsed time and points at Cancel rather than declaring a fault.
	if !strings.Contains(notice, "Cancel") {
		t.Errorf("the stall notice does not say what the user can do about it: %q", notice)
	}

	// And activity clears it, or it would latch on and cry wolf for the rest
	// of the run.
	srv.syncJob.publish(jobEvent{Kind: "file_downloaded", Course: "Analysis I", File: "woche01.pdf"})
	if _, err := page.WaitForFunction(`document.getElementById('stale').style.display === 'none'`,
		nil, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(5000)}); err != nil {
		t.Fatalf("the stall notice did not clear when the run produced an event again: %v", err)
	}

	if len(pageErrors) > 0 {
		t.Fatalf("uncaught JavaScript errors: %v", pageErrors)
	}
}
