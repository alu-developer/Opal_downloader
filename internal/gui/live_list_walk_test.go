package gui

// "List courses", clicked in a real browser, against the real OPAL account.
//
// WHY A LIVE ONE IS WORTH HAVING
// ------------------------------
// The other browser walks use a temp config and never talk to OPAL, so they
// stop at the point where the GUI hands work to the scraper. Everything past
// that button - reusing the saved session, the background job, the SSE stream
// that reports progress, the final result rendering - is only ever exercised
// by a real run. `list_only=1` is the cheap end of that: dashboard discovery,
// no crawl of section pages and no downloads.
//
// SAFETY, because this one touches real things
// --------------------------------------------
//   - The real config.yaml is READ, never written. The test builds its own
//     config in t.TempDir(). A probe of this same journey wrote to the live
//     config on 2026-07-23 and destroyed settings; that is not repeatable here
//     because nothing here has the real path to write to.
//   - The session state file is COPIED into the temp dir and the copy is used,
//     so a session refresh during the run cannot touch the real one.
//   - Nothing is downloaded and no OPAL state is modified: discovery is a read.
//
// Opt in explicitly - it needs a valid saved session and hits the network:
//
//	OPAL_GUI_LIVE_LIST=1 go test ./internal/gui/ -run TestLiveListCoursesInBrowser -v -timeout 10m

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"net/http/httptest"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
	"github.com/mxschmitt/playwright-go"
)

func TestLiveListCoursesInBrowser(t *testing.T) {
	if os.Getenv("OPAL_GUI_LIVE_LIST") == "" {
		t.Skip("set OPAL_GUI_LIVE_LIST=1 to run the live list walk (needs a real saved OPAL session)")
	}

	realConfig := os.Getenv("OPAL_GUI_LIVE_CONFIG")
	if realConfig == "" {
		realConfig = "../../config.yaml"
	}
	real, err := config.Load(realConfig)
	if err != nil {
		t.Skipf("no usable real config at %s: %v", realConfig, err)
	}
	if _, err := os.Stat(real.Credentials.StateFile); err != nil {
		t.Skipf("no saved session at %s: %v", real.Credentials.StateFile, err)
	}

	dir := t.TempDir()

	// Work on a copy so a session refresh mid-run cannot write to the real
	// state file.
	stateCopy := filepath.Join(dir, "state.json")
	stateBytes, err := os.ReadFile(real.Credentials.StateFile)
	if err != nil {
		t.Fatalf("read saved session: %v", err)
	}
	if err := os.WriteFile(stateCopy, stateBytes, 0o600); err != nil {
		t.Fatalf("copy saved session: %v", err)
	}

	configPath := filepath.Join(dir, "config.yaml")
	yaml := "download_path: " + filepath.ToSlash(filepath.Join(dir, "downloads")) + "\n" +
		"courses:\n  - \"*\"\nsync: true\n" +
		"opal_url: " + real.Credentials.URL + "\n" +
		"session_state_file: " + filepath.ToSlash(stateCopy) + "\n"
	if err := os.WriteFile(configPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
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

	if _, err := page.Goto(ts.URL+"/sync", playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateLoad,
	}); err != nil {
		t.Fatalf("goto /sync: %v", err)
	}

	// A saved session means the page must consider itself ready; a disabled
	// button here is the readiness logic disagreeing with reality.
	disabled, err := page.Locator("#btn-list").IsDisabled()
	if err != nil {
		t.Fatalf("check List button: %v", err)
	}
	if disabled {
		body, _ := page.Content()
		t.Fatalf("List is disabled despite a saved session, page was:\n%s", body)
	}

	if err := page.Click("#btn-list"); err != nil {
		t.Fatalf("click List: %v", err)
	}

	// Completion is taken from the UI's own source of truth - the SSE `state`
	// event drives setRunning(), which re-enables Sync/List and disables
	// Cancel. Grepping the progress log for a word like "done" is guesswork
	// about wording; this is the same signal the user reads off the buttons.
	started := false
	for deadline := time.Now().Add(2 * time.Minute); time.Now().Before(deadline); {
		if running, err := page.Locator("#btn-cancel").IsEnabled(); err == nil && running {
			started = true
			break
		}
		time.Sleep(time.Second)
	}
	if !started {
		body, _ := page.Locator("#log").InnerText()
		t.Fatalf("the job never reported itself running; log was:\n%s", body)
	}

	// Generous: "List courses" crawls every course's sections, so it costs the
	// same as a sync's discovery phase - measured at ~8 minutes on the
	// maintainer's account, not the quick lookup the button name suggests.
	finished := false
	for deadline := time.Now().Add(20 * time.Minute); time.Now().Before(deadline); {
		if idle, err := page.Locator("#btn-cancel").IsDisabled(); err == nil && idle {
			finished = true
			break
		}
		time.Sleep(2 * time.Second)
	}

	logText, err := page.Locator("#log").InnerText()
	if err != nil {
		t.Fatalf("read progress log: %v", err)
	}
	t.Logf("live List progress log:\n%s", logText)

	if !finished {
		t.Fatalf("live List did not finish within the deadline; log was:\n%s", logText)
	}
	lower := strings.ToLower(logText)
	if strings.Contains(lower, "failed") || strings.Contains(lower, "error") ||
		strings.Contains(lower, "cancelled") {
		t.Fatalf("live List reported a failure:\n%s", logText)
	}

	// The point of the button: it has to actually name courses. Finishing with
	// an empty list would mean the plumbing works and the feature does not.
	if !strings.Contains(logText, "Found ") {
		t.Fatalf("live List finished but reported no course count:\n%s", logText)
	}
	if !strings.Contains(logText, "[") {
		t.Fatalf("live List finished but named no courses:\n%s", logText)
	}

	// The real session file must be untouched.
	after, err := os.ReadFile(real.Credentials.StateFile)
	if err != nil {
		t.Fatalf("re-read real session file: %v", err)
	}
	if len(after) != len(stateBytes) {
		t.Fatalf("the real session file changed during the run (%d -> %d bytes); "+
			"it must never be written by a test", len(stateBytes), len(after))
	}
	fmt.Fprintln(os.Stderr, "live list walk: real session file untouched")
}
