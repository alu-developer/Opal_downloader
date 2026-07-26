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

	goSettings := func() {
		t.Helper()
		if _, err := page.Goto(ts.URL+"/settings", playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateLoad,
		}); err != nil {
			t.Fatalf("goto /settings: %v", err)
		}
	}

	goSettings()

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
	goSettings()
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
