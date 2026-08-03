package gui

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"

	"github.com/alu-developer/opal-downloader/internal/scraper"
)

// The two bits of flavour on the sync page (maintainer's request, 2026-08-03:
// "so paar kleine sachen, die einfach bissl aufpeppen") are still UI, and
// decoration that quietly breaks is worse than no decoration - a stuck quip
// line or a summary that stops rendering because a helper threw would both
// look like the app itself is broken. So they get the same real-browser
// treatment as the rest of the page.
//
// This drives the job directly rather than running a sync: publish is the
// same seam TestBrowserSyncProgressWalk uses, and it means the test needs no
// OPAL account, no network, and no three minutes.
func TestBrowserSyncFlavourWalk(t *testing.T) {
	if os.Getenv("OPAL_GUI_BROWSER_WALK") == "" {
		t.Skip("set OPAL_GUI_BROWSER_WALK=1 to run the browser walk (needs Playwright browsers)")
	}

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

	// 40ms instead of the shipped 9s, so the rotation assertion below costs a
	// second rather than a minute and a half. Same escape hatch the page
	// already provides for the stall detector's threshold.
	if err := page.AddInitScript(playwright.Script{
		Content: playwright.String(`window.OPAL_QUIP_EVERY_MS = 40;`),
	}); err != nil {
		t.Fatalf("add init script: %v", err)
	}

	if _, err := page.Goto(ts.URL+"/sync", playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		t.Fatalf("goto sync: %v", err)
	}
	if _, err := page.WaitForFunction(`document.getElementById('status').textContent === 'Idle.'`, nil); err != nil {
		t.Fatalf("sync page never settled to idle: %v", err)
	}

	// Idle is the resting state, and it must be quiet. A quip sitting there
	// before anything has started would read as a status line about a run
	// that is not happening.
	if quip, _ := page.Locator("#quip").TextContent(); strings.TrimSpace(quip) != "" {
		t.Fatalf("the flavour line is showing while idle: %q", quip)
	}

	// --- a run starts: flavour appears, status stays the status --------------
	srv.syncJob.start(jobKindSync, func() {})
	srv.syncJob.publish(jobEvent{Kind: "course_started", Course: "Analysis I", CourseIndex: 1, TotalCourses: 1})

	if _, err := page.WaitForFunction(
		`document.getElementById('quip').textContent.trim().length > 0`, nil,
	); err != nil {
		t.Fatalf("no flavour line once a run was in flight: %v", err)
	}

	// The rule the quips are allowed to exist under: they never touch
	// #status. That line is the stall detector's only evidence, and a
	// rotating message there would make a hung run look busy.
	status, err := page.Locator("#status").TextContent()
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	if !strings.Contains(status, "Analysis I") {
		t.Fatalf("the flavour line displaced the real status: %q", status)
	}

	// Rotation must actually rotate, and it must not repeat a line while
	// others are unused - drawing independently at random would show the
	// same line twice in a row often enough to look stuck, which is the
	// specific failure this shuffle exists to prevent.
	//
	// Sampled over a window of 8 ticks at 40ms; the pool is 10, so every
	// sample in that window must be distinct.
	if _, err := page.AddScriptTag(playwright.PageAddScriptTagOptions{
		Content: playwright.String(`
			window.__opalQuipSeen = [];
			setInterval(function () {
				var text = document.getElementById('quip').textContent;
				var seen = window.__opalQuipSeen;
				if (text && seen[seen.length - 1] !== text) { seen.push(text); }
			}, 5);
		`),
	}); err != nil {
		t.Fatalf("add sampler: %v", err)
	}
	if _, err := page.WaitForFunction(`window.__opalQuipSeen.length >= 8`, nil); err != nil {
		samples, _ := page.Evaluate(`window.__opalQuipSeen`)
		t.Fatalf("the flavour line never rotated (samples: %v): %v", samples, err)
	}
	samples, err := page.Evaluate(`window.__opalQuipSeen.slice(0, 8)`)
	if err != nil {
		t.Fatalf("read samples: %v", err)
	}
	seen := map[string]bool{}
	for _, s := range samples.([]any) {
		text, _ := s.(string)
		if seen[text] {
			t.Errorf("a flavour line repeated while others were unused: %q in %v", text, samples)
			break
		}
		seen[text] = true
	}

	// --- the run ends: flavour stops, and the summary carries the reward -----
	srv.syncJob.publish(jobEvent{Kind: "done", Downloaded: 12, Skipped: 333, Errors: 0,
		Message: "Done. 12 downloaded, 333 already up to date, 0 failed."})
	srv.syncJob.finish()

	if _, err := page.WaitForFunction(
		`document.getElementById('summary').textContent.length > 0`, nil,
	); err != nil {
		t.Fatalf("no summary after the run finished: %v", err)
	}

	if _, err := page.WaitForFunction(
		`document.getElementById('quip').textContent.trim().length === 0`, nil,
	); err != nil {
		quip, _ := page.Locator("#quip").TextContent()
		t.Fatalf("the flavour line kept going after the run ended (%q): %v", quip, err)
	}

	summary, err := page.Locator("#summary").TextContent()
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	// 345 files checked * 4 clicks = 1380, rounded to the nearest hundred.
	if !strings.Contains(summary, "1,400 clicks you did not make") {
		t.Errorf("summary is missing the clicks-spared tail, or miscounted it: %q", summary)
	}
	// It is a tail, not a replacement: the real numbers must still be there.
	if !strings.Contains(summary, "12 new files downloaded") || !strings.Contains(summary, "333 already up to date") {
		t.Errorf("the flavour tail displaced the actual result: %q", summary)
	}

	if len(pageErrors) > 0 {
		t.Fatalf("the sync page threw JavaScript errors: %v", pageErrors)
	}
}
